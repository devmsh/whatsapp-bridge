package db

import (
	"database/sql"
	"strings"
	"time"
)

const (
	MUTranscript  = "transcript"
	MUDescription = "description"

	MUPending = "pending"
	MUOK      = "ok"
	MUError   = "error"
	MUSkipped = "skipped"
)

// MediaUnderstanding is one analysis row for one message.
type MediaUnderstanding struct {
	ChatJID     string `json:"chat_jid"`
	MessageID   string `json:"message_id"`
	Kind        string `json:"kind"`
	Content     string `json:"content"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
	Refined     int    `json:"refined,omitempty"` // 1 = transcript went through LLM refine pass
	GeneratedAt int64  `json:"generated_at"`
}

// GetMU returns the row for (chat,message,kind) or nil.
func (s *Store) GetMU(chatJID, messageID, kind string) (*MediaUnderstanding, error) {
	mu := &MediaUnderstanding{}
	err := s.DB.QueryRow(`SELECT chat_jid, message_id, kind, content, status, error, generated_at
		FROM media_understanding WHERE chat_jid = ? AND message_id = ? AND kind = ?`,
		chatJID, messageID, kind).
		Scan(&mu.ChatJID, &mu.MessageID, &mu.Kind, &mu.Content, &mu.Status, &mu.Error, &mu.GeneratedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return mu, err
}

// UpsertMU writes a row (insert or replace) for an analysis result.
func (s *Store) UpsertMU(mu *MediaUnderstanding) error {
	if mu.GeneratedAt == 0 {
		mu.GeneratedAt = time.Now().Unix()
	}
	_, err := s.DB.Exec(`INSERT INTO media_understanding
		(chat_jid, message_id, kind, content, status, error, refined, generated_at)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(chat_jid, message_id, kind) DO UPDATE SET
			content = excluded.content,
			status = excluded.status,
			error = excluded.error,
			refined = excluded.refined,
			generated_at = excluded.generated_at`,
		mu.ChatJID, mu.MessageID, mu.Kind, mu.Content, mu.Status, mu.Error, mu.Refined, mu.GeneratedAt)
	return err
}

// PendingTranscriptsToRefine returns transcript rows that have a usable raw
// transcript but were never sent through the LLM refinement pass. Used by the
// audio worker to backfill the refinement on existing rows without re-running
// whisper.
type RefineTarget struct {
	ChatJID   string
	MessageID string
	Raw       string
}

func (s *Store) PendingTranscriptsToRefine(limit int) []RefineTarget {
	if limit <= 0 {
		limit = 25
	}
	rows, err := s.DB.Query(`SELECT chat_jid, message_id, content
		FROM media_understanding
		WHERE kind = ? AND status = ? AND refined = 0 AND content != ''
		ORDER BY generated_at DESC LIMIT ?`, MUTranscript, MUOK, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []RefineTarget
	for rows.Next() {
		var t RefineTarget
		if rows.Scan(&t.ChatJID, &t.MessageID, &t.Raw) == nil {
			out = append(out, t)
		}
	}
	return out
}

// SetTranscriptRefined updates a transcript row with the refined text and
// flips its `refined` flag. Used by the worker after the LLM refinement.
func (s *Store) SetTranscriptRefined(chatJID, messageID, refined string) error {
	_, err := s.DB.Exec(
		`UPDATE media_understanding
		 SET content = ?, refined = 1, generated_at = ?
		 WHERE chat_jid = ? AND message_id = ? AND kind = ?`,
		refined, time.Now().Unix(), chatJID, messageID, MUTranscript,
	)
	return err
}

// MediaStats summarises analysis progress for the dashboard.
type MediaStats struct {
	AudioTotal       int `json:"audio_total"`
	AudioTranscribed int `json:"audio_transcribed"`
	AudioPending     int `json:"audio_pending"`
	AudioError       int `json:"audio_error"`
	ImageTotal       int `json:"image_total"`
	ImageDescribed   int `json:"image_described"`
	ImagePending     int `json:"image_pending"`
	ImageError       int `json:"image_error"`
}

// CountMedia returns counts by media type and analysis status. "Audio"
// includes both `audio` and `voice_note` — voice notes are the main use case
// on WhatsApp and the transcription worker treats them identically.
func (s *Store) CountMedia() MediaStats {
	var st MediaStats
	s.DB.QueryRow(`SELECT COUNT(*) FROM messages
		WHERE media_type IN ('audio','voice_note') AND media_path != ''`).Scan(&st.AudioTotal)
	s.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE media_type='image' AND media_path != ''`).Scan(&st.ImageTotal)

	s.DB.QueryRow(`SELECT COUNT(*) FROM media_understanding WHERE kind = ? AND status = ?`, MUTranscript, MUOK).Scan(&st.AudioTranscribed)
	s.DB.QueryRow(`SELECT COUNT(*) FROM media_understanding WHERE kind = ? AND status = ?`, MUTranscript, MUError).Scan(&st.AudioError)
	s.DB.QueryRow(`SELECT COUNT(*) FROM media_understanding WHERE kind = ? AND status = ?`, MUDescription, MUOK).Scan(&st.ImageDescribed)
	s.DB.QueryRow(`SELECT COUNT(*) FROM media_understanding WHERE kind = ? AND status = ?`, MUDescription, MUError).Scan(&st.ImageError)

	// pending = total media - already-processed rows (ok/error/skipped)
	var audioDone, imageDone int
	s.DB.QueryRow(`SELECT COUNT(*) FROM media_understanding WHERE kind = ? AND status IN ('ok','error','skipped')`, MUTranscript).Scan(&audioDone)
	s.DB.QueryRow(`SELECT COUNT(*) FROM media_understanding WHERE kind = ? AND status IN ('ok','error','skipped')`, MUDescription).Scan(&imageDone)
	st.AudioPending = st.AudioTotal - audioDone
	if st.AudioPending < 0 {
		st.AudioPending = 0
	}
	st.ImagePending = st.ImageTotal - imageDone
	if st.ImagePending < 0 {
		st.ImagePending = 0
	}
	return st
}

// PendingMediaMessage is what the worker pops off the queue.
type PendingMediaMessage struct {
	ChatJID   string
	MessageID string
	MediaPath string
	MediaType string // 'audio' | 'image'
}

// PendingMedia returns up to limit messages whose media has no analysis row yet
// (or where the row is still 'pending'). kind: 'audio' or 'image'.
// Audio includes both `audio` and WhatsApp's `voice_note` media type.
func (s *Store) PendingMedia(kind string, limit int) []PendingMediaMessage {
	if limit <= 0 {
		limit = 50
	}
	muKind := MUTranscript
	mediaTypes := []string{"audio", "voice_note"}
	if kind == "image" {
		muKind = MUDescription
		mediaTypes = []string{"image"}
	}
	args := []any{muKind}
	placeholders := make([]string, len(mediaTypes))
	for i, t := range mediaTypes {
		placeholders[i] = "?"
		args = append(args, t)
	}
	args = append(args, limit)
	rows, err := s.DB.Query(`SELECT m.chat_jid, m.id, m.media_path
		FROM messages m
		LEFT JOIN media_understanding mu
		  ON mu.chat_jid = m.chat_jid AND mu.message_id = m.id AND mu.kind = ?
		WHERE m.media_type IN (`+strings.Join(placeholders, ",")+`) AND m.media_path != ''
		  AND (mu.status IS NULL OR mu.status = 'pending' OR mu.status = 'error')
		ORDER BY m.timestamp DESC LIMIT ?`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []PendingMediaMessage
	for rows.Next() {
		var p PendingMediaMessage
		if rows.Scan(&p.ChatJID, &p.MessageID, &p.MediaPath) == nil {
			p.MediaType = kind
			out = append(out, p)
		}
	}
	return out
}
