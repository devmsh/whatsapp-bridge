package api

import (
	"net/http"
	"strings"

	"whatsapp-bridge-v2/internal/db"
)

// validProfileType reports whether t is a known entity type.
func validProfileType(t string) bool {
	switch t {
	case db.ProfileCircle, db.ProfileGroup, db.ProfileContact:
		return true
	}
	return false
}

// handleProfile reads or edits one entity's profile.
//   GET    /api/v2/profiles?type=group&ref=JID     -> the profile (or null)
//   PUT    /api/v2/profiles  {type, ref, description} -> manual edit (pins manual)
//   POST   /api/v2/profiles/regenerate {type, ref}    -> queue a fresh generation
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		t := r.URL.Query().Get("type")
		ref := r.URL.Query().Get("ref")
		if !validProfileType(t) || ref == "" {
			jsonError(w, 400, "type and ref required")
			return
		}
		p, err := s.store.GetProfile(t, ref)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, p) // p may be nil → null

	case http.MethodPut:
		var req struct {
			Type        string `json:"type"`
			Ref         string `json:"ref"`
			Description string `json:"description"`
		}
		if err := decodeJSON(r, &req); err != nil || !validProfileType(req.Type) || req.Ref == "" {
			jsonError(w, 400, "type, ref required")
			return
		}
		if err := s.store.SaveProfileManual(req.Type, req.Ref, strings.TrimSpace(req.Description)); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		p, _ := s.store.GetProfile(req.Type, req.Ref)
		jsonOK(w, p)

	default:
		methodNotAllowed(w)
	}
}

// handleProfileRegenerate queues one entity for fresh AI generation.
// POST /api/v2/profiles/regenerate {type, ref}
func (s *Server) handleProfileRegenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Type string `json:"type"`
		Ref  string `json:"ref"`
	}
	if err := decodeJSON(r, &req); err != nil || !validProfileType(req.Type) || req.Ref == "" {
		jsonError(w, 400, "type, ref required")
		return
	}
	if s.profiles == nil {
		jsonError(w, 503, "profiler not running")
		return
	}
	s.profiles.RegenerateNow(req.Type, req.Ref)
	jsonOK(w, map[string]any{"queued": true})
}

// handleProfilesStatus reports profiling progress and lets the UI trigger a
// full rescan.
//   GET  /api/v2/profiles/status   -> stats + queue size + active entity
//   POST /api/v2/profiles/status   -> rescan all entities for staleness now
func (s *Server) handleProfilesStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		// Enable profiling (if not already) and scan for work now.
		s.profiles.Enable()
		jsonOK(w, map[string]any{"enabled": true, "scanning": true})
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	stats, err := s.store.CountProfilesByStatus()
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	stats.QueueSize = s.profiles.queued()
	active := []string{}
	s.profiles.mu.Lock()
	for k := range s.profiles.active {
		active = append(active, k)
	}
	s.profiles.mu.Unlock()
	jsonOK(w, map[string]any{
		"stats":   stats,
		"active":  active,
		"enabled": s.profiles.enabled(),
	})
}
