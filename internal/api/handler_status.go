package api

import (
	"context"
	"fmt"
	"net/http"
)

func (s *Server) handleStatusAbout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}

	wa := s.client.GetWhatsmeowClient()
	err := wa.SetStatusMessage(context.Background(), req.Text)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("set status: %v", err))
		return
	}
	jsonOK(w, map[string]bool{"success": true})
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
