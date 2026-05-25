package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// Message maps to the messages table.
type Message struct {
	ID              string  `json:"id"`
	ChatJID         string  `json:"chat_jid"`
	Sender          string  `json:"sender"`
	SenderName      string  `json:"sender_name"`
	PushName        string  `json:"push_name"`
	Content         string  `json:"content"`
	Timestamp       int64   `json:"timestamp"`
	IsFromMe        bool    `json:"is_from_me"`
	IsGroup         bool    `json:"is_group"`
	MessageType     string  `json:"message_type"`
	DeviceID        string  `json:"device_id"`
	IsEphemeral     bool    `json:"is_ephemeral"`
	IsViewOnce      bool    `json:"is_view_once"`
	IsForwarded     bool    `json:"is_forwarded"`
	ForwardScore    int     `json:"forward_score"`
	IsEdit          bool    `json:"is_edit"`
	EditTimestamp   int64   `json:"edit_timestamp,omitempty"`
	OriginalID      string  `json:"original_id,omitempty"`
	IsDeleted       bool    `json:"is_deleted"`
	DeletedAt       int64   `json:"deleted_at,omitempty"`
	DeletedBy       string  `json:"deleted_by,omitempty"`
	MediaType       string  `json:"media_type,omitempty"`
	MediaPath       string  `json:"media_path,omitempty"`
	MediaMime       string  `json:"media_mime,omitempty"`
	MediaSize       int     `json:"media_size,omitempty"`
	MediaCaption    string  `json:"media_caption,omitempty"`
	MediaFilename   string  `json:"media_filename,omitempty"`
	ThumbnailPath   string  `json:"thumbnail_path,omitempty"`
	ReplyToID       string  `json:"reply_to_id,omitempty"`
	ReplyToSender   string  `json:"reply_to_sender,omitempty"`
	ReplyToContent  string  `json:"reply_to_content,omitempty"`
	Mentions        string  `json:"mentions,omitempty"`
	Latitude        float64 `json:"latitude,omitempty"`
	Longitude       float64 `json:"longitude,omitempty"`
	LocationName    string  `json:"location_name,omitempty"`
	LocationAddress string  `json:"location_address,omitempty"`
	VCardName       string  `json:"vcard_name,omitempty"`
	VCardData       string  `json:"vcard_data,omitempty"`
	PollID          string  `json:"poll_id,omitempty"`
	StickerPack     string  `json:"sticker_pack,omitempty"`
	BroadcastListJID string `json:"broadcast_list_jid,omitempty"`

	// AI-derived (media_understanding):
	Transcript      string `json:"transcript,omitempty"`         // voice note text
	MediaDescription string `json:"media_description,omitempty"`  // image caption
}

// AttachMediaUnderstanding fills Transcript and MediaDescription on the
// provided messages from the media_understanding table in one query. Used to
// enrich GetMessages results without re-shaping the main SELECT.
func (s *Store) AttachMediaUnderstanding(chatJID string, msgs []Message) {
	if len(msgs) == 0 {
		return
	}
	rows, err := s.DB.Query(
		`SELECT message_id, kind, content FROM media_understanding
		 WHERE chat_jid = ? AND status = 'ok'`, chatJID)
	if err != nil {
		return
	}
	defer rows.Close()
	type mu struct{ Transcript, Description string }
	byID := map[string]*mu{}
	for rows.Next() {
		var id, kind, content string
		if rows.Scan(&id, &kind, &content) != nil {
			continue
		}
		row := byID[id]
		if row == nil {
			row = &mu{}
			byID[id] = row
		}
		switch kind {
		case MUTranscript:
			row.Transcript = content
		case MUDescription:
			row.Description = content
		}
	}
	for i := range msgs {
		if row, ok := byID[msgs[i].ID]; ok {
			msgs[i].Transcript = row.Transcript
			msgs[i].MediaDescription = row.Description
		}
	}
}

// StoreMessage upserts a message into the database.
func (s *Store) StoreMessage(m *Message) error {
	_, err := s.DB.Exec(`INSERT OR REPLACE INTO messages (
		id, chat_jid, sender, sender_name, push_name, content, timestamp,
		is_from_me, is_group, message_type, device_id,
		is_ephemeral, is_view_once, is_forwarded, forward_score,
		is_edit, edit_timestamp, original_id,
		is_deleted, deleted_at, deleted_by,
		media_type, media_path, media_mime, media_size, media_caption, media_filename, thumbnail_path,
		reply_to_id, reply_to_sender, reply_to_content, mentions,
		latitude, longitude, location_name, location_address,
		vcard_name, vcard_data, poll_id, sticker_pack, broadcast_list_jid
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.ChatJID, m.Sender, m.SenderName, m.PushName, m.Content, m.Timestamp,
		m.IsFromMe, m.IsGroup, m.MessageType, m.DeviceID,
		m.IsEphemeral, m.IsViewOnce, m.IsForwarded, m.ForwardScore,
		m.IsEdit, m.EditTimestamp, m.OriginalID,
		m.IsDeleted, m.DeletedAt, m.DeletedBy,
		m.MediaType, m.MediaPath, m.MediaMime, m.MediaSize, m.MediaCaption, m.MediaFilename, m.ThumbnailPath,
		m.ReplyToID, m.ReplyToSender, m.ReplyToContent, m.Mentions,
		m.Latitude, m.Longitude, m.LocationName, m.LocationAddress,
		m.VCardName, m.VCardData, m.PollID, m.StickerPack, m.BroadcastListJID,
	)
	return err
}

// GetMessages returns messages for a chat, ordered by timestamp descending (latest first),
// then reversed to ascending order for the caller.
func (s *Store) GetMessages(chatJID string, since int64, limit int) ([]Message, error) {
	query := `SELECT id, chat_jid, sender, sender_name, push_name, content, timestamp,
		is_from_me, is_group, message_type, device_id,
		is_ephemeral, is_view_once, is_forwarded, forward_score,
		is_edit, edit_timestamp, original_id,
		is_deleted, deleted_at, deleted_by,
		media_type, media_path, media_mime, media_size, media_caption, media_filename, thumbnail_path,
		reply_to_id, reply_to_sender, reply_to_content, mentions,
		latitude, longitude, location_name, location_address,
		vcard_name, vcard_data, poll_id, sticker_pack, broadcast_list_jid
		FROM messages WHERE chat_jid = ? AND timestamp > ? ORDER BY timestamp DESC LIMIT ?`
	rows, err := s.DB.Query(query, chatJID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	msgs, err := scanMessages(rows)
	if err != nil {
		return nil, err
	}
	// Reverse to return in ascending (chronological) order
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	s.AttachMediaUnderstanding(chatJID, msgs)
	return msgs, nil
}

// GetMessagesMerged returns messages for a chat, merging results from multiple
// chat JIDs (e.g. phone JID + LID) into a single timeline. This handles
// WhatsApp's LID migration where the same conversation may be split across JIDs.
func (s *Store) GetMessagesMerged(chatJIDs []string, since int64, limit int) ([]Message, error) {
	if len(chatJIDs) == 0 {
		return nil, nil
	}
	if len(chatJIDs) == 1 {
		return s.GetMessages(chatJIDs[0], since, limit)
	}
	placeholders := make([]string, len(chatJIDs))
	args := make([]interface{}, 0, len(chatJIDs)+2)
	for i, jid := range chatJIDs {
		placeholders[i] = "?"
		args = append(args, jid)
	}
	args = append(args, since, limit)

	query := fmt.Sprintf(`SELECT id, chat_jid, sender, sender_name, push_name, content, timestamp,
		is_from_me, is_group, message_type, device_id,
		is_ephemeral, is_view_once, is_forwarded, forward_score,
		is_edit, edit_timestamp, original_id,
		is_deleted, deleted_at, deleted_by,
		media_type, media_path, media_mime, media_size, media_caption, media_filename, thumbnail_path,
		reply_to_id, reply_to_sender, reply_to_content, mentions,
		latitude, longitude, location_name, location_address,
		vcard_name, vcard_data, poll_id, sticker_pack, broadcast_list_jid
		FROM messages WHERE chat_jid IN (%s) AND timestamp > ? ORDER BY timestamp DESC LIMIT ?`,
		strings.Join(placeholders, ","))

	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	msgs, err := scanMessages(rows)
	if err != nil {
		return nil, err
	}
	// Reverse to return in ascending (chronological) order
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	// Enrich each contributing chat's media understanding in one pass per JID.
	for _, jid := range chatJIDs {
		s.AttachMediaUnderstanding(jid, msgs)
	}
	return msgs, nil
}

// GetMessage returns a single message by ID and chat JID.
func (s *Store) GetMessage(id, chatJID string) (*Message, error) {
	row := s.DB.QueryRow(`SELECT id, chat_jid, sender, sender_name, push_name, content, timestamp,
		is_from_me, is_group, message_type, device_id,
		is_ephemeral, is_view_once, is_forwarded, forward_score,
		is_edit, edit_timestamp, original_id,
		is_deleted, deleted_at, deleted_by,
		media_type, media_path, media_mime, media_size, media_caption, media_filename, thumbnail_path,
		reply_to_id, reply_to_sender, reply_to_content, mentions,
		latitude, longitude, location_name, location_address,
		vcard_name, vcard_data, poll_id, sticker_pack, broadcast_list_jid
		FROM messages WHERE id = ? AND chat_jid = ?`, id, chatJID)
	m := &Message{}
	err := scanMessage(row, m)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

// ChatPreview is a compact view of a chat's most recent message, for the
// chat-list second line (like WhatsApp's preview).
type ChatPreview struct {
	ChatJID      string `json:"chat_jid"`
	Sender       string `json:"sender"`
	SenderName   string `json:"sender_name"`
	PushName     string `json:"push_name"`
	Content      string `json:"content"`
	MediaType    string `json:"media_type"`
	MediaCaption string `json:"media_caption"`
	IsFromMe     bool   `json:"is_from_me"`
	IsGroup      bool   `json:"is_group"`
	IsDeleted    bool   `json:"is_deleted"`
	Timestamp    int64  `json:"timestamp"`
}

// GetChatPreviews returns the latest message per chat, keyed by chat JID.
func (s *Store) GetChatPreviews() (map[string]ChatPreview, error) {
	rows, err := s.DB.Query(`SELECT m.chat_jid, m.sender, m.sender_name, m.push_name, m.content,
		m.media_type, m.media_caption, m.is_from_me, m.is_group, m.is_deleted, m.timestamp
		FROM messages m
		JOIN (SELECT chat_jid, MAX(timestamp) AS mt FROM messages GROUP BY chat_jid) l
		  ON m.chat_jid = l.chat_jid AND m.timestamp = l.mt`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]ChatPreview)
	for rows.Next() {
		var p ChatPreview
		if err := rows.Scan(&p.ChatJID, &p.Sender, &p.SenderName, &p.PushName, &p.Content,
			&p.MediaType, &p.MediaCaption, &p.IsFromMe, &p.IsGroup, &p.IsDeleted, &p.Timestamp); err != nil {
			return out, err
		}
		out[p.ChatJID] = p
	}
	return out, rows.Err()
}

// MarkDeleted marks a message as deleted.
func (s *Store) MarkDeleted(id, chatJID, deletedBy string, deletedAt int64) error {
	_, err := s.DB.Exec(
		`UPDATE messages SET is_deleted = 1, deleted_at = ?, deleted_by = ? WHERE id = ? AND chat_jid = ?`,
		deletedAt, deletedBy, id, chatJID,
	)
	return err
}

// MarkChatMessagesDeleted bulk-marks every non-deleted message in a chat as
// deleted. Used when the user clears a chat from another device (DeleteChat
// app-state event). Returns the count of rows affected.
func (s *Store) MarkChatMessagesDeleted(chatJID, deletedBy string, deletedAt int64) (int64, error) {
	res, err := s.DB.Exec(
		`UPDATE messages SET is_deleted = 1, deleted_at = ?, deleted_by = ?
		 WHERE chat_jid = ? AND is_deleted = 0`,
		deletedAt, deletedBy, chatJID,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// MarkEdited stores an edit of a message.
func (s *Store) MarkEdited(id, chatJID, newContent string, editTS int64) error {
	_, err := s.DB.Exec(
		`UPDATE messages SET content = ?, is_edit = 1, edit_timestamp = ? WHERE id = ? AND chat_jid = ?`,
		newContent, editTS, id, chatJID,
	)
	return err
}

func scanMessages(rows *sql.Rows) ([]Message, error) {
	var msgs []Message
	for rows.Next() {
		var m Message
		if err := scanRow(rows, &m); err != nil {
			return msgs, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanMessage(row *sql.Row, m *Message) error {
	return row.Scan(
		&m.ID, &m.ChatJID, &m.Sender, &m.SenderName, &m.PushName, &m.Content, &m.Timestamp,
		&m.IsFromMe, &m.IsGroup, &m.MessageType, &m.DeviceID,
		&m.IsEphemeral, &m.IsViewOnce, &m.IsForwarded, &m.ForwardScore,
		&m.IsEdit, &m.EditTimestamp, &m.OriginalID,
		&m.IsDeleted, &m.DeletedAt, &m.DeletedBy,
		&m.MediaType, &m.MediaPath, &m.MediaMime, &m.MediaSize, &m.MediaCaption, &m.MediaFilename, &m.ThumbnailPath,
		&m.ReplyToID, &m.ReplyToSender, &m.ReplyToContent, &m.Mentions,
		&m.Latitude, &m.Longitude, &m.LocationName, &m.LocationAddress,
		&m.VCardName, &m.VCardData, &m.PollID, &m.StickerPack, &m.BroadcastListJID,
	)
}

func scanRow(rows *sql.Rows, m *Message) error {
	return rows.Scan(
		&m.ID, &m.ChatJID, &m.Sender, &m.SenderName, &m.PushName, &m.Content, &m.Timestamp,
		&m.IsFromMe, &m.IsGroup, &m.MessageType, &m.DeviceID,
		&m.IsEphemeral, &m.IsViewOnce, &m.IsForwarded, &m.ForwardScore,
		&m.IsEdit, &m.EditTimestamp, &m.OriginalID,
		&m.IsDeleted, &m.DeletedAt, &m.DeletedBy,
		&m.MediaType, &m.MediaPath, &m.MediaMime, &m.MediaSize, &m.MediaCaption, &m.MediaFilename, &m.ThumbnailPath,
		&m.ReplyToID, &m.ReplyToSender, &m.ReplyToContent, &m.Mentions,
		&m.Latitude, &m.Longitude, &m.LocationName, &m.LocationAddress,
		&m.VCardName, &m.VCardData, &m.PollID, &m.StickerPack, &m.BroadcastListJID,
	)
}
