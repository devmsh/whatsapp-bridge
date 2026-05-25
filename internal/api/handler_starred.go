package api

import (
	"net/http"

	"whatsapp-bridge-v2/internal/db"
)

// handleStarMessage flips the star on (chatJID, msgID). starred=true marks,
// false clears. Both are idempotent: re-starring just refreshes the
// starred_at; un-starring something that wasn't starred is a no-op.
func (s *Server) handleStarMessage(w http.ResponseWriter, r *http.Request, msgID, chatJID string, starred bool) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if chatJID == "" {
		// Allow the chat_jid in the body too — keeps the client free to
		// pick either form, matching the surrounding /messages/{id}/* shape.
		var req struct {
			ChatJID string `json:"chat_jid"`
		}
		_ = decodeJSON(r, &req)
		chatJID = req.ChatJID
	}
	if chatJID == "" {
		jsonError(w, 400, "chat_jid required")
		return
	}
	if !s.guardChatAccess(w, r, chatJID) {
		return
	}
	var err error
	if starred {
		err = s.store.StarMessage(chatJID, msgID)
	} else {
		err = s.store.UnstarMessage(chatJID, msgID)
	}
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, map[string]bool{"success": true, "starred": starred})
}

// handleStarredList returns every starred message with its full Message body
// + chat name attached, newest-first. The Starred panel uses this to render
// rows with a snippet + chat label so the user can find what they bookmarked.
//
// Hidden chats are skipped — their content shouldn't leak through a global
// panel that isn't behind the per-chat unlock flow.
func (s *Server) handleStarredList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	refs, err := s.store.ListStarred(500)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}

	// Build a small chat-name cache so we only hit the chats table once per
	// distinct chat the user has starred something from. Hidden membership
	// comes from the separate hidden_chats table.
	type starredOut struct {
		db.Message
		ChatName  string `json:"chat_name,omitempty"`
		StarredAt int64  `json:"starred_at"`
		IsStarred bool   `json:"is_starred"`
	}
	hidden := s.store.HiddenChatJIDs()
	chatNames := map[string]string{}
	getName := func(jid string) string {
		if name, ok := chatNames[jid]; ok {
			return name
		}
		c, _ := s.store.GetChat(jid)
		if c == nil {
			chatNames[jid] = ""
			return ""
		}
		chatNames[jid] = c.Name
		return c.Name
	}

	out := make([]starredOut, 0, len(refs))
	for _, ref := range refs {
		if hidden[ref.ChatJID] {
			// Privacy: starred items from hidden chats are filtered out here
			// rather than relying on the client to do it.
			continue
		}
		name := getName(ref.ChatJID)
		msg, err := s.store.GetMessage(ref.MessageID, ref.ChatJID)
		if err != nil || msg == nil {
			// The message may have been deleted / revoked / GC'd. Skip.
			continue
		}
		out = append(out, starredOut{
			Message:   *msg,
			ChatName:  name,
			StarredAt: ref.StarredAt,
			IsStarred: true,
		})
	}
	jsonOK(w, out)
}
