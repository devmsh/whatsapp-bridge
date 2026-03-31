package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"whatsapp-bridge-v2/internal/db"
)

func (s *Server) handleNewsletters(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		nls, err := s.store.GetNewsletters()
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		if nls == nil {
			nls = []db.Newsletter{}
		}
		jsonOK(w, nls)
	case http.MethodPost:
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := decodeJSON(r, &req); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		wa := s.client.GetWhatsmeowClient()
		meta, err := wa.CreateNewsletter(context.Background(), whatsmeow.CreateNewsletterParams{
			Name:        req.Name,
			Description: req.Description,
		})
		if err != nil {
			jsonError(w, 500, fmt.Sprintf("create newsletter: %v", err))
			return
		}
		jsonCreated(w, map[string]string{"jid": meta.ID.String(), "name": meta.ThreadMeta.Name.Text})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleNewsletterByJID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v2/newsletters/")
	parts := strings.SplitN(path, "/", 2)
	jid := parts[0]
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}

	switch sub {
	case "follow":
		s.handleNewsletterFollow(w, r, jid)
	case "unfollow":
		s.handleNewsletterUnfollow(w, r, jid)
	case "mute":
		s.handleNewsletterMute(w, r, jid)
	case "messages":
		s.handleNewsletterMessages(w, r, jid)
	case "react":
		s.handleNewsletterReact(w, r, jid)
	default:
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		nl, err := s.store.GetNewsletter(jid)
		if err != nil {
			jsonError(w, 404, "newsletter not found")
			return
		}
		jsonOK(w, nl)
	}
}

func (s *Server) handleNewsletterFollow(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	parsedJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid JID")
		return
	}
	wa := s.client.GetWhatsmeowClient()
	err = wa.FollowNewsletter(context.Background(), parsedJID)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("follow: %v", err))
		return
	}
	jsonOK(w, map[string]bool{"success": true})
}

func (s *Server) handleNewsletterUnfollow(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	parsedJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid JID")
		return
	}
	wa := s.client.GetWhatsmeowClient()
	err = wa.UnfollowNewsletter(context.Background(), parsedJID)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("unfollow: %v", err))
		return
	}
	s.store.DeleteNewsletter(jid)
	jsonOK(w, map[string]bool{"success": true})
}

func (s *Server) handleNewsletterMute(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct{ Mute bool `json:"mute"` }
	decodeJSON(r, &req)

	parsedJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid JID")
		return
	}
	wa := s.client.GetWhatsmeowClient()
	err = wa.NewsletterToggleMute(context.Background(), parsedJID, req.Mute)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("mute: %v", err))
		return
	}
	jsonOK(w, map[string]bool{"success": true})
}

func (s *Server) handleNewsletterMessages(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	parsedJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid JID")
		return
	}

	count := 20
	if v := r.URL.Query().Get("count"); v != "" {
		count, _ = strconv.Atoi(v)
	}
	before := 0
	if v := r.URL.Query().Get("before"); v != "" {
		before, _ = strconv.Atoi(v)
	}

	wa := s.client.GetWhatsmeowClient()
	msgs, err := wa.GetNewsletterMessages(context.Background(), parsedJID, &whatsmeow.GetNewsletterMessagesParams{
		Count:  count,
		Before: before,
	})
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("get messages: %v", err))
		return
	}
	jsonOK(w, msgs)
}

func (s *Server) handleNewsletterReact(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		ServerID  int    `json:"server_id"`
		Reaction  string `json:"reaction"`
		MessageID string `json:"message_id"`
	}
	decodeJSON(r, &req)

	parsedJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid JID")
		return
	}
	wa := s.client.GetWhatsmeowClient()
	err = wa.NewsletterSendReaction(context.Background(), parsedJID, req.ServerID, req.Reaction, req.MessageID)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("react: %v", err))
		return
	}
	jsonOK(w, map[string]bool{"success": true})
}
