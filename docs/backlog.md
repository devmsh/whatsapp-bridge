# Backlog

Ideas we decided not to do now. Top = most valuable next. One idea = one
block. When an idea is done or dropped, delete its block.

## Grow the task-extraction golden set

Why: the set is 7 slices, 37 tasks, labelled by one reader, all from one
10-week window. A single slice moves recall by two points, and only 4 tasks
sit on a message with a mention or reply, so owner accuracy cannot be measured
at all. Every number in the design's Evidence section rests on this.
Where: `store/eval/` (gitignored). `go run ./cmd/extract-eval -label "<chat>"
-from <date> -to <date>` prints a slice and writes the skeleton to fill in.
Four unlabelled slices are already waiting in `store/eval-unlabelled/`.
Size: M
Added: 2026-09-12
Depends on: nothing

## Reach the extraction quality bar, or agree a lower one

Why: the engine scores 0.65 precision and 0.54 recall against a bar of 0.85
and 0.75 (design §7). Known: it is not the model (four are within ten points),
not confidence (the range barely separates), and not code rules (five rounds
bought four points). The second-opinion call bought twenty-three points and is
the only lever that has moved it. Options not yet tried: a third pass on what
survives; sending only the doubtful chunks to Claude; merging bursts of
messages from one sender into one line so a list of examples cannot become six
tasks. Or accept the number: every task lands in a review queue, and two in
three being right is a working queue.
Where: `internal/extract/`, measured by `cmd/extract-eval`
Size: L
Added: 2026-09-12
Depends on: a bigger golden set

## Escalate low-confidence chunks to Claude

Why: keep local as the default and spend subscription quota only where the
local model is unsure (confidence < 0.6 or verify rejected most proposals).
Why now: it is one of the untried levers for the quality bar, and the Claude
chunk adapter (`adapters/claude.go`) is already built.
Where: `internal/extract/pipeline.go`; `internal/extract/adapters/claude.go`.
Size: S
Added: 2026-09-12
Depends on: nothing

## Meetings extraction on the local engine

Why: the meetings backfill runs on the Claude subscription and takes hours.
The task-extraction engine (specs/2026-09-12) has every stage meetings need:
chunking, roster, verify, link. Reusing it removes the last big Claude batch job.
Where: `agent/extract-meetings.mjs` → `internal/extract` with a meetings
schema; keep the join-code matcher (`MeetingLinkCode`) as the cross-chat key.
Size: M
Added: 2026-09-12
Depends on: the engine passing the quality bar (see "Reach the extraction
quality bar")

## Digest, briefing, drafts and profiles on the local model

Why: each is already a single prompt with no tools, so each becomes one
adapter call. Cuts subscription use to interactive work only.
Where: `agent/circle-digest.mjs`, `briefing.mjs`, `draft-reply.mjs`,
`profile.mjs` → calls through `internal/extract/adapters`.
Size: M
Added: 2026-09-12
Depends on: nothing

## Embedding-based duplicate detection

Why: title-overlap dedupe misses the same task said in different words, and
forwarded copies with edited text. `bge-m3` is installed locally and handles
Arabic.
Where: `internal/extract/dedupe.go`; Ollama `POST /api/embed`; store vectors
per task in a new table.
Size: M
Added: 2026-09-12
Depends on: nothing

## Stop tracking the 14 MB `whatsapp-mcp` binary in git

Why: every rebuild adds 14 MB to the repo history. A `make mcp` target plus a
`.gitignore` entry keeps the repo small; the bridge needs the binary at
runtime, not in git.
Where: `.gitignore`, `Makefile`, README quick start.
Size: S
Added: 2026-09-11
Depends on: nothing

## Status videos block message handling

Why: a 15 MB status video download blocks the event loop for up to 90 s
(seen in the bridge log). The hotspot spec
(`specs/2026-07-18-pause-media-download-on-hotspot-design.md`) covers the same
problem and was never built.
Where: `internal/wa` media download path.
Size: M
Added: 2026-09-11
Depends on: nothing
