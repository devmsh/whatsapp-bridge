# Task extraction engine — design and decision

Date: 2026-09-12
Status: decided, not built
Builder: another model. Read this first, then the plan file next to it.

## 1. The question, and the short answer

Question: should task extraction be **code-based** or **LLM-based**?

Answer: **neither on its own. A split.** Code does everything that has a right
answer. The model does the one thing code cannot do: read a piece of chat and
say "this is work someone must do".

The rule for the split is simple:

> If a step can be checked, code does it. If a step needs judgement, the model
> does it, and code checks the model's answer afterwards.

The model runs **locally** (Ollama) by default. Claude stays available behind
the same interface as a fallback, but it is not on the main path.

## 2. What "task extraction" must cover

This is everything the current system supports. The new engine must keep all
of it. File references are for the builder.

| Capability | Where it lives today |
|---|---|
| Read a chat from a watermark, in pages | `wa_scan` in `internal/mcp/tools_read.go`; watermark in `chat_extraction_state` |
| Voice notes and images as text | `internal/mcp/media_enrich.go` inlines `[transcript] …` and `[image] …` from `media_understanding` |
| Who is in a group, with names | `wa_group_info`; `group_participants` + `contacts` |
| Mentions (`@person`) | `messages.mentions` — a JSON list of JIDs, e.g. `["200291672703053@lid"]` |
| Replies (thread structure) | `messages.reply_to_id`, `reply_to_sender`, `reply_to_content` |
| Forwarded messages | `messages.is_forwarded`, `forward_score` — 877 since 1 July |
| Create a task | `CreateTask` in `internal/db/tasks.go`; fields: title, description, status, priority, assignee_jid, creator_jid, due_at, origin_chat_jid, origin_message_id, review_status, parent_id |
| Link a task to messages in **any** chat | `LinkTaskMessage(taskID, chatJID, messageID, role)`; roles: origin, completion, comment, attachment, related |
| Completion found in another chat | role `completion` marks the task done (see `handleLinkTaskMessage` in `internal/mcp/tools_tasks.go`) |
| Circle-wide extraction | `agent/extract-circle.mjs` |
| Tasks belong to circles | `task_circles`; `AddTaskCircle` |
| Review queue | `review_status = pending_review`; `SetTaskReview` |
| Background schedule | `internal/api/auto_extract.go` (10-minute ticker, per-circle cooldown) |
| Progress in the UI | `s.runs.Start(...)`, SSE run events, `executeExtraction` in `handler_extract.go` |
| Hidden / archived / deleted chats are never read | `AIExcludedJIDs` in `internal/db/ai_scope.go`; `FlattenCircleChats` filters |
| Your own notes on a person | `contacts.kunya`, `contacts.how_we_met` (added 2026-09-11) |

Two things are new since the old extractor was written and must be respected:

- **Meetings are not tasks.** "Let's meet Thursday" goes to the meetings
  module, not to tasks. A meeting's *next steps* become tasks through
  `meeting_items.task_id`.
- **Intro chats** are a separate axis (labels), not tasks.

## 3. Why the two pure options fail

### Pure code (rules, regex)

Detecting work in dialect Arabic mixed with English needs understanding.
"جهز عقد" is a task. "جهزت العقد" is a status update. "بدنا نقعد مع محمود"
might be a meeting. A rule set that gets this right does not exist, and every
rule we add breaks on the next chat. Code cannot do the **detection**.

But code is excellent at everything *around* detection. Today's session
proved it twice: the intro detector (`internal/db/intros.go`) and the meeting
link-code matcher (`MeetingLinkCode`) are both pure code and both work.

### Pure LLM agent (what we have now)

`agent/extract.mjs` is a **120-turn agentic loop**. The model paginates,
resolves names, decides duplicates, creates tasks, links messages. It works on
Claude. It will not work on a local model, and even on Claude it has problems:

- It costs subscription quota on every run. The meetings backfill has been
  running for hours today.
- Dates are guessed by the model. Today it put a Sunday meeting on Monday
  (`meetings` id 4). Code computing dates from the message timestamp would not
  have.
- Owners are guessed. In today's local tests every model either invented an
  owner or returned raw `@lid` strings.
- Duplicates are guessed. One model returned the same task four ways.
- Nothing is verifiable. If the model says "task X from message Y", nothing
  checks that message Y says that.

Local models (14B–30B) are far weaker at long tool loops than at reading a
block of text and answering. So the loop is the wrong shape for them.

## 4. The split

| Step | Who | Why |
|---|---|---|
| Choose chats and the start point (watermark, AI scope) | code | has a right answer |
| Fetch messages, inline transcripts and image text | code | copy of existing logic |
| Resolve mention JIDs and reply targets to names | code | lookup |
| Build the roster: name, kunya, how-we-met, role in group | code | lookup |
| Cut the chat into chunks that keep threads together | code | deterministic |
| Skip chunks with zero actionable signal | code | cheap; saves model calls; must never drop a chunk that has any signal |
| **Say which messages contain work, and what the work is** | **model** | judgement |
| **Say whether a message reports finishing a known open task** | **model** | judgement |
| Check the model's evidence quote appears verbatim in the chunk | code | kills hallucination |
| Resolve the owner from the evidence message's mentions / reply / roster / kunya | code | lookup, not guessing |
| Resolve a due date from the model's *phrase* + the message time | code | avoids the Sunday/Monday bug |
| Deduplicate against existing tasks | code | keys: evidence message id, then title similarity |
| Link forwarded copies of the same message across chats | code | content match |
| Detect "done" replies to a task's origin message | code | reply + done-marker is unambiguous |
| Create task, link messages, attach circles, set pending_review | code | persistence |
| Advance the watermark only after success | code | idempotence |

The model never talks to the database. It receives text and returns JSON. Code
owns every side effect.

## 5. The pipeline

One run = one chat (or one circle, which is a loop over its chats).

```
select ──▶ fetch ──▶ enrich ──▶ chunk ──▶ prefilter ──▶ MODEL: extract
                                                          │
                                   ┌──────────────────────┘
                                   ▼
                    verify ──▶ resolve ──▶ dedupe ──▶ link ──▶ persist ──▶ watermark
                                   ▲
                    MODEL: completion check (open tasks × chunk)
```

### 5.1 Select

Input: chat JID (or circle id → `FlattenCircleChats`, which already removes
hidden/archived/deleted). Start = `chat_extraction_state.last_msg_ts` for that
chat, or the 1 July cut-off on first run. Stop = now.

### 5.2 Fetch and enrich

Same rows `wa_scan` returns. For each message build a line:

```
[#<message_id>] [<local time>] <sender name>: <text>
```

- Voice/image: append `[transcript] …` / `[image] …` exactly as
  `mergeAIText` does today.
- Reply: prefix `↳ replying to #<reply_to_id> (<first 60 chars>)`.
- Mentions: replace each `@<jid>` in the text with `@<name>` and keep the JID
  in a side table for the resolver.
- Forwarded: prefix `[forwarded]`.

The `#<message_id>` is important. The model must quote it back. That is what
makes verification exact instead of fuzzy.

### 5.3 Roster

For a group: every participant, with `name`, `kunya`, `how_we_met` (short),
and `is_admin`. For a DM: the two people. Also the user's own name and kunya.
This is the first real payoff of the kunya field: "أبو يمان" in a message
resolves to a person.

### 5.4 Chunk

- Target ~180 messages or ~14,000 characters, whichever first.
- Never split a reply thread across chunks: if message N replies to message M
  and M is in the previous chunk, carry M forward as context (marked
  `[context]`, not extractable).
- Overlap: the last 10 messages of a chunk are repeated as `[context]` at the
  top of the next chunk.
- Boundaries prefer day changes.

### 5.5 Prefilter (optional, recall-safe)

Score each message for actionable signal: request verbs (ابعت، جهز، راجع،
لازم، بدي منك، please, can you, need to), a mention, a question mark, a
deadline word (بكرة، الخميس، by Monday), or a reply to a known task origin.
Skip a chunk **only** if every message scores zero. Log skipped chunks. This
is an optimisation, not a filter; if in doubt, send it.

### 5.6 Model: extract

Structured output (JSON schema, which Ollama 0.30.10 supports). The model
returns, per task:

```json
{ "title": "...", "owner_text": "as written in the chat, or unknown",
  "evidence_id": "#message_id", "evidence": "verbatim quote",
  "due_text": "the phrase, or empty", "priority_hint": "low|normal|high",
  "confidence": 0.0 }
```

Prompt rules that today's tests proved necessary:

- Past tense is a status update, not a task.
- A meeting being arranged is not a task.
- Owner must be a name from the roster or `unknown`. Never invent.
- Evidence must be a verbatim quote and must name the message id.
- Empty list is a good answer.

### 5.7 Model: completion check

Input: the chunk + the open tasks for this circle (and for people in this chat
across circles). Output: `[{task_id, evidence_id, evidence, confidence}]`.
Code links role `completion` only when verification passes and confidence ≥
0.8. Before asking the model at all, code runs the cheap rule: a **reply to a
task's origin message** containing a done-marker (✅, تم, done, خلصت, انجزت)
is a completion at confidence 1.0.

### 5.8 Verify

Reject a proposed task if any of these fail:

1. `evidence_id` is a message in this chunk (not a `[context]` line).
2. `evidence` appears in that message's text after whitespace normalisation
   (tolerate up to 10% edit distance for transcripts, which the model may
   lightly correct).
3. `title` is non-empty and under 140 characters.

Rejections are logged with the reason. They are the eval signal.

### 5.9 Resolve owner (in order, stop at first hit)

1. A mention JID in the evidence message → that person.
2. The evidence message is a reply → the replied-to sender, if the text is a
   request ("ابعتلي", "can you").
3. `owner_text` matches a roster name (exact, then token overlap ≥ 1 word of
   3+ letters).
4. `owner_text` matches a roster **kunya**.
5. The evidence message says "انا/I'll" → the sender.
6. Otherwise `assignee_jid = ""` and the task still goes to review.

Always resolve to the **phone JID** form when both forms exist (the LID/phone
split bit us five times this session).

### 5.10 Resolve due date

Code, not model. Take `due_text` and the evidence message timestamp, in
`Asia/Riyadh`. Support: weekday names (Arabic and English), بكرة/غدا/tomorrow,
اليوم/today, "end of week/month", explicit dates, "in N days". Unknown phrase →
`due_at = 0`, keep the phrase in `description`.

### 5.11 Dedupe

In order:

1. Same `(origin_chat_jid, origin_message_id)` as an existing task → skip
   (this is what makes re-runs safe).
2. Same circle, open task, title token overlap ≥ 0.6 within 14 days → attach
   as `comment` link instead of a new task.
3. Otherwise new.

Phase 2 adds embedding similarity with the local `bge-m3` model (multilingual,
Arabic works) for near-duplicates with different wording.

### 5.12 Link forwarded copies

If the evidence message `is_forwarded`, search other live chats for the same
text (exact after normalisation, or same media file) within ±7 days. The
earliest copy becomes the origin; later copies get a `related` link. Code
only.

### 5.13 Persist

`CreateTask` with `review_status = pending_review`, then `LinkTaskMessage`
(origin, plus any related/completion links), then circles: the chat's circles
plus the assignee's circles — the same inheritance rule meetings use
(`SyncMeetingCircles`). Write one row per model call to a new
`extraction_calls` table (see §8) for cost and quality tracking.

### 5.14 Watermark

Advance `chat_extraction_state.last_msg_ts` to the last message of the last
**successful** chunk. A failed chunk stops the run and leaves the watermark
where it was.

## 6. Model tier and the port

The LLM is the volatile layer, so it sits behind a port the app owns:

```go
// internal/extract/port.go — plain data in, plain data out. No DB, no HTTP.
type Extractor interface {
    Extract(ctx, ExtractInput) (ExtractOutput, error)
    CheckCompletion(ctx, CompletionInput) (CompletionOutput, error)
    Name() string
}
```

Adapters, one file each, in `internal/extract/adapters/`:

- `ollama.go` — default. `POST /api/chat` with `format` = JSON schema,
  `temperature 0`, `num_ctx 16384`, `keep_alive 30m`.
- `claude.go` — wraps a **new, non-agentic** sidecar `agent/extract-chunk.mjs`
  (one prompt → JSON, no tools). Same shape as Ollama. Kept for comparison and
  for an optional escalation on low-confidence chunks.

The old agentic `extract.mjs` / `extract-circle.mjs` stay in the repo until the
new engine passes the bar, then are deleted.

Setting: `sync_state.extract_engine = ollama | claude`, and
`extract_model` (default `qwen2.5:14b`). Auto-extract and the manual button
both read it. Exposed in Settings.

## 7. Evidence, quality bar, evaluation

### What was measured today (2026-09-12)

Hardware: Apple M5 Max, 48 GB. Ollama 0.30.10. Sample: 222 messages,
3 days, "The 3 committee", Arabic/English, ~19k characters.

| Model | Time | Tasks | Notes |
|---|---|---|---|
| qwen3-coder:30b | 12 s | 1 | precise, missed most |
| command-r7b-arabic | 12 s | 5 | best Arabic wording, owners wrong |
| qwen2.5:14b | 24 s | 6 | best recall, 3 were past-tense noise |
| qwen2.5:14b + strict prompt + roster + evidence | 29 s | 6 | the 2 real contract tasks with exact quotes; 4 near-duplicates of one item |

Reading of the result: recall is fine, precision needs the code layers
(verify, resolve, dedupe). Requiring a verbatim quote was the single biggest
improvement. That is why it is mandatory in the design.

### The bar (must pass before switching the default)

On a golden set of ≥ 10 chunks labelled by hand (Arabic-heavy, from real
chats, stored in `store/eval/` which is gitignored):

- Precision of **verified** tasks ≥ 0.85
- Recall ≥ 0.75 against human labels
- Owner correct ≥ 0.90 when the evidence message carries a mention
- Zero tasks whose evidence quote is not in the source (must be exactly 0)
- Median chunk latency ≤ 30 s on qwen2.5:14b

`go run ./cmd/extract-eval` prints these per adapter and per model so a model
upgrade is a measured decision, not a hope.

### Measured, 2026-09-12

Golden set: 7 slices of real chats, 42 chunks, 37 tasks labelled by hand.
Arabic-heavy, a mix of groups and direct chats, work and small talk. One slice
(a friend, no work in it at all) is there to measure precision, not recall.
The set lives in `store/eval/` and is not committed: it is real chat text.

The build order was measure, fix, measure again. qwen2.5:14b through five
rounds:

| Round | What changed | Precision | Recall | Owner | p50 |
|---|---|---|---|---|---|
| 1 | as designed | 0.46 | 0.35 | 0.55 | 14.8 s |
| 2 | bug reports count as tasks; 5k chunks | 0.37 | 0.51 | 0.75 | 12.5 s |
| 3 | context lines may produce tasks | 0.40 | 0.51 | 0.75 | 9.6 s |
| 4 | short quotes and more past-tense verbs | 0.51 | 0.54 | 0.75 | 9.7 s |
| 5 | 2.6k chunks; misfiled quotes repaired | 0.38 | 0.62 | 1.00 | 9.0 s |

Then across models, same golden set, same prompts:

| Model | Precision | Recall | Owner | Bad quotes | p50 |
|---|---|---|---|---|---|
| qwen2.5:14b | 0.40 | 0.62 | 1.00 | 11 | 8.7 s |
| qwen3-coder:30b | 0.69 | 0.30 | 0.33 | 2 | 1.0 s |
| command-r7b-arabic | 0.32 | 0.62 | 0.50 | 11 | 7.4 s |
| qwen3:30b-a3b-instruct-2507 | 0.42 | 0.65 | 0.60 | 8 | 4.9 s |

Four models within ten points of each other on precision said the problem was
not the model. Nor was it confidence — dropping everything the model was less
than fully sure of reached 0.71 precision at 0.27 recall, which is not a
trade worth making:

| Confidence floor | Precision | Recall |
|---|---|---|
| 0.4 (as shipped) | 0.42 | 0.65 |
| 0.8 | 0.47 | 0.59 |
| 1.0 | 0.71 | 0.27 |

So a second model call was added: the same model, asked the opposite question
(§4a). With it:

| Model | Precision | Recall | Bad quotes | p50 |
|---|---|---|---|---|
| qwen2.5:14b | 0.54 | 0.51 | 11 | 9.1 s |
| **qwen3:30b-a3b-instruct-2507** | **0.65** | **0.54** | 8 | **4.2 s** |

qwen3:30b-a3b-instruct-2507 is the default, on those numbers. It is a mixture
of experts: 30B parameters, 3B of them active per token, so it is twice as
fast as the 14B dense model while scoring better on everything.

Reading of the result:

- **Owner resolution and quote checking are solved.** Owner is 1.00 on
  qwen2.5:14b, and no task has ever reached the database with a quote that is
  not in the message it names. Both are code, not the model, which is why they
  behave.
- **Latency is a non-issue.** Every model is far inside the 30 s budget.
- **Precision is the open problem, and it is judgement, not rules.** Five
  rounds of code rules moved it from 0.46 to 0.51 and then smaller chunks
  traded it back down for recall. What remains is the model deciding whether
  "send me the pin", "I'm around if you need anything" and a list of six
  example questions are work. No pattern separates those from real requests.
- **The precision/recall trade is the chunk size.** Smaller chunks find more
  work and more noise, in roughly equal measure.

### The bar is not met

Precision 0.65 against a bar of 0.85; recall 0.54 against 0.75. Quotes and
latency pass; the owner sample is too small to call either way — only 4 of the
37 labelled tasks sit on a message carrying a mention or a reply, which is all
an owner can honestly be resolved from.

What is known about the gap:

- It is not the model. Four models, ten points apart.
- It is not confidence. The range barely separates good from bad.
- It is not code rules. Five rounds bought four points.
- A second opinion bought twenty-three points, which is the largest single
  move anything has made, and the same lever a third pass would pull again
  with diminishing returns.

What this means in practice: every extracted task lands as `pending_review`
(§8). At 0.65 precision a reviewer keeps about two of every three they see.
That is a working review queue, and far better than the agent it replaces —
but it is not the unattended accuracy the bar was written for, and the bar
should not be quietly lowered to match what was built.

Three caveats on the numbers, stated so they are not read as better than they
are. The golden set is labelled by one reader, and perhaps one in six "extra"
tasks is defensible work the labels do not list — so true precision is a
little better than 0.65. 37 tasks is a small set: one slice moves recall by
two points. And the whole set is Arabic-heavy chats from a single 10-week
window, which is the real traffic but not all of it.

### Feedback loop

Every accept/reject in the review queue is kept (§8). Rejected evidence
message ids are never re-proposed. Over time the accepted/rejected pairs are
the golden set growing on its own.

## 8. Data model changes

```sql
-- One row per model call. Cost and quality live here.
CREATE TABLE extraction_calls (
  id INTEGER PRIMARY KEY, run_id TEXT, chat_jid TEXT, chunk_index INTEGER,
  engine TEXT, model TEXT, kind TEXT,            -- extract | completion
  messages INTEGER, chars INTEGER, latency_ms INTEGER,
  proposed INTEGER, verified INTEGER, rejected INTEGER,
  created_at INTEGER
);

-- Why a proposal was dropped, so precision problems are visible.
CREATE TABLE extraction_rejections (
  id INTEGER PRIMARY KEY, run_id TEXT, chat_jid TEXT, evidence_id TEXT,
  title TEXT, reason TEXT,                       -- no_evidence | bad_quote | duplicate | ...
  created_at INTEGER
);

ALTER TABLE tasks ADD COLUMN evidence TEXT NOT NULL DEFAULT '';   -- the verbatim quote
ALTER TABLE tasks ADD COLUMN engine  TEXT NOT NULL DEFAULT '';    -- ollama:qwen2.5:14b
ALTER TABLE tasks ADD COLUMN confidence REAL NOT NULL DEFAULT 0;
```

`chat_extraction_state` is reused as is. Rejected-by-human tasks keep their
row with `review_status = rejected` (already the case), and the dedupe step
treats a rejected task's origin message as "do not propose again".

## 9. API, settings, UI

- `POST /api/v2/tasks/extract` — unchanged contract, plus optional
  `engine` and `model` overrides. Returns `run_id` as today.
- `GET /api/v2/extractions/stats` — totals from `extraction_calls`:
  calls, latency, proposed/verified/rejected, by engine and day.
- Settings → "Task extraction": engine (Ollama / Claude), model dropdown
  (from `GET /api/tags` on Ollama), and a "Run evaluation" button that shows
  the bar numbers.
- Task detail: show `evidence` under the title with a link to the message.
  This is how a reviewer judges a task in two seconds.

## 10. Risks and how they are handled

| Risk | Handling |
|---|---|
| Dialect Arabic misread by a small model | evidence requirement + review queue; bar measured on Arabic chunks; `command-r7b-arabic` stays a candidate |
| Invented owners | code resolves owners; model's text is only a hint |
| Wrong dates | code resolves from the phrase + message time |
| Duplicates across chats | dedupe by origin message, forwarded-copy linking, later embeddings |
| Model upgrade changes quality silently | golden set + eval command; adapter swap is one file |
| Ollama returns invalid JSON | validate; retry once at temperature 0; then log and skip the chunk |
| Long chunks exceed context | chunk by characters as well as count; `num_ctx 16384` |
| A run dies half way | watermark advances per successful chunk only; re-run is idempotent |
| Meetings extracted as tasks | prompt rule + a meeting-phrase check in verify that routes to `extraction_rejections` with reason `meeting` |

## 11. Phases

1. **Engine for tasks** (this plan): pipeline, Ollama adapter, Claude chunk
   adapter, verify/resolve/dedupe/link, eval command, settings, UI evidence.
   Exit: bar passed on the golden set; default switched to Ollama.
2. **Meetings on the same engine**: replace `extract-meetings.mjs` with a
   chunk-based extractor reusing chunking, roster, verify, and the link-code
   join. Same review queue.
3. **The rest of the AI jobs**: circle digest, briefing, drafts, profiles —
   each is a single prompt already, so each is an adapter call.
4. **Embeddings** (`bge-m3`) for near-duplicate tasks and forwarded copies with
   edited text.

Items outside phase 1 are recorded in `docs/backlog.md`.

## 12. Decision record

**Decision:** replace the agentic extractor with a code-owned pipeline that
calls a local model for detection only, behind a one-file adapter port.

**Because:**

- Every failure seen this session — wrong date, invented owner, duplicate
  proposals, unverifiable output — is a step that has a right answer and
  belongs in code.
- Local models handled the detection step in 12–29 s per chunk on real Arabic
  data, and the evidence requirement made their output checkable.
- Cost drops to zero per run; the Claude subscription is kept for interactive
  work, not batch.
- The port means the model can change without touching the pipeline.

**Rejected:** pure rules (cannot detect intent in dialect); porting the
agentic loop to a local model (wrong shape for a 14B model, and unverifiable
either way).
