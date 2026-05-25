package db

import (
	"strings"
	"time"
)

// HideChat marks a chat JID as hidden. Idempotent.
func (s *Store) HideChat(jid string) error {
	_, err := s.DB.Exec(`INSERT OR IGNORE INTO hidden_chats (chat_jid, added_at) VALUES (?, ?)`,
		jid, time.Now().Unix())
	return err
}

// UnhideChat removes the hidden marker.
func (s *Store) UnhideChat(jid string) error {
	_, err := s.DB.Exec(`DELETE FROM hidden_chats WHERE chat_jid = ?`, jid)
	return err
}

// IsChatHidden reports whether jid is hidden.
func (s *Store) IsChatHidden(jid string) bool {
	var n int
	s.DB.QueryRow(`SELECT 1 FROM hidden_chats WHERE chat_jid = ?`, jid).Scan(&n)
	return n == 1
}

// HiddenChatJIDs returns the full set of hidden JIDs as a map for cheap lookups.
func (s *Store) HiddenChatJIDs() map[string]bool {
	out := map[string]bool{}
	rows, err := s.DB.Query(`SELECT chat_jid FROM hidden_chats`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var jid string
		if rows.Scan(&jid) == nil {
			out[jid] = true
		}
	}
	return out
}

// ChatJIDForMediaPath returns the chat JID that owns a stored media file, or
// "" if no message references that path. The media handler uses this to gate
// downloads through the hidden-chats guard — without it, anyone with the URL
// could fetch a file from a locked DM.
func (s *Store) ChatJIDForMediaPath(mediaPath string) string {
	var jid string
	s.DB.QueryRow(`SELECT chat_jid FROM messages WHERE media_path = ? LIMIT 1`, mediaPath).Scan(&jid)
	return jid
}

// HiddenChatJIDsList returns the same as a slice (handy for SQL IN clauses or
// for passing to the MCP layer).
func (s *Store) HiddenChatJIDsList() []string {
	rows, err := s.DB.Query(`SELECT chat_jid FROM hidden_chats ORDER BY chat_jid`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var jid string
		if rows.Scan(&jid) == nil {
			out = append(out, jid)
		}
	}
	return out
}

// SQLPlaceholders returns "?, ?, ..." with n placeholders. Used by callers that
// want to embed a hidden-chat filter in their own queries.
func SQLPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}

// HidePreview is what HidePreviewFor returns: counts of every AI-derived row
// that will be removed when this chat is hidden. UI uses this for a confirm
// dialog before the irreversible cleanup.
type HidePreview struct {
	JID                     string `json:"jid"`
	IsGroup                 bool   `json:"is_group"`
	TasksOriginatedHere     int    `json:"tasks_originated_here"`
	TasksLinked             int    `json:"tasks_linked"`
	TaskMessageLinks        int    `json:"task_message_links"`
	ProfileExists           bool   `json:"profile_exists"`
	MediaUnderstandingRows  int    `json:"media_understanding_rows"`
	ExtractionWatermarkSet  bool   `json:"extraction_watermark_set"`
	CircleMembershipCount   int    `json:"circle_membership_count"`
}

// HidePreviewFor returns a HidePreview without changing anything.
func (s *Store) HidePreviewFor(jid string) HidePreview {
	p := HidePreview{JID: jid, IsGroup: strings.HasSuffix(jid, "@g.us")}

	s.DB.QueryRow(`SELECT COUNT(*) FROM tasks WHERE origin_chat_jid = ?`, jid).Scan(&p.TasksOriginatedHere)
	s.DB.QueryRow(`SELECT COUNT(DISTINCT t.id) FROM tasks t
		JOIN task_messages tm ON tm.task_id = t.id
		WHERE tm.chat_jid = ? AND t.origin_chat_jid != ?`, jid, jid).Scan(&p.TasksLinked)
	s.DB.QueryRow(`SELECT COUNT(*) FROM task_messages WHERE chat_jid = ?`, jid).Scan(&p.TaskMessageLinks)

	profileType := ProfileContact
	if p.IsGroup {
		profileType = ProfileGroup
	}
	if pr, _ := s.GetProfile(profileType, jid); pr != nil {
		p.ProfileExists = true
	}

	s.DB.QueryRow(`SELECT COUNT(*) FROM media_understanding WHERE chat_jid = ?`, jid).Scan(&p.MediaUnderstandingRows)

	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM chat_extraction_state WHERE chat_jid = ?`, jid).Scan(&n)
	p.ExtractionWatermarkSet = n > 0

	s.DB.QueryRow(`SELECT COUNT(*) FROM circle_members
		WHERE member_type IN ('group','contact') AND member_ref = ?`, jid).Scan(&p.CircleMembershipCount)
	return p
}

// HideClearResult reports what HideChatAndClear actually removed.
type HideClearResult struct {
	HidePreview
	TasksDeleted        int `json:"tasks_deleted"` // tasks fully removed (origin or only-linked-here)
	TaskLinksDeleted    int `json:"task_links_deleted"`
	ProfileDeleted      bool `json:"profile_deleted"`
	MediaRowsDeleted    int  `json:"media_rows_deleted"`
	BriefingsDeleted    int  `json:"briefings_deleted"`    // all current briefings (they may reference the chat)
	CircleEdgesDeleted  int  `json:"circle_edges_deleted"`
	WatermarkDeleted    bool `json:"watermark_deleted"`
}

// HideChatAndClear marks the chat hidden AND removes every piece of AI-derived
// data about it: tasks originated here (and tasks whose ONLY links are here),
// per-task links to this chat, the chat's purpose profile, audio transcripts +
// image descriptions for its messages, the extraction watermark, and the chat's
// membership in any circle. All briefings are also deleted (they may include
// narratives referencing the chat). Extraction session files must be deleted
// out-of-band by the caller via sessions.mjs delete-for-chat.
func (s *Store) HideChatAndClear(jid string) (HideClearResult, error) {
	res := HideClearResult{HidePreview: s.HidePreviewFor(jid)}

	tx, err := s.DB.Begin()
	if err != nil {
		return res, err
	}
	commit := false
	defer func() {
		if !commit {
			tx.Rollback()
		}
	}()

	// 1) Tasks whose origin is this chat — delete entirely (cascades task_messages, task_circles).
	if r, e := tx.Exec(`DELETE FROM tasks WHERE origin_chat_jid = ?`, jid); e == nil {
		n, _ := r.RowsAffected()
		res.TasksDeleted += int(n)
	}
	// 2) Tasks whose ONLY links are to this chat (origin elsewhere now empty) — delete.
	if r, e := tx.Exec(`DELETE FROM tasks WHERE id IN (
		SELECT t.id FROM tasks t
		WHERE EXISTS (SELECT 1 FROM task_messages WHERE task_id = t.id AND chat_jid = ?)
		  AND NOT EXISTS (SELECT 1 FROM task_messages WHERE task_id = t.id AND chat_jid != ?)
	)`, jid, jid); e == nil {
		n, _ := r.RowsAffected()
		res.TasksDeleted += int(n)
	}
	// 3) For surviving cross-chat tasks: just delete their links to this chat.
	if r, e := tx.Exec(`DELETE FROM task_messages WHERE chat_jid = ?`, jid); e == nil {
		n, _ := r.RowsAffected()
		res.TaskLinksDeleted = int(n)
	}

	// 4) Profile.
	profileType := ProfileContact
	if res.IsGroup {
		profileType = ProfileGroup
	}
	if r, e := tx.Exec(`DELETE FROM entity_profiles WHERE entity_type = ? AND entity_ref = ?`,
		profileType, jid); e == nil {
		n, _ := r.RowsAffected()
		res.ProfileDeleted = n > 0
	}

	// 5) Media understanding rows for this chat.
	if r, e := tx.Exec(`DELETE FROM media_understanding WHERE chat_jid = ?`, jid); e == nil {
		n, _ := r.RowsAffected()
		res.MediaRowsDeleted = int(n)
	}

	// 6) Extraction watermark.
	if r, e := tx.Exec(`DELETE FROM chat_extraction_state WHERE chat_jid = ?`, jid); e == nil {
		n, _ := r.RowsAffected()
		res.WatermarkDeleted = n > 0
	}

	// 7) All briefings (their stored narratives may reference this chat). They
	//    regenerate cheaply on the next click. This is intentionally strict.
	if r, e := tx.Exec(`DELETE FROM briefings`); e == nil {
		n, _ := r.RowsAffected()
		res.BriefingsDeleted = int(n)
	}

	// 8) Circle memberships of this chat.
	if r, e := tx.Exec(`DELETE FROM circle_members WHERE member_type IN ('group','contact') AND member_ref = ?`, jid); e == nil {
		n, _ := r.RowsAffected()
		res.CircleEdgesDeleted = int(n)
	}

	// 9) Finally, mark hidden.
	if _, e := tx.Exec(`INSERT OR IGNORE INTO hidden_chats (chat_jid, added_at) VALUES (?, ?)`,
		jid, time.Now().Unix()); e != nil {
		return res, e
	}

	if e := tx.Commit(); e != nil {
		return res, e
	}
	commit = true
	return res, nil
}
