package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"whatsapp-bridge-v2/internal/wa"
)

// POST /api/v2/contacts/lookup-username {"username":"..."}
// Live-queries WhatsApp for the account behind @username. Leading @ is
// stripped and the name is lowercased. 404 with {"found":false} when the
// server returns nothing. ?debug=1 adds the raw usync node XML as "debug".
func (s *Server) handleLookupUsername(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Username string `json:"username"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	username := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(req.Username), "@")))
	if username == "" {
		jsonError(w, 400, "username required")
		return
	}
	if s.client == nil {
		jsonError(w, 503, "not connected")
		return
	}

	result, err := s.client.LookupUsername(r.Context(), username)
	if err != nil {
		if isWAUnavailable(err) {
			jsonError(w, 503, "not connected")
			return
		}
		jsonError(w, 500, err.Error())
		return
	}

	writeUsernameLookup(w, r, result)
}

// POST /api/v2/contacts/fetch-usernames {"jids":["..."]}
func (s *Server) handleFetchUsernames(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		JIDs []string `json:"jids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	if s.client == nil {
		jsonError(w, 503, "not connected")
		return
	}

	results, err := s.client.FetchUsernames(r.Context(), req.JIDs)
	if err != nil {
		if isWAUnavailable(err) {
			jsonError(w, 503, "not connected")
			return
		}
		jsonError(w, 500, err.Error())
		return
	}
	if results == nil {
		results = []wa.UsernameLookupResult{}
	}

	type row struct {
		wa.UsernameLookupResult
		Debug string `json:"debug,omitempty"`
	}
	out := make([]row, 0, len(results))
	debug := r.URL.Query().Get("debug") == "1"
	for _, res := range results {
		item := row{UsernameLookupResult: res}
		if debug {
			item.Debug = res.RawNode
		}
		out = append(out, item)
	}
	jsonOK(w, map[string]any{"results": out})
}

func isWAUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "not connected") || strings.Contains(msg, "not ready")
}

func writeUsernameLookup(w http.ResponseWriter, r *http.Request, result wa.UsernameLookupResult) {
	type body struct {
		wa.UsernameLookupResult
		Debug string `json:"debug,omitempty"`
	}
	out := body{UsernameLookupResult: result}
	if r.URL.Query().Get("debug") == "1" {
		out.Debug = result.RawNode
	}
	if !result.Found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(out)
		return
	}
	jsonOK(w, out)
}
