package db

// Reaction maps to the reactions table.
type Reaction struct {
	MessageID  string `json:"message_id"`
	ChatJID    string `json:"chat_jid"`
	Sender     string `json:"sender"`
	SenderName string `json:"sender_name"`
	Emoji      string `json:"emoji"`
	Timestamp  int64  `json:"timestamp"`
}

// StoreReaction upserts a reaction. An empty emoji means removal.
func (s *Store) StoreReaction(r *Reaction) error {
	if r.Emoji == "" {
		_, err := s.DB.Exec(
			`DELETE FROM reactions WHERE message_id = ? AND chat_jid = ? AND sender = ?`,
			r.MessageID, r.ChatJID, r.Sender,
		)
		return err
	}
	_, err := s.DB.Exec(`INSERT OR REPLACE INTO reactions (
		message_id, chat_jid, sender, sender_name, emoji, timestamp
	) VALUES (?,?,?,?,?,?)`,
		r.MessageID, r.ChatJID, r.Sender, r.SenderName, r.Emoji, r.Timestamp,
	)
	return err
}

// GetReactions returns all reactions for a message.
func (s *Store) GetReactions(messageID, chatJID string) ([]Reaction, error) {
	rows, err := s.DB.Query(
		`SELECT message_id, chat_jid, sender, sender_name, emoji, timestamp
		 FROM reactions WHERE message_id = ? AND chat_jid = ?`,
		messageID, chatJID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reactions []Reaction
	for rows.Next() {
		var r Reaction
		if err := rows.Scan(&r.MessageID, &r.ChatJID, &r.Sender, &r.SenderName, &r.Emoji, &r.Timestamp); err != nil {
			return reactions, err
		}
		reactions = append(reactions, r)
	}
	return reactions, rows.Err()
}
