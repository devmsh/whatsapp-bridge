package api

import (
	"net/http"
	"strconv"
	"strings"

	"whatsapp-bridge-v2/internal/db"
)

func (s *Server) handleCallsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil && l > 0 {
			limit = l
		}
	}
	calls, err := s.store.GetCalls(limit)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	if calls == nil {
		calls = []db.CallEvent{}
	}
	jsonOK(w, calls)
}

func (s *Server) handleCallByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v2/calls/")
	parts := strings.SplitN(path, "/", 2)
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}

	switch sub {
	case "reject":
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		// Call rejection is handled by not answering; log it
		jsonOK(w, map[string]bool{"success": true})
	default:
		jsonError(w, 404, "not found")
	}
}
