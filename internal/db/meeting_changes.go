package db

import (
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Who made a meeting_changes row. The model reads a new chat message, a rule
// closes a dead meeting, a user edits a field by hand.
const (
	ChangeByModel = "model"
	ChangeByRule  = "rule"
	ChangeByUser  = "user"
)

// MeetingChange is one row of a meeting's history: one field, its old and new
// value (as text), why it changed, and who changed it.
type MeetingChange struct {
	ID        int64  `json:"id"`
	MeetingID int64  `json:"meeting_id"`
	Field     string `json:"field"`
	OldValue  string `json:"old_value"`
	NewValue  string `json:"new_value"`
	Note      string `json:"note"`
	Source    string `json:"source"`
	ChatJID   string `json:"chat_jid"`
	MessageID string `json:"message_id"`
	RunID     string `json:"run_id"`
	CreatedAt int64  `json:"created_at"`
}

// MeetingPatch is a set of proposed edits to one meeting, plus who is
// proposing them and why. A nil pointer field means "leave this alone".
type MeetingPatch struct {
	Status      *string
	StartsAt    *int64
	TimeOptions *string // JSON array text, same format the column already holds
	Mode        *string
	Location    *string
	Link        *string
	AgendaAdd   []string // lines to append; a line that already exists is skipped

	Note   string // one short sentence, stored on every change row this patch writes
	Source string // ChangeByModel | ChangeByRule | ChangeByUser

	// Evidence: the message that justifies this patch. ChatJID may be empty
	// for a rule, which has no one message behind it.
	ChatJID    string
	MessageID  string
	EvidenceTS int64 // when the evidence message was sent; 0 = now
	RunID      string
}

// ApplyMeetingPatch writes the fields of p that actually change, each as one
// meeting_changes row, in a single transaction.
//
// The user's edit wins: if p is not itself a user edit, and the newest change
// on a field was made by the user after the evidence for this patch, that
// field is left untouched. Otherwise the model would "correct" a field the
// person just fixed by hand, using a chat message that is now stale.
func (s *Store) ApplyMeetingPatch(meetingID int64, p MeetingPatch) ([]MeetingChange, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	// Take the write lock before reading. A transaction that reads first and
	// writes later can be refused with "busy" the moment it tries to write, if
	// anything else wrote in between — and the bridge stores incoming messages
	// all day. SQLite does not wait on that kind of refusal. A write that
	// changes nothing makes this transaction a writer from its first
	// statement, and for that the busy timeout does wait.
	if _, err := tx.Exec(`UPDATE meetings SET id = id WHERE id = ?`, meetingID); err != nil {
		return nil, err
	}

	m, err := scanMeeting(tx.QueryRow(`SELECT `+meetingCols+` FROM meetings WHERE id = ?`, meetingID))
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	evidenceTS := p.EvidenceTS
	if evidenceTS == 0 {
		evidenceTS = now
	}

	var changes []MeetingChange
	writeChange := func(field, oldVal, newVal string) error {
		res, err := tx.Exec(`INSERT INTO meeting_changes
			(meeting_id, field, old_value, new_value, note, source, chat_jid, message_id, run_id, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`,
			meetingID, field, oldVal, newVal, p.Note, p.Source, p.ChatJID, p.MessageID, p.RunID, now)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		changes = append(changes, MeetingChange{
			ID: id, MeetingID: meetingID, Field: field, OldValue: oldVal, NewValue: newVal,
			Note: p.Note, Source: p.Source, ChatJID: p.ChatJID, MessageID: p.MessageID,
			RunID: p.RunID, CreatedAt: now,
		})
		return nil
	}

	// userWins reports whether the person already edited this field, after
	// the evidence behind this patch was said. A patch that is itself a user
	// edit never loses to an earlier one — the newest user edit always wins.
	userWins := func(field string) (bool, error) {
		if p.Source == ChangeByUser {
			return false, nil
		}
		var source string
		var createdAt int64
		err := tx.QueryRow(`SELECT source, created_at FROM meeting_changes
			WHERE meeting_id = ? AND field = ? ORDER BY created_at DESC, id DESC LIMIT 1`,
			meetingID, field).Scan(&source, &createdAt)
		if err == sql.ErrNoRows {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return source == ChangeByUser && createdAt > evidenceTS, nil
	}

	// applyText handles the plain text/number fields that share one shape:
	// compare, check who wins, write the column, write the change row.
	applyText := func(field, oldVal, newVal string, setCol func() error) error {
		if newVal == oldVal {
			return nil
		}
		skip, err := userWins(field)
		if err != nil || skip {
			return err
		}
		if err := setCol(); err != nil {
			return err
		}
		return writeChange(field, oldVal, newVal)
	}

	if p.Status != nil {
		if err := applyText("status", m.Status, *p.Status, func() error {
			_, err := tx.Exec(`UPDATE meetings SET status = ? WHERE id = ?`, *p.Status, meetingID)
			return err
		}); err != nil {
			return nil, err
		}
	}
	if p.StartsAt != nil {
		oldVal := strconv.FormatInt(m.StartsAt, 10)
		newVal := strconv.FormatInt(*p.StartsAt, 10)
		if err := applyText("starts_at", oldVal, newVal, func() error {
			_, err := tx.Exec(`UPDATE meetings SET starts_at = ? WHERE id = ?`, *p.StartsAt, meetingID)
			return err
		}); err != nil {
			return nil, err
		}
	}
	if p.TimeOptions != nil {
		if err := applyText("time_options", m.TimeOptions, *p.TimeOptions, func() error {
			_, err := tx.Exec(`UPDATE meetings SET time_options = ? WHERE id = ?`, *p.TimeOptions, meetingID)
			return err
		}); err != nil {
			return nil, err
		}
	}
	if p.Mode != nil {
		if err := applyText("mode", m.Mode, *p.Mode, func() error {
			_, err := tx.Exec(`UPDATE meetings SET mode = ? WHERE id = ?`, *p.Mode, meetingID)
			return err
		}); err != nil {
			return nil, err
		}
	}
	if p.Location != nil {
		if err := applyText("location", m.Location, *p.Location, func() error {
			// The map link only follows the location text when it was derived
			// from it in the first place. A location_url pasted straight into
			// the location_url column (not from the free text) must survive a
			// text-only edit untouched.
			newURL := m.LocationURL
			if m.LocationURL == "" || m.LocationURL == MeetingMapLink(m.Location) {
				newURL = MeetingMapLink(*p.Location)
			}
			_, err := tx.Exec(`UPDATE meetings SET location = ?, location_url = ? WHERE id = ?`,
				*p.Location, newURL, meetingID)
			return err
		}); err != nil {
			return nil, err
		}
	}
	if p.Link != nil {
		if err := applyText("link", m.Link, *p.Link, func() error {
			_, err := tx.Exec(`UPDATE meetings SET link = ?, link_code = ? WHERE id = ?`,
				*p.Link, MeetingLinkCode(*p.Link), meetingID)
			return err
		}); err != nil {
			return nil, err
		}
	}

	if len(p.AgendaAdd) > 0 {
		existing, err := queryStringsTx(tx, `SELECT text FROM meeting_items WHERE meeting_id = ? AND kind = ?`,
			meetingID, ItemAgenda)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, e := range existing {
			seen[normalizeAgendaLine(e)] = true
		}
		var pos int
		if err := tx.QueryRow(`SELECT COALESCE(MAX(position),0) FROM meeting_items
			WHERE meeting_id = ? AND kind = ?`, meetingID, ItemAgenda).Scan(&pos); err != nil {
			return nil, err
		}
		for _, line := range p.AgendaAdd {
			key := normalizeAgendaLine(line)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			pos++
			if _, err := tx.Exec(`INSERT INTO meeting_items
				(meeting_id, kind, text, done, owner_jid, task_id, position, created_at)
				VALUES (?,?,?,0,'',NULL,?,?)`,
				meetingID, ItemAgenda, line, pos, now); err != nil {
				return nil, err
			}
			if err := writeChange("agenda", "", line); err != nil {
				return nil, err
			}
		}
	}

	if len(changes) == 0 {
		return []MeetingChange{}, nil
	}

	if _, err := tx.Exec(`UPDATE meetings SET updated_at = ? WHERE id = ?`, now, meetingID); err != nil {
		return nil, err
	}
	if p.ChatJID != "" && p.MessageID != "" {
		// Both are needed: meeting_messages is keyed on (meeting_id, chat_jid,
		// message_id), so a message id with no chat would write a junk row
		// under chat_jid = '' that nothing else expects to see.
		//
		// INSERT OR IGNORE, not the upsert LinkMeetingMessage does: a message
		// already linked as "origin" or "scheduling" keeps that role, since
		// that says more than "update" would.
		if _, err := tx.Exec(`INSERT OR IGNORE INTO meeting_messages
			(meeting_id, chat_jid, message_id, role, added_at) VALUES (?,?,?,'update',?)`,
			meetingID, p.ChatJID, p.MessageID, now); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return changes, nil
}

// normalizeAgendaLine is the "same line" test for agenda dedup: case and
// surrounding whitespace do not make two lines different.
func normalizeAgendaLine(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// queryStringsTx is queryStrings run inside an existing transaction, so
// ApplyMeetingPatch's reads and writes see a consistent snapshot.
func queryStringsTx(tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if rows.Scan(&v) == nil {
			out = append(out, v)
		}
	}
	return out, rows.Err()
}

// RecordMeetingEdit writes one change row per tracked field that differs
// between before and after. It does not write the meeting itself — the
// caller (the API, right after saving a user's edit) already did that. This
// only keeps the trail, so a later model patch knows the user touched it.
func (s *Store) RecordMeetingEdit(before, after *Meeting, source string) error {
	now := time.Now().Unix()
	type fieldDiff struct{ field, oldVal, newVal string }
	diffs := []fieldDiff{}
	add := func(field, oldVal, newVal string) {
		if oldVal != newVal {
			diffs = append(diffs, fieldDiff{field, oldVal, newVal})
		}
	}
	add("title", before.Title, after.Title)
	add("purpose", before.Purpose, after.Purpose)
	add("status", before.Status, after.Status)
	add("starts_at", strconv.FormatInt(before.StartsAt, 10), strconv.FormatInt(after.StartsAt, 10))
	add("time_options", before.TimeOptions, after.TimeOptions)
	add("mode", before.Mode, after.Mode)
	add("location", before.Location, after.Location)
	add("link", before.Link, after.Link)

	for _, d := range diffs {
		if _, err := s.DB.Exec(`INSERT INTO meeting_changes
			(meeting_id, field, old_value, new_value, note, source, chat_jid, message_id, run_id, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`,
			after.ID, d.field, d.oldVal, d.newVal, "", source, "", "", "", now); err != nil {
			return err
		}
	}
	return nil
}

// ListMeetingChanges returns a meeting's history, newest first.
func (s *Store) ListMeetingChanges(meetingID int64) ([]MeetingChange, error) {
	rows, err := s.DB.Query(`SELECT id, meeting_id, field, old_value, new_value, note, source,
		chat_jid, message_id, run_id, created_at
		FROM meeting_changes WHERE meeting_id = ? ORDER BY created_at DESC, id DESC`, meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MeetingChange{}
	for rows.Next() {
		var c MeetingChange
		if err := rows.Scan(&c.ID, &c.MeetingID, &c.Field, &c.OldValue, &c.NewValue, &c.Note, &c.Source,
			&c.ChatJID, &c.MessageID, &c.RunID, &c.CreatedAt); err != nil {
			return out, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetMeetingChecked bumps the follow-up watermark: it never moves backwards,
// and it does NOT touch updated_at, because checking a chat for changes is
// not itself a change — the meeting list must not reorder just because a
// background sweep looked at it and found nothing new.
func (s *Store) SetMeetingChecked(meetingID int64, ts int64) error {
	_, err := s.DB.Exec(`UPDATE meetings SET checked_ts = MAX(checked_ts, ?) WHERE id = ?`, ts, meetingID)
	return err
}

// Follow-up window: how long after its last real activity a meeting still
// deserves a re-read, before it is left to CloseStaleMeetings.
const (
	// MeetingFollowDatedDays: a meeting with an agreed date is watched until
	// 3 days after that date, in case a message says it moved or was cancelled.
	MeetingFollowDatedDays = 3
	// MeetingFollowUndatedDays: a meeting with no date yet is watched for 21
	// days of chat silence before it is presumed dead.
	MeetingFollowUndatedDays = 21
)

// MeetingAnchorTS is a meeting's most recent real activity: the newest
// timestamp among the messages linked to it. When none of those messages can
// be resolved (none linked yet, or the chat/message was removed), it falls
// back to when the meeting was created, so a meeting is never anchored at
// time zero.
func (s *Store) MeetingAnchorTS(meetingID int64) int64 {
	var ts sql.NullInt64
	s.DB.QueryRow(`SELECT MAX(msg.timestamp) FROM meeting_messages mm
		JOIN messages msg ON msg.chat_jid = mm.chat_jid AND msg.id = mm.message_id
		WHERE mm.meeting_id = ?`, meetingID).Scan(&ts)
	if ts.Valid && ts.Int64 > 0 {
		return ts.Int64
	}
	var created int64
	s.DB.QueryRow(`SELECT created_at FROM meetings WHERE id = ?`, meetingID).Scan(&created)
	return created
}

// MeetingFollowUp is one meeting that has new chat activity worth reading
// again, and the window of messages that made it qualify.
type MeetingFollowUp struct {
	Meeting Meeting `json:"meeting"`
	Since   int64   `json:"since"` // messages after this time are new
	Until   int64   `json:"until"` // the follow-up window closes here
}

// openMeetingsInChat returns the open (proposed/confirmed, not rejected)
// meetings linked to chatJID — the shared candidate list behind every
// follow-up check below, so there is only one place that defines "open
// meeting in this chat".
func (s *Store) openMeetingsInChat(chatJID string) ([]*Meeting, error) {
	rows, err := s.DB.Query(`SELECT `+meetingCols+` FROM meetings m
		WHERE m.review_status != 'rejected'
		  AND m.status IN (?, ?)
		  AND EXISTS (SELECT 1 FROM meeting_messages mm
		               WHERE mm.meeting_id = m.id AND mm.chat_jid = ?)`,
		MeetingProposed, MeetingConfirmed, chatJID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Meeting
	for rows.Next() {
		m, err := scanMeeting(rows)
		if err != nil {
			return out, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// meetingFollowUpDue is the one-meeting, one-chat version of the follow-up
// test: does chatJID hold a new, real message for meeting m since it was
// last checked, inside its follow-up window? It runs a single "does a row
// exist" query and loads no participants or items — those are only useful
// to a caller that is about to show the meeting, so they are left to that
// caller. anchor is MeetingAnchorTS(m.ID), passed in so a caller checking one
// meeting across several chats computes it once, not once per chat.
func (s *Store) meetingFollowUpDue(m *Meeting, anchor int64, chatJID string, now int64) (MeetingFollowUp, bool, error) {
	since := m.CheckedTS
	if anchor > since {
		since = anchor
	}
	var until int64
	if m.StartsAt > 0 {
		until = m.StartsAt + MeetingFollowDatedDays*86400
	} else {
		until = anchor + MeetingFollowUndatedDays*86400
	}
	upper := until
	if now < upper {
		upper = now
	}
	var exists int
	err := s.DB.QueryRow(`SELECT 1 FROM messages
		WHERE chat_jid = ? AND is_deleted = 0 AND timestamp > ? AND timestamp <= ? LIMIT 1`,
		chatJID, since, upper).Scan(&exists)
	if err == sql.ErrNoRows {
		return MeetingFollowUp{}, false, nil
	}
	if err != nil {
		return MeetingFollowUp{}, false, err
	}
	return MeetingFollowUp{Meeting: *m, Since: since, Until: until}, true, nil
}

// MeetingFollowUpsForChat finds the open meetings tied to chatJID that have
// at least one new, real message since they were last checked — the ones the
// meeting updater should read next. Ordered oldest-anchor first, so the
// meeting that has waited longest is read first. Participants and items are
// loaded only for the meetings actually returned, since this is the one
// caller that renders them.
func (s *Store) MeetingFollowUpsForChat(chatJID string, now int64) ([]MeetingFollowUp, error) {
	candidates, err := s.openMeetingsInChat(chatJID)
	if err != nil {
		return nil, err
	}

	type ranked struct {
		fu     MeetingFollowUp
		anchor int64
	}
	var found []ranked
	for _, m := range candidates {
		anchor := s.MeetingAnchorTS(m.ID)
		fu, due, err := s.meetingFollowUpDue(m, anchor, chatJID, now)
		if err != nil {
			return nil, err
		}
		if !due {
			continue
		}
		fu.Meeting.Participants, _ = s.MeetingParticipants(m.ID)
		fu.Meeting.Items, _ = s.MeetingItems(m.ID)
		found = append(found, ranked{fu, anchor})
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].anchor < found[j].anchor })
	out := make([]MeetingFollowUp, len(found))
	for i, f := range found {
		out[i] = f.fu
	}
	return out, nil
}

// chatHasFollowUpDue is the yes/no version of MeetingFollowUpsForChat: same
// candidate meetings and the same due test, but it stops at the first match
// and never loads participants or items. ChatsWithMeetingFollowUps only needs
// to know whether a chat qualifies, not to render what it found.
func (s *Store) chatHasFollowUpDue(chatJID string, now int64) (bool, error) {
	candidates, err := s.openMeetingsInChat(chatJID)
	if err != nil {
		return false, err
	}
	for _, m := range candidates {
		anchor := s.MeetingAnchorTS(m.ID)
		_, due, err := s.meetingFollowUpDue(m, anchor, chatJID, now)
		if err != nil {
			return false, err
		}
		if due {
			return true, nil
		}
	}
	return false, nil
}

// ChatsWithMeetingFollowUps returns the chats holding a meeting that is due a
// re-read, most recently active chat first. There are under 300 open meetings
// at a time, so a plain loop over the candidate chats is cheap enough.
func (s *Store) ChatsWithMeetingFollowUps(now int64, limit int) ([]string, error) {
	chats, err := s.queryStrings(`SELECT DISTINCT mm.chat_jid FROM meeting_messages mm
		JOIN meetings m ON m.id = mm.meeting_id
		WHERE m.review_status != 'rejected' AND m.status IN (?, ?)`,
		MeetingProposed, MeetingConfirmed)
	if err != nil {
		return nil, err
	}

	type recent struct {
		jid       string
		lastMsgAt int64
	}
	var active []recent
	for _, jid := range chats {
		if s.IsChatExcludedFromAI(jid) {
			continue
		}
		due, err := s.chatHasFollowUpDue(jid, now)
		if err != nil {
			return nil, err
		}
		if !due {
			continue
		}
		var lastMsgAt int64
		s.DB.QueryRow(`SELECT last_message_at FROM chats WHERE jid = ?`, jid).Scan(&lastMsgAt)
		active = append(active, recent{jid, lastMsgAt})
	}
	sort.SliceStable(active, func(i, j int) bool { return active[i].lastMsgAt > active[j].lastMsgAt })

	out := []string{}
	for _, a := range active {
		out = append(out, a.jid)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// meetingHasUnreadFollowUp reports whether meeting m still has a follow-up
// waiting in any chat the AI is allowed to read — the same due test as
// MeetingFollowUpsForChat, just asked "does this one meeting qualify"
// instead of "which meetings qualify". CloseStaleMeetings uses it so the
// model always gets first look. anchor is MeetingAnchorTS(m.ID), computed
// once by the caller and reused across every chat checked here.
//
// A chat IsChatExcludedFromAI (hidden, archived or deleted) is skipped: the
// updater never reads that chat, so checked_ts can never move past a message
// sitting there. Counting it as "unread" would keep the meeting open
// forever, which is the exact bug this rule exists to fix.
func (s *Store) meetingHasUnreadFollowUp(m *Meeting, anchor, now int64) (bool, error) {
	chats, err := s.MeetingChatJIDs(m.ID)
	if err != nil {
		return false, err
	}
	for _, chat := range chats {
		if s.IsChatExcludedFromAI(chat) {
			continue
		}
		_, due, err := s.meetingFollowUpDue(m, anchor, chat, now)
		if err != nil {
			return false, err
		}
		if due {
			return true, nil
		}
	}
	return false, nil
}

// CloseStaleMeetings applies the rules that let a meeting die on its own (see
// docs/superpowers/specs/2026-09-18-live-meetings-design.md, decision 7):
//
//   - confirmed, dated, the date passed by 24h -> held
//   - proposed, dated, the date passed by 24h -> lapsed
//   - proposed or confirmed, no date, 21 days of chat silence -> lapsed
//
// A meeting is skipped when it still has an unread follow-up (the model has
// not had its say yet) or when the person set its status by hand (a rule
// never overrides a human). One meeting failing to patch (for example a busy
// database) is logged and skipped rather than stopping the whole sweep — the
// other candidates must still get their chance to close.
func (s *Store) CloseStaleMeetings(now int64) ([]MeetingChange, error) {
	rows, err := s.DB.Query(`SELECT `+meetingCols+` FROM meetings m
		WHERE m.review_status != 'rejected' AND m.status IN (?, ?)`,
		MeetingProposed, MeetingConfirmed)
	if err != nil {
		return nil, err
	}
	var candidates []*Meeting
	for rows.Next() {
		m, err := scanMeeting(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var out []MeetingChange
	for _, m := range candidates {
		// Computed once per meeting, not once per chat it happens to be in.
		anchor := s.MeetingAnchorTS(m.ID)

		unread, err := s.meetingHasUnreadFollowUp(m, anchor, now)
		if err != nil {
			return nil, err
		}
		if unread {
			continue
		}
		userSet, err := s.newestChangeIsUser(m.ID, "status")
		if err != nil {
			return nil, err
		}
		if userSet {
			continue
		}

		var newStatus, note string
		switch {
		case m.Status == MeetingConfirmed && m.StartsAt > 0 && m.StartsAt < now-24*3600:
			newStatus, note = MeetingHeld, "The date passed and it was confirmed."
		case m.Status == MeetingProposed && m.StartsAt > 0 && m.StartsAt < now-24*3600:
			newStatus, note = MeetingLapsed, "The date passed and nobody confirmed it."
		case m.StartsAt == 0 && anchor < now-MeetingFollowUndatedDays*86400:
			newStatus, note = MeetingLapsed, "No date was set and the chat went quiet for 21 days."
		default:
			continue
		}

		changes, err := s.ApplyMeetingPatch(m.ID, MeetingPatch{
			Status: &newStatus,
			Note:   note,
			Source: ChangeByRule,
		})
		if err != nil {
			fmt.Printf("meeting rules: meeting %d: %v\n", m.ID, err)
			continue
		}
		out = append(out, changes...)
	}
	return out, nil
}

// newestChangeIsUser reports whether the newest change row for one field was
// made by the user, whatever the timestamp — a rule must never override a
// human decision, no matter how old it is.
func (s *Store) newestChangeIsUser(meetingID int64, field string) (bool, error) {
	var source string
	err := s.DB.QueryRow(`SELECT source FROM meeting_changes
		WHERE meeting_id = ? AND field = ? ORDER BY created_at DESC, id DESC LIMIT 1`,
		meetingID, field).Scan(&source)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return source == ChangeByUser, nil
}
