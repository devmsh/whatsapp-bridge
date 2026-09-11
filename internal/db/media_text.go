package db

import (
	"database/sql"
	"strings"
)

// AI-derived text for media, shared by the MCP read tools and the extraction
// pipeline. It lives here because both need the identical answer: a voice note
// must read the same to the agent and to the extractor, or a task's evidence
// quote will not match the text it was drawn from.

// EnrichedMedia is what a message's media adds to its text.
type EnrichedMedia struct {
	Transcript  string
	Description string
}

// MediaRef is (chat, message) — deliberately tiny so callers can build the
// batch from whatever struct they already have.
type MediaRef struct{ ChatJID, MessageID string }

// LoadMediaUnderstanding fetches AI-derived text for a batch of messages,
// keyed by chat_jid + "|" + message_id.
func (s *Store) LoadMediaUnderstanding(refs []MediaRef) map[string]EnrichedMedia {
	return LoadMediaUnderstandingDB(s.DB, refs)
}

// LoadMediaUnderstandingDB is the same on a bare connection, for callers that
// hold one rather than a Store (the MCP server does). One query per chat.
func LoadMediaUnderstandingDB(conn *sql.DB, refs []MediaRef) map[string]EnrichedMedia {
	out := map[string]EnrichedMedia{}
	if len(refs) == 0 {
		return out
	}
	byChat := map[string][]string{}
	for _, r := range refs {
		byChat[r.ChatJID] = append(byChat[r.ChatJID], r.MessageID)
	}
	for chat, ids := range byChat {
		if len(ids) == 0 {
			continue
		}
		args := []any{chat}
		placeholders := make([]string, len(ids))
		for i, id := range ids {
			placeholders[i] = "?"
			args = append(args, id)
		}
		rows, err := conn.Query(
			`SELECT message_id, kind, content FROM media_understanding
			 WHERE chat_jid = ? AND status = 'ok'
			   AND message_id IN (`+strings.Join(placeholders, ",")+`)`,
			args...)
		if err != nil {
			continue
		}
		for rows.Next() {
			var id, kind, content string
			if rows.Scan(&id, &kind, &content) != nil {
				continue
			}
			key := chat + "|" + id
			em := out[key]
			switch kind {
			case "transcript":
				em.Transcript = content
			case "description":
				em.Description = content
			}
			out[key] = em
		}
		rows.Close()
	}
	return out
}

// MergeAIText returns what a reader should see as a message's content:
//
//   - voice note / audio: the transcript, marked, after any typed text
//   - image:              the description, marked, unless there is a caption
//   - anything else:      untouched
//
// The marker matters. It keeps AI text distinguishable from what a human
// actually wrote, so a model weighing evidence knows which is which.
func MergeAIText(content, mediaType string, em EnrichedMedia) string {
	content = strings.TrimSpace(content)
	switch mediaType {
	case "voice_note", "audio":
		if em.Transcript != "" {
			if content != "" {
				return content + "\n[transcript] " + em.Transcript
			}
			return "[transcript] " + em.Transcript
		}
	case "image":
		if em.Description != "" {
			if content != "" {
				return content + "\n[image] " + em.Description
			}
			return "[image] " + em.Description
		}
	}
	return content
}
