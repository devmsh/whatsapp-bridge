package api

import (
	"context"
	"fmt"
	"net/http"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func (s *Server) handlePrivacy(w http.ResponseWriter, r *http.Request) {
	wa := s.client.GetWhatsmeowClient()

	switch r.Method {
	case http.MethodGet:
		settings := wa.GetPrivacySettings(context.Background())
		jsonOK(w, settings)
	case http.MethodPut:
		var req struct {
			Setting string `json:"setting"`
			Value   string `json:"value"`
		}
		if err := decodeJSON(r, &req); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		_, err := wa.SetPrivacySetting(context.Background(), types.PrivacySettingType(req.Setting), types.PrivacySetting(req.Value))
		if err != nil {
			jsonError(w, 500, fmt.Sprintf("set privacy: %v", err))
			return
		}
		jsonOK(w, map[string]bool{"success": true})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleBlocklist(w http.ResponseWriter, r *http.Request) {
	wa := s.client.GetWhatsmeowClient()

	switch r.Method {
	case http.MethodGet:
		blocklist, err := wa.GetBlocklist(context.Background())
		if err != nil {
			jsonError(w, 500, fmt.Sprintf("get blocklist: %v", err))
			return
		}
		jsonOK(w, blocklist)
	case http.MethodPost:
		var req struct {
			JID    string `json:"jid"`
			Action string `json:"action"`
		}
		if err := decodeJSON(r, &req); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		parsedJID, err := parseJID(req.JID)
		if err != nil {
			jsonError(w, 400, "invalid JID")
			return
		}

		var action events.BlocklistChangeAction
		switch req.Action {
		case "block":
			action = events.BlocklistChangeActionBlock
		case "unblock":
			action = events.BlocklistChangeActionUnblock
		default:
			jsonError(w, 400, "action must be block or unblock")
			return
		}

		_, err = wa.UpdateBlocklist(context.Background(), parsedJID, action)
		if err != nil {
			jsonError(w, 500, fmt.Sprintf("update blocklist: %v", err))
			return
		}
		jsonOK(w, map[string]bool{"success": true})
	default:
		methodNotAllowed(w)
	}
}
