package mcp

import "strings"

// enrichedMedia is what loadMediaUnderstanding returns for a batch: per
// (chat_jid, message_id) the transcript (for voice notes) or description (for
// images). Used by the read tools to surface AI-derived text to the
// extraction agent so voice notes and images aren't invisible to it.
type enrichedMedia struct {
	Transcript  string
	Description string
}

// loadMediaUnderstanding fetches AI-derived text for a batch of messages in
// one query. Returns map keyed by chat_jid+"|"+message_id.
func (s *Server) loadMediaUnderstanding(refs []mediaRef) map[string]enrichedMedia {
	out := map[string]enrichedMedia{}
	if len(refs) == 0 {
		return out
	}
	// We can OR the per-message conditions; simpler to do one query per chat.
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
		rows, err := s.db.Query(
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

// mediaRef is just (chat, message) — kept tiny so the read tools can build
// the batch with whatever struct shape they already use.
type mediaRef struct{ ChatJID, MessageID string }

// mergeAIText returns what the extraction agent should see as the message's
// content. Rules:
//   - voice_note / audio: use the transcript if any, prefixed with a marker.
//     If the user also typed text (rare), keep it before the transcript.
//   - image:              if the user wrote a caption, that's the content;
//                         otherwise, expose the AI description with a marker.
//   - everything else:    untouched.
//
// The marker keeps the source clear so the agent can weigh AI text vs. a
// human-written caption.
func mergeAIText(content, mediaType string, em enrichedMedia) string {
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
