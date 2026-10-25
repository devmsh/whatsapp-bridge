package api

import (
	"net/http"

	"whatsapp-bridge-v2/internal/db"
)

// handleMentionsList returns every message where the user was @-mentioned,
// newest first, each with a little context: the message right before it and
// right after it in that chat — or two messages before, when the mention is
// the last message in the chat (there's nothing after it yet).
//
// Same privacy rule as Starred: hidden chats are filtered out here, not left
// to the client. Dismissed mentions are filtered out too.
func (s *Server) handleMentionsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	pats := s.client.SelfMentionPatterns()
	mentions, err := s.store.ListSelfMentions(pats, 200)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}

	hidden := s.store.HiddenChatJIDs()
	dismissed, _ := s.store.DismissedMentionIDs()

	type mentionOut struct {
		db.Message
		ChatName      string       `json:"chat_name,omitempty"`
		ContextBefore []db.Message `json:"context_before,omitempty"`
		ContextAfter  []db.Message `json:"context_after,omitempty"`
	}

	// Same chat-name lookup chain (chats → groups → contacts) as the Starred
	// panel, cached per chat since one chat often has several mentions.
	chatNames := map[string]string{}
	getName := func(jid string) string {
		if name, ok := chatNames[jid]; ok {
			return name
		}
		var name string
		if c, _ := s.store.GetChat(jid); c != nil && c.Name != "" && c.Name != jid {
			name = c.Name
		}
		if name == "" {
			if g, _ := s.store.GetGroup(jid); g != nil && g.Name != "" {
				name = g.Name
			}
		}
		if name == "" {
			if k, _ := s.store.GetContact(jid); k != nil {
				if k.Name != "" {
					name = k.Name
				} else if k.PushName != "" {
					name = k.PushName
				}
			}
		}
		chatNames[jid] = name
		return name
	}

	out := make([]mentionOut, 0, len(mentions))
	for _, m := range mentions {
		if hidden[m.ChatJID] || dismissed[m.ID] || s.store.IsChatDeleted(m.ChatJID) {
			continue
		}
		before, after, err := s.store.GetMessagesAround(m.ChatJID, m.Timestamp, m.RowID, 1, 1)
		if err != nil {
			continue
		}
		if len(after) == 0 {
			// Nothing after the mention — it's the last message in the chat.
			// Use two before instead, so the card still shows 3 messages.
			before, _, err = s.store.GetMessagesAround(m.ChatJID, m.Timestamp, m.RowID, 2, 0)
			if err != nil {
				continue
			}
		}
		out = append(out, mentionOut{
			Message:       m.Message,
			ChatName:      getName(m.ChatJID),
			ContextBefore: before,
			ContextAfter:  after,
		})
	}
	jsonOK(w, out)
}

// handleMentionDismiss marks a mention dismissed so it drops off the
// Mentions page without needing a reply. Pass "dismissed": false to undo —
// the same reversible-toggle shape as star/unstar.
func (s *Server) handleMentionDismiss(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		ChatJID   string `json:"chat_jid"`
		MessageID string `json:"message_id"`
		Dismissed *bool  `json:"dismissed"`
	}
	if err := decodeJSON(r, &req); err != nil || req.ChatJID == "" || req.MessageID == "" {
		jsonError(w, 400, "chat_jid and message_id required")
		return
	}
	dismiss := req.Dismissed == nil || *req.Dismissed
	var err error
	if dismiss {
		err = s.store.DismissMention(req.ChatJID, req.MessageID)
	} else {
		err = s.store.UndismissMention(req.ChatJID, req.MessageID)
	}
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, map[string]bool{"success": true, "dismissed": dismiss})
}
