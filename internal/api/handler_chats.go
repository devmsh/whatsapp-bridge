package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"

	"whatsapp-bridge-v2/internal/db"
)

func (s *Server) handleChats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	chats, err := s.store.GetChats()
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}

	previews, _ := s.store.GetChatPreviews()
	// Hidden chats: include only if the request is unlocked.
	hidden := map[string]bool{}
	if !s.isUnlocked(r) {
		hidden = s.store.HiddenChatJIDs()
	}

	type chatWithPreview struct {
		db.Chat
		LastMessage *db.ChatPreview `json:"last_message,omitempty"`
		IsHidden    bool            `json:"is_hidden,omitempty"`
	}
	out := make([]chatWithPreview, 0, len(chats))
	for _, c := range chats {
		if hidden[c.JID] {
			continue
		}
		row := chatWithPreview{Chat: c}
		if s.store.IsChatHidden(c.JID) {
			row.IsHidden = true // unlocked: still mark them so the UI can show the lock icon
		}
		if p, ok := previews[c.JID]; ok {
			pv := p
			row.LastMessage = &pv
		}
		out = append(out, row)
	}
	jsonOK(w, out)
}

func (s *Server) handleChatByJID(w http.ResponseWriter, r *http.Request) {
	// /api/v2/chats/{jid} or /api/v2/chats/{jid}/action or /api/v2/chats/{jid}/disappearing
	path := strings.TrimPrefix(r.URL.Path, "/api/v2/chats/")
	parts := strings.SplitN(path, "/", 2)
	jid := parts[0]
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}

	switch sub {
	case "action":
		s.handleChatAction(w, r, jid)
	case "disappearing":
		s.handleChatDisappearing(w, r, jid)
	case "draft-replies":
		s.handleDraftReplies(w, r, jid)
	case "hide":
		s.handleChatHide(w, r, jid)
	case "hide-preview":
		s.handleChatHidePreview(w, r, jid)
	case "unhide":
		s.handleChatUnhide(w, r, jid)
	default:
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		chat, err := s.store.GetChat(jid)
		if err != nil {
			jsonError(w, 404, "chat not found")
			return
		}
		jsonOK(w, chat)
	}
}

func (s *Server) handleChatAction(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Action   string `json:"action"`
		Duration int64  `json:"duration,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}

	wa := s.client.GetWhatsmeowClient()
	chatJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid jid")
		return
	}

	switch req.Action {
	case "archive":
		wa.SendAppState(context.Background(), appstate.BuildArchive(chatJID, true, time.Now(), nil))
		s.store.SetChatArchived(jid, true)
	case "unarchive":
		wa.SendAppState(context.Background(), appstate.BuildArchive(chatJID, false, time.Now(), nil))
		s.store.SetChatArchived(jid, false)
	case "pin":
		wa.SendAppState(context.Background(), appstate.BuildPin(chatJID, true))
		s.store.SetChatPinned(jid, true)
	case "unpin":
		wa.SendAppState(context.Background(), appstate.BuildPin(chatJID, false))
		s.store.SetChatPinned(jid, false)
	case "mute":
		dur := time.Duration(req.Duration) * time.Hour
		if req.Duration == 0 {
			dur = -1 // forever
		}
		wa.SendAppState(context.Background(), appstate.BuildMute(chatJID, true, dur))
		s.store.SetChatMuted(jid, true, req.Duration)
	case "unmute":
		wa.SendAppState(context.Background(), appstate.BuildMute(chatJID, false, 0))
		s.store.SetChatMuted(jid, false, 0)
	case "read":
		msgs, _ := s.store.GetMessages(jid, 0, 1)
		if len(msgs) > 0 {
			wa.MarkRead(context.Background(), []types.MessageID{msgs[0].ID}, time.Now(), chatJID, chatJID)
		}
		wa.SendAppState(context.Background(), appstate.BuildMarkChatAsRead(chatJID, true, time.Now(), nil))
		s.store.StoreChat(&db.Chat{JID: jid, UnreadCount: 0})
	case "unread":
		wa.SendAppState(context.Background(), appstate.BuildMarkChatAsRead(chatJID, false, time.Now(), nil))
		s.store.StoreChat(&db.Chat{JID: jid, UnreadCount: 1})
	default:
		jsonError(w, 400, "unknown action: "+req.Action)
		return
	}

	jsonOK(w, map[string]bool{"success": true})
}

func (s *Server) handleChatDisappearing(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Timer int64 `json:"timer"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}

	chatJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid jid")
		return
	}

	// Set disappearing timer on WhatsApp
	wa := s.client.GetWhatsmeowClient()
	dur := time.Duration(req.Timer) * time.Second
	if err := wa.SetDisappearingTimer(context.Background(), chatJID, dur, time.Now()); err != nil {
		jsonError(w, 500, "failed to set timer: "+err.Error())
		return
	}

	// Update local DB
	chat, _ := s.store.GetChat(jid)
	if chat != nil {
		chat.DisappearingTimer = req.Timer
		s.store.StoreChat(chat)
	}

	jsonOK(w, map[string]bool{"success": true})
}
