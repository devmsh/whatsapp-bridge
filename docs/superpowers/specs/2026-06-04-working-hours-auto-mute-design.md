# Working Hours Auto-Mute — Design

**Date:** 2026-06-04
**Branch:** `turbo/working-hours-auto-mute-2026-06-04`
**Status:** Approved (user brainstorm) + advisor review

## Goal

Automatically mute a chosen set of WhatsApp chats during the user's working
hours (focus mode), and unmute them outside working hours. A background
scheduler inside the Go bridge switches the mute state on time, and self-heals
after restarts, offline periods, or config edits.

## Core principle: desired-state reconciliation (not edge events)

The heart of the feature is one **pure function**:

```
DesiredMute(now time.Time, cfg WorkingHoursConfig) bool
```

Given the current time and the config, it returns whether the selected chats
*should* be muted right now. A background goroutine ticks every **1 minute** and
**reconciles**: it compares each selected chat's recorded state to the desired
state and fixes any mismatch. The same reconcile runs on startup and is retried
on every tick when the client reconnects. So a restart, an offline boundary, or
a config edit all self-heal — no missed transitions.

Mute logic (mute-DURING-hours / focus mode):

- Working day **and** inside the working window → `true` (muted).
- Outside the window, or on a weekend day → `false` (unmuted).
- Weekend = Friday + Saturday (working days = Sunday–Thursday) by default,
  stored as an explicit list so it can change later.
- Window is interpreted in **server local time**.
- The window is a same-day daytime window: `Start < End` (e.g. 09:00–18:00).
  Overnight windows are **out of scope** (YAGNI — the confirmed use is daytime;
  an overnight window crossing into a weekend day creates an untested ambiguity).
  If `Start >= End` the config is treated as invalid → `DesiredMute` returns
  `false` (never mutes), and the handler rejects the save with a 400.

## Ownership: never wipe a manual mute

Scope is "selected chats only." At the end of the working window the feature
*unmutes* the selected chats. If the user muted one of them by hand, a blind
unmute would erase their intent.

Rule: the feature records the JIDs **it** muted (`feature_muted` set in the
config). Reconcile only touches chats in `chat_jids`, and:

- desired = muted, chat not yet in `feature_muted` → mute it; add to `feature_muted`.
- desired = unmuted, chat **is** in `feature_muted` → unmute it; remove from `feature_muted`.
- otherwise → leave the chat alone.

A chat the user muted manually is never in `feature_muted`, so it is never
auto-unmuted.

### Releasing mutes on config change (no orphaned silent chats)

Two transitions must actively *release* feature mutes, or a chat can be left
permanently silent with the user unaware:

1. **Chat removed from the selection.** On PUT, before saving, compute
   `removed = (old.ChatJIDs ∩ old.FeatureMuted) − new.ChatJIDs` and **unmute
   each removed chat** (and drop it from `feature_muted`). Otherwise a
   feature-muted chat that leaves `chat_jids` stays muted on WhatsApp and is
   never iterated again.
2. **Feature disabled.** When the config is saved with `Enabled=false` (or
   disabled at any time), **unmute everything currently in `feature_muted`** and
   clear the set. This matches the ownership principle: the feature undoes only
   what it did. The 1-minute ticker still early-returns while disabled; the
   release happens once, at the moment of disabling, via the PUT path /
   `ReconcileNow`.

Both releases require a live connection; if offline at the moment of the change,
they are applied on the next reconcile (the JIDs stay in `feature_muted` until
actually unmuted).

### Concurrency

The 1-minute ticker goroutine and the PUT handler's `ReconcileNow` both run
load→mutate `feature_muted`→save on the same `sync_state` blob and both call
`SendAppState`. To avoid a lost update / double send, **all reconcile work is
serialized behind a package-level `sync.Mutex`** in `internal/wa/working_hours.go`.
`Reconcile`, the disable/remove release path, and `ReconcileNow` all acquire it.

## Data & config

Stored as one JSON blob in the existing `sync_state` table under key
`working_hours_config` (via `store.GetSyncState` / `store.PutSyncState`).

```go
type WorkingHoursConfig struct {
    Enabled      bool     `json:"enabled"`
    Start        string   `json:"start"`         // "HH:MM", 24h, server local time
    End          string   `json:"end"`           // "HH:MM"
    WorkingDays  []int    `json:"working_days"`   // time.Weekday ints: Sun=0 .. Sat=6
    ChatJIDs     []string `json:"chat_jids"`      // user-selected chats
    FeatureMuted []string `json:"feature_muted"`  // managed by the scheduler ONLY
}
```

Defaults when no config exists: `Enabled=false`, `Start="09:00"`, `End="18:00"`,
`WorkingDays=[0,1,2,3,4]` (Sun–Thu), empty `ChatJIDs`, empty `FeatureMuted`.

## Mute mechanics

Reuse the existing pattern from `internal/api/handler_chats.go`:

```go
wa.SendAppState(ctx, appstate.BuildMute(chatJID, true, -1))  // mute "forever"
store.SetChatMuted(jid, true, endUnix)                       // real Unix ts
```

- Mute uses duration `-1` (forever) so WhatsApp's own mute-expiry timer never
  competes with the scheduler — **the scheduler is the single source of truth**.
- Unmute: `appstate.BuildMute(chatJID, false, 0)` + `store.SetChatMuted(jid, false, 0)`.
- `muted_until` stores a **real Unix timestamp** = the end of the current
  working window (so the UI can show "muted until 18:00"). This deliberately
  avoids the latent bug in the current `handleChatAction` mute case, which
  stores the raw hour count into `muted_until`.

`SendAppState` needs a live connection. Reconcile **skips** (does not error) when
`client.IsConnected()` is false, mirroring `StartPeriodicSync`, and retries on
the next tick — so a boundary crossed while offline is applied on reconnect.

## Components

### `internal/wa/working_hours.go` (new)
- `WorkingHoursConfig` struct + JSON marshal helpers.
- `LoadWorkingHoursConfig(store) WorkingHoursConfig` — reads the blob, applies
  defaults when absent/invalid.
- `SaveWorkingHoursConfig(store, cfg) error` — writes the blob.
- `DesiredMute(now, cfg) bool` — **pure**, no clock/IO. The unit-tested core.
- `windowEndUnix(now, cfg) int64` — end-of-current-window timestamp for `muted_until`.
- `Reconcile(client, store, now)` — acquire the package mutex; load cfg; return
  early if `!Enabled` or `!client.IsConnected()`; compute desired; apply the
  ownership rule per chat; persist `feature_muted` if it changed.
- `ReconcileNow(client, store)` — convenience wrapper used by the handler after a
  config save (applies the new config immediately; same mutex).
- `ReleaseMutes(client, store, jids)` — unmute the given JIDs and remove them from
  `feature_muted`; used by the handler for the remove-from-selection and disable
  paths. Acquires the same mutex.
- `StartWorkingHoursScheduler(client, store)` — goroutine, 1-minute ticker, calls
  `Reconcile`; also runs once at startup.

### `internal/api/handler_working_hours.go` (new)
- `GET /api/v2/working-hours` → returns the config JSON (`feature_muted` included,
  read-only for the client).
- `PUT /api/v2/working-hours` → accepts `enabled`, `start`, `end`, `working_days`,
  `chat_jids`. Validation: reject (400) if `start`/`end` are not `HH:MM` or if
  `start >= end`. Order of operations:
  1. Load old config.
  2. Compute `removed = (old.ChatJIDs ∩ old.FeatureMuted) − new.ChatJIDs`; if any,
     `wa.ReleaseMutes(client, store, removed)`.
  3. If the new config is `Enabled=false`, `wa.ReleaseMutes(client, store, old.FeatureMuted)`.
  4. Save the new config (carrying over the now-trimmed `feature_muted`; the
     client never sets `feature_muted`).
  5. `wa.ReconcileNow(client, store)` so an enabled change applies immediately.
  6. Return the saved config.

### `internal/api/server.go` (modify)
- Register `/api/v2/working-hours` → the new handler.

### `main.go` (modify)
- Call `wa.StartWorkingHoursScheduler(client, store)` next to `StartPeriodicSync`.

### `web/src/api.ts` (modify)
- Add `WorkingHoursConfig` type + `api.workingHours()` (GET) and
  `api.setWorkingHours(cfg)` (PUT) client functions, following existing
  `mediaSettings`/`setMediaSettings` patterns.

### `web/src/explorer/WorkingHours.tsx` (new)
- A settings modal (following `PrivacySettings`/`MediaSettings`): enable toggle,
  start/end time inputs, working-day checkboxes (Sun–Sat), and a chat picker for
  the selected set. Loads via `api.workingHours()`, saves via
  `api.setWorkingHours()`.

### `web/src/explorer/Explorer.tsx` (modify)
- Import `WorkingHours`, add open state + a menu/button entry to launch it
  (mirroring how `MediaSettings`/`PrivacySettings` are opened).

## Testing

- `internal/wa/working_hours_test.go` — table-driven tests for `DesiredMute`:
  inside the window, outside (before start / after end), exactly at start minute
  (muted), exactly at end minute (unmuted — half-open `[start, end)`), weekend
  day (unmuted regardless of time), invalid window `start >= end` (unmuted), and
  disabled config (unmuted). Go testing is zero-config (`go test ./internal/wa/`).
- Backend implementer tasks verify by building **only their own package**
  (e.g. `go build ./internal/api/`, `go vet ./internal/wa/`) — NOT `go build ./...`,
  which would race the other half-written files in the same parallel wave. One
  whole-module `go build ./... && go vet ./...` runs once in the Review phase.
- Frontend tasks verify with `cd web && npx tsc --noEmit` (TS strict is the
  de-facto gate; no eslint config in repo).

## Assumptions

- `[ASSUMPTION]` Weekend = Friday + Saturday (MENA/KSA), working days Sun–Thu —
  user confirmed in brainstorm. Stored as a list so it stays editable.
- `[ASSUMPTION]` Mute logic is mute-DURING-hours (focus mode) — user confirmed.
- `[ASSUMPTION]` Scope is "selected chats only" with manual-mute protection —
  user confirmed; the safe-ownership model (`feature_muted`) is the design's
  interpretation of "don't clobber manual mutes."
- `[ASSUMPTION]` Server local time (host TZ, e.g. Asia/Riyadh) — user confirmed.
- `[ASSUMPTION]` Base branch for this work is `feat/wa-all-merged` (the current
  integration branch holding 8 merged features), not `main`. The feature merges
  back into `feat/wa-all-merged`.

## Known v1 simplification

Reconcile is **transition-guarded, not fully state-driven**: it keys off the
`feature_muted` flag, not the chat's live mute state. If the user manually
unmutes a feature-muted chat mid-window from their phone, the feature will not
re-mute it until the next boundary. This is an accepted v1 trade-off (avoids
fighting the user inside a window). Full state reconciliation can be added later
by comparing against `store.GetChat(jid).IsMuted`.

## Open questions

None blocking.
