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

// StoreChat upserts a chat record.
func (s *Store) StoreChat(c *Chat) error {
	_, err := s.DB.Exec(`INSERT OR REPLACE INTO chats (
		jid, name, chat_type, last_message_at, unread_count,
		is_archived, is_pinned, is_muted, muted_until, disappearing_timer
	) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		c.JID, c.Name, c.ChatType, c.LastMessageAt, c.UnreadCount,
		c.IsArchived, c.IsPinned, c.IsMuted, c.MutedUntil, c.DisappearingTimer,
	)
	return err
}

// GetChats returns all chats ordered by last message time.
func (s *Store) GetChats() ([]Chat, error) {
	rows, err := s.DB.Query(`SELECT jid, name, chat_type, last_message_at, unread_count,
		is_archived, is_pinned, is_muted, muted_until, disappearing_timer
		FROM chats ORDER BY last_message_at DESC`)
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
			last_message_at = MAX(chats.last_message_at, excluded.last_message_at)`,
		jid, name, ts,
	)
	return err
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
