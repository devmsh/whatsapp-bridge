package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"whatsapp-bridge-v2/internal/db"
)

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	chatJID := r.URL.Query().Get("chat_jid")
	if chatJID == "" {
		jsonError(w, 400, "chat_jid required")
		return
	}
	if !s.guardChatAccess(w, r, chatJID) {
		return
	}

	since := int64(0)
	if v := r.URL.Query().Get("since"); v != "" {
		since, _ = strconv.ParseInt(v, 10, 64)
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil && l > 0 {
			limit = l
		}
	}

	// Resolve LID↔phone mapping so we merge messages from both JIDs.
	// WhatsApp's LID migration means the same DM conversation may be split
	// across a phone JID and a LID JID.
	chatJIDs := []string{chatJID}
	if lid := s.client.ResolveLIDForJID(chatJID); lid != "" {
		chatJIDs = append(chatJIDs, lid)
	} else if pn := s.client.ResolvePhoneForLID(chatJID); pn != "" {
		chatJIDs = append(chatJIDs, pn)
	}

	msgs, err := s.store.GetMessagesMerged(chatJIDs, since, limit)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}

	type enriched struct {
		db.Message
		Reactions []db.Reaction    `json:"reactions,omitempty"`
		ChatName  string           `json:"chat_name,omitempty"`
		Status    db.MessageStatus `json:"status,omitempty"`
	}

	chatName := ""
	chat, _ := s.store.GetChat(chatJID)
	if chat != nil {
		chatName = chat.Name
	}

	// Collect IDs of our own messages so we can resolve their tick status
	// (sent/delivered/read/played) from the receipts table in one query.
	myIDs := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if m.IsFromMe {
			myIDs = append(myIDs, m.ID)
		}
	}
	statuses, _ := s.store.GetMessageStatuses(chatJIDs, myIDs)

	var results []enriched
	for _, m := range msgs {
		e := enriched{Message: m, ChatName: chatName}
		reactions, _ := s.store.GetReactions(m.ID, m.ChatJID)
		if len(reactions) > 0 {
			e.Reactions = reactions
		}
		if m.IsFromMe {
			if st, ok := statuses[m.ID]; ok {
				e.Status = st
			} else {
				e.Status = db.StatusSent
			}
		}
		results = append(results, e)
	}
	if results == nil {
		results = []enriched{}
	}

	jsonOK(w, results)
}

func (s *Server) handleMessageByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v2/messages/")
	parts := strings.SplitN(path, "/", 2)
	msgID := parts[0]
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}

	chatJID := r.URL.Query().Get("chat_jid")

	switch sub {
	case "revoke":
		s.handleRevoke(w, r, msgID, chatJID)
	case "edit":
		s.handleEdit(w, r, msgID, chatJID)
	case "receipts":
		s.handleMessageReceipts(w, r, msgID, chatJID)
	default:
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if chatJID == "" {
			jsonError(w, 400, "chat_jid required")
			return
		}
		if !s.guardChatAccess(w, r, chatJID) {
			return
		}
		msg, err := s.store.GetMessage(msgID, chatJID)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		if msg == nil {
			jsonError(w, 404, "message not found")
			return
		}
		jsonOK(w, msg)
	}
}

func (s *Server) handleRevoke(w http.ResponseWriter, r *http.Request, msgID, chatJID string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if chatJID == "" {
		var req struct{ ChatJID string `json:"chat_jid"` }
		decodeJSON(r, &req)
		chatJID = req.ChatJID
	}
	if chatJID == "" {
		jsonError(w, 400, "chat_jid required")
		return
	}
	if !s.guardChatAccess(w, r, chatJID) {
		return
	}

	jid, err := parseJID(chatJID)
	if err != nil {
		jsonError(w, 400, "invalid chat_jid")
		return
	}

	wa := s.client.GetWhatsmeowClient()

	_, err = wa.RevokeMessage(context.Background(), jid, msgID)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("revoke failed: %v", err))
		return
	}

	s.store.MarkDeleted(msgID, chatJID, "self", time.Now().Unix())
	jsonOK(w, map[string]bool{"success": true})
}

func (s *Server) handleEdit(w http.ResponseWriter, r *http.Request, msgID, chatJID string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		ChatJID string `json:"chat_jid"`
		NewText string `json:"new_text"`
	}
	decodeJSON(r, &req)
	if req.ChatJID != "" {
		chatJID = req.ChatJID
	}
	if chatJID == "" || req.NewText == "" {
		jsonError(w, 400, "chat_jid and new_text required")
		return
	}
	if !s.guardChatAccess(w, r, chatJID) {
		return
	}

	jid, err := parseJID(chatJID)
	if err != nil {
		jsonError(w, 400, "invalid chat_jid")
		return
	}

	wa := s.client.GetWhatsmeowClient()

	newContent := &waE2E.Message{
		Conversation: proto.String(req.NewText),
	}
	editMsg := wa.BuildEdit(jid, msgID, newContent)

	_, err = wa.SendMessage(context.Background(), jid, editMsg)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("edit failed: %v", err))
		return
	}

	s.store.MarkEdited(msgID, chatJID, req.NewText, time.Now().Unix())
	jsonOK(w, map[string]bool{"success": true})
}

func (s *Server) handleMessageReceipts(w http.ResponseWriter, r *http.Request, msgID, chatJID string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if chatJID == "" {
		jsonError(w, 400, "chat_jid required")
		return
	}
	if !s.guardChatAccess(w, r, chatJID) {
		return
	}
	receipts, err := s.store.GetReceipts(msgID, chatJID)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	if receipts == nil {
		receipts = []db.Receipt{}
	}
	jsonOK(w, receipts)
}

func (s *Server) handleMarkRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		ChatJID    string   `json:"chat_jid"`
		MessageIDs []string `json:"message_ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}

	if !s.guardChatAccess(w, r, req.ChatJID) {
		return
	}
	wa := s.client.GetWhatsmeowClient()
	chatJID, err := parseJID(req.ChatJID)
	if err != nil {
		jsonError(w, 400, "invalid chat_jid")
		return
	}

	err = wa.MarkRead(context.Background(), req.MessageIDs, time.Now(), chatJID, chatJID)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("mark read failed: %v", err))
		return
	}

	jsonOK(w, map[string]bool{"success": true})
}

func (s *Server) handleUnread(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	chats, err := s.store.GetChats()
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	var unread []db.Chat
	for _, c := range chats {
		if c.UnreadCount > 0 {
			unread = append(unread, c)
		}
	}
	if unread == nil {
		unread = []db.Chat{}
	}
	jsonOK(w, unread)
}
