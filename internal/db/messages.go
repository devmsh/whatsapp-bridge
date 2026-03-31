package db

import "database/sql"

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

// GetMessages returns messages for a chat, ordered by timestamp descending.
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
		FROM messages WHERE chat_jid = ? AND timestamp > ? ORDER BY timestamp ASC LIMIT ?`
	rows, err := s.DB.Query(query, chatJID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
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

// MarkDeleted marks a message as deleted.
func (s *Store) MarkDeleted(id, chatJID, deletedBy string, deletedAt int64) error {
	_, err := s.DB.Exec(
		`UPDATE messages SET is_deleted = 1, deleted_at = ?, deleted_by = ? WHERE id = ? AND chat_jid = ?`,
		deletedAt, deletedBy, id, chatJID,
	)
	return err
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
