package api

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"whatsapp-bridge-v2/internal/db"
)

// dashContact is the "everything in one screen" payload for a contact.
type dashContact struct {
	JID            string       `json:"jid"`
	Name           string       `json:"name"`
	Phone          string       `json:"phone,omitempty"`
	BusinessName   string       `json:"business_name,omitempty"`
	IsBusiness     bool         `json:"is_business,omitempty"`
	// VerifiedName is set on businesses that passed WA's official
	// verification — the green-check accounts. When non-empty the client
	// renders a "✓ Verified" badge in the hero card. Empty (the common
	// case) on personal accounts and on unverified businesses.
	VerifiedName   string       `json:"verified_name,omitempty"`
	Profile        *db.Profile  `json:"profile"`
	Tags           []db.Tag     `json:"tags"`
	Circles        []db.Circle  `json:"circles"`
	TasksOpen      []db.Task    `json:"tasks_open"`
	TasksDoneCount int          `json:"tasks_done_count"`
	LastActive     int64        `json:"last_active"`
	MessageCount   int          `json:"message_count"`
	Recent         []dashRecent `json:"recent"`
}

type dashGroup struct {
	JID              string             `json:"jid"`
	Name             string             `json:"name"`
	Topic            string             `json:"topic,omitempty"`
	ParticipantCount int                `json:"participant_count"`
	Profile          *db.Profile        `json:"profile"`
	Circles          []db.Circle        `json:"circles"`
	TasksOpen        []db.Task          `json:"tasks_open"`
	TasksDoneCount   int                `json:"tasks_done_count"`
	LastActive       int64              `json:"last_active"`
	MessageCount     int                `json:"message_count"`
	TopContributors  []dashContributor  `json:"top_contributors"`
	Recent           []dashRecent       `json:"recent"`
}

type dashContributor struct {
	JID      string `json:"jid"`
	Name     string `json:"name"`
	Messages int    `json:"messages"`
	IsAdmin  bool   `json:"is_admin"`
}

type dashRecent struct {
	Timestamp int64  `json:"timestamp"`
	IsFromMe  bool   `json:"is_from_me"`
	SenderJID string `json:"sender_jid,omitempty"`
	From      string `json:"from"`
	Content   string `json:"content"`
}

// handleContactDashboard returns the everything-about-this-contact payload.
// GET /api/v2/contacts/{jid}/dashboard
func (s *Server) handleContactDashboard(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	// Hidden chats: 404 unless unlocked (UI never reaches the dashboard for them).
	if s.notFoundIfHidden(w, r, jid) {
		return
	}
	d := &dashContact{JID: jid, Tags: []db.Tag{}, Circles: []db.Circle{}, TasksOpen: []db.Task{}, Recent: []dashRecent{}}

	// Identity / profile metadata from contacts row.
	s.store.DB.QueryRow(`SELECT
		COALESCE(NULLIF(name,''), NULLIF(push_name,''), NULLIF(business_name,''), ''),
		COALESCE(phone,''),
		COALESCE(business_name,''),
		is_business,
		COALESCE(verified_name,'')
		FROM contacts WHERE jid = ?`, jid).Scan(&d.Name, &d.Phone, &d.BusinessName, &d.IsBusiness, &d.VerifiedName)
	if d.Name == "" {
		d.Name = jidUser(jid)
	}

	// AI profile.
	d.Profile, _ = s.store.GetProfile(db.ProfileContact, jid)

	// Tags.
	if t, err := s.store.TagsForContact(jid); err == nil && t != nil {
		d.Tags = t
	}

	// Circles.
	if c, err := s.store.GetCirclesForMember(db.MemberContact, jid); err == nil && c != nil {
		d.Circles = c
	}

	// Tasks: open assigned to this contact + done count.
	if rows, err := s.store.DB.Query(`SELECT `+taskColumnsRaw()+` FROM tasks
		WHERE assignee_jid = ? AND review_status='accepted' AND status IN ('open','in_progress')
		ORDER BY CASE priority WHEN 'high' THEN 0 WHEN 'normal' THEN 1 ELSE 2 END, updated_at DESC
		LIMIT 30`, jid); err == nil {
		for rows.Next() {
			var t db.Task
			if err := scanDashTask(rows, &t); err == nil {
				d.TasksOpen = append(d.TasksOpen, t)
			}
		}
		rows.Close()
	}
	s.store.DB.QueryRow(`SELECT COUNT(*) FROM tasks WHERE assignee_jid = ? AND status = 'done'`, jid).
		Scan(&d.TasksDoneCount)

	// Recent DM activity.
	s.store.DB.QueryRow(`SELECT COUNT(*), COALESCE(MAX(timestamp),0) FROM messages WHERE chat_jid = ?`, jid).
		Scan(&d.MessageCount, &d.LastActive)
	d.Recent = s.recentDashMessages(jid, 5)

	jsonOK(w, d)
}

// handleGroupDashboard is the same for a group.
// GET /api/v2/groups/{jid}/dashboard
func (s *Server) handleGroupDashboard(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if s.notFoundIfHidden(w, r, jid) {
		return
	}
	d := &dashGroup{JID: jid, Circles: []db.Circle{}, TasksOpen: []db.Task{}, TopContributors: []dashContributor{}, Recent: []dashRecent{}}

	s.store.DB.QueryRow(`SELECT COALESCE(name,''), COALESCE(topic,''), COALESCE(participant_count,0)
		FROM groups WHERE jid = ?`, jid).Scan(&d.Name, &d.Topic, &d.ParticipantCount)
	if d.Name == "" {
		d.Name = jid
	}
	d.Profile, _ = s.store.GetProfile(db.ProfileGroup, jid)
	if c, err := s.store.GetCirclesForMember(db.MemberGroup, jid); err == nil && c != nil {
		d.Circles = c
	}

	// Tasks linked to this chat (origin or via task_messages).
	if rows, err := s.store.DB.Query(`SELECT DISTINCT `+taskColumnsRaw("t")+` FROM tasks t
		WHERE t.review_status='accepted' AND t.status IN ('open','in_progress') AND (
		  t.origin_chat_jid = ? OR
		  EXISTS (SELECT 1 FROM task_messages tm WHERE tm.task_id = t.id AND tm.chat_jid = ?)
		)
		ORDER BY CASE t.priority WHEN 'high' THEN 0 WHEN 'normal' THEN 1 ELSE 2 END, t.updated_at DESC
		LIMIT 30`, jid, jid); err == nil {
		for rows.Next() {
			var t db.Task
			if err := scanDashTask(rows, &t); err == nil {
				d.TasksOpen = append(d.TasksOpen, t)
			}
		}
		rows.Close()
	}
	s.store.DB.QueryRow(`SELECT COUNT(*) FROM tasks t
		WHERE t.status = 'done' AND (
		  t.origin_chat_jid = ? OR EXISTS (SELECT 1 FROM task_messages tm WHERE tm.task_id = t.id AND tm.chat_jid = ?)
		)`, jid, jid).Scan(&d.TasksDoneCount)

	s.store.DB.QueryRow(`SELECT COUNT(*), COALESCE(MAX(timestamp),0) FROM messages WHERE chat_jid = ?`, jid).
		Scan(&d.MessageCount, &d.LastActive)

	// Top contributors over the last 7 days.
	weekAgo := time.Now().Add(-7 * 24 * time.Hour).Unix()
	if rows, err := s.store.DB.Query(`SELECT m.sender,
		COALESCE(NULLIF(c.name,''), NULLIF(c.push_name,''), NULLIF(c.business_name,''),
		         NULLIF(m.sender_name,''), NULLIF(m.push_name,''), m.sender) AS who,
		COUNT(*) AS n
		FROM messages m
		LEFT JOIN contacts c ON c.jid = m.sender
		WHERE m.chat_jid = ? AND m.timestamp >= ? AND m.is_from_me = 0
		GROUP BY m.sender ORDER BY n DESC LIMIT 8`, jid, weekAgo); err == nil {
		// Pull admins once.
		adminSet := map[string]bool{}
		if arows, err := s.store.DB.Query(`SELECT jid FROM group_participants WHERE group_jid = ? AND (is_admin = 1 OR is_super_admin = 1)`, jid); err == nil {
			for arows.Next() {
				var aj string
				if arows.Scan(&aj) == nil {
					adminSet[aj] = true
				}
			}
			arows.Close()
		}
		for rows.Next() {
			var senderJID, name string
			var n int
			if rows.Scan(&senderJID, &name, &n) != nil {
				continue
			}
			d.TopContributors = append(d.TopContributors, dashContributor{
				JID: senderJID, Name: name, Messages: n, IsAdmin: adminSet[senderJID],
			})
		}
		rows.Close()
	}

	d.Recent = s.recentDashMessages(jid, 5)
	jsonOK(w, d)
}

func (s *Server) recentDashMessages(jid string, n int) []dashRecent {
	rows, err := s.store.DB.Query(`SELECT
		m.timestamp, m.is_from_me, m.sender,
		COALESCE(NULLIF(c.name,''), NULLIF(c.push_name,''), NULLIF(c.business_name,''),
		         NULLIF(m.sender_name,''), NULLIF(m.push_name,''), '') AS from_name,
		SUBSTR(COALESCE(m.content,''), 1, 180) AS body,
		COALESCE(m.media_type,'') AS media
		FROM messages m
		LEFT JOIN contacts c ON c.jid = m.sender
		WHERE m.chat_jid = ?
		ORDER BY m.timestamp DESC LIMIT ?`, jid, n)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []dashRecent{}
	for rows.Next() {
		var r dashRecent
		var fromMe bool
		var body, media string
		if rows.Scan(&r.Timestamp, &fromMe, &r.SenderJID, &r.From, &body, &media) != nil {
			continue
		}
		r.IsFromMe = fromMe
		r.Content = strings.ReplaceAll(strings.TrimSpace(body), "\n", " ")
		if r.Content == "" && media != "" {
			r.Content = "[" + media + "]"
		}
		out = append(out, r)
	}
	return out
}

// taskColumnsRaw returns the task columns expanded (optionally aliased) for
// dashboard queries that need to scan into a Task.
func taskColumnsRaw(alias ...string) string {
	a := ""
	if len(alias) > 0 && alias[0] != "" {
		a = alias[0] + "."
	}
	return a + "id, " + a + "title, " + a + "description, " + a + "status, " + a + "priority, " +
		a + "assignee_jid, " + a + "creator_jid, " + a + "due_at, " + a + "completed_at, " +
		a + "origin_chat_jid, " + a + "origin_message_id, " + a + "review_status, " +
		a + "parent_id, " + a + "created_at, " + a + "updated_at"
}

func scanDashTask(rows interface{ Scan(...any) error }, t *db.Task) error {
	var parent sql.NullInt64
	if err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.Status, &t.Priority, &t.AssigneeJID,
		&t.CreatorJID, &t.DueAt, &t.CompletedAt, &t.OriginChatJID, &t.OriginMessageID,
		&t.ReviewStatus, &parent, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return err
	}
	if parent.Valid {
		v := parent.Int64
		t.ParentID = &v
	}
	return nil
}

func jidUser(jid string) string {
	if i := strings.Index(jid, "@"); i > 0 {
		return jid[:i]
	}
	return jid
}
