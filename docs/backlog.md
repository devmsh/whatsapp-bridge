# Backlog

Ideas we decided not to do now. Top = most valuable next. One idea = one
block. When an idea is done or dropped, delete its block.

## Measure the meeting updater against labelled chats

Why: the "what changed?" engine went live on 2026-09-18 judged by eye only:
three known cases (Sami moved to tonight, Founders settled in chat, meeting 90
not bent by an ad hoc call) and one read-through of a June pass on the Karim DM.
That pass showed the failure shape: the first model call proposes a wrong date
move about 3 times in 4, and the second-opinion call is what stops them. It
also showed one miss ("مش حنقعد ل acme اليوم" is a real postponement and the
judge said no). Nobody knows the real precision or recall. Needs a labelled set
of (meeting, later slice, expected change) and a `cmd/meeting-update-eval`.
Where: `internal/extract/meetings_update.go`, prompts in
`adapters/prompts_meetings_update.go`, `store/eval/`
Size: M
Added: 2026-09-18
Depends on: nothing

## Record an ad hoc call as its own thing

Why: the engine now tells a call starting now ("خلال ١٠ دقايق", a bare Meet
link, "وقت ما تكون جاهز ابعتلي") from a planned meeting, and throws it away. It
is still a real conversation with a person about a subject (Tarek, 2026-09-18:
a quick sit-down about starting with a new client). A light record — who, when, the
subject, no review, never "upcoming" — would give the chat a history of calls
next to its meetings. Needs a `kind` on meetings or a small table of its own.
Where: `extract.startsNow` in `internal/extract/meetings_verify.go`,
`internal/db/meetings.go`, the meeting bar
Size: M
Added: 2026-09-18
Depends on: nothing

## Two small gaps in the meeting updater

Why: (1) A link sent right before a meeting is tied to it by time (45 minutes
before to 90 after). When the same pass also MOVES the meeting, the link is
checked against the old time and dropped: Sami's real Meet link at 22:28 was
lost because the meeting was still at 12:00 when it was checked, not 21:00.
Check against the patched time. (2) One update has one evidence message, so a
link or agenda line found in a nearby message points at the wrong proof in the
History list. The value is right, the "open chat" target is a little off. Give
each field its own evidence, or split such updates.
Where: `internal/extract/meetings_update.go` (VerifyUpdate, the run loop)
Size: S
Added: 2026-09-18
Depends on: nothing

## Refresh a meeting the moment its chat gets a message

Why: live updates ride the 10 minute scanner tick, and a finder run can push a
refresh to the next tick, so "moved to tonight" can take 20 minutes to show. A
debounced trigger (2 minutes after the last new message in a chat that has an
open meeting) would make it feel live. The model slot already keeps jobs apart.
Where: `internal/api/meeting_refresh.go`, the message event path in `internal/wa`
Size: S
Added: 2026-09-18
Depends on: nothing

## Undo one automatic meeting change from the history

Why: every automatic change is logged with its old value, but the History list
is read-only. One wrong date move means editing the meeting by hand. An "undo"
on a history row would write the old value back as a user edit, which also
stops the model from changing that field again from older messages.
Where: `web/src/explorer/MeetingsView.tsx` (MeetingHistory),
`internal/api/handler_meetings.go`, `db.ApplyMeetingPatch`
Size: S
Added: 2026-09-18
Depends on: nothing

## Let the model pick a meeting's circle when the votes cannot

Why: circles are now voted on (group chat, people weighted 1/n, circle name in
the title). That removed the 16-circle noise, but 96 of 268 meetings have no
circle: a DM with a person who sits in many circles, titled "اجتماع اتخاذ
القرار", carries no signal outside the conversation itself. The finder already
reads that conversation. Hand it the candidate circles (the old union) and let
it return 0 to 2. It also fixes a passing mention winning a vote ("AAA×ZED
call" in a long purpose put ZED on the BOD meeting).
Where: `internal/extract/adapters/prompts_meetings.go`, `meetings_run.go`,
`db.SyncMeetingCircles`. Measure with `cmd/meeting-eval`.
Size: M
Added: 2026-09-18
Depends on: nothing

## Pin a meeting to a circle by hand

Why: a meeting is never filed by hand, so a wrong or missing circle cannot be
fixed. A pinned circle should win and survive a re-sync. Needs a `pinned`
column on `meeting_circles` that `SyncMeetingCircles` leaves alone, plus a
control in the meeting view.
Where: `internal/db/meetings.go`, `internal/api/handler_meetings.go`, web
Size: S
Added: 2026-09-18
Depends on: nothing

## Fill in circle keywords

Why: only 2 of 22 circles have keywords, and a keyword in a meeting title is a
full vote. "اجتماع Acmi" and "الخطوة التالية في Atlas" have no circle today
only because "Acmi" and "Atlas" are not keywords of Acme and Atlas
Transformation. This is data entry in the Circles screen, not code.
Where: Circles screen, `circles.keywords`
Size: S
Added: 2026-09-18
Depends on: nothing

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

## Measure the meeting finder against labelled chats

Why: the task engine has `cmd/extract-eval` and a golden set; meetings has
`cmd/meeting-eval`, which prints what the model said but scores nothing. On the
first live sweep of one busy DM the engine kept 58 of 177 proposals — the ones
we read were plausible, but "plausible on a read-through" is not a number, and
a prompt change cannot be judged without one.
Where: `cmd/meeting-eval` — add expected-meeting labels like `expectedTask`,
and print precision and recall.
Size: M
Added: 2026-09-12
Depends on: nothing

## Keep every timing option a meeting was offered

Why: when three people give their availability ("انا اليوم متاح بالكامل",
"بكرة بعد الساعة ٥", "انا اليوم برضو متاح"), only one phrase reaches
`time_options`, because the model returns a single `when_text`. The one thing a
meeting still being arranged consists of is the list of options.
Where: `ProposedMeeting.WhenText` → a list, and `MeetingTimeOptions` to match.
Size: S
Added: 2026-09-12
Depends on: nothing

## Backfill old meetings on demand

Why: the scanner now reads only the last 7 days of a chat it has never seen,
which is right for a timer and wrong for the first time you use the feature —
the meetings arranged last month are invisible until somebody asks for them.
Where: a date range on the "Read now" button in Debugging → `meeting_scan`
`chat_jid` plus an explicit `since` (RunMeetings already honours it).
Size: S
Added: 2026-09-12
Depends on: nothing

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

## Escalate low-confidence chunks to Claude

Why: keep local as the default and spend subscription quota only where the
local model is unsure (confidence < 0.6 or verify rejected most proposals).
Where: `internal/extract/pipeline.go`; the Claude chunk adapter already
exists after phase 1.
Size: S
Added: 2026-09-12
Depends on: eval numbers showing where local is weak

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

## Count @everyone as a mention of you

Why: an "@everyone" group ping is stored as a synthetic `"@all:<groupJID>"`
entry in `messages.mentions`, which doesn't match a plain self-JID substring
check. So it doesn't light up the chat-list `@` badge or show up on the new
Mentions page, even though real WhatsApp treats it as pinging you. Decided to
ship the Mentions page without this rather than block on the product call.
Where: `internal/db/mentions.go` (`UnreadMentionCounts`, `ListSelfMentions`) —
both would need an `"@all:"+groupJID` check alongside the self-pattern one.
Size: S
Added: 2026-09-13
Depends on: nothing
