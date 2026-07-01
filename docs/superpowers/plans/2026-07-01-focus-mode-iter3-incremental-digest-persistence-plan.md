# Plan — Focus Mode Iter 3: Incremental Circle Digest + Last-Circle Persistence

- **Spec**: `docs/superpowers/specs/2026-07-01-focus-mode-iter3-incremental-digest-persistence-design.md`
- **Branch**: `worktree-turbo+iter-03-incremental-digest-persistence-2026-07-01`
- **Execution**: turbo subagent-driven, per-wave parallel implementers + serialized committer.
- **QA**: skipped this iteration (no `web/` test script; T01 does not touch existing tested Go files). See `dag_warnings`.
- **Lint / build gates**: Backend — `go build ./...` AND `go vet ./...` clean. Frontend — `npx tsc -b` AND `npx vite build` (run in `web/`) both succeed. No web lint configured.
- **Commit granularity**: per task; each task ends with `verification-before-completion` evidence and a commit.

## Waves overview

- **Wave 0** (3 tasks, independent, no `files_write` overlap): T01 backend digest engine, T02 frontend digest UI, T03 last-circle persistence.
- **Wave 1** (1 task): T04 wire `FocusDigest` into `FocusMode.tsx` (depends on T02).

`wave_count = 2`, `max_wave_width = 3`.

---

## Wave 0

### T01 — Backend circle-digest engine (Go + new Node sidecar)

- **id**: T01
- **wave**: 0
- **depends_on**: []
- **files_write**:
  - `internal/db/schema.go`
  - `internal/db/circle_digests.go`
  - `internal/api/handler_circle_digest.go`
  - `internal/api/handler_circles.go`
  - `agent/circle-digest.mjs`
- **files_read**:
  - `internal/api/handler_briefings.go`
  - `internal/db/tasks.go`
  - `internal/db/circles.go`
  - `internal/db/media_understanding.go`
  - `internal/api/auto_extract.go`
  - `internal/api/handler_extract.go`
  - `internal/api/server.go`
  - `agent/briefing.mjs`

**Steps**

1. `internal/db/schema.go`: append a `circle_digests` table to the `Schema`
   const, near the `briefings` / `chat_extraction_state` tables, same style:
   ```sql
   CREATE TABLE IF NOT EXISTS circle_digests (
       circle_id     INTEGER PRIMARY KEY,
       summary       TEXT    NOT NULL DEFAULT '',
       data          TEXT    NOT NULL DEFAULT '',
       last_msg_ts   INTEGER NOT NULL DEFAULT 0,
       generated_at  INTEGER NOT NULL DEFAULT 0
   );
   ```
   (`CREATE TABLE IF NOT EXISTS` runs on every boot, so no migration entry is
   needed — matches how every other table in this const is created.)

2. `internal/db/circle_digests.go` (new): `CircleDigest` struct (json tags
   `circle_id`/`summary`/`data`/`last_msg_ts`/`generated_at`);
   `GetCircleDigest(circleID int64) (*CircleDigest, error)` returning `nil, nil`
   on `sql.ErrNoRows` (mirror `GetCircle`, `circles.go:122-124`);
   `SaveCircleDigest(circleID int64, summary, data string, lastMsgTS int64) error`
   using the `ON CONFLICT(circle_id) DO UPDATE SET ...=excluded....` upsert
   idiom (mirror `media_understanding.go:52-58`).

3. `internal/api/handler_circle_digest.go` (new):
   - package-level in-flight tracker
     `var circleDigestInFlight = struct{ mu sync.Mutex; ids map[int64]bool }{ids: map[int64]bool{}}`.
   - `handleCircleDigest(w, r, id)` — GET-only; read cached row; flatten chats;
     count messages since watermark; `needsRefresh := existing == nil || newCount >= 10`;
     if `needsRefresh` and not in-flight, mark in-flight and spawn
     `go regenerateCircleDigest(...)` (defer-clear the in-flight flag); respond
     immediately `{"digest": <existing.Data as json.RawMessage | null>, "refreshing": needsRefresh}`.
   - helper `circleMessageCountSince(jids []string, watermark int64) int` —
     placeholder build like `tasks.go:281-288`, raw query on `s.store.DB`
     (returns 0 if `jids` empty).
   - `regenerateCircleDigest(id int64, jids []string, existing *db.CircleDigest)` —
     background goroutine body per the spec: tasks via `TasksForCircle` filtered
     in Go (overdue ≤10, today ≤5); watermark-floored signal-chat query (adapt
     `handler_briefings.go:161-186`, floor = `watermark`); awaiting-reply query
     (adapt `handler_briefings.go:190-217`, floor = `max(watermark, weekAgo)`,
     `chat_jid IN (jids)`); per-signal-chat samples via new
     `recentChatLinesByCount(jid, 15)`; call `runAgentInput(3*time.Minute, inputJSON, "circle-digest.mjs")`
     with `{circle_name, previous_summary, new_message_count, tasks_top, tasks_overdue, awaiting_reply, signal_chats}`;
     parse `{ok, summary, signal_summaries}` via `lastJSONLine`; assemble
     `briefingPayload`, marshal; compute new watermark via
     `SELECT MAX(timestamp) ... WHERE chat_jid IN (jids)`; `SaveCircleDigest`.
     Best-effort — log errors, never panic.
   - helper `recentChatLinesByCount(jid string, n int) []string` — same shape as
     `recentChatLines` (`handler_briefings.go:297-338`) minus the
     `AND timestamp >= ?` clause; `ORDER BY timestamp DESC LIMIT ?` then reverse.

4. `internal/api/handler_circles.go`: add `case "digest": s.handleCircleDigest(w, r, id)`
   to the `handleCircleByID` switch (`handler_circles.go:208-223`).

5. `agent/circle-digest.mjs` (new): adapt `agent/briefing.mjs` — stdin gains
   `circle_name`, `previous_summary`, `new_message_count`; system prompt scoped
   to one circle by name plus the "UPDATE, don't rewrite from scratch"
   instruction; output shape unchanged `{ok, summary, signal_summaries}`.

**Verification**: `go build ./...` and `go vet ./...` both pass cleanly. Commit.

---

### T02 — Frontend digest UI (API client + shared rows + FocusDigest panel)

- **id**: T02
- **wave**: 0
- **depends_on**: []
- **files_write**:
  - `web/src/api.ts`
  - `web/src/explorer/BriefingView.tsx`
  - `web/src/explorer/FocusDigest.tsx`
- **files_read**: []

**Steps**

1. `web/src/api.ts`: add a `circleDigest(id)` method next to the briefing
   methods (`api.ts:1375-1384`), returning
   `Promise<{ digest: BriefingPayload | null; refreshing: boolean }>` by
   `fetch(`/api/v2/circles/${id}/digest`)`. Reuse the existing `BriefingPayload`
   type (`api.ts:591-600`); do not invent a new type.

2. `web/src/explorer/BriefingView.tsx`: prefix `export` on the four pure
   presentational functions — `Section` (line 158), `TaskRow` (line 180),
   `SignalChatRow` (line 220), `AwaitingRow` (line 243). No other change; they
   only reference module-level `daysSince`/`timeAgo` and imported `jidUser`.

3. `web/src/explorer/FocusDigest.tsx` (new): props
   `{ circleId: number; onOpenTask: (id: number) => void; onOpenChat: (jid: string) => void }`.
   Fetch `api.circleDigest(circleId)` on mount and when `circleId` changes, with
   a race guard (cancelled/`circleId`-match check before `setState`). When the
   response has `refreshing: true`, schedule one re-fetch after ~4s (same guard),
   stopping once `refreshing: false` or the component unmounts / `circleId`
   changes. Render via the imported `Section`/`TaskRow`/`SignalChatRow`/
   `AwaitingRow` from `./BriefingView`, mirroring `BriefingModal`'s body
   (`BriefingView.tsx:100-150`) minus the modal chrome; add a "Refreshing…"
   indicator when refreshing and a "No digest yet — refreshing…" empty state
   when `digest` is null.

**Verification**: in `web/`, `npx tsc -b` and `npx vite build` both succeed.
Commit.

---

### T03 — Last-focused-circle persistence

- **id**: T03
- **wave**: 0
- **depends_on**: []
- **files_write**:
  - `web/src/explorer/Explorer.tsx`
- **files_read**: []

**Steps**

1. `web/src/explorer/Explorer.tsx`: change the `focusCircleId` `useState`
   initializer (line 67) to lazily read `localStorage.getItem('wa.focus-circle-id')`
   (`saved ? Number(saved) : null`, wrapped in try/catch), mirroring the
   `wa.last-chat-jid` idiom already in this file (lines 376-401).

2. Add a `useEffect` keyed on `focusCircleId` that writes it back
   (`setItem`/`removeItem` on `wa.focus-circle-id`, in try/catch), mirroring
   lines 376-381.

3. Add a `useEffect` keyed on `circles` that clears `focusCircleId` when
   `circles.length > 0 && focusCircleId != null && !circles.some(c => c.id === focusCircleId)`,
   so a deleted circle is not resurrected into a broken Focus Mode view.

**Verification**: in `web/`, `npx tsc -b` and `npx vite build` both succeed.
Commit.

---

## Wave 1

### T04 — Wire FocusDigest into FocusMode.tsx

- **id**: T04
- **wave**: 1
- **depends_on**: [T02]
- **files_write**:
  - `web/src/explorer/FocusMode.tsx`
- **files_read**:
  - `web/src/explorer/FocusDigest.tsx`

**Steps**

1. `web/src/explorer/FocusMode.tsx`: import `FocusDigest` from `./FocusDigest`.

2. In the left column (`FocusMode.tsx:103-122`), add a THIRD
   `min-h-0 flex-1 overflow-hidden rounded-lg border border-neutral-800` wrapper
   div ABOVE the `FocusProfile` (line 104) and `FocusTasks` (line 108) wrappers,
   containing
   `<FocusDigest circleId={circleId} onOpenTask={onOpenTask} onOpenChat={onOpenChat} />`.
   `circleId`, `onOpenTask`, and `onOpenChat` are already props on `FocusMode`
   (`FocusMode.tsx:41,50,45`) — no new prop-threading from `Explorer.tsx`.

**Verification**: in `web/`, `npx tsc -b` and `npx vite build` both succeed.
Commit.

---

## Self-validation

1. **No write-write collision in a wave.**
   - Wave 0 `files_write`:
     - T01: `internal/db/schema.go`, `internal/db/circle_digests.go`,
       `internal/api/handler_circle_digest.go`, `internal/api/handler_circles.go`,
       `agent/circle-digest.mjs`
     - T02: `web/src/api.ts`, `web/src/explorer/BriefingView.tsx`,
       `web/src/explorer/FocusDigest.tsx`
     - T03: `web/src/explorer/Explorer.tsx`
     - No path appears in more than one task. PASS.
   - Wave 1 `files_write`: T04 only (`web/src/explorer/FocusMode.tsx`). No
     collision. PASS.
2. **Wave monotonicity.** T04 (wave 1) `depends_on [T02]` (wave 0); 1 > 0. PASS.
   All other tasks are wave 0 with empty `depends_on`. PASS.
3. **No placeholders.** Every `files_write` / `files_read` entry is a concrete
   path (no globs, no TBD). The only `<...>` occurrences are inside illustrative
   SQL/JSON/TS code blocks that explicitly show response/placeholder shapes, not
   in path fields. PASS.
4. **Wave 0 independence.** T01/T02/T03 have empty `depends_on`. PASS.

DAG validation: PASS.

## Notes / warnings

- `server.go` is intentionally NOT written: the digest route rides the existing
  `/api/v2/circles/` catch-all (`server.go:162`) via a new `case "digest"` in
  `handleCircleByID`. This deviates from the manifest's *projected* backend
  files (`handler_briefings.go`, `internal/db/briefings.go`, `server.go`), which
  are estimates; the finalized design uses new files and leaves the
  global-briefing engine untouched.
- **QA recommendation (not a task):** T01 is genuinely new backend logic and
  `internal/db` has real tests in the package. A future `circle_digests_test.go`
  covering `Save/GetCircleDigest` upsert round-trip and `circleMessageCountSince`
  placeholder building would be valuable. Per the turbo convention carried from
  iters 1-2, no TDD task is added this iteration; this is logged as a
  recommendation only.
