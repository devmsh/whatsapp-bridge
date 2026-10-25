-- Undo the automatic meeting changes whose proof was NOT a new message
-- (the proof was the message that created the meeting, or an older one).
-- Safe to run twice: the second run finds nothing.
.timeout 8000
BEGIN IMMEDIATE;
CREATE TEMP TABLE bad AS
WITH known AS (
  SELECT mm.meeting_id, MAX(msg.timestamp) ts
  FROM meeting_messages mm
  JOIN messages msg ON msg.id = mm.message_id AND msg.chat_jid = mm.chat_jid
  WHERE mm.role != 'update'
  GROUP BY mm.meeting_id)
SELECT c.id, c.meeting_id, c.field, c.old_value, c.new_value
FROM meeting_changes c
JOIN messages ev ON ev.id = c.message_id AND ev.chat_jid = c.chat_jid
JOIN known k ON k.meeting_id = c.meeting_id
WHERE c.source = 'model' AND ev.timestamp <= k.ts;

SELECT 'to undo: ' || COUNT(*) || ' changes on ' || COUNT(DISTINCT meeting_id) || ' meetings' FROM bad;

-- 1. Status goes back to what it was.
UPDATE meetings
   SET status = (SELECT old_value FROM bad WHERE bad.meeting_id = meetings.id AND bad.field = 'status')
 WHERE id IN (SELECT meeting_id FROM bad WHERE field = 'status');

-- 2. Agenda lines that were added are removed again.
DELETE FROM meeting_items
 WHERE kind = 'agenda'
   AND EXISTS (SELECT 1 FROM bad
                WHERE bad.field = 'agenda'
                  AND bad.meeting_id = meeting_items.meeting_id
                  AND bad.new_value = meeting_items.text);

-- 3. The history rows go.
DELETE FROM meeting_changes WHERE id IN (SELECT id FROM bad);

-- 4. These meetings are read again from the start of their window.
UPDATE meetings SET checked_ts = 0 WHERE id IN (SELECT meeting_id FROM bad);
COMMIT;

SELECT 'Founders #286 is now: ' || status FROM meetings WHERE id = 286;
