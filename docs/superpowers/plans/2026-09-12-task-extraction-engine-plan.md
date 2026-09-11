# Task extraction engine — build plan

Read the design first:
`docs/superpowers/specs/2026-09-12-task-extraction-engine-design.md`.
Every rule below comes from there. If this plan and the design disagree, the
design wins; fix the plan.

Language: Go for everything in the pipeline. Node only inside the Claude
adapter's sidecar. Match the surrounding code's style (see `internal/db/*.go`
and `internal/api/*.go`). Comments explain *why*, in plain English.

Do the steps in order. Each step ends with tests passing (`go test
./internal/...`) and a build (`go build ./...`). Do not restart the live bridge
until step 11; it runs as a LaunchAgent and the user relies on it.

## Ground rules

- The model never touches the database. Only `internal/extract` does.
- Never commit real chat data. Golden-set files go under `store/eval/`
  (gitignored). Test fixtures under `internal/extract/testdata/` must be
  invented text, in Arabic and English, shaped like the real thing.
- Prefer phone-form JIDs (`…@s.whatsapp.net`). When a `@lid` is met, resolve
  it through `contacts.lid` and the whatsmeow LID store
  (`client.ResolvePhoneForLID`) before storing.
- Hidden, archived and deleted chats are never read. Use
  `store.AIExcludedJIDs()` and `FlattenCircleChats`.

## Step 1 — Package skeleton and the port

Create `internal/extract/` with:

```
port.go        the Extractor interface and the plain-data types
pipeline.go    Run(ctx, RunSpec) — orchestrates the stages
select.go      chats + watermark
fetch.go       messages → Lines (with media text, mentions, replies)
roster.go      participants with name, kunya, how_we_met
chunk.go       Lines → Chunks
prefilter.go   actionable-signal score
verify.go      evidence check
resolve.go     owner + due date
dedupe.go      existing-task matching
link.go        forwarded copies, completion replies
persist.go     CreateTask / LinkTaskMessage / circles / extraction_calls
adapters/
  ollama.go
  claude.go
testdata/      invented fixtures
```

`port.go`:

```go
package extract

type RosterPerson struct {
    JID, Name, Kunya, HowWeMet string
    IsAdmin bool
}

type Line struct {
    MessageID string
    TS        int64
    Sender    string   // display name
    SenderJID string
    Text      string   // final text incl. [transcript]/[image]
    Mentions  []string // JIDs, resolved to phone form when possible
    ReplyTo   string   // message id or ""
    Forwarded bool
    Context   bool     // true = carried in for context, not extractable
}

type Chunk struct {
    ChatJID string
    Index   int
    Lines   []Line
    Rendered string   // what the model sees, built by Render(chunk)
}

type ProposedTask struct {
    Title        string  `json:"title"`
    OwnerText    string  `json:"owner_text"`
    EvidenceID   string  `json:"evidence_id"`
    Evidence     string  `json:"evidence"`
    DueText      string  `json:"due_text"`
    PriorityHint string  `json:"priority_hint"`
    Confidence   float64 `json:"confidence"`
}

type OpenTask struct {
    ID    int64
    Title string
    OwnerName string
}

type ProposedCompletion struct {
    TaskID     int64   `json:"task_id"`
    EvidenceID string  `json:"evidence_id"`
    Evidence   string  `json:"evidence"`
    Confidence float64 `json:"confidence"`
}

type ExtractInput struct {
    Chunk   Chunk
    Roster  []RosterPerson
    OwnName string
    ChatName string
    IsGroup bool
}
type ExtractOutput struct{ Tasks []ProposedTask }

type CompletionInput struct {
    Chunk Chunk
    Open  []OpenTask
}
type CompletionOutput struct{ Completions []ProposedCompletion }

// Extractor is the one volatile layer: the model. Plain data in, plain data
// out. Nothing here may open the database or call the bridge API.
type Extractor interface {
    Name() string
    Extract(ctx context.Context, in ExtractInput) (ExtractOutput, error)
    CheckCompletion(ctx context.Context, in CompletionInput) (CompletionOutput, error)
}
```

Done when: package compiles with the types and an empty `Run`.

## Step 2 — Fetch, enrich, roster

`fetch.go`: `func FetchLines(store *db.Store, chatJID string, since, until int64) ([]Line, error)`.

- Query `messages` where `chat_jid = ?`, `timestamp > since AND <= until`,
  `COALESCE(is_deleted,0) = 0`, ordered by timestamp.
- Media text: copy the logic of `mergeAIText` in `internal/mcp/media_enrich.go`
  (read `media_understanding` rows with status `ok` for the message ids in
  the window). Do not import the `mcp` package; move `mergeAIText` and
  `loadMediaUnderstanding` into `internal/db` and call them from both places.
- Mentions: parse the JSON list; resolve each through
  `ResolvePhoneForLID`-equivalent in the store (`contacts.lid` → `jid`).
- Reply: keep `reply_to_id`.
- Sender display name: same fallback order used everywhere:
  `contacts.name → push_name → business_name → sender_name → digits`.

`roster.go`: `func Roster(store *db.Store, chatJID string) ([]RosterPerson, string /*ownName*/, error)`.
For groups read `group_participants` joined to `contacts`, including `kunya`
and `how_we_met`. For DMs return the contact and the user.

`Render(chunk)` produces exactly this per line:

```
[#3EB0…] [Mon 27 Jul 11:27] Fady Mondy: ↳ replying to #A1B2… (شباب رجاء استخدام…)
[forwarded] text… @Alaa Saqer …
```

with `[context]` prepended on context lines. Mentions are shown as `@Name`.

Tests (`fetch_test.go`, `roster_test.go`) using `newTestStore`-style temp DBs
as the db tests do: transcript inlined; mention rendered as a name; reply
prefix present; hidden chat returns nothing.

## Step 3 — Chunking and prefilter

`chunk.go`: `func Chunk(lines []Line, opt ChunkOptions) []Chunk`.

Defaults: `MaxMessages 180`, `MaxChars 14000`, `Overlap 10`. Rules from design
§5.4: prefer day boundaries; carry reply targets forward as context; repeat
the last `Overlap` lines as context at the top of the next chunk.

`prefilter.go`: `func Signal(line Line) int` and `func Skippable(c Chunk) bool`.
`Skippable` is true only when every non-context line scores 0. Signal words
(both scripts): request verbs, mention present, `?`/`؟`, deadline words, reply
to a known task origin (pass the set in). Log skips.

Tests: a thread never splits; overlap lines are marked context; a chunk with
one request is not skippable; a chunk of greetings is.

## Step 4 — Ollama adapter

`adapters/ollama.go`: `func NewOllama(baseURL, model string) extract.Extractor`.

- `POST {base}/api/chat`, `stream:false`, `format: <JSON schema>`,
  `options: {temperature: 0, num_ctx: 16384}`, `keep_alive: "30m"`.
- Schema for `Extract`: object `{tasks: [ProposedTask]}` with all fields
  required. Schema for `CheckCompletion`: `{completions: [ProposedCompletion]}`.
- Timeout 10 minutes per call. On invalid JSON: retry once, then return an
  error (the pipeline logs and skips that chunk).
- Prompts live in `adapters/prompts.go` as Go constants so both adapters share
  them. Start from the prompt that worked today (design §5.6), including the
  roster block and the five hard rules.

Test with `httptest.Server` returning canned JSON: request body carries the
schema; invalid JSON is retried once.

## Step 5 — Claude chunk adapter

`agent/extract-chunk.mjs`: a **non-agentic** sidecar. Reads one JSON object
on stdin `{system, user, schema}`, calls the Claude Agent SDK `query` with
`allowedTools: []` and `maxTurns: 1`, asks for JSON only, prints the JSON on
stdout. No MCP server. Reuse the auth notes at the top of `agent/extract.mjs`.

`adapters/claude.go`: shells out via the same spawner as
`runAgentInput` in `internal/api/handler_extract.go` (move the spawner into a
small package the adapter can import without importing `api`).

Done when: `go test ./internal/extract/adapters/...` passes with the sidecar
mocked, and a manual run against one real chunk returns the same JSON shape
as Ollama.

## Step 6 — Verify, resolve, dedupe, link

`verify.go`: `func Verify(c Chunk, p ProposedTask) (ok bool, reason string)`.
Rules from design §5.8. Normalise whitespace and Arabic diacritics before
comparing. For lines that carry `[transcript]`, allow edit distance ≤ 10% of
the quote length.

`resolve.go`:
- `func ResolveOwner(c Chunk, p ProposedTask, roster []RosterPerson, evidence Line) (jid string, how string)` — the six-step order in design §5.9. Return `how` for logging (`mention`, `reply`, `name`, `kunya`, `self`, `none`).
- `func ResolveDue(dueText string, at int64, loc *time.Location) int64` — weekday names in Arabic and English, بكرة/غدا/tomorrow, اليوم/today, end of week/month, `in N days`, explicit `dd/mm`. Unknown → 0. Table-driven test with ≥ 20 cases, including the Sunday case that failed today: given "الأحد" on a Monday 7 Sep 2026, the answer is Sunday 13 Sep, not Monday 14.

`dedupe.go`: `func FindExisting(store *db.Store, circleIDs []int64, p ProposedTask, evidenceChat, evidenceID string) (existingID int64, action string)` with actions `skip` (same origin), `attach` (title overlap ≥ 0.6, same circle, open, ≤ 14 days), `new`. Rejected tasks' origin messages return `skip`.

`link.go`:
- `func ForwardedOrigin(store *db.Store, l Line, chatJID string) (originChat, originID string)` — exact text match after normalisation, or same `media_path`, other live chats, ±7 days, earliest wins.
- `func DoneReplies(store *db.Store, lines []Line, openTasks []OpenTask) []ProposedCompletion` — a reply to a task's origin message whose text contains a done marker (✅ ✔ تم خلصت انجزت done finished) → confidence 1.0.

Tests for every function. Verify tests must include a hallucinated quote being
rejected.

## Step 7 — Persist and record

`persist.go`:
- `func Persist(store *db.Store, run RunMeta, chunk Chunk, verified []VerifiedTask) error` — `CreateTask` with `ReviewStatus = pending_review`, `Evidence`, `Engine`, `Confidence`; `LinkTaskMessage(origin)`; forwarded `related` link; circles = chat's circles ∪ assignee's circles (mirror `SyncMeetingCircles` logic; add `TaskCirclesFromContext` in `internal/db/tasks.go`).
- `func RecordCall(store, ExtractionCall)` and `func RecordRejection(store, ExtractionRejection)`.

Schema additions in `internal/db/schema.go` **and** `ALTER TABLE` migrations in
`internal/db/db.go` (both, as the repo does for every column): `tasks.evidence`,
`tasks.engine`, `tasks.confidence`; tables `extraction_calls`,
`extraction_rejections` (design §8).

## Step 8 — The pipeline

`pipeline.go`: `func Run(ctx, deps Deps, spec RunSpec, progress func(string)) (Result, error)`.

```go
type Deps struct { Store *db.Store; Extractor Extractor; Loc *time.Location }
type RunSpec struct { ChatJID string; CircleID int64; Since, Until int64; Engine, Model string }
type Result struct { Chunks, Proposed, Verified, Rejected, Completions int; Skipped int }
```

Order per chunk: prefilter → `DoneReplies` (code completions) → `Extract` →
verify → resolve → dedupe → forwarded link → persist → `CheckCompletion`
(only if there are open tasks) → verify completions → link → advance
watermark to the chunk's last message timestamp
(`chat_extraction_state`, same upsert `handleExtractionMark` does today).

A circle run is `FlattenCircleChats(circleID)` then `Run` per chat, sharing
one open-task list loaded once.

`progress(msg)` feeds the existing run event stream so the UI shows the same
kind of lines the sidecar wrote.

Integration test with a fake `Extractor` that returns a fixed proposal:
one task persisted with evidence, owner resolved from a mention, watermark
advanced; a second run over the same window creates nothing.

## Step 9 — Wire the API and the scheduler

- `internal/api/handler_extract.go`: `handleTaskExtract` reads
  `sync_state.extract_engine` (default `ollama`) and `extract_model` (default
  `qwen2.5:14b`), builds the adapter, and runs `extract.Run` inside the
  existing `s.runs.Start` / run lifecycle instead of spawning `extract.mjs`.
  Accept optional `engine` / `model` in the request body.
- `handleCircleExtract` (in `handler_circles.go`) does the same for circles.
- `internal/api/auto_extract.go`: call the new pipeline; keep its cooldown
  and circle-picking logic unchanged.
- New: `GET /api/v2/extractions/stats` from `extraction_calls`.
- Settings endpoint: add `extract_engine`, `extract_model` to the existing
  settings handler (`handler_settings.go`), and `GET /api/v2/extractions/models`
  that proxies Ollama `GET /api/tags` (names only).
- Do **not** delete `extract.mjs` / `extract-circle.mjs` yet.

## Step 10 — Evaluation command

`cmd/extract-eval/main.go`: reads `store/eval/*.json` — each file is one
chunk: `{chat_jid, since, until, expected: [{title, owner_jid, evidence_id}]}`
— runs the pipeline in **dry mode** (a `Deps.DryRun` flag that skips persist
and watermark) for each configured engine/model, and prints:

```
engine          model            chunks  precision  recall  owner_ok  bad_quotes  p50_ms
ollama          qwen2.5:14b          12       0.87    0.79      0.92           0    24100
ollama          qwen3-coder:30b      12       0.95    0.61      0.90           0    12800
claude          chunk                12       0.93    0.88      0.94           0    18000
```

Matching rule: a proposal matches an expected task if `evidence_id` is equal,
or titles overlap ≥ 0.6. Build the first golden set from 10 chunks of real
chats chosen by the user; the user labels them (the tool can print a chunk
with numbered messages to make labelling quick).

Done when the bar in design §7 is met for the chosen default model. Record
the numbers in the design file under "Evidence".

## Step 11 — UI, switch-over, clean-up

- Settings → "Task extraction": engine radio, model dropdown, "Run
  evaluation" showing the table above, and the last 7 days from
  `/extractions/stats`.
- Task detail (`web/src/explorer/TaskView.tsx`): show `evidence` under the
  title, with the engine and confidence in small text, and a link that opens
  the origin message (`onOpenChat(jid)` + `pendingJumpId`).
- Rebuild `web/dist` (`cd web && npx vite build`), `go build -o
  whatsapp-bridge-v2 .`, then `launchctl kickstart -k
  gui/501/com.devmsh.whatsapp-bridge`. Rebuild `whatsapp-mcp` only if MCP
  tools changed (they should not in this plan).
- Flip `extract_engine` to `ollama` after the bar passes.
- Delete `agent/extract.mjs` and `agent/extract-circle.mjs` and the MCP
  tools only they used (`wa_chat_since`, `wa_mark_extracted` can stay; they
  are harmless) in a **separate** commit, so the switch is easy to revert.

## Commits

One commit per step, messages in the repo's style: a one-line subject that
says what changed, a body that says why, ending with the attribution line the
session provides. Push to `main` (the user does not use branches).

## Out of scope for this plan

Recorded in `docs/backlog.md`: meetings on the same engine; digests,
briefings, drafts and profiles on the local model; embedding-based dedupe;
Claude escalation on low-confidence chunks.
