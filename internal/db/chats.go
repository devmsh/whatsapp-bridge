package db

// Chat maps to the chats table.
type Chat struct {
	JID              string `json:"jid"`
	Name             string `json:"name"`
	ChatType         string `json:"chat_type"`
	LastMessageAt    int64  `json:"last_message_at"`
	UnreadCount      int    `json:"unread_count"`
	IsArchived       bool   `json:"is_archived"`
	IsPinned         bool   `json:"is_pinned"`
	IsMuted          bool   `json:"is_muted"`
	MutedUntil       int64  `json:"muted_until,omitempty"`
	DisappearingTimer int64 `json:"disappearing_timer,omitempty"`
}

// StoreChat writes a COMPLETE chat record — every field on the struct lands in
// the row, so a zero value really does mean "not pinned", "not archived".
// Callers that only know one field must not use it: see SetChatUnread.
//
// It used to be INSERT OR REPLACE, which quietly wiped every column the caller
// left at its zero value. Marking a chat read passes only a JID and an unread
// count, so each read unpinned the chat, unarchived it, unmuted it and zeroed
// its last-message time — which is also how chats ended up sorting to the
// bottom of the list with no timestamp at all.
//
// deleted_at is left out on purpose. It is the bridge's own state, not
// something a WhatsApp sync has an opinion about, so an upsert from history
// sync must never resurrect a chat you deleted.
func (s *Store) StoreChat(c *Chat) error {
	_, err := s.DB.Exec(`INSERT INTO chats (
		jid, name, chat_type, last_message_at, unread_count,
		is_archived, is_pinned, is_muted, muted_until, disappearing_timer
	) VALUES (?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(jid) DO UPDATE SET
		name               = excluded.name,
		chat_type          = excluded.chat_type,
		last_message_at    = excluded.last_message_at,
		unread_count       = excluded.unread_count,
		is_archived        = excluded.is_archived,
		is_pinned          = excluded.is_pinned,
		is_muted           = excluded.is_muted,
		muted_until        = excluded.muted_until,
		disappearing_timer = excluded.disappearing_timer`,
		c.JID, c.Name, c.ChatType, c.LastMessageAt, c.UnreadCount,
		c.IsArchived, c.IsPinned, c.IsMuted, c.MutedUntil, c.DisappearingTimer,
	)
	return err
}

// SetChatUnread sets just the unread count, leaving every other flag alone.
// This is what "mark as read" needs — it knows nothing about pins or archives
// and must not overwrite them.
func (s *Store) SetChatUnread(jid string, unread int) error {
	_, err := s.DB.Exec(`INSERT INTO chats (jid, unread_count) VALUES (?, ?)
		ON CONFLICT(jid) DO UPDATE SET unread_count = excluded.unread_count`,
		jid, unread)
	return err
}

// GetChats returns the chats worth showing, ordered with pinned chats first
// (matching WhatsApp's chat-list rule), then by most-recent activity.
//
// Two kinds never come back. Deleted chats: you removed them in WhatsApp, so
// they are gone from the list there too. Empty chats: rows created by contact
// and group syncs for conversations that never had a single message. There
// were 518 of those against 628 real ones — half the list was noise on a
// bridge that exists for work.
func (s *Store) GetChats() ([]Chat, error) {
	rows, err := s.DB.Query(`SELECT jid, name, chat_type, last_message_at, unread_count,
		is_archived, is_pinned, is_muted, muted_until, disappearing_timer
		FROM chats c
		WHERE deleted_at = 0
		  AND EXISTS (SELECT 1 FROM messages m WHERE m.chat_jid = c.jid)
		ORDER BY is_pinned DESC, last_message_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chats []Chat
	for rows.Next() {
		var c Chat
		if err := rows.Scan(&c.JID, &c.Name, &c.ChatType, &c.LastMessageAt, &c.UnreadCount,
			&c.IsArchived, &c.IsPinned, &c.IsMuted, &c.MutedUntil, &c.DisappearingTimer); err != nil {
			return chats, err
		}
		chats = append(chats, c)
	}
	return chats, rows.Err()
}

// GetChat returns a single chat by JID.
func (s *Store) GetChat(jid string) (*Chat, error) {
	row := s.DB.QueryRow(`SELECT jid, name, chat_type, last_message_at, unread_count,
		is_archived, is_pinned, is_muted, muted_until, disappearing_timer
		FROM chats WHERE jid = ?`, jid)
	c := &Chat{}
	err := row.Scan(&c.JID, &c.Name, &c.ChatType, &c.LastMessageAt, &c.UnreadCount,
		&c.IsArchived, &c.IsPinned, &c.IsMuted, &c.MutedUntil, &c.DisappearingTimer)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// UpdateChatLastMessage updates a chat's last message timestamp and name.
func (s *Store) UpdateChatLastMessage(jid, name string, ts int64) error {
	_, err := s.DB.Exec(`INSERT INTO chats (jid, name, last_message_at) VALUES (?, ?, ?)
		ON CONFLICT(jid) DO UPDATE SET
			name = CASE WHEN excluded.name != '' THEN excluded.name ELSE chats.name END,
			last_message_at = MAX(chats.last_message_at, excluded.last_message_at),
			-- A message newer than the deletion brings the chat back, exactly
			-- as WhatsApp does. Older backfill must not resurrect it.
			deleted_at = CASE WHEN excluded.last_message_at > chats.deleted_at
			                  THEN 0 ELSE chats.deleted_at END`,
		jid, name, ts,
	)
	return err
}

// MarkChatDeleted records that the user deleted the whole chat (exit + delete a
// group, or delete a conversation) on another device. WhatsApp drops such a
// chat from the list entirely, so we hide it rather than showing it full of
// deleted-message placeholders. Rows are kept, so nothing is lost; a message
// newer than ts brings the chat back, which is WhatsApp's behaviour too.
func (s *Store) MarkChatDeleted(chatJID string, ts int64) error {
	_, err := s.DB.Exec(
		`INSERT INTO chats (jid, deleted_at) VALUES (?, ?)
		 ON CONFLICT(jid) DO UPDATE SET deleted_at = excluded.deleted_at`,
		chatJID, ts,
	)
	return err
}

// IsChatDeleted reports whether the chat was deleted outright.
func (s *Store) IsChatDeleted(chatJID string) bool {
	var n int64
	s.DB.QueryRow(`SELECT deleted_at FROM chats WHERE jid = ?`, chatJID).Scan(&n)
	return n > 0
}

// SetChatArchived sets the archived flag on a chat.
func (s *Store) SetChatArchived(jid string, archived bool) error {
	_, err := s.DB.Exec(`UPDATE chats SET is_archived = ? WHERE jid = ?`, archived, jid)
	return err
}

// SetChatPinned sets the pinned flag on a chat.
func (s *Store) SetChatPinned(jid string, pinned bool) error {
	_, err := s.DB.Exec(`UPDATE chats SET is_pinned = ? WHERE jid = ?`, pinned, jid)
	return err
}

// SetChatMuted sets the muted flag and muted_until on a chat.
func (s *Store) SetChatMuted(jid string, muted bool, until int64) error {
	_, err := s.DB.Exec(`UPDATE chats SET is_muted = ?, muted_until = ? WHERE jid = ?`, muted, until, jid)
	return err
}
