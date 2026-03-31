package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

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

	maxSel := uint32(req.MaxSelections)
	if maxSel == 0 {
		maxSel = 1
	}

	var options []*waE2E.PollCreationMessage_Option
	for _, o := range req.Options {
		options = append(options, &waE2E.PollCreationMessage_Option{
			OptionName: proto.String(o),
		})
	}

	wa := s.client.GetWhatsmeowClient()
	msg := &waE2E.Message{
		PollCreationMessage: &waE2E.PollCreationMessage{
			Name:                   proto.String(req.Question),
			Options:                options,
			SelectableOptionsCount: proto.Uint32(maxSel),
		},
	}

	resp, err := wa.SendMessage(context.Background(), chatJID, msg)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("send poll: %v", err))
		return
	}

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

	wa := s.client.GetWhatsmeowClient()
	optionsJSON, _ := json.Marshal(req.Options)

	s.store.StorePollVote(&db.PollVote{
		PollMessageID:   pollID,
		PollChatJID:     req.ChatJID,
		VoterJID:        wa.Store.ID.String(),
		SelectedOptions: string(optionsJSON),
	})

	jsonOK(w, map[string]bool{"success": true})
}
