package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"whatsapp-bridge-v2/internal/db"
)

// draftReply is one of N candidate replies generated for a chat.
type draftReply struct {
	Text   string `json:"text"`
	Style  string `json:"style,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// handleDraftReplies generates 2-3 candidate replies for a chat using the
// reply-assistant sidecar.
//
// POST /api/v2/chats/{jid}/draft-replies
//   -> { drafts: [{text, style, reason}], ... }
func (s *Server) handleDraftReplies(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if jid == "" {
		jsonError(w, 400, "jid required")
		return
	}
	isGroup := strings.HasSuffix(jid, "@g.us")
	kind := "contact"
	entityType := db.ProfileContact
	if isGroup {
		kind = "group"
		entityType = db.ProfileGroup
	}

	// Label for the prompt.
	var label string
	if isGroup {
		s.store.DB.QueryRow(`SELECT name FROM groups WHERE jid = ?`, jid).Scan(&label)
	} else {
		var name, push, biz string
		s.store.DB.QueryRow(`SELECT name, push_name, business_name FROM contacts WHERE jid = ?`, jid).
			Scan(&name, &push, &biz)
		for _, v := range []string{name, biz, push} {
			if strings.TrimSpace(v) != "" {
				label = v
				break
			}
		}
	}

	// Profile description (if present).
	var profile string
	if p, _ := s.store.GetProfile(entityType, jid); p != nil && p.Status == db.ProfileOK {
		profile = p.Description
	}

	// Last 30 messages (text only) chronologically, plus up to 8 of the user's
	// previous outgoing messages in this chat as tone samples.
	recent := s.recentChatLines(jid, 30)
	tone := s.recentMyTone(jid, 8)

	input := map[string]any{
		"kind":             kind,
		"chat_label":       label,
		"profile":          profile,
		"recent_messages":  recent,
		"my_recent_tone":   tone,
	}
	in, _ := json.Marshal(input)

	out, err := s.runAgentInput(2*time.Minute, string(in), "draft-reply.mjs")
	var res struct {
		OK     bool         `json:"ok"`
		Drafts []draftReply `json:"drafts"`
	}
	if line := lastJSONLine(out); line != nil {
		json.Unmarshal(line, &res)
	}
	if err != nil && !res.OK {
		jsonError(w, 500, "draft generation failed")
		return
	}
	if res.Drafts == nil {
		res.Drafts = []draftReply{}
	}
	jsonOK(w, map[string]any{"drafts": res.Drafts})
}

// recentMyTone returns up to n of the user's own recent outgoing messages in
// this chat (oldest→newest), as raw strings. Used as a tone sample so drafts
// match the user's voice with this specific contact/group.
func (s *Server) recentMyTone(jid string, n int) []string {
	rows, err := s.store.DB.Query(`SELECT SUBSTR(COALESCE(content,''), 1, 280) AS body
		FROM messages WHERE chat_jid = ? AND is_from_me = 1 AND COALESCE(content,'') != ''
		ORDER BY timestamp DESC LIMIT ?`, jid, n)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var body string
		if rows.Scan(&body) != nil {
			continue
		}
		body = strings.ReplaceAll(strings.TrimSpace(body), "\n", " ")
		if body == "" {
			continue
		}
		out = append(out, body)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
