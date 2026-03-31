package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"

	"whatsapp-bridge-v2/internal/db"
)

func (s *Server) handlePollCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		ChatJID       string   `json:"chat_jid"`
		Question      string   `json:"question"`
		Options       []string `json:"options"`
		MaxSelections int      `json:"max_selections"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	if req.ChatJID == "" || req.Question == "" || len(req.Options) < 2 {
		jsonError(w, 400, "chat_jid, question, and at least 2 options required")
		return
	}

	chatJID, err := parseJID(req.ChatJID)
	if err != nil {
		jsonError(w, 400, "invalid chat_jid")
		return
	}

	maxSel := req.MaxSelections
	if maxSel == 0 {
		maxSel = 1
	}

	// Use whatsmeow's BuildPollCreation — it adds the critical MessageSecret
	wa := s.client.GetWhatsmeowClient()
	msg := wa.BuildPollCreation(req.Question, req.Options, maxSel)

	resp, err := wa.SendMessage(context.Background(), chatJID, msg)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("send poll: %v", err))
		return
	}

	// Store poll in local DB
	optionsJSON, _ := json.Marshal(req.Options)
	now := time.Now().Unix()
	s.store.StorePoll(&db.Poll{
		MessageID:     resp.ID,
		ChatJID:       req.ChatJID,
		Question:      req.Question,
		Options:       string(optionsJSON),
		MaxSelections: maxSel,
		CreatedAt:     now,
	})
	s.storeOutgoingMessage(resp.ID, req.ChatJID, "[poll] "+req.Question, "poll")

	jsonCreated(w, map[string]interface{}{
		"success":    true,
		"message_id": resp.ID,
	})
}

func (s *Server) handlePollByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v2/polls/")
	parts := strings.SplitN(path, "/", 2)
	pollID := parts[0]
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}

	chatJID := r.URL.Query().Get("chat_jid")

	switch sub {
	case "vote":
		s.handlePollVote(w, r, pollID)
	default:
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if chatJID == "" {
			jsonError(w, 400, "chat_jid required")
			return
		}
		poll, err := s.store.GetPoll(pollID, chatJID)
		if err != nil {
			jsonError(w, 404, "poll not found")
			return
		}
		votes, _ := s.store.GetPollVotes(pollID, chatJID)
		jsonOK(w, map[string]interface{}{
			"poll":  poll,
			"votes": votes,
		})
	}
}

func (s *Server) handlePollVote(w http.ResponseWriter, r *http.Request, pollID string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		ChatJID string   `json:"chat_jid"`
		Options []string `json:"options"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}

	if req.ChatJID == "" || len(req.Options) == 0 {
		jsonError(w, 400, "chat_jid and options required")
		return
	}

	chatJID, err := parseJID(req.ChatJID)
	if err != nil {
		jsonError(w, 400, "invalid chat_jid")
		return
	}

	wa := s.client.GetWhatsmeowClient()

	// Reconstruct MessageInfo — use phone-based JIDs (whatsmeow's SQL has LID↔PN fallback)
	ownID := wa.Store.ID.ToNonAD()

	pollInfo := &types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:     chatJID,
			Sender:   ownID,
			IsFromMe: true,
			IsGroup:  chatJID.Server == "g.us",
		},
		ID: types.MessageID(pollID),
	}

	msg, err := wa.BuildPollVote(context.Background(), pollInfo, req.Options)
	if err != nil {
		fmt.Printf("[poll-vote] BuildPollVote error: %v\n", err)
		jsonError(w, 500, fmt.Sprintf("build poll vote failed: %v", err))
		return
	}

	resp, err := wa.SendMessage(context.Background(), chatJID, msg)
	fmt.Printf("[poll-vote] SendMessage result: id=%s err=%v\n", resp.ID, err)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("send poll vote failed: %v", err))
		return
	}

	optionsJSON, _ := json.Marshal(req.Options)
	s.store.StorePollVote(&db.PollVote{
		PollMessageID:   pollID,
		PollChatJID:     req.ChatJID,
		VoterJID:        wa.Store.ID.String(),
		SelectedOptions: string(optionsJSON),
		Timestamp:       time.Now().Unix(),
	})

	jsonOK(w, map[string]bool{"success": true})
}
