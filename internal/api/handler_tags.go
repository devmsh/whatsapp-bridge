package api

import (
	"net/http"
	"strconv"
	"strings"
)

// handleTags: GET list all tags, POST create a tag.
// /api/v2/tags
func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tags, err := s.store.ListTags()
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, tags)
	case http.MethodPost:
		var req struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		}
		if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
			jsonError(w, 400, "name required")
			return
		}
		t, err := s.store.GetOrCreateTag(req.Name, req.Color)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, t)
	default:
		methodNotAllowed(w)
	}
}

// handleTagByID: DELETE /api/v2/tags/{id}
func (s *Server) handleTagByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/v2/tags/"), 10, 64)
	if err != nil {
		jsonError(w, 400, "invalid tag id")
		return
	}
	if err := s.store.DeleteTag(id); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, map[string]bool{"success": true})
}

// handleContactTagsMap: GET /api/v2/contact-tags -> { jid: [tags] }
func (s *Server) handleContactTagsMap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	m, err := s.store.AllContactTags()
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, m)
}

// handleContactTags handles /api/v2/contacts/{jid}/tags.
// GET list; POST assign {tag_id} or {name}; DELETE unassign {tag_id}.
func (s *Server) handleContactTags(w http.ResponseWriter, r *http.Request, jid string) {
	switch r.Method {
	case http.MethodGet:
		tags, err := s.store.TagsForContact(jid)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, tags)
	case http.MethodPost:
		var req struct {
			TagID int64  `json:"tag_id"`
			Name  string `json:"name"`
			Color string `json:"color"`
		}
		if err := decodeJSON(r, &req); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		tagID := req.TagID
		if tagID == 0 && strings.TrimSpace(req.Name) != "" {
			t, err := s.store.GetOrCreateTag(req.Name, req.Color)
			if err != nil {
				jsonError(w, 500, err.Error())
				return
			}
			tagID = t.ID
		}
		if tagID == 0 {
			jsonError(w, 400, "tag_id or name required")
			return
		}
		if err := s.store.AssignTag(jid, tagID); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		tags, _ := s.store.TagsForContact(jid)
		jsonOK(w, tags)
	case http.MethodDelete:
		var req struct {
			TagID int64 `json:"tag_id"`
		}
		if err := decodeJSON(r, &req); err != nil || req.TagID == 0 {
			jsonError(w, 400, "tag_id required")
			return
		}
		if err := s.store.UnassignTag(jid, req.TagID); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, map[string]bool{"success": true})
	default:
		methodNotAllowed(w)
	}
}
