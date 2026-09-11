# Backlog

Ideas we decided not to do now. Top = most valuable next. One idea = one
block. When an idea is done or dropped, delete its block.

## Meetings extraction on the local engine

Why: the meetings backfill runs on the Claude subscription and takes hours.
The task-extraction engine (specs/2026-09-12) has every stage meetings need:
chunking, roster, verify, link. Reusing it removes the last big Claude batch job.
Where: `agent/extract-meetings.mjs` → `internal/extract` with a meetings
schema; keep the join-code matcher (`MeetingLinkCode`) as the cross-chat key.
Size: M
Added: 2026-09-12
Depends on: task extraction engine, phase 1 passed the quality bar

## Digest, briefing, drafts and profiles on the local model

Why: each is already a single prompt with no tools, so each becomes one
adapter call. Cuts subscription use to interactive work only.
Where: `agent/circle-digest.mjs`, `briefing.mjs`, `draft-reply.mjs`,
`profile.mjs` → calls through `internal/extract/adapters`.
Size: M
Added: 2026-09-12
Depends on: task extraction engine (the adapter port)

## Embedding-based duplicate detection

Why: title-overlap dedupe misses the same task said in different words, and
forwarded copies with edited text. `bge-m3` is installed locally and handles
Arabic.
Where: `internal/extract/dedupe.go`; Ollama `POST /api/embed`; store vectors
per task in a new table.
Size: M
Added: 2026-09-12
Depends on: task extraction engine

## Escalate low-confidence chunks to Claude

Why: keep local as the default and spend subscription quota only where the
local model is unsure (confidence < 0.6 or verify rejected most proposals).
Where: `internal/extract/pipeline.go`; the Claude chunk adapter already
exists after phase 1.
Size: S
Added: 2026-09-12
Depends on: task extraction engine, eval numbers showing where local is weak

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
