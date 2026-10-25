-- Undo every AUTOMATIC meeting change (source model or rule) and make the
-- engine read every open meeting again from the start.
-- Your own edits (source user) are kept.
-- Safe to run twice: the second run finds nothing.
.timeout 8000
BEGIN IMMEDIATE;

SELECT 'automatic changes to undo: ' || COUNT(*) || ' on ' || COUNT(DISTINCT meeting_id) || ' meetings'
  FROM meeting_changes WHERE source IN ('model', 'rule');

-- The value each field had BEFORE its first automatic change.
CREATE TEMP TABLE first_auto AS
SELECT c.meeting_id, c.field, c.old_value
  FROM meeting_changes c
 WHERE c.source IN ('model', 'rule')
   AND c.field IN ('status', 'starts_at', 'time_options', 'mode', 'location', 'link')
   AND c.id = (SELECT MIN(c2.id) FROM meeting_changes c2
                WHERE c2.meeting_id = c.meeting_id AND c2.field = c.field
                  AND c2.source IN ('model', 'rule'));

UPDATE meetings SET status = (SELECT old_value FROM first_auto f WHERE f.meeting_id = meetings.id AND f.field = 'status')
 WHERE id IN (SELECT meeting_id FROM first_auto WHERE field = 'status');
UPDATE meetings SET starts_at = CAST((SELECT old_value FROM first_auto f WHERE f.meeting_id = meetings.id AND f.field = 'starts_at') AS INTEGER)
 WHERE id IN (SELECT meeting_id FROM first_auto WHERE field = 'starts_at');
UPDATE meetings SET time_options = (SELECT old_value FROM first_auto f WHERE f.meeting_id = meetings.id AND f.field = 'time_options')
 WHERE id IN (SELECT meeting_id FROM first_auto WHERE field = 'time_options');
UPDATE meetings SET mode = (SELECT old_value FROM first_auto f WHERE f.meeting_id = meetings.id AND f.field = 'mode')
 WHERE id IN (SELECT meeting_id FROM first_auto WHERE field = 'mode');
UPDATE meetings SET location = (SELECT old_value FROM first_auto f WHERE f.meeting_id = meetings.id AND f.field = 'location')
 WHERE id IN (SELECT meeting_id FROM first_auto WHERE field = 'location');
-- A link that was added goes, and its join code with it.
UPDATE meetings SET link = (SELECT old_value FROM first_auto f WHERE f.meeting_id = meetings.id AND f.field = 'link'),
                    link_code = CASE WHEN (SELECT old_value FROM first_auto f WHERE f.meeting_id = meetings.id AND f.field = 'link') = '' THEN '' ELSE link_code END
 WHERE id IN (SELECT meeting_id FROM first_auto WHERE field = 'link');

-- Agenda lines that were added automatically are removed.
DELETE FROM meeting_items
 WHERE kind = 'agenda'
   AND EXISTS (SELECT 1 FROM meeting_changes c
                WHERE c.source IN ('model', 'rule') AND c.field = 'agenda'
                  AND c.meeting_id = meeting_items.meeting_id
                  AND c.new_value = meeting_items.text);
SELECT 'agenda lines removed: ' || changes();

-- Messages linked only as proof of an update are unlinked.
DELETE FROM meeting_messages WHERE role = 'update';

DELETE FROM meeting_changes WHERE source IN ('model', 'rule');

-- Everything is read again.
UPDATE meetings SET checked_ts = 0;
COMMIT;

SELECT 'status now: ' || status || ' = ' || COUNT(*) FROM meetings GROUP BY status ORDER BY COUNT(*) DESC;
