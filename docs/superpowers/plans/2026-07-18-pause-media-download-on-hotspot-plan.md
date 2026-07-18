# Pause media download on hotspot — implementation plan (file-ownership DAG)

Spec: `docs/superpowers/specs/2026-07-18-pause-media-download-on-hotspot-design.md`

All paths are relative to the worktree root:
`/Users/devmsh/projects/whatsapp-bridge/.claude/worktrees/turbo+pause-media-download-on-hotspot-2026-07-18`

**Go is not on PATH.** Every Go command in this plan means `~/.local/go/bin/go`.
**`make build` is broken** (pnpm skips the esbuild install script). Frontend is built with
`npx vite build` inside `web/`.

Waves: 5. Max wave width: 4.
No two tasks in the same wave share any path in `files_write`.

---

## Shared contracts (pin these exactly — cross-task compile coupling)

These names are agreed up front so tasks in the same wave compile against each other.

```go
// internal/netcheck (T01)
package netcheck
type Checker struct{ /* unexported */ }
func New(cidrs []string) *Checker
func (c *Checker) SetCIDRs(cidrs []string)
func (c *Checker) IsBlocked() (blocked bool, gateway string, err error)
func DefaultCIDRs() []string // []string{"172.20.10.0/28", "192.168.43.0/24"}

// internal/db (T02)
type PendingMediaDownload struct {
    MessageID, ChatJID, MediaType, MediaMime string
    MediaSize int64
    Proto     []byte
    CreatedAt, Attempts, LastAttempt int64
    LastError string
    Failed    bool
}
func (s *Store) AddPendingMediaDownload(p PendingMediaDownload) error
func (s *Store) NextPendingMediaDownloads(limit int, now int64) []PendingMediaDownload
func (s *Store) DeletePendingMediaDownload(chatJID, messageID string) error
func (s *Store) MarkPendingMediaDownloadError(chatJID, messageID, errMsg string, now int64, maxAttempts int) error
func (s *Store) CountPendingMediaDownloads() (pending int, failed int)

// internal/wa (T03)
type MediaPolicy struct {
    Images, Video, Audio, Documents, Stickers bool
    MaxSizeMB int
    PauseOnMetered bool     `json:"pause_on_metered"`
    BlockedCIDRs   []string `json:"blocked_cidrs"`
}

// internal/wa (T05)
type NetChecker interface{ IsBlocked() (bool, string, error) }
type MediaDeferCtx struct {
    ChatJID string
    Store   *db.Store
    Net     NetChecker
    Enabled bool
}
func DownloadMedia(wa *whatsmeow.Client, msg *waE2E.Message, msgID, baseDir string,
    policy MediaPolicy, dctx *MediaDeferCtx, log waLog.Logger) *MediaInfo
func (p MediaPolicy) Allows(mediaType string, size uint64) bool // exported for T06
// wa.Client gains: Net NetChecker

// internal/wa (T05) — package-private test seam for the actual byte transfer.
// Verified against the vendored whatsmeow: func (cli *Client) Download(ctx context.Context,
// msg DownloadableMessage) ([]byte, error)
var downloadFn = func(ctx context.Context, cli *whatsmeow.Client,
    d whatsmeow.DownloadableMessage) ([]byte, error) {
    return cli.Download(ctx, d)
}

// internal/api (T06)
type MediaCatchupManager struct{ /* unexported */ }
func newMediaCatchupManager(s *Server) *MediaCatchupManager
func (m *MediaCatchupManager) Start()
// Server gains: mediaCatchup *MediaCatchupManager, netCheck *netcheck.Checker
// (the field is named netCheck, never net — `net` would shadow the stdlib package
//  in a package that already imports net/http)
// Server.netCheck is constructed in NewServer from client.MediaPolicy().BlockedCIDRs
```

TypeScript contract (T04, consumed by T08):

```ts
export interface MediaPolicy {
  images: boolean; video: boolean; audio: boolean; documents: boolean; stickers: boolean
  max_size_mb: number
  pause_on_metered: boolean
  blocked_cidrs: string[]
}
export interface NetworkStatus { gateway: string; blocked: boolean; error: string }
export interface MediaSettingsResponse extends MediaPolicy {
  network: NetworkStatus
  pending_count: number
  failed_count: number
}
```

---

# Wave 0

## T01 — netcheck package: default-gateway detection + CIDR matching

- **id:** T01
- **wave:** 0
- **depends_on:** []
- **files_write:**
  - `internal/netcheck/checker.go`
  - `internal/netcheck/gateway_darwin.go`
  - `internal/netcheck/gateway_other.go`
  - `internal/netcheck/checker_test.go`
  - `go.mod`
  - `go.sum`
- **files_read:**
  - `internal/wa/media_policy.go`
  - `go.mod`

**Description**

Create `internal/netcheck` implementing the contract above.

`checker.go` (platform-independent):
- `Checker` holds parsed `[]*net.IPNet`, a `gatewayFn func() (string, error)` (defaults to the
  platform reader, overridable in tests), a 10s result cache, and a `sync.Mutex`.
- `New(cidrs)` parses each CIDR with `net.ParseCIDR`, silently dropping malformed entries.
- `SetCIDRs` re-parses and invalidates the cache.
- `IsBlocked()`: returns the cached result if fresher than 10s; otherwise calls `gatewayFn`.
  On error return `(false, "", err)` — **fail open** — and do not cache the failure for the full
  window (cache it for 2s so a transient failure retries soon).
  With zero parsed CIDRs, always `(false, gw, nil)`.
- `DefaultCIDRs()` returns `[]string{"172.20.10.0/28", "192.168.43.0/24"}` — a literal slice, never
  a nil slice.

`gateway_darwin.go` (`//go:build darwin`): read the routing table with
`golang.org/x/net/route` — `route.FetchRIB(syscall.AF_UNSPEC, route.RIBTypeRoute, 0)` then
`route.ParseRIB`. This is a confirmed-working, unprivileged, pure-Go approach and returns a
`*route.Inet4Addr` for the normal case. Walk the messages and select a `*route.RouteMessage` only
when **all** of these hold:

- the destination is the default route — `Addrs[syscall.RTAX_DST]` is a `*route.Inet4Addr` equal to
  `0.0.0.0` (a zero/absent netmask), and
- `m.Flags & syscall.RTF_GATEWAY != 0` — **required**, not optional, and
- `Addrs[syscall.RTAX_GATEWAY]` type-asserts to `*route.Inet4Addr`.

**Skip any candidate whose gateway is a link-layer address** (`*route.LinkAddr`). A VPN (utun) can
install a second default route whose gateway is a link address rather than an IP; taking it would
report a bogus gateway and could mis-classify the network. Skip those and keep walking. Return the
first surviving `*route.Inet4Addr` as a dotted string; return an error if no default route with an
IPv4 gateway is found.

`gateway_other.go` (`//go:build !darwin`): return `("", errors.New("default gateway lookup not
supported on this platform"))`. This makes non-Darwin fail open by construction.

`checker_test.go`: table-driven, injecting a fake `gatewayFn`. Cases at minimum:
`172.20.10.1` blocked; `192.168.43.1` blocked; `192.168.1.1` not blocked; `10.0.0.1` not blocked;
malformed CIDR in the list is ignored and the rest still match; empty CIDR list ⇒ never blocked;
`gatewayFn` returning an error ⇒ `blocked=false` and the error propagated (fail open);
a second call within the cache window does not re-invoke `gatewayFn`.

Promoting `golang.org/x/net` from indirect to direct: **do not run `go mod tidy`.** Three other
wave-0 tasks are writing Go files concurrently in this same worktree, and `tidy` parses the whole
module — it can trip over a half-written file that has nothing to do with this task. Instead run
exactly:

```
~/.local/go/bin/go mod edit -require=golang.org/x/net@v0.54.0
~/.local/go/bin/go build ./internal/netcheck/
```

`go mod edit` is a pure text edit of `go.mod` and parses nothing else; the scoped `go build` then
verifies the dependency resolves. `x/net v0.54.0` is already in `go.sum` and present in the local
module cache, so this works offline and `go.sum` needs no change.

**Verification before completion**
- `~/.local/go/bin/go build ./internal/netcheck/` succeeds.
- `~/.local/go/bin/go test ./internal/netcheck/ -v` passes and shows every table case.
- `~/.local/go/bin/go vet ./internal/netcheck/` is clean.
- `git diff go.mod` shows `golang.org/x/net` moved into the direct require block, with no other
  module version changed.
- Paste the test output as evidence.

---

## T02 — `pending_media_downloads` table + store methods

- **id:** T02
- **wave:** 0
- **depends_on:** []
- **files_write:**
  - `internal/db/schema.go`
  - `internal/db/db.go`
  - `internal/db/pending_media.go`
  - `internal/db/pending_media_test.go`
- **files_read:**
  - `internal/db/media_understanding.go`
  - `internal/db/sync_state.go`

**Description**

Append to the `Schema` DDL constant in `internal/db/schema.go` (place it after the
`circle_digests` table, before the closing backtick), following the commenting style of the
neighbouring tables:

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
```

Add the index to the idempotent migration list in `internal/db/db.go` (the `for _, stmt := range`
block at lines 37-51), matching how `idx_mu_refined` is handled there:

```go
`CREATE INDEX IF NOT EXISTS idx_pending_media_dl ON pending_media_downloads(failed, created_at)`,
```

New file `internal/db/pending_media.go` implementing the contract above. Notes:

- `AddPendingMediaDownload` uses `INSERT OR REPLACE` so a re-received message does not error.
- `NextPendingMediaDownloads(limit, now)` selects `WHERE failed = 0` and respects backoff.
  **The backoff condition must be evaluated in SQL, inside the `WHERE` clause — do not select a
  wider window and filter in Go afterwards.** Post-hoc filtering starves the queue: with
  `ORDER BY created_at ASC LIMIT ?*4`, if the oldest rows all happen to sit inside their backoff
  window the query returns only ineligible rows, the Go filter drops every one of them, and newer
  rows that *are* eligible are never seen — the backlog stalls until the oldest rows age out.
  Eligibility must be decided by the query so `LIMIT` only ever counts eligible rows.

  Backoff is 1m, 4m, 16m, 64m, 256m — i.e. `60 * pow(4, attempts)` seconds, capped at the 5th step.
  SQLite has no `POW`, so express it with a shift on an even exponent: `4^n == 1 << (2*n)`. Use

  ```sql
  SELECT message_id, chat_jid, media_type, media_mime, media_size, proto,
         created_at, attempts, last_attempt, last_error, failed
    FROM pending_media_downloads
   WHERE failed = 0
     AND (attempts = 0
          OR last_attempt + (60 * (1 << (2 * MIN(attempts, 4)))) <= ?)
   ORDER BY created_at ASC
   LIMIT ?
  ```

  bound with `(now, limit)`. `MIN(attempts, 4)` caps the wait at 256 minutes, and `attempts = 0`
  short-circuits a never-tried row regardless of its zero `last_attempt`. Order oldest-first by
  `created_at ASC`.
- `MarkPendingMediaDownloadError` increments `attempts`, sets `last_error` and `last_attempt`, and
  sets `failed = 1` when the new `attempts` value reaches `maxAttempts`.
- `CountPendingMediaDownloads` returns two counts from one query
  (`SUM(failed = 0)`, `SUM(failed = 1)`).
- Return `[]PendingMediaDownload{}` (never a nil slice) from `NextPendingMediaDownloads`.

`pending_media_test.go`: open a temp-file SQLite store via `NewStore(t.TempDir()+"/t.db")` and
cover: insert then read back (proto bytes byte-identical); oldest-first ordering; a row with
`attempts=1` and a recent `last_attempt` is **not** returned, but is returned after its backoff
window; `MarkPendingMediaDownloadError` five times sets `failed=1` and the row disappears from
`NextPendingMediaDownloads`; `CountPendingMediaDownloads` reports the right split.

**Required anti-starvation test:** insert 5 old rows (small `created_at`) that are all inside their
backoff window (`attempts=1`, `last_attempt = now`), plus 1 newer row with `attempts=0`. Call
`NextPendingMediaDownloads(2, now)` and assert it returns exactly the newer eligible row. A
select-then-filter-in-Go implementation returns zero rows here and fails this test.

**Verification before completion**
- `~/.local/go/bin/go build ./internal/db/` succeeds.
- `~/.local/go/bin/go test ./internal/db/ -run PendingMedia -v` passes.
- Open a scratch DB and confirm the table exists:
  `~/.local/go/bin/go run ./cmd/... ` is not needed — instead assert it inside the test with
  `SELECT name FROM sqlite_master WHERE type='table' AND name='pending_media_downloads'`.
- Paste the test output as evidence.

---

## T03 — MediaPolicy: new fields + explicit backward-compatible load

- **id:** T03
- **wave:** 0
- **depends_on:** []
- **files_write:**
  - `internal/wa/media_policy.go`
  - `internal/wa/media_policy_test.go`
- **files_read:**
  - `internal/db/sync_state.go`
  - `internal/wa/working_hours_test.go`
  - `main.go` (read only — to see the `def` literal at line 55; **do not edit it**)

**Description**

Extend `MediaPolicy` with `PauseOnMetered bool \`json:"pause_on_metered"\`` and
`BlockedCIDRs []string \`json:"blocked_cidrs"\``.

`DefaultMediaPolicy()` returns `PauseOnMetered: true` and
`BlockedCIDRs: []string{"172.20.10.0/28", "192.168.43.0/24"}`. Do **not** import
`internal/netcheck` here — keep `wa` free of that dependency; duplicate the two literals with a
comment pointing at `netcheck.DefaultCIDRs()`.

Add two package-private normalisers with **different** jobs — this split is load-bearing:

```go
// normalizeLoadedMediaPolicy is LOAD-ONLY. hasPause reports whether the
// "pause_on_metered" key was actually present in the source. Absent ⇒ true.
func normalizeLoadedMediaPolicy(p MediaPolicy, hasPause bool) MediaPolicy

// normalizeSavedMediaPolicy is SAVE-ONLY. It NEVER touches PauseOnMetered.
func normalizeSavedMediaPolicy(p MediaPolicy) MediaPolicy
```

**Presence detection is a LOAD-ONLY concern. Use it only in `InitMediaPolicy`.** It must not leak
into `SetMediaPolicy` — see the save rules below.

Rewrite `InitMediaPolicy` so the stored blob is decoded **presence-aware**. A plain
`json.Unmarshal` into a zero-value struct yields `false` / `nil` and would silently disable the
feature on every existing install. Decode into `map[string]json.RawMessage` first (or a shadow
struct with `PauseOnMetered *bool`), then:

- `pause_on_metered` key absent ⇒ `true`
- `blocked_cidrs` key absent, `null`, or an empty array ⇒ the two default CIDRs

**Critically, the normalisation must be applied to the FINAL policy, whichever branch produced it —
not only to the JSON-decoded branch.** Today `InitMediaPolicy` (`internal/wa/media_policy.go:73-84`)
starts from `p := def` and only replaces `p` when a stored row exists. `main.go:55` builds that
`def` as a literal `wa.MediaPolicy{Images: cfg.MediaImages, …}` with **no** `PauseOnMetered` and
**no** `BlockedCIDRs`. So on a fresh install with no `media_policy` row, the effective policy today
would be `PauseOnMetered=false` and `BlockedCIDRs=nil` — the feature silently off, nothing blocked,
and `blocked_cidrs` marshalling to JSON `null` (this repo's known frontend-crash class).

**Do not fix this by editing `main.go`** — `main.go` is not in any task's `files_write`. Fix it
inside `InitMediaPolicy`:

- The `def` branch (no stored row, or a stored row that fails to decode) is treated as
  **"pause key absent"** ⇒ `hasPause = false` ⇒ `PauseOnMetered = true`.
- After choosing the branch, **always** run the load normaliser on the final `p`, so
  `BlockedCIDRs` is backfilled to the two defaults whenever it is `nil` **or** empty, and is always
  a non-nil `[]string{…}`-based slice that can never marshal to `null`.

Sketch:

```go
func (c *Client) InitMediaPolicy(def MediaPolicy) {
    p, hasPause := def, false // def comes from env flags and carries neither new field
    if v, _, _ := c.Store.GetSyncState(mediaPolicyKey); v != "" {
        var raw map[string]json.RawMessage
        var saved MediaPolicy
        if json.Unmarshal([]byte(v), &raw) == nil && json.Unmarshal([]byte(v), &saved) == nil {
            p = saved
            _, hasPause = raw["pause_on_metered"]
        }
    }
    p = normalizeLoadedMediaPolicy(p, hasPause) // applied to the FINAL policy, both branches
    c.policyMu.Lock()
    c.mediaPolicy = p
    c.policyMu.Unlock()
}
```

`SetMediaPolicy` applies **`normalizeSavedMediaPolicy` only**:

- **NEVER modify `PauseOnMetered` on save.** Persist exactly what the caller sent. Normalising on
  save would either flip an explicit `false` back to `true` (making the UI toggle impossible to turn
  off) or silently persist a `false` the caller never chose. The save path has no key-presence
  information at all — `handler_settings.go:19` decodes into a plain `wa.MediaPolicy`, which
  destroys presence — so it must not try to infer intent.
- Backfill `BlockedCIDRs` **only** when it is `nil` or empty, and always keep it non-nil, so it is
  never persisted as `null` and never persisted empty.
- Keep the existing `policyMu` locking shape, and store the normalised value in memory too (so the
  in-memory policy and the persisted blob never diverge).

`media_policy_test.go`: table-driven over stored-JSON inputs, asserting the loaded policy.
Cases: the exact legacy blob
`{"images":true,"video":true,"audio":true,"documents":true,"stickers":true,"max_size_mb":0}`
⇒ `PauseOnMetered==true` and `len(BlockedCIDRs)==2`; `{"pause_on_metered":false}` ⇒ stays `false`
(an explicit opt-out is honoured); `{"blocked_cidrs":[]}` ⇒ defaults restored;
`{"blocked_cidrs":["10.0.0.0/8"]}` ⇒ preserved as given; empty string / invalid JSON ⇒ the passed
default policy, still normalised. Test the normalisers directly where the `Client`/`Store` wiring is
inconvenient.

**Required fresh-install test (fix 2):** open a store with an **empty** `sync_state` (no
`media_policy` row) and call `InitMediaPolicy` with a `def` literal that mirrors `main.go:55` —
i.e. it sets only `Images/Video/Audio/Documents/Stickers/MaxSizeMB` and **omits both new fields**.
Assert `MediaPolicy().PauseOnMetered == true` and `len(MediaPolicy().BlockedCIDRs) == 2`, and that
`json.Marshal` of the result contains `"blocked_cidrs":[` and not `"blocked_cidrs":null`.

**Required save-semantics test (fix 3):** call `SetMediaPolicy` with `PauseOnMetered: false`, then
read `MediaPolicy()` back and re-load via a fresh `InitMediaPolicy` — `PauseOnMetered` must still be
`false` in both. The toggle must be turn-off-able.

**Verification before completion**
- `~/.local/go/bin/go build ./internal/wa/` succeeds.
- `~/.local/go/bin/go test ./internal/wa/ -run MediaPolicy -v` passes with every table case named.
- Confirm by inspection that `json.Marshal(DefaultMediaPolicy())` contains
  `"blocked_cidrs":["172.20.10.0/28","192.168.43.0/24"]` and not `null` — assert this in the test.
- Paste the test output as evidence.

---

## T04 — TypeScript types + API client for the richer media settings response

- **id:** T04
- **wave:** 0
- **depends_on:** []
- **files_write:**
  - `web/src/api.ts`
- **files_read:**
  - `internal/api/handler_settings.go`
  - `web/src/Settings.tsx`

**Description**

In `web/src/api.ts`:

- Extend `interface MediaPolicy` (currently line 49) with `pause_on_metered: boolean` and
  `blocked_cidrs: string[]`.
- Add `interface NetworkStatus` and `interface MediaSettingsResponse` exactly as in the shared
  contract above, placed directly after `MediaPolicy`.
- Change `mediaSettings()` (currently line 842) to `Promise<MediaSettingsResponse>`.
- Leave `setMediaSettings()` (line 846) taking a `MediaPolicy`, but change its return type to
  `MediaSettingsResponse` — the `PUT` handler echoes the same enriched body as `GET`.

Do not touch anything else in this 1640-line file. This task owns `web/src/api.ts` exclusively;
no other task writes it in any wave.

**Verification before completion**
- `cd web && npx tsc --noEmit` reports no new errors. (Record the before/after error count if the
  baseline is not already zero.)
- `git diff --stat web/src/api.ts` shows a small, additive diff confined to the two regions above.
- Paste the `tsc` output as evidence.

---

# Wave 1

## T05 — Defer network-skipped downloads in `DownloadMedia`

- **id:** T05
- **wave:** 1
- **depends_on:** [T01, T02, T03]
- **files_write:**
  - `internal/wa/media.go`
  - `internal/wa/client.go`
  - `internal/wa/handler_messages.go`
  - `internal/wa/handler_newsletters.go`
  - `internal/wa/media_defer_test.go`
- **files_read:**
  - `internal/netcheck/checker.go`
  - `internal/db/pending_media.go`
  - `internal/wa/media_policy.go`
  - `internal/wa/handler_history.go`

**Description**

Add `NetChecker` and `MediaDeferCtx` to `internal/wa/media.go` per the shared contract, and change
`DownloadMedia`'s signature to take `dctx *MediaDeferCtx` in place of nothing (inserted after
`policy`). A `nil` `dctx` means "never defer" and preserves today's behavior — this keeps existing
and future tests simple.

**Reorder `DownloadMedia` before inserting the gate.** Today the `ext` / `filePath` computation
(lines 66-72) and the `os.Stat(filePath)` idempotency short-circuit (lines 75-78) sit *below*
`os.MkdirAll` at line 61. Neither of them touches the network — they are pure path math plus one
`stat`. Move both of them **above** the network gate. Concretely the new order inside
`DownloadMedia` is:

1. `detectMedia` + build `metaOnly` (unchanged).
2. `!policy.allows(type)` ⇒ metadata only, no pending row (unchanged).
3. over `policy.maxBytes()` ⇒ metadata only, no pending row (unchanged).
4. `dir := filepath.Join(baseDir, desc.subdir)`, `os.MkdirAll(dir, 0755)`, compute `ext` and
   `filePath`.
5. **`os.Stat(filePath)` idempotency short-circuit** — if the file already exists, return the
   existing `MediaInfo` with its `Path` set, exactly as today.
6. network blocked ⇒ metadata only **and** a `pending_media_downloads` row.
7. otherwise download.

Why the stat must come first (fix 4): during a pause the same media is routinely re-presented —
history-sync reprocessing, a re-received message, a duplicate `MessageID`. If the gate ran first,
a file that is already on disk would be deferred anyway: `DownloadMedia` returns `metaOnly`, the
caller writes an **empty** `rec.MediaPath` over a message whose file exists, and a junk pending row
is queued for a download that has nothing to do. Statting first makes an already-downloaded file
return its real `Path` and never defer.

The gate:

```go
if dctx != nil && dctx.Enabled && dctx.Net != nil && dctx.Store != nil {
    blocked, gw, err := dctx.Net.IsBlocked()
    if err != nil {
        log.Warnf("network check failed (treating as safe): %v", err)
    } else if blocked {
        if b, mErr := proto.Marshal(msg); mErr == nil {
            dctx.Store.AddPendingMediaDownload(db.PendingMediaDownload{ /* … */ })
            log.Infof("network %s is metered — deferring %s for %s", gw, desc.mediaType, msgID)
        } else {
            log.Warnf("cannot marshal message for deferred download: %v", mErr)
        }
        writeThumbnail(baseDir, msgID, desc.thumbnail, metaOnly) // see below
        return metaOnly
    }
}
```

Use `google.golang.org/protobuf/proto` (already an indirect dep at v1.36.11 and used by whatsmeow).
Set `CreatedAt: time.Now().Unix()`.

**Write the thumbnail even when deferring (fix 10).** `desc.thumbnail` is the `JPEGThumbnail` bytes
already embedded in the protobuf (`media.go` lines ~119, ~126, ~144) — it is already in memory and
costs **zero** cellular bytes. Extract the existing thumbnail-writing block (`media.go:98-105`) into
a small helper, e.g.

```go
func writeThumbnail(baseDir, msgID string, thumb []byte, info *MediaInfo)
```

that no-ops on empty `thumb`, and call it from both the success path and the defer path. Result: a
deferred image still shows a preview in the UI during the pause instead of a blank tile. Per-type
skips and oversize skips keep their current behavior (no thumbnail) — only the network-defer path
gains this.

**Add a test seam for the byte transfer (fix 5).** The fail-open case must reach the real download
call, and there is nothing to stub there today: a nil or partially-built `*whatsmeow.Client` panics
inside whatsmeow rather than returning an error, so the most safety-critical behavior would be
untested. Add a package-private function variable to `internal/wa/media.go` and route the real call
through it:

```go
// downloadFn is the byte-transfer seam. Tests swap it; production uses cli.Download.
var downloadFn = func(ctx context.Context, cli *whatsmeow.Client,
    d whatsmeow.DownloadableMessage) ([]byte, error) {
    return cli.Download(ctx, d)
}
```

then replace `wa.Download(context.Background(), desc.download)` at `media.go:80` with
`downloadFn(context.Background(), wa, desc.download)`. The signature is verified against the
vendored module — `go.mau.fi/whatsmeow/download.go`:
`func (cli *Client) Download(ctx context.Context, msg DownloadableMessage) ([]byte, error)`.
Do not guess it; re-confirm before writing.

Also export a small policy predicate from `internal/wa/media.go` for T06's use (the existing
`allows` / `maxBytes` are unexported and live in `media_policy.go`, which this task must not write):

```go
// Allows reports whether a media item of this type and size passes the policy.
// Thin exported wrapper over the unexported allows/maxBytes.
func (p MediaPolicy) Allows(mediaType string, size uint64) bool {
    if !p.allows(mediaType) {
        return false
    }
    cap := p.maxBytes()
    return cap == 0 || size <= cap
}
```

`internal/wa/client.go`: add a `Net NetChecker` field to `Client`. It is populated by the caller
(the API server, T06) after `InitMediaPolicy`; leave it nil-safe everywhere.

Update both call sites to build the ctx:

- `internal/wa/handler_messages.go:76` →
  `DownloadMedia(c.WA, msg, info.ID, c.MediaDir, p, &MediaDeferCtx{ChatJID: rec.ChatJID, Store: c.Store, Net: c.Net, Enabled: p.PauseOnMetered}, c.Log)`
  where `p := c.MediaPolicy()` is hoisted to one call.
- `internal/wa/handler_newsletters.go:80` → the same shape with `ChatJID: chatJID`.

History sync (`internal/wa/handler_history.go`) routes through `handleMessage`, so it needs no
change — confirm this by reading the file and state it in your evidence.

`media_defer_test.go`: build a `*waE2E.Message` with an `ImageMessage` (mimetype `image/jpeg`,
`FileLength` set) and drive `DownloadMedia` against a real temp `db.Store` and a stub
`NetChecker`. Cases:

- blocked network, image allowed, under cap ⇒ exactly one row in `pending_media_downloads`; the
  stored `proto` unmarshals back to a message whose `ImageMessage` mimetype and file length match.
- blocked network but `Images: false` ⇒ **zero** rows (per-type skip stays a drop).
- blocked network but the file is over `MaxSizeMB` ⇒ **zero** rows (oversize stays a drop).
- blocked network but `Enabled: false` (`pause_on_metered` off) ⇒ zero rows.
- **Fail open (fix 5):** `NetChecker` returning an error ⇒ swap `downloadFn` for a fake that records
  it was called and returns fixed bytes. Assert **both** that `pending_media_downloads` has zero
  rows **and** that the fake `downloadFn` was actually invoked. Asserting only the row count would
  pass even if the code silently dropped the media. Restore the original `downloadFn` with
  `t.Cleanup`.
- **Already on disk (fix 4):** blocked network, image allowed, under cap, but the target file
  already exists at `<baseDir>/images/<msgID>.jpg` (create it in the test with `os.WriteFile`).
  Assert **zero** pending rows **and** a non-empty `MediaInfo.Path` pointing at that file. Also
  assert `downloadFn` was **not** called.
- **Thumbnail on defer (fix 10):** blocked network with a non-empty `JPEGThumbnail` on the
  `ImageMessage` ⇒ one pending row **and** `<baseDir>/thumbnails/<msgID>.jpg` exists with the same
  bytes, and the returned `MediaInfo.ThumbnailPath` is set while `Path` stays empty.

Never perform a real network download in the test — every case either stubs `downloadFn` or never
reaches it.

**Verification before completion**
- `~/.local/go/bin/go build ./...` succeeds.
- `~/.local/go/bin/go test ./internal/wa/ -v` passes, including the pre-existing
  `working_hours_test.go`.
- `~/.local/go/bin/go vet ./internal/wa/` is clean.
- Paste the test output and the confirmation about `handler_history.go`.

---

## T08 — Settings modal: hotspot pause checkbox, live status, backlog counts

- **id:** T08
- **wave:** 1
- **depends_on:** [T04]
- **files_write:**
  - `web/src/Settings.tsx`
- **files_read:**
  - `web/src/api.ts`
  - `web/src/explorer/Explorer.tsx`

**Description**

In `MediaSettings`, switch the state type from `MediaPolicy | null` to
`MediaSettingsResponse | null` (the response is a superset, so the existing `TYPES.map` rows and
the `max_size_mb` input keep working unchanged).

Add, between the per-type list (currently ends line 84) and the max-size row (line 86), a new
block:

- A row in the same style as the type rows, with label "Pause downloads on phone hotspot" and hint
  "Resumes automatically on Wi-Fi", and
  `<input type="checkbox" checked={!!policy.pause_on_metered} onChange={() =>
  toggle('pause_on_metered')} className="h-4 w-4 accent-emerald-500" />`.
  Widen `toggle`'s parameter type so it accepts `'pause_on_metered'` alongside the existing
  `keyof MediaPolicy` boolean keys.
- Directly under it, a status line at `text-xs text-neutral-500`:
  - when `policy.network.error` is non-empty: `Network check unavailable — downloads continue.`
  - when `policy.network.blocked`: `On {gateway} — counts as a hotspot, downloads paused.`
  - otherwise: `On {gateway} — normal network.`
  - when `gateway` is empty, render `Network: unknown`.
- A counts line, rendered only when either count is above zero:
  `{pending_count} file(s) waiting to download` and, when `failed_count > 0`, a second span at
  `text-amber-400/80`: `{failed_count} could not be recovered`, with the hint
  "WhatsApp no longer has these files."

Guard every array/optional access — `policy.blocked_cidrs` may be absent on a stale response, so
use `(policy.blocked_cidrs ?? [])` if you render it at all, and treat `policy.network` as possibly
undefined.

`save()` must send only the policy fields, not the enriched ones. Build the `PUT` body explicitly:

```ts
const { network, pending_count, failed_count, ...policyOnly } = policy
const next = await api.setMediaSettings(policyOnly)
setPolicy(next)
```

Keep the existing explicit **Save** button and its `saving` / `saved` states. No auto-save.
Do not modify `web/src/explorer/Explorer.tsx` — the modal is rendered there unchanged at line 640.

**Verification before completion**
- `cd web && npx tsc --noEmit` reports no new errors.
- `cd web && npm run dev`, open `https://whatsapp-bridge.test` (or the vite origin on :5173),
  open the media settings modal, and confirm: the new checkbox renders and toggles, the status line
  shows a gateway, and Save still returns to "Saved ✓". Do **not** restart the live bridge.
- Paste the `tsc` output and describe what you observed in the running UI.

---

# Wave 2

## T06 — Catch-up worker + server wiring

- **id:** T06
- **wave:** 2
- **depends_on:** [T01, T02, T03, T05]
- **files_write:**
  - `internal/api/media_catchup_worker.go`
  - `internal/api/server.go`
  - `internal/api/media_catchup_worker_test.go`
- **files_read:**
  - `internal/api/media_worker.go`
  - `internal/db/pending_media.go`
  - `internal/db/media_understanding.go`
  - `internal/wa/media.go`
  - `internal/netcheck/checker.go`

**Description**

**Why this is wave 2, not wave 1:** this task compiles against two symbols that **T05 creates** —
the `Net NetChecker` field on `wa.Client` (`internal/wa/client.go`) and the new 7-argument
`wa.DownloadMedia` signature. Neither exists until T05 lands, so T06 cannot build alongside it.
Running both in one wave would give every parallel implementer a broken tree.

New file `internal/api/media_catchup_worker.go`. **Mirror the idiom in
`internal/api/media_worker.go`** — do not invent a new pattern:

- `MediaCatchupManager` with `s *Server`, `mu sync.Mutex`, `running map[string]bool`
  (see `media_worker.go` lines 34-40 and 203-216).
- `newMediaCatchupManager(s *Server) *MediaCatchupManager`.
- `Start()` launches `go m.loop()`.
- `loop()`: `time.Sleep(20 * time.Second)` warm-up, then a `time.NewTicker(30 * time.Second)`
  loop (same shape as `workerLoop`, line 161). Each tick: if `s.netCheck.IsBlocked()` returns
  `blocked == true`, log at debug and **skip the whole tick**. On a detector error, log a warning
  and proceed (fail open). Otherwise call `m.drain(2)`.
- `drain(parallel int)`: repeat `s.store.NextPendingMediaDownloads(parallel*3, time.Now().Unix())`
  until empty, fanning each batch out over a channel to `parallel` goroutines — the exact structure
  of `media_worker.go` lines 177-201.
- `processOne(p db.PendingMediaDownload)`: dedupe on key `p.ChatJID + ":" + p.MessageID` through the
  `running` map with the same lock/defer-delete pattern as `media_worker.go:203-216`. Then:
  `proto.Unmarshal(p.Proto, &waE2E.Message{})`, call the shared download helper, and on success
  write the file + thumbnail, `UPDATE messages SET media_path = ?, thumbnail_path = ? WHERE id = ?
  AND chat_jid = ?`, then `DeletePendingMediaDownload`. On failure call
  `MarkPendingMediaDownloadError(..., maxAttempts=5)`.

To avoid a second copy of the download logic, call
`wa.DownloadMedia(s.client.WA, msg, p.MessageID, s.mediaDir, s.client.MediaPolicy(), nil, log)`
with a **nil** `MediaDeferCtx` — nil disables deferring, so a retry can never re-defer itself into
a loop, and the file/thumbnail writing stays in exactly one place. `wa.DownloadMedia` already
handles the thumbnail and the idempotent "already on disk" case (T05 moves that check above the
network gate, so a file already on disk comes back with its `Path` set and the row is simply
deleted).

An empty `Path` in the returned `MediaInfo` means "no file", but **not every empty `Path` is a
failure** — distinguish the two before touching `attempts`:

- **Policy skip — not a failure.** While the pause was on, the user may have turned that media type
  off or lowered `max_size_mb`. The pending row then comes back metadata-only forever, and burning
  5 retries plus 5 hours of backoff on a user's deliberate opt-out is wrong. Re-check the *current*
  policy in `processOne` before calling `DownloadMedia`: read `pol := s.client.MediaPolicy()`,
  run `wa.DownloadMedia`'s own `detectMedia` result — or simply compare against `p.MediaType` and
  `p.MediaSize` from the pending row, which already carry the type and size — and if
  either the type is now disabled or `pol.MaxSizeMB > 0 && p.MediaSize > int64(pol.MaxSizeMB)*1024*1024`,
  **`DeletePendingMediaDownload` the row**, log at info ("policy now excludes %s — dropping deferred
  download"), and return. Do **not** call `MarkPendingMediaDownloadError`.
  Because `wa.MediaPolicy.allows` / `maxBytes` are unexported, add a tiny exported helper in T05's
  `internal/wa/media.go` for this — `func (p MediaPolicy) Allows(mediaType string, size uint64) bool`
  — and call it from `processOne`. (T05 owns `internal/wa/media.go`; T06 only reads it, and T06 is
  in a later wave, so the helper exists by then.)
- **Real failure** — any other empty `Path` (network error, media gone from WhatsApp's servers):
  call `MarkPendingMediaDownloadError(..., maxAttempts=5)` as described above.

`internal/api/server.go`:
- Add `mediaCatchup *MediaCatchupManager` and `netCheck *netcheck.Checker` to the `Server` struct.
  **Name the field `netCheck`, never `net`** — `net` would shadow the stdlib `net` package inside a
  package that already imports `net/http`. Use `netCheck` consistently here and in T07.
- In `NewServer`, after the existing manager construction (lines 53-57):
  `s.netCheck = netcheck.New(client.MediaPolicy().BlockedCIDRs)`, then
  `client.Net = s.netCheck`, then `s.mediaCatchup = newMediaCatchupManager(s)`.
- In `StartProfiler()` (lines 33-37) add `s.mediaCatchup.Start()` alongside the existing three.
- Add the `internal/netcheck` import. Do not touch `registerRoutes` — no new route.

`media_catchup_worker_test.go`: exercise `drain` and `processOne` against a temp `db.Store` with a
stubbed network and a download function that is injected (add an unexported
`download func(...) *wa.MediaInfo` field on the manager, defaulting to the real one, so the test
can substitute a fake). Cases:

- network blocked ⇒ one tick performs zero `NextPendingMediaDownloads` calls (assert with a counter
  on a wrapped store or by asserting rows are untouched after a tick).
- network safe ⇒ rows drain oldest-first (assert the order the fake download saw).
- fake download failing ⇒ `attempts` becomes 1, `last_error` is set, and the row is not returned
  again immediately (backoff).
- five consecutive failures ⇒ `failed = 1` and the row is no longer returned.
- **Policy-skip is not a failure (fix 7):** a pending image row plus a current policy with
  `Images: false` ⇒ the row is **deleted**, `attempts` never increments, `failed` stays 0, and the
  fake download is never called. Same for a row whose `MediaSize` now exceeds `max_size_mb`.

Also verify the AI hand-off assumption: after a successful catch-up the message row has a
non-empty `media_path`, which is exactly what `db.PendingMedia`
(`internal/db/media_understanding.go`, the `m.media_path != ''` predicate) requires. Assert this in
the success-path test and state it in your evidence.

**Verification before completion**
- `~/.local/go/bin/go build ./...` succeeds.
- `~/.local/go/bin/go test ./internal/api/ -v` passes, including the pre-existing
  `auto_extract_test.go` and `handler_circle_export_test.go`.
- `~/.local/go/bin/go vet ./internal/api/` is clean.
- Paste the test output plus the explicit statement that `media_path` is set on success.

---

# Wave 3

## T07 — Enrich `GET/PUT /api/v2/settings/media` with network status and backlog counts

- **id:** T07
- **wave:** 3
- **depends_on:** [T06]
- **files_write:**
  - `internal/api/handler_settings.go`
  - `internal/api/handler_settings_test.go`
- **files_read:**
  - `internal/api/server.go`
  - `internal/api/media_catchup_worker.go`
  - `internal/api/response.go`
  - `internal/db/pending_media.go`
  - `internal/wa/media_policy.go`
  - `web/src/api.ts`

**Description**

Depends on T06 because `Server.netCheck` is declared there.

In `handleSettingsMedia`, replace the two bare `jsonOK(w, s.client.MediaPolicy())` calls with a
shared builder:

```go
type networkStatusBody struct {
    Gateway string `json:"gateway"`
    Blocked bool   `json:"blocked"`
    Error   string `json:"error"`
}

func (s *Server) mediaSettingsBody() map[string]any
```

`mediaSettingsBody` marshals the current `wa.MediaPolicy` into a `map[string]any` (round-trip
through `json.Marshal` / `json.Unmarshal` so the policy's own JSON tags stay the single source of
truth), then adds:

- `"network"`: gateway, blocked, and the error string from `s.netCheck.IsBlocked()` (empty string when
  there is no error — never `null`).
- `"pending_count"` and `"failed_count"` from `s.store.CountPendingMediaDownloads()`.

Guard against `s.netCheck == nil` (returns a zero `networkStatusBody` with
`Error: "network detection unavailable"`).

On `PUT`: decode into `wa.MediaPolicy` as today (`handler_settings.go:19`) — the extra `network` /
`*_count` keys in a round-tripped body are simply ignored by `encoding/json`. After a successful
`SetMediaPolicy`, call `s.netCheck.SetCIDRs(s.client.MediaPolicy().BlockedCIDRs)` so a changed CIDR
list takes effect without a restart. Respond with `mediaSettingsBody()`.

**LOAD vs SAVE presence semantics — do not blur these (fix 3).** Decoding into a plain
`wa.MediaPolicy` destroys JSON key presence: an absent `pause_on_metered` and an explicit
`"pause_on_metered": false` both arrive as `false`. That is fine, because **presence detection is a
LOAD-ONLY concern that lives in `InitMediaPolicy` (T03) and nowhere else.**

- The `PUT` handler must **not** try to recover presence, and must **not** re-apply the load-time
  "absent ⇒ true" rule. It hands the decoded struct straight to `SetMediaPolicy`.
- `SetMediaPolicy` (T03) **never modifies `PauseOnMetered`** — it persists exactly what the handler
  sent. If it normalised on save, an explicit `false` from the UI checkbox would be flipped back to
  `true` and the toggle could never be turned off.
- The only save-time backfill is `BlockedCIDRs`: filled with the two defaults when `nil` or empty,
  and always kept non-nil so it never marshals to `null`.

**Required round-trip test (fix 3):** `PUT` a body with `"pause_on_metered": false`, assert 200,
then `GET` the same endpoint and assert the response still has `"pause_on_metered": false`. Then
`PUT` again with `true` and assert it flips back. This is the regression guard for a toggle that
cannot be turned off.

The JSON shape must match the TypeScript `MediaSettingsResponse` from T04 exactly.

`handler_settings_test.go`: use `httptest` against a `Server` built with a temp store and confirm:
`GET` returns all six existing policy keys plus `pause_on_metered`, `blocked_cidrs` (a JSON
**array**, never `null`), `network`, `pending_count`, `failed_count`;
a `PUT` of a body that includes the enriched keys round-trips without error and persists the policy
change; `blocked_cidrs` in the response is never `null` even when the stored policy predates the
field.

**Verification before completion**
- `~/.local/go/bin/go build ./...` succeeds.
- `~/.local/go/bin/go test ./internal/api/ -v` passes (both the new test and T06's).
- Assert in the test — and show it — that the raw response body contains
  `"blocked_cidrs":[` and not `"blocked_cidrs":null`.
- Paste the test output.

---

# Wave 4

## T09 — Rebuild `web/dist`, run the full suite, restart the LaunchAgent, verify end to end

- **id:** T09
- **wave:** 4
- **depends_on:** [T05, T06, T07, T08]
- **files_write:**
  - `web/dist/index.html`
  - `web/dist/assets`
- **files_read:**
  - `web/src/Settings.tsx`
  - `web/src/api.ts`
  - `internal/api/handler_settings.go`
  - `Makefile`

**Description**

`web/dist/assets` is the built-asset directory owned wholly by this task (vite emits hashed
filenames into it). No other task writes anything under `web/dist`.

Steps:

1. `cd web && npx vite build`. **Do not run `make build`** — it fails because pnpm ignores the
   esbuild install script. Confirm `web/dist/index.html` and fresh files under `web/dist/assets`
   were regenerated.
2. `~/.local/go/bin/gofmt -l ./internal ./cmd` returns nothing (format anything it lists).
3. `~/.local/go/bin/go vet ./...` is clean.
4. `~/.local/go/bin/go test ./...` is green — the three pre-existing test files
   (`internal/api/auto_extract_test.go`, `internal/api/handler_circle_export_test.go`,
   `internal/wa/working_hours_test.go`) plus every test added by T01, T02, T03, T05, T06, T07.
5. `~/.local/go/bin/go build -o bin/whatsapp-bridge .` succeeds (the binary embeds `web/dist`).
6. Restart the LaunchAgent — never run the binary by hand:
   `launchctl kickstart -k gui/501/com.devmsh.whatsapp-bridge`.
7. End-to-end check against the running bridge:
   - `curl -s localhost:8080/api/v2/settings/media | python3 -m json.tool` — confirm
     `pause_on_metered` is `true`, `blocked_cidrs` is a two-element array (not `null`), `network`
     carries a real gateway, and both counts are present. Use the port from the bridge's config if
     it is not 8080.
   - Open the app at `https://whatsapp-bridge.test`, open the media settings modal, confirm the
     new checkbox, status line, and counts render, and that Save succeeds.
   - Confirm the reported gateway matches the machine's real default route
     (`netstat -rn | grep default`), on both the normal Wi-Fi and — if the user can tether — the
     iPhone hotspot, where `blocked` must flip to `true`.
   - **AI hand-off check (fix 12):** after a catch-up download succeeds, confirm the corresponding
     `messages` row actually has a non-empty `media_path` — this is the exact predicate
     `db.PendingMedia` (`internal/db/media_understanding.go`, `m.media_path != ''`) uses to feed the
     media-understanding workers. If `media_path` stays empty, the recovered file is invisible to
     the AI pipeline even though the bytes are on disk. Verify with:

     ```
     sqlite3 store/messages.db "SELECT id, chat_jid, media_path FROM messages
                          WHERE media_path != '' ORDER BY rowid DESC LIMIT 5;"
     ```

     and confirm at least one row is a message that was deferred during the pause. If no live
     hotspot session is available, insert a synthetic pending row, run one catch-up tick, and check
     the same query.
   - **Fresh-install check (fix 2):** with `media_policy` absent from `sync_state` (use a scratch DB
     copy, not the live one), confirm the API reports `pause_on_metered: true` and a two-element
     `blocked_cidrs` — i.e. the feature is ON by default and nothing marshals to `null`.
8. `git status` shows `web/dist` changes staged for commit alongside the source changes.

**Verification before completion**
- Paste the full `go test ./...` output.
- Paste the `curl` response for `/api/v2/settings/media`.
- Paste the `netstat -rn | grep default` output next to the gateway the API reported, and state
  whether they match.
- Paste the `messages` / `media_path` query output showing a recovered file is visible to
  `db.PendingMedia`.
- State explicitly whether the hotspot case was tested live or only via the unit tests.

---

## DAG self-validation

**Same-wave `files_write` collisions:** none.

| Wave | Tasks | Written paths |
|---|---|---|
| 0 | T01, T02, T03, T04 | T01: `internal/netcheck/checker.go`, `internal/netcheck/gateway_darwin.go`, `internal/netcheck/gateway_other.go`, `internal/netcheck/checker_test.go`, `go.mod`, `go.sum` · T02: `internal/db/schema.go`, `internal/db/db.go`, `internal/db/pending_media.go`, `internal/db/pending_media_test.go` · T03: `internal/wa/media_policy.go`, `internal/wa/media_policy_test.go` · T04: `web/src/api.ts` — **disjoint** |
| 1 | T05, T08 | T05: `internal/wa/media.go`, `internal/wa/client.go`, `internal/wa/handler_messages.go`, `internal/wa/handler_newsletters.go`, `internal/wa/media_defer_test.go` · T08: `web/src/Settings.tsx` — **disjoint** (Go vs TypeScript, no overlap) |
| 2 | T06 | `internal/api/media_catchup_worker.go`, `internal/api/server.go`, `internal/api/media_catchup_worker_test.go` — single task |
| 3 | T07 | `internal/api/handler_settings.go`, `internal/api/handler_settings_test.go` — single task |
| 4 | T09 | `web/dist/index.html`, `web/dist/assets` — single task |

`internal/wa/media_policy.go` is written only by T03 (wave 0); T05 (wave 1) only reads it.
`web/src/api.ts` is written only by T04 (wave 0); T08 (wave 1) and T07 (wave 3) only read it.
`internal/api/server.go` is written only by T06 (wave 2); T07 only reads it.
`internal/wa/media.go` is written only by T05 (wave 1); T06 (wave 2) only reads it.
`main.go` is written by **no** task — the fresh-install default is fixed inside `InitMediaPolicy`
(T03), not by editing `main.go`.

**Wave monotonicity** — every edge `A -> B` has `B.wave > A.wave`:

- T05 ← T01(0), T02(0), T03(0); T05.wave = 1 ✓
- T08 ← T04(0); T08.wave = 1 ✓
- T06 ← T01(0), T02(0), T03(0), T05(1); T06.wave = 2 ✓ — T06 needs T05's `Net NetChecker` field,
  the 7-arg `DownloadMedia`, and the exported `MediaPolicy.Allows`, so it cannot share T05's wave.
- T07 ← T06(2); T07.wave = 3 ✓
- T09 ← T05(1), T06(2), T07(3), T08(1); T09.wave = 4 ✓

**Wave 0 tasks have empty `depends_on`:** T01, T02, T03, T04 ✓

**Wave count:** 5 (waves 0-4). **Max wave width:** 4 (wave 0: T01, T02, T03, T04) ✓

**Placeholder scan:** no `TBD`, `TODO`, `FIXME`, `<...>`, or trailing `etc.` remain in any task's
paths or descriptions.
