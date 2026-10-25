# Live meetings: keep a meeting up to date after it is found

Date: 2026-09-18

## Problem

A meeting is extracted once. After that, nothing reads the chat again for it.

- When people move the time, add an agenda point, or cancel, the meeting does
  not change. Example: meeting 285 (Sami) was "today after Asr". Later both
  sides moved it to "tonight". The meeting still says "after Asr".
- No Go code ever sets `held` or `cancelled`. Only the old agent did.
- Result on 2026-09-18: 179 open meetings. 99 have no date, 74 have a date in
  the past, 1 is in the future. The "upcoming" badge shows 99+ and most of it
  is dead.

## Goal

A meeting follows its chat. New messages can change its time, place, link,
agenda, and status. Dead meetings leave the upcoming list on their own. Every
automatic change can be traced to one message.

## Decisions

1. **A second model question, "what changed?"**, separate from "find
   meetings". Port: `extract.MeetingUpdater`. Same engines (Ollama, Claude),
   one prompt, JSON back.
2. **Per chat, in time order, one meeting per call.** One call reads one chunk
   of chat plus ONE known meeting that is alive at that time. It began as a
   batch of 6. On live data the local model found the right message and the
   right outcome and pinned them on the wrong meeting's number, which code
   cannot catch. Recent meetings (last 14 days) are read first.
3. **No keyword filter on this path.** "خلص الليلة بنحكي" and "مش حقدر اليوم"
   have no meeting word. If a chat has an open meeting, its new messages are
   read. A local call takes about 3.4 s, so this is affordable.
4. **Code verifies, code computes dates.** The quote must be in a real
   non-context message of the chunk. Dates come from
   `ResolveMeetingStart`, never from the model.
5. **Apply at once, keep a history.** Table `meeting_changes` keeps field,
   old value, new value, note, source (`model`, `rule`, `user`), and the
   message. No second review queue.
6. **The user's edit wins.** If the user changed a field after the evidence
   message was sent, the model does not overwrite that field.
7. **New status `lapsed`.** Set by code rules, not by the model:
   - `confirmed`, dated, date passed by 24 h: becomes `held`.
   - `proposed`, dated, date passed by 24 h: becomes `lapsed`.
   - `proposed` or `confirmed`, no date, no linked message for 21 days:
     becomes `lapsed`.
   Rules run only on a meeting that has no unread follow-up, so the model
   always gets the first word.
8. **A lapsed meeting can come back.** When the finder attaches a new
   scheduling message to a lapsed meeting, it becomes `proposed` again.
9. **New status `resolved`: settled in chat.** Set by the model. The meeting
   was proposed to decide something, and later messages decided it, so nothing
   is left to meet about. Example: "should I join and pay the fee?" then "yes,
   take the risk". The note keeps the decision in one sentence. It needs a real
   answer from the other side, not the question asked again. Not upcoming.
10. **Guards learned from the first real-data run.** An agenda line is added
   only when somebody says it is for that meeting, at most 3 per update. A call
   starting now or "in 10 minutes" never updates a planned meeting. A timing
   change needs confidence 0.7. "الليلة" with no clock time means 21:00, not
   the neutral noon.
11. **Follow-up window.** A meeting is followed from its last linked message
   until: its date + 3 days (dated), or last linked message + 21 days
   (undated). At most 600 lines per chat per run.

## Data

`meetings.checked_ts` (INTEGER, default 0): messages in the meeting's chats up
to this time were already read for changes.

```sql
CREATE TABLE IF NOT EXISTS meeting_changes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    meeting_id INTEGER NOT NULL REFERENCES meetings(id) ON DELETE CASCADE,
    field      TEXT    NOT NULL,   -- status, starts_at, time_options, mode, location, link, agenda, title, purpose
    old_value  TEXT    NOT NULL DEFAULT '',
    new_value  TEXT    NOT NULL DEFAULT '',
    note       TEXT    NOT NULL DEFAULT '',   -- one short sentence: what happened
    source     TEXT    NOT NULL DEFAULT 'model',  -- model | rule | user
    chat_jid   TEXT    NOT NULL DEFAULT '',
    message_id TEXT    NOT NULL DEFAULT '',
    run_id     TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_meeting_changes ON meeting_changes(meeting_id, created_at);
```

## API contract (fixed here so web and api can be built apart)

- `GET /api/v2/meetings/{id}` adds `"changes": [MeetingChange]`, newest first.
  `MeetingChange` JSON: `id, meeting_id, field, old_value, new_value, note,
  source, chat_jid, message_id, run_id, created_at`.
- `Meeting.status` may now be `lapsed`. `Meeting.checked_ts` is added.
- `GET /api/v2/meetings/refresh` returns
  `{"running": bool, "run_id": string, "waiting_chats": int, "last": {"chats": int, "calls": int, "changes": int, "lapsed": int, "finished_at": int}}`.
- `POST /api/v2/meetings/refresh` body `{"all": true}` or `{"chat_jid": "..."}`
  or `{"meeting_id": 12}`. Starts one background run. 409 when one is going.
- `upcoming=1` excludes `held`, `cancelled`, `lapsed`, and `resolved`.
- `Meeting.status` may also be `resolved`.

## Out of scope (in docs/backlog.md)

- Measuring the updater against labelled chats.
- Instant refresh on message arrival. The 10 minute tick is enough for now.
