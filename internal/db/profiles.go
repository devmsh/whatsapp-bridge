package db

import (
	"database/sql"
	"strconv"
	"time"
)

// Entity types for entity_profiles.entity_type.
const (
	ProfileCircle  = "circle"
	ProfileGroup   = "group"
	ProfileContact = "contact"
)

// Profile statuses.
const (
	ProfilePending = "pending" // queued, not yet generated
	ProfileOK      = "ok"      // generated from real content
	ProfileEmpty   = "empty"   // no content to summarize (stub)
	ProfileError   = "error"   // generation failed
)

// Weekend days for the "working days" refresh cadence. The user is in a
// Friday/Saturday-weekend region; change here if that differs.
var weekendDays = map[time.Weekday]bool{time.Friday: true, time.Saturday: true}

// Profile is the AI-written (and user-editable) purpose description for one
// circle, group, or contact/DM.
type Profile struct {
	EntityType    string `json:"entity_type"`
	EntityRef     string `json:"entity_ref"`
	Description   string `json:"description"`
	Source        string `json:"source"` // auto | manual
	MsgCountAtGen int    `json:"msg_count_at_gen"`
	Status        string `json:"status"`
	Error         string `json:"error,omitempty"`
	GeneratedAt   int64  `json:"generated_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

func scanProfile(sc scanner, p *Profile) error {
	return sc.Scan(&p.EntityType, &p.EntityRef, &p.Description, &p.Source,
		&p.MsgCountAtGen, &p.Status, &p.Error, &p.GeneratedAt, &p.UpdatedAt)
}

const profileCols = `entity_type, entity_ref, description, source, msg_count_at_gen, status, error, generated_at, updated_at`

// GetProfile returns one profile, or nil if none exists yet.
func (s *Store) GetProfile(entityType, ref string) (*Profile, error) {
	p := &Profile{}
	err := scanProfile(
		s.DB.QueryRow(`SELECT `+profileCols+` FROM entity_profiles WHERE entity_type = ? AND entity_ref = ?`, entityType, ref),
		p,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// GetProfileDescriptions returns ref -> description for a set of refs of one
// type (only profiles that have a non-empty, ok/empty description).
func (s *Store) GetProfileDescriptions(entityType string, refs []string) map[string]string {
	out := map[string]string{}
	for _, ref := range refs {
		if p, _ := s.GetProfile(entityType, ref); p != nil && p.Description != "" {
			out[ref] = p.Description
		}
	}
	return out
}

// SaveProfileResult upserts a generated profile (source stays 'auto' unless the
// row was manually edited — a manual edit is preserved on re-generation only if
// keepManual is false; the worker passes keepManual=false to overwrite).
func (s *Store) SaveProfileResult(entityType, ref, description, status, errMsg string, msgCount int) error {
	now := time.Now().Unix()
	_, err := s.DB.Exec(`INSERT INTO entity_profiles
		(entity_type, entity_ref, description, source, msg_count_at_gen, status, error, generated_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(entity_type, entity_ref) DO UPDATE SET
			description = excluded.description,
			source = 'auto',
			msg_count_at_gen = excluded.msg_count_at_gen,
			status = excluded.status,
			error = excluded.error,
			generated_at = excluded.generated_at,
			updated_at = excluded.updated_at`,
		entityType, ref, description, "auto", msgCount, status, errMsg, now, now)
	return err
}

// SaveProfileManual stores a human-edited description (pinned source='manual').
func (s *Store) SaveProfileManual(entityType, ref, description string) error {
	now := time.Now().Unix()
	_, err := s.DB.Exec(`INSERT INTO entity_profiles
		(entity_type, entity_ref, description, source, msg_count_at_gen, status, error, generated_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(entity_type, entity_ref) DO UPDATE SET
			description = excluded.description,
			source = 'manual',
			status = 'ok',
			error = '',
			generated_at = excluded.generated_at,
			updated_at = excluded.updated_at`,
		entityType, ref, description, "manual", 0, ProfileOK, "", now, now)
	return err
}

// ChatMessageCount counts stored messages for a chat JID (group or DM).
func (s *Store) ChatMessageCount(jid string) int {
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE chat_jid = ?`, jid).Scan(&n)
	return n
}

// ChatMessageCounts returns chat_jid -> message count for every chat in ONE
// query. Used to decide, in bulk, which chats are empty (instant stub) vs. need
// a model call — instead of probing each chat one at a time.
func (s *Store) ChatMessageCounts() map[string]int {
	out := map[string]int{}
	rows, err := s.DB.Query(`SELECT chat_jid, COUNT(*) FROM messages GROUP BY chat_jid`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var jid string
		var n int
		if rows.Scan(&jid, &n) == nil {
			out[jid] = n
		}
	}
	return out
}

// AllProfiles returns every profile row keyed by "type:ref", in one query.
func (s *Store) AllProfiles() map[string]*Profile {
	out := map[string]*Profile{}
	rows, err := s.DB.Query(`SELECT ` + profileCols + ` FROM entity_profiles`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		p := &Profile{}
		if scanProfile(rows, p) == nil {
			out[p.EntityType+":"+p.EntityRef] = p
		}
	}
	return out
}

// ProfileRef identifies one entity for bulk operations.
type ProfileRef struct {
	Type string
	Ref  string
}

// StubEmptyProfiles writes "empty" stubs for many chats with no messages, in one
// transaction and with no model calls. Existing rows are left untouched.
func (s *Store) StubEmptyProfiles(entries []ProfileRef) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	stmt, err := tx.Prepare(`INSERT INTO entity_profiles
		(entity_type, entity_ref, description, source, msg_count_at_gen, status, error, generated_at, updated_at)
		VALUES (?,?,?,?,0,?,'',?,?)
		ON CONFLICT(entity_type, entity_ref) DO NOTHING`)
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	defer stmt.Close()
	now := time.Now().Unix()
	n := 0
	for _, e := range entries {
		if _, err := stmt.Exec(e.Type, e.Ref, "No conversation yet.", "auto", ProfileEmpty, now, now); err == nil {
			n++
		}
	}
	return n, tx.Commit()
}

// ProfileStats summarizes how far profiling has progressed.
type ProfileStats struct {
	Total     int `json:"total"`     // profile rows
	OK        int `json:"ok"`        // generated from content
	Empty     int `json:"empty"`     // stubs (no content)
	Error     int `json:"error"`     // failed
	Pending   int `json:"pending"`   // queued
	Manual    int `json:"manual"`    // human-edited
	Stale     int `json:"stale"`     // older than the refresh cadence
	QueueSize int `json:"queue_size"` // entities waiting to be (re)generated now
}

// workingDaysAgoUnix returns the unix time of the day n working days before now
// (weekends skipped). Profiles generated before this are due for refresh.
func workingDaysAgoUnix(n int) int64 {
	t := time.Now()
	count := 0
	for count < n {
		t = t.AddDate(0, 0, -1)
		if !weekendDays[t.Weekday()] {
			count++
		}
	}
	return t.Unix()
}

// ProfileStaleCutoff is the generated_at boundary for the 7-working-day refresh.
func ProfileStaleCutoff() int64 { return workingDaysAgoUnix(7) }

// CountProfilesByStatus returns aggregate counts for the dashboard.
func (s *Store) CountProfilesByStatus() (ProfileStats, error) {
	var st ProfileStats
	rows, err := s.DB.Query(`SELECT status, source, COUNT(*) FROM entity_profiles GROUP BY status, source`)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var status, source string
		var n int
		if rows.Scan(&status, &source, &n) != nil {
			continue
		}
		st.Total += n
		switch status {
		case ProfileOK:
			st.OK += n
		case ProfileEmpty:
			st.Empty += n
		case ProfileError:
			st.Error += n
		case ProfilePending:
			st.Pending += n
		}
		if source == "manual" {
			st.Manual += n
		}
	}
	cutoff := ProfileStaleCutoff()
	s.DB.QueryRow(`SELECT COUNT(*) FROM entity_profiles WHERE source = 'auto' AND generated_at < ?`, cutoff).Scan(&st.Stale)
	return st, rows.Err()
}

// circleRefStr formats a circle id as its profile ref.
func circleRefStr(id int64) string { return strconv.FormatInt(id, 10) }
