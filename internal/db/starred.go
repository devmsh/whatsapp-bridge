package db

import (
	"fmt"
	"strings"
	"time"
)

// StarredRef is one row in starred_messages — just (chat, message) and when.
// The full message body is fetched separately via GetMessage when needed.
type StarredRef struct {
	ChatJID   string `json:"chat_jid"`
	MessageID string `json:"message_id"`
	StarredAt int64  `json:"starred_at"`
}

// StarMessage marks a message as starred. INSERT OR REPLACE keeps the row
// idempotent — re-starring just refreshes starred_at (handy if the user
// re-stars a message to bump it to the top of the Starred panel).
func (s *Store) StarMessage(chatJID, messageID string) error {
	_, err := s.DB.Exec(
		`INSERT OR REPLACE INTO starred_messages (chat_jid, message_id, starred_at)
		 VALUES (?, ?, ?)`,
		chatJID, messageID, time.Now().Unix(),
	)
	return err
}

// UnstarMessage removes the star. No error when the row doesn't exist —
// callers treat star/unstar as idempotent toggles.
func (s *Store) UnstarMessage(chatJID, messageID string) error {
	_, err := s.DB.Exec(
		`DELETE FROM starred_messages WHERE chat_jid = ? AND message_id = ?`,
		chatJID, messageID,
	)
	return err
}

// GetStarredIDs returns the set of message IDs that are starred within the
// given chats. Used by the /messages handler to stamp each message with
// is_starred=true without an N+1 query.
func (s *Store) GetStarredIDs(chatJIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(chatJIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(chatJIDs))
	args := make([]interface{}, len(chatJIDs))
	for i, j := range chatJIDs {
		placeholders[i] = "?"
		args[i] = j
	}
	q := fmt.Sprintf(`SELECT message_id FROM starred_messages WHERE chat_jid IN (%s)`,
		strings.Join(placeholders, ","))
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return out, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ListStarred returns all starred refs, newest-first, capped at limit.
// The API enriches each ref with its full Message body before returning to
// the client — this keeps the table narrow and avoids duplicating message
// data.
func (s *Store) ListStarred(limit int) ([]StarredRef, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.DB.Query(
		`SELECT chat_jid, message_id, starred_at FROM starred_messages
		 ORDER BY starred_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StarredRef
	for rows.Next() {
		var r StarredRef
		if err := rows.Scan(&r.ChatJID, &r.MessageID, &r.StarredAt); err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
