package api

import (
	"context"
	"fmt"
	"net/http"

	"go.mau.fi/whatsmeow/types"
)

// handleStatusAbout reads or writes the current user's "About" line — the
// short bio shown under your name in profile cards. GET resolves it via
// GetUserInfo against the connected device's own JID; PUT pipes through
// whatsmeow's SetStatusMessage.
func (s *Server) handleStatusAbout(w http.ResponseWriter, r *http.Request) {
	wa := s.client.GetWhatsmeowClient()

	switch r.Method {
	case http.MethodGet:
		// Resolve self JID from the connected device. .Store.ID is the
		// device-suffixed JID; we strip ":NN" because GetUserInfo wants the
		// bare user JID (whatsmeow rejects device-suffixed ones).
		dev := wa.Store
		if dev == nil || dev.ID == nil {
			jsonError(w, 503, "not connected")
			return
		}
		selfJID := dev.ID.ToNonAD()
		infos, err := wa.GetUserInfo(context.Background(), []types.JID{selfJID})
		if err != nil {
			jsonError(w, 500, fmt.Sprintf("get self info: %v", err))
			return
		}
		info := infos[selfJID]
		jsonOK(w, map[string]string{"text": info.Status})
	case http.MethodPut:
		var req struct {
			Text string `json:"text"`
		}
		if err := decodeJSON(r, &req); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		err := wa.SetStatusMessage(context.Background(), req.Text)
		if err != nil {
			jsonError(w, 500, fmt.Sprintf("set status: %v", err))
			return
		}
		jsonOK(w, map[string]bool{"success": true})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleStatusPrivacy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	wa := s.client.GetWhatsmeowClient()
	settings, err := wa.GetStatusPrivacy(context.Background())
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("get status privacy: %v", err))
		return
	}
	jsonOK(w, settings)
}
