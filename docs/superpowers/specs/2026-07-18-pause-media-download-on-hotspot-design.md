# Pause media download on hotspot — design

Date: 2026-07-18
Status: approved (brainstormed with the user; this document records the approved design, it does not re-open it)

## Problem

The bridge auto-downloads every media file WhatsApp delivers. When the Mac is tethered to the
user's iPhone Personal Hotspot, that burns cellular data. Turning media download off entirely
(the existing per-type toggles in `internal/wa/media_policy.go`) is too blunt: it stays off, and
anything skipped is lost forever because `DownloadMedia` drops the whatsmeow media keys.

## Goal

Media auto-download stays **ON by default** but **pauses** while the Mac's default gateway sits in a
blocked CIDR (the hotspot). Anything skipped during the pause is **deferred, not dropped**, and is
downloaded automatically once the machine is back on a safe network.

## Non-goals

- Changing the behavior of the existing per-type toggles or `max_size_mb`. Those keep their current
  "skip and forget" semantics. Someone who turned images off does not want a growing image backlog.
- Metering by data volume, time of day, or download quota.
- Any new HTTP route. The feature rides the existing `GET/PUT /api/v2/settings/media`.

## Key research constraint: detect by gateway, not SSID

Verified on the user's machine: `ipconfig getsummary en0` returns `SSID : <redacted>` unless the
calling process holds Location Services permission. The bridge runs as a LaunchAgent
(`com.devmsh.whatsapp-bridge`) and will not have that permission. **SSID detection is therefore
unusable.**

Instead we read the **default route's gateway** from the kernel routing table and compare it against
a list of blocked CIDRs.

- `172.20.10.0/28` — iOS Personal Hotspot. iOS hard-codes this range; the gateway is always
  `172.20.10.1`. This is deterministic, not a heuristic.
- `192.168.43.0/24` — the typical Android hotspot range.

## Design

### 1. `internal/netcheck` — the detector

New package. Pure Go, no cgo, no shell-out to `route` / `networksetup` / `ipconfig`.

```go
type Checker struct { /* CIDRs, cache, mutex */ }

func New(cidrs []string) *Checker
func (c *Checker) SetCIDRs(cidrs []string)
func (c *Checker) IsBlocked() (blocked bool, gateway string, err error)
```

- Reads the default gateway via `golang.org/x/net/route` (`route.FetchRIB` +
  `route.ParseRIB` over `AF_UNSPEC` / `RIBTypeRoute`), picking the route whose destination is
  `0.0.0.0/0`. `x/net` v0.54.0 is already in `go.sum` as an indirect dependency and present in the
  local module cache, so promoting it to a direct dependency is offline-safe.
- `x/net/route` is BSD/Darwin-only, so the gateway reader is split:
  `gateway_darwin.go` (real implementation, `//go:build darwin`) and `gateway_other.go`
  (`//go:build !darwin`, returns a "not supported on this platform" error). Non-Darwin therefore
  fails open — correct, since this feature exists for one Mac.
- Result cached ~10s so a burst of incoming messages does not hit the routing table per message.
- **Fail open.** On any error `IsBlocked` returns `blocked=false` with the error, and the caller
  logs it. A broken detector must never strand downloads.
- Empty CIDR list ⇒ never blocked.

### 2. Defer instead of drop — `internal/wa/media.go`

Today `DownloadMedia` returns metadata-only on a policy skip. The `whatsmeow.DownloadableMessage`
(media key, direct path, file SHA256, enc SHA256) is discarded, so the file can never be fetched
later.

Change: when the skip reason is **network blocked**, persist the **marshaled protobuf of the whole
`waE2E.Message`** into a new `pending_media_downloads` row. On retry we `proto.Unmarshal` it, run
`detectMedia` again, and call the exact same `wa.Download(...)` path. One copy of the download
logic, no drift.

Signature change (both call sites have everything needed):

```go
// MediaDeferCtx carries what DownloadMedia needs to defer a network-skipped download.
// A nil ctx disables deferring entirely (old behavior).
type MediaDeferCtx struct {
    ChatJID string
    Store   *db.Store
    Net     NetChecker // interface { IsBlocked() (bool, string, error) }
    Enabled bool       // policy.PauseOnMetered
}

func DownloadMedia(wa *whatsmeow.Client, msg *waE2E.Message, msgID, baseDir string,
    policy MediaPolicy, dctx *MediaDeferCtx, log waLog.Logger) *MediaInfo
```

Order of checks inside `DownloadMedia` (load-bearing — the scope boundary lives here):

1. `policy.allows(type)` false ⇒ metadata only, **no pending row** (unchanged).
2. over `policy.maxBytes()` ⇒ metadata only, **no pending row** (unchanged).
3. the target `filePath` already exists on disk ⇒ return it as-is, **no pending row**. This stat
   check moves *above* the network gate: during a pause the same media is routinely re-presented
   (history-sync reprocessing, a re-received message, a duplicate `MessageID`), and deferring a file
   that is already downloaded would blank out `media_path` and queue a junk row.
4. `dctx != nil && dctx.Enabled && dctx.Net.IsBlocked()` ⇒ metadata only **plus a pending row**.
   The embedded `JPEGThumbnail` is still written here — it is already in the protobuf, so it costs
   zero cellular bytes and a deferred image keeps its preview during the pause.
5. otherwise download as today.

Call sites: `internal/wa/handler_messages.go:76` and `internal/wa/handler_newsletters.go:80`.
History sync (`internal/wa/handler_history.go`) reuses `handleMessage`, so it is covered by the
first call site with no change of its own.

`wa.Client` gains a `Net NetChecker` field, initialised at startup from the policy's CIDR list and
re-pointed whenever `SetMediaPolicy` changes the list.

### 3. Table `pending_media_downloads`

```sql
CREATE TABLE IF NOT EXISTS pending_media_downloads (
    message_id   TEXT    NOT NULL,
    chat_jid     TEXT    NOT NULL,
    media_type   TEXT    NOT NULL DEFAULT '',
    media_mime   TEXT    NOT NULL DEFAULT '',
    media_size   INTEGER NOT NULL DEFAULT 0,
    proto        BLOB    NOT NULL,
    created_at   INTEGER NOT NULL DEFAULT 0,
    attempts     INTEGER NOT NULL DEFAULT 0,
    last_attempt INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT    NOT NULL DEFAULT '',
    failed       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (chat_jid, message_id)
);
CREATE INDEX IF NOT EXISTS idx_pending_media_dl ON pending_media_downloads(failed, created_at);
```

Added to `internal/db/schema.go` (the DDL constant applied on every open) and, for existing
databases, guarded by the same idempotent `sqlDB.Exec` list in `internal/db/db.go:37-51`. Since the
whole statement is `CREATE TABLE IF NOT EXISTS`, running it from `Schema` is already sufficient;
the index line goes in the migration list to match the file's existing convention.

Store methods in a new `internal/db/pending_media.go`:
`AddPendingMediaDownload`, `NextPendingMediaDownloads(limit)` (oldest-first, `failed=0`, backoff
window respected), `DeletePendingMediaDownload`, `MarkPendingMediaDownloadError`,
`CountPendingMediaDownloads() (pending, failed int)`.

### 4. Catch-up worker — `internal/api/media_catchup_worker.go`

Mirrors the existing idiom in `internal/api/media_worker.go`: a poll loop (`workerLoop`, ~line 161),
a fan-out over a channel to N goroutines (`drain`, ~line 177), and a `running map[string]bool`
dedupe guard under a mutex (`processOne`, ~line 203). Same shape, new file.

- Ticks every 30s. If `netcheck` says blocked, **skip the whole tick** (log at debug).
- Otherwise drain oldest-first with **2 parallel** workers.
- Success: write the media file and thumbnail exactly as `DownloadMedia` does, `UPDATE messages SET
  media_path=?, thumbnail_path=? WHERE id=? AND chat_jid=?`, delete the pending row.
- Failure: `attempts++`, record `last_error`, set `last_attempt`. Backoff is exponential on
  `attempts` (1m, 4m, 16m, 64m, 256m) and enforced by the `NextPendingMediaDownloads` query.
- After **5 attempts** set `failed=1` and stop retrying.
- Started from `Server.StartProfiler()` (`internal/api/server.go:33-37`) alongside the existing
  managers.

**Caveat, by design:** WhatsApp servers do not retain media indefinitely. A long pause means some
deferred files come back as gone/not-found. Those exhaust their attempts and land in `failed=1`,
where they surface in the UI as permanently unavailable. They are never retried forever and never
silently stuck in "pending".

Once `media_path` is non-empty the existing AI media-understanding workers pick the message up with
no change: `db.PendingMedia` (`internal/db/media_understanding.go:175`) selects on
`m.media_path != ''`. Verified — this still holds, because the catch-up worker writes `media_path`
onto the same `messages` row.

### 5. Settings — reuse the existing plumbing

`wa.MediaPolicy` (`internal/wa/media_policy.go:11`) gains:

```go
PauseOnMetered bool     `json:"pause_on_metered"`
BlockedCIDRs   []string `json:"blocked_cidrs"`
```

`DefaultMediaPolicy()` returns `PauseOnMetered: true` and the two default CIDRs.

**Backward compatibility is explicit, not incidental.** `InitMediaPolicy` currently does a plain
`json.Unmarshal` into a zero-value struct, which would give `false` / `nil` for an old stored blob —
silently disabling the feature. Instead, unmarshal into a struct with `*bool` / `[]string` presence
detection (or unmarshal into `map[string]json.RawMessage` first) and:

- key `pause_on_metered` absent ⇒ `true`
- key `blocked_cidrs` absent **or empty** ⇒ the two default CIDRs

**The normalisation applies to the final policy, whichever branch produced it — not only to the
decoded-blob branch.** `InitMediaPolicy` starts from the `def` argument, and `main.go:55` builds
that `def` as a literal carrying neither new field. So a **fresh install** with no stored
`media_policy` row would otherwise end up with `PauseOnMetered=false` and `BlockedCIDRs=nil` — the
feature off by default and `blocked_cidrs` marshalling to `null`. The `def` branch is therefore
treated as "pause key absent" ⇒ `PauseOnMetered = true`, and `BlockedCIDRs` is backfilled to the two
defaults (and kept non-nil) on every path. `main.go` itself is not changed.

**LOAD vs SAVE are deliberately asymmetric.** Presence detection is a **load-only** concern:

- **Load** (`InitMediaPolicy`): absent `pause_on_metered` ⇒ `true`.
- **Save** (`SetMediaPolicy`): **never** touches `PauseOnMetered` — it persists exactly what the
  caller sent. The `PUT` handler decodes into a plain `wa.MediaPolicy`
  (`internal/api/handler_settings.go:19`), which destroys key presence, so an explicit
  `"pause_on_metered": false` and an absent key are indistinguishable there. Normalising on save
  would flip a deliberate `false` back to `true` and make the UI toggle impossible to turn off.
  The only save-time backfill is `BlockedCIDRs` when `nil` or empty.

`SetMediaPolicy` also re-points `c.Net`'s CIDR list.

`BlockedCIDRs` must always be initialised as `[]string{...}`, never a nil slice — a nil slice
marshals to JSON `null` and crashes `.map` on the frontend.

`GET /api/v2/settings/media` (`internal/api/handler_settings.go:14`) changes from returning the bare
policy to returning:

```json
{
  "images": true, "video": true, "audio": true, "documents": true, "stickers": true,
  "max_size_mb": 0,
  "pause_on_metered": true,
  "blocked_cidrs": ["172.20.10.0/28", "192.168.43.0/24"],
  "network": { "gateway": "172.20.10.1", "blocked": true, "error": "" },
  "pending_count": 12,
  "failed_count": 1
}
```

The policy fields stay at the top level so `PUT` can accept the same object back unchanged (the
`network` / `*_count` fields are ignored on write). Route registration in
`internal/api/server.go:215` is unchanged.

### 6. Web UI — `web/src/Settings.tsx`

Inside the existing `MediaSettings` modal (rendered from `web/src/explorer/Explorer.tsx:640`):

- A checkbox "Pause downloads on phone hotspot", styled exactly like the existing rows:
  `<input type="checkbox" className="h-4 w-4 accent-emerald-500">`.
- A live status line under it: current gateway and whether it counts as blocked.
- "N files waiting to download" and, separately, "N permanently failed".
- The existing explicit **Save** button stays. No optimistic auto-save.

`web/src/api.ts`: `interface MediaPolicy` (line 49) gains the two new fields; a new
`interface MediaSettingsResponse extends MediaPolicy` carries `network`, `pending_count`,
`failed_count`; `mediaSettings()` (line 842) returns the richer type; `setMediaSettings()`
(line 846) keeps sending a `MediaPolicy`.

## Assumptions

- [ASSUMPTION] `golang.org/x/net/route` can enumerate the Darwin routing table without elevated
  privileges from inside a LaunchAgent. It reads via `sysctl`, which is unprivileged, so this should
  hold — but it is verified for real only when the rebuilt binary runs under launchd.
- [ASSUMPTION] The iOS Personal Hotspot gateway is always inside `172.20.10.0/28`. This matches
  Apple's fixed allocation and the user's observed setup.
- [ASSUMPTION] Storing the marshaled `waE2E.Message` is enough to re-download later — i.e. whatsmeow
  needs nothing from the surrounding `events.Message` envelope for `Download`. `detectMedia` only
  ever touches fields on the message itself, so this holds.
- [ASSUMPTION] Deferred media rows are low-volume (tens to low hundreds), so storing the full
  protobuf blob per row is an acceptable size cost.
- [ASSUMPTION] Five attempts with exponential backoff is the right cap. It spans roughly 5 hours,
  long enough to cover a normal hotspot session.
- [ASSUMPTION] The user is on macOS only. Non-Darwin builds compile but always report "safe".

## Open questions

- Should a permanently-failed row be user-retryable from the UI (a "retry" button), or is showing
  the count enough for now? This design ships the count only.
- Should the pending backlog be capped (e.g. drop the oldest beyond N) so a very long hotspot
  session cannot grow the table without bound? **Currently uncapped, and this is a real risk, not a
  theoretical one:** every `pending_media_downloads` row stores the **full marshaled `waE2E.Message`
  protobuf**, so a multi-day hotspot session on a busy group could grow the table without any bound.
  Nothing in this design trims it — rows leave only by succeeding (deleted) or by being marked
  `failed=1` (kept forever). No cap, no TTL, no vacuum. If this becomes a problem the cheapest fix
  is a row-count cap on insert (drop the oldest `failed=0` row beyond N) plus periodic deletion of
  `failed=1` rows older than a few days.
- Should `voice_note` be exempt from the pause, since voice notes are small and feed the
  transcription pipeline? Currently no exemption — the pause is all-or-nothing.
- Should the status line auto-refresh while the modal is open, or only on open? This design fetches
  on open and after Save.

## Build and runtime gotchas (repo-specific)

- Go is **not** on `PATH`. Use `~/.local/go/bin/go`.
- `make build` **fails** (pnpm ignores the esbuild install script). Regenerate the frontend with
  `npx vite build` inside `web/`, then commit `web/dist`. The Go binary embeds `web/dist`.
- `var x []T` that is never appended marshals to JSON `null` and crashes an unguarded `.length` /
  `.map` on the frontend. Always initialise as `[]T{}` — this applies directly to `blocked_cidrs`
  and to any pending-list response.
- The bridge runs as a LaunchAgent. Do **not** run the binary by hand. After a rebuild:
  `launchctl kickstart -k gui/501/com.devmsh.whatsapp-bridge`.
- UI verification: `cd web && npm run dev` (vite on :5173). Do not restart the live bridge for UI
  work.
- New tables follow the existing pattern in `internal/db/schema.go` (see `sync_state`, line 231) plus
  the idempotent migration list in `internal/db/db.go:37`.

## Tests

- `internal/netcheck`: table-driven CIDR matching, including malformed CIDRs, an empty CIDR list, and
  a gateway-read error proving fail-open.
- Policy gate: a network-blocked download writes a `pending_media_downloads` row whose protobuf
  round-trips back to the same media descriptor; a per-type-disabled download and an oversize
  download write **no** row.
- Worker: skips the tick while blocked; drains oldest-first when safe; increments `attempts` and
  respects backoff on failure; sets `failed=1` after the attempt cap.
- Backward compat: a stored `media_policy` JSON without the new keys loads as
  `pause_on_metered=true` with the two default CIDRs.
