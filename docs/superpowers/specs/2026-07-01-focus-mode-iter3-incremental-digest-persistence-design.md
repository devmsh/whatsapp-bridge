# Focus Mode — Iter 3: Incremental Circle Digest + Last-Circle Persistence

- **Date**: 2026-07-01
- **Iteration**: iter-03-incremental-digest-persistence (3 of 4)
- **Manifest**: `docs/superpowers/turbo-manifests/i-need-to-develop-a-focus-mode-where-i-can-choose-manifest.yml`
- **Branch**: `worktree-turbo+iter-03-incremental-digest-persistence-2026-07-01` (from `main`, which has iters 1-2 merged)
- **Status**: ready to build

## Summary

Add a circle-scoped digest panel to Focus Mode and remember the last-focused
circle across app reloads.

The digest shows the same four blocks as the global daily briefing — overdue
tasks, today's tasks, signal chats, and DMs awaiting reply — but scoped to one
circle's flattened chats. Unlike the global briefing (a once-daily batch), this
digest refreshes **incrementally**: it only regenerates after at least 10 new
messages have arrived in the circle since the last generation, and it feeds the
LLM the previous summary plus the new context instead of reprocessing the whole
history.

Persistence: the currently-focused circle id is saved to `localStorage`, so
relaunching the app reopens Focus Mode on the same circle.

## Goal

- A per-circle digest, cached in a new `circle_digests` table (one row per
  circle, upserted on regenerate).
- A watermark (`last_msg_ts`) tracks the newest message seen at generation time.
- `GET /api/v2/circles/{id}/digest` returns the cached digest **fast** and,
  when a refresh is warranted, kicks off regeneration in a background goroutine
  and returns `refreshing: true`.
- A `FocusDigest.tsx` panel renders the digest inside Focus Mode's left column,
  polling again while `refreshing` is true.
- The last-focused circle id persists in `localStorage`.

## Non-goals

- No history browser for digests (single-row-per-circle upsert, only "the
  latest" is kept).
- No media-understanding block (explicitly excluded from the whole feature).
- No changes to the global daily briefing engine (`buildBriefing` /
  `enrichBriefing` / `briefing.mjs` are read for reference and reused where
  noted, but not modified).

## Critical architectural constraint (fix from adversarial review)

The codebase **never** calls an LLM sidecar synchronously inside a GET handler.
Two existing patterns confirm this:

- **Global briefing**: `GET /api/v2/briefings/today` just reads the last saved
  row (`handler_briefings.go:73-84`). The LLM call only happens on the explicit
  `POST /api/v2/briefings/generate` (`handler_briefings.go:92-112`).
- **Circle extraction**: `POST /api/v2/circles/{id}/extract` returns
  immediately and runs the sidecar in a goroutine via `RunManager`
  (`handler_extract.go:232-249`); `AutoExtractor` guards overlapping sidecar
  runs with an explicit mutex + `running bool` (`auto_extract.go:16-22,64-96`).

Therefore `GET /api/v2/circles/{id}/digest` MUST **always return fast** — the
cached row or `null` — and never block on an LLM call. If regeneration is
needed (no cached row, or ≥10 new messages since the watermark), it kicks off a
background goroutine guarded by a per-circle in-flight tracker and returns the
(possibly stale or null) cached data immediately with `refreshing: true`. The
frontend polls again shortly when `refreshing` is true.

## Design

### Backend — circle digest engine

**New table** (`internal/db/schema.go`, appended near `briefings` /
`chat_extraction_state`, same style):

```sql
CREATE TABLE IF NOT EXISTS circle_digests (
    circle_id     INTEGER PRIMARY KEY,
    summary       TEXT    NOT NULL DEFAULT '',
    data          TEXT    NOT NULL DEFAULT '', -- full briefingPayload-shaped JSON
    last_msg_ts   INTEGER NOT NULL DEFAULT 0,  -- watermark: max message ts across the circle's flattened chats at generation time
    generated_at  INTEGER NOT NULL DEFAULT 0
);
```

One row per circle, upserted on regenerate (not append-only).

**New store file** (`internal/db/circle_digests.go`):

- `type CircleDigest struct { CircleID int64 "json:circle_id"; Summary string "json:summary"; Data string "json:data"; LastMsgTS int64 "json:last_msg_ts"; GeneratedAt int64 "json:generated_at" }`
  (json tags matching the snake_case column names).
- `func (s *Store) GetCircleDigest(circleID int64) (*CircleDigest, error)` —
  returns `nil, nil` (not an error) when no row exists (mirror `GetCircle`'s
  `sql.ErrNoRows -> nil, nil` handling, `circles.go:122-124`).
- `func (s *Store) SaveCircleDigest(circleID int64, summary, data string, lastMsgTS int64) error`
  — upsert:
  ```sql
  INSERT INTO circle_digests (circle_id, summary, data, last_msg_ts, generated_at)
  VALUES (?,?,?,?,?)
  ON CONFLICT(circle_id) DO UPDATE SET
      summary=excluded.summary, data=excluded.data,
      last_msg_ts=excluded.last_msg_ts, generated_at=excluded.generated_at
  ```
  This `ON CONFLICT(...) DO UPDATE SET excluded.x` idiom is already used in this
  codebase (verified: `internal/db/media_understanding.go:52-58`; also
  `contacts.go`, `chats.go`, `profiles.go`).

**New handler file** (`internal/api/handler_circle_digest.go`):

- Package-level in-flight tracker so concurrent requests for the SAME circle
  don't spawn duplicate regenerations, while DIFFERENT circles regenerate
  concurrently:
  ```go
  var circleDigestInFlight = struct {
      mu  sync.Mutex
      ids map[int64]bool
  }{ids: map[int64]bool{}}
  ```
  A package-level var is chosen over a `Server` field because the digest route
  is registered inside `handleCircleByID`'s switch (see below), so `server.go`
  is not touched, matching how `handler_briefings.go` adds no `Server` state.

- `func (s *Server) handleCircleDigest(w http.ResponseWriter, r *http.Request, id int64)`:
  1. GET only (`methodNotAllowed` otherwise).
  2. `existing, err := s.store.GetCircleDigest(id)` — 500 on real error; `nil`
     is fine.
  3. `jids, err := s.store.FlattenCircleChats(id)` — 500 on error.
  4. `watermark := int64(0); if existing != nil { watermark = existing.LastMsgTS }`.
  5. `newCount := s.circleMessageCountSince(jids, watermark)` (see helper).
  6. `needsRefresh := existing == nil || newCount >= 10`.
  7. If `needsRefresh` AND not already in-flight for `id`: mark in-flight and
     `go func(){ defer <clear in-flight>; s.regenerateCircleDigest(id, jids, existing) }()`.
     Do NOT wait.
  8. Respond immediately with
     `{"digest": <existing.Data as json.RawMessage, or null>, "refreshing": needsRefresh}`.
     Because `existing.Data` is already a JSON string, wrap it in
     `json.RawMessage` so it is not double-escaped.

- Helper `func (s *Server) circleMessageCountSince(jids []string, watermark int64) int`
  — builds a placeholder list from `jids` exactly like
  `TasksForCircle` (`tasks.go:281-288`), then
  `SELECT COUNT(*) FROM messages WHERE chat_jid IN (<placeholders>) AND timestamp > ?`
  run directly against `s.store.DB` (the api package already runs raw queries
  against `s.store.DB` — `handler_briefings.go:124,161,190,298`). Returns 0 when
  `jids` is empty.

- `func (s *Server) regenerateCircleDigest(id int64, jids []string, existing *db.CircleDigest)`
  (runs in a background goroutine; best-effort, logs errors, never panics —
  there is no request to respond to):
  1. **Tasks** — reuse `s.store.TasksForCircle(id)`. In Go, filter to
     `ReviewStatus == "accepted"` and `Status in ("open","in_progress")`, then:
     - `overdue` = `DueAt > 0 && DueAt < time.Now().Unix()`, sorted by `DueAt`
       ascending, capped at 10 (mirrors `handler_briefings.go:128-140`).
     - `today` = same filtered set, sorted by priority (`high` first) then
       `UpdatedAt` descending, capped at 5 (mirrors
       `handler_briefings.go:144-157`).
     - Map each into a `briefingTask` (reuse the existing struct — same
       package).
  2. **Signal chats + awaiting-reply — NO fixed 24h/7d floor.** A circle can go
     quiet for days between count-based regenerations, so `buildBriefing`'s
     hardcoded 24h window would silently return an empty digest despite real
     content. Write NEW circle-scoped queries:
     - **Signal chats**: adapt `handler_briefings.go:161-186` — add
       `m.chat_jid IN (<jids>)`, keep the same newsletter/status/hidden/
       empty-content exclusions and the `HAVING COUNT(*) >= 3 ... LIMIT 8`, but
       use `watermark` (not `dayAgo`) as the `m.timestamp > ?` floor, so it only
       surfaces genuinely new activity.
     - **Awaiting-reply**: adapt `handler_briefings.go:190-217` — add
       `chat_jid IN (<jids>)`, and use `max(watermark, weekAgo)` as the floor so
       a circle that has been quiet for months does not dump excessive history.
  3. **Per-signal-chat samples** — a NEW helper
     `func (s *Server) recentChatLinesByCount(jid string, n int) []string`
     (same query shape as `recentChatLines`, `handler_briefings.go:297-338`,
     **minus** the `AND timestamp >= ?` clause: `ORDER BY timestamp DESC LIMIT ?`,
     then reversed to chronological). Use `n = 15` (above the 10-message trigger,
     giving natural overlap with previously-seen context).
  4. **Sidecar** — `s.runAgentInput(3*time.Minute, string(inputJSON), "circle-digest.mjs")`
     with input `{circle_name, previous_summary: existing?.Summary ?? "",
     new_message_count: newCount, tasks_top, tasks_overdue, awaiting_reply,
     signal_chats}`. Fetch `circle_name` via `s.store.GetCircle(id)`
     (`circles.go:115`).
  5. Parse `{ok, summary, signal_summaries}` from the sidecar's last JSON line
     (same `lastJSONLine` parsing as `enrichBriefing`,
     `handler_briefings.go:277-291`).
  6. Assemble a `briefingPayload` (`ForDate` = today, `GeneratedAt` = now,
     `Summary`, `Today`, `Overdue`, `SignalChats` with narratives merged in,
     `AwaitingReply`, `StatsTasksOpen` = count of accepted open tasks in the
     circle), marshal to JSON.
  7. Compute the new watermark:
     `SELECT MAX(timestamp) FROM messages WHERE chat_jid IN (<jids>)` right now.
  8. `s.store.SaveCircleDigest(id, summary, jsonData, newWatermark)`.

**Routing** (`internal/api/handler_circles.go`): add `case "digest":
s.handleCircleDigest(w, r, id)` to the switch in `handleCircleByID`
(`handler_circles.go:208-223`), alongside `"chats"`, `"extract"`, `"export"`,
etc. The `/api/v2/circles/` catch-all is already registered
(`server.go:162`), so `server.go` needs no change.

**New sidecar** (`agent/circle-digest.mjs`): adapted from `agent/briefing.mjs`
(same stdin reading, Claude Agent SDK single-shot query, JSON-fence stripping,
output shape `{ok, summary, signal_summaries}`). stdin gains `circle_name`,
`previous_summary`, and `new_message_count`. The system prompt keeps
`briefing.mjs`'s core instructions but is scoped to one circle by name and adds:
"If `previous_summary` is non-empty, you are UPDATING an existing digest, not
writing from scratch: keep what is still accurate, drop what is now stale, and
weave in what is new from the supplied signal chats — produce one coherent,
current summary, not an appended log."

### Frontend — digest UI

**`web/src/api.ts`**: add, next to the existing briefing methods
(`api.ts:1375-1384`):
```ts
circleDigest: async (
  id: number,
): Promise<{ digest: BriefingPayload | null; refreshing: boolean }> => {
  const res = await fetch(`/api/v2/circles/${id}/digest`)
  return res.json()
},
```
Reuse the existing `BriefingPayload` type (`api.ts:591-600`) — the Go side
returns the exact same shape wrapped in `{digest, refreshing}`.

**`web/src/explorer/BriefingView.tsx`**: change the four pure presentational
functions `Section` (line 158), `TaskRow` (line 180), `SignalChatRow` (line
220), and `AwaitingRow` (line 243) from `function ...` to `export function ...`.
They are self-contained (they only reference module-level `daysSince`/`timeAgo`
and the imported `jidUser`), so exporting them changes nothing else. No other
edits to this file.

**`web/src/explorer/FocusDigest.tsx`** (new): props
`{ circleId: number; onOpenTask: (id: number) => void; onOpenChat: (jid: string) => void }`.
- Fetches `api.circleDigest(circleId)` on mount and whenever `circleId` changes.
- **Race guard**: a fetch can resolve after the user has switched circles, so
  guard `setState` — capture the requested `circleId` in the effect closure and
  apply the response only if it still matches (a `cancelled` flag set in the
  effect cleanup, or an `AbortController`).
- **Poll while refreshing**: if the response has `refreshing: true`, schedule
  one re-fetch after ~4 seconds (guarded by the same cancelled/`circleId`
  check), and stop polling once a response returns `refreshing: false` or the
  component unmounts / `circleId` changes.
- Renders using the imported `Section` / `TaskRow` / `SignalChatRow` /
  `AwaitingRow` from `./BriefingView`, matching `BriefingModal`'s body
  (`BriefingView.tsx:100-150`) minus the modal chrome, plus a small "Refreshing…"
  indicator when `refreshing` is true and a "No digest yet — refreshing…" empty
  state when `digest` is `null`.

### Frontend — wire into Focus Mode

**`web/src/explorer/FocusMode.tsx`**: the left column stacks two
`flex-1 overflow-hidden` wrapper divs (`FocusProfile` at line 104-107 and
`FocusTasks` at line 108-121; the file even carries a comment at lines 93-98
anticipating a digest panel). Add a THIRD wrapper (same pattern) ABOVE those
two, containing
`<FocusDigest circleId={circleId} onOpenTask={onOpenTask} onOpenChat={onOpenChat} />`.
`onOpenTask` and `onOpenChat` are already props on `FocusMode`
(`FocusMode.tsx:50,45`) forwarded from `Explorer.tsx` — no new prop-threading is
needed. Import `FocusDigest` from `./FocusDigest`.

### Frontend — last-focused-circle persistence

**`web/src/explorer/Explorer.tsx`**: `focusCircleId` is declared at line 67
(`useState<number | null>(null)`), and the file already persists the last chat
via `wa.last-chat-jid` (lines 376-381 write; 388-401 mount-replay). Mirror that
idiom for the focused circle:
- Initialize from `localStorage`:
  ```ts
  const [focusCircleId, setFocusCircleId] = useState<number | null>(() => {
    try {
      const saved = localStorage.getItem('wa.focus-circle-id')
      return saved ? Number(saved) : null
    } catch { return null }
  })
  ```
- Write back on every change:
  ```ts
  useEffect(() => {
    try {
      if (focusCircleId != null) localStorage.setItem('wa.focus-circle-id', String(focusCircleId))
      else localStorage.removeItem('wa.focus-circle-id')
    } catch {}
  }, [focusCircleId])
  ```
- **Validate against loaded circles**: `circles` is fetched async
  (`Explorer.tsx:153`). Once it loads, drop a restored id that no longer maps to
  a real circle, so a deleted circle is not resurrected into a broken Focus Mode
  view:
  ```ts
  useEffect(() => {
    if (circles.length > 0 && focusCircleId != null &&
        !circles.some((c) => c.id === focusCircleId)) {
      setFocusCircleId(null)
    }
  }, [circles])
  ```

## Data flow

1. Focus Mode mounts → `FocusDigest` calls `GET /api/v2/circles/{id}/digest`.
2. Handler reads the cached row, flattens the circle's chats, counts messages
   since the watermark.
3. If no cached row or ≥10 new messages → mark in-flight, spawn
   `regenerateCircleDigest` in a goroutine, return the cached data (or `null`)
   with `refreshing: true`.
4. `FocusDigest` renders what it got; if `refreshing`, it re-fetches in ~4s.
5. The goroutine builds tasks/signal/awaiting facts (watermark-floored),
   samples the last 15 messages per signal chat, calls `circle-digest.mjs` with
   the previous summary, and upserts the fresh row + new watermark.
6. The next poll returns the fresh digest with `refreshing: false`.

## Verification gates (per task)

- **Backend (T01)**: `go build ./...` and `go vet ./...` both pass cleanly.
- **Frontend (T02, T03, T04)**: `tsc -b` (via `npx tsc -b` in `web/`) and
  `npx vite build` (in `web/`) both succeed. No web lint is configured.
- QA is skipped this iteration (consistent with iters 1-2; no test script/files
  under `web/`; T01 does not touch the existing tested Go files in
  `internal/api`/`internal/wa`). Commit granularity: per task.

## Assumptions

- **[ARCHITECTURAL FIX]** The naive design would have called the LLM sidecar
  synchronously inside the GET handler — this NEVER happens elsewhere in the
  codebase (global briefing splits GET / POST-generate; circle extraction runs
  the sidecar in a goroutine with SSE progress). Fixed: GET always returns fast
  (cached data + a `refreshing` flag), regeneration happens in a background
  goroutine guarded by a per-circle in-flight tracker.
- **[FIX]** Signal-chat and per-chat sample queries cannot reuse
  `buildBriefing`'s / `recentChatLines`'s hardcoded 24h window — a circle can go
  quiet for days between count-based regenerations, and a 24h floor would return
  an empty digest despite real content existing. New watermark-floored queries
  (signal chats) and last-N-by-count sampling (per-chat message context, N=15)
  were written instead.
- **[CONFIRMED via review]** `go-sqlite3`'s `ON CONFLICT(...) DO UPDATE SET
  excluded.x` upsert idiom is already used elsewhere in this codebase
  (`media_understanding.go:52-58`) — reused directly, no compatibility concern.
- **[CONFIRMED via review]** The Go `briefingPayload` struct is unexported but
  lives in the same `api` package as the new handler — reused directly, no
  duplication needed.
- **[DECISION]** `circle_digests` is a single-row-per-circle upsert table
  (unlike `briefings`' append-only history) — no history browser is needed for
  this UI, only "the latest."
- **[FIX]** `FocusDigest.tsx`'s fetch is guarded against out-of-order responses
  when the user switches circles quickly (a cancelled / `circleId`-match check
  before applying `setState`).
- **[FIX]** Restoring a persisted `focusCircleId` from `localStorage` validates
  the circle still exists once `circles` loads, to avoid resurrecting a deleted
  circle into a broken Focus Mode view.
- **[VERIFIED]** `server.go` is NOT modified: the digest route rides the
  existing `/api/v2/circles/` catch-all (`server.go:162`) via a new `case
  "digest"` in `handleCircleByID` (`handler_circles.go:208-223`). This deviates
  from the manifest's *projected* backend files (which guessed
  `handler_briefings.go`, `internal/db/briefings.go`, `server.go`); the
  finalized design uses new files (`circle_digests.go`, `handler_circle_digest.go`,
  `circle-digest.mjs`) and leaves the global-briefing files untouched. Projected
  files in the manifest are estimates, not contracts.

## Open questions

- None material — every concern raised by the adversarial design review has a
  concrete fix folded into the tasks above.
