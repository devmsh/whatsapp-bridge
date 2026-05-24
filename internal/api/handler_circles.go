package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"whatsapp-bridge-v2/internal/db"
	"whatsapp-bridge-v2/internal/reco"
)

const dismissedRecsKey = "dismissed_recs"

func (s *Server) loadDismissedRecs() map[string]bool {
	set := map[string]bool{}
	if v, _, _ := s.store.GetSyncState(dismissedRecsKey); v != "" {
		var ids []string
		if json.Unmarshal([]byte(v), &ids) == nil {
			for _, id := range ids {
				set[id] = true
			}
		}
	}
	return set
}

func (s *Server) saveDismissedRecs(set map[string]bool) error {
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	b, _ := json.Marshal(ids)
	return s.store.PutSyncState(dismissedRecsKey, string(b))
}

// handleCircleRecommendations returns active suggestions (non-dismissed, top
// `limit`) and the hidden (dismissed) ones, so the UI can offer restore.
// GET /api/v2/circles/recommendations?limit=5
func (s *Server) handleCircleRecommendations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	limit := 5
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	all, err := reco.New(s.store).Recommend(s.ownPhone())
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	dismissed := s.loadDismissedRecs()
	active := []reco.Recommendation{}
	hidden := []reco.Recommendation{}
	for _, rec := range all {
		if dismissed[rec.ID] {
			hidden = append(hidden, rec)
		} else if len(active) < limit {
			active = append(active, rec)
		}
	}
	jsonOK(w, map[string]interface{}{"active": active, "hidden": hidden})
}

// handleRecDismiss / handleRecRestore persist a recommendation's hidden state.
// POST /api/v2/circles/recommendations/dismiss  body {"id": "..."}
// POST /api/v2/circles/recommendations/restore  body {"id": "..."}
func (s *Server) handleRecDismiss(w http.ResponseWriter, r *http.Request) {
	s.toggleDismiss(w, r, true)
}

func (s *Server) handleRecRestore(w http.ResponseWriter, r *http.Request) {
	s.toggleDismiss(w, r, false)
}

func (s *Server) toggleDismiss(w http.ResponseWriter, r *http.Request, dismiss bool) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(r, &req); err != nil || req.ID == "" {
		jsonError(w, 400, "id required")
		return
	}
	set := s.loadDismissedRecs()
	if dismiss {
		set[req.ID] = true
	} else {
		delete(set, req.ID)
	}
	if err := s.saveDismissedRecs(set); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, map[string]bool{"success": true})
}

// handleCircles handles the collection: GET list, POST create.
// /api/v2/circles
func (s *Server) handleCircles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		circles, err := s.store.ListCircles()
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		// child circle ids per parent, so the UI can render a tree
		childMap := map[int64][]int64{}
		if rows, qerr := s.store.DB.Query(
			`SELECT circle_id, member_ref FROM circle_members WHERE member_type = 'circle'`,
		); qerr == nil {
			for rows.Next() {
				var pid int64
				var ref string
				if rows.Scan(&pid, &ref) == nil {
					if cid, perr := strconv.ParseInt(ref, 10, 64); perr == nil {
						childMap[pid] = append(childMap[pid], cid)
					}
				}
			}
			rows.Close()
		}
		type circleOut struct {
			db.Circle
			ChildCircles []int64 `json:"child_circles"`
		}
		out := make([]circleOut, 0, len(circles))
		for _, c := range circles {
			ch := childMap[c.ID]
			if ch == nil {
				ch = []int64{}
			}
			out = append(out, circleOut{Circle: c, ChildCircles: ch})
		}
		jsonOK(w, out)
	case http.MethodPost:
		var req struct {
			Name  string `json:"name"`
			Color string `json:"color"`
			Notes string `json:"notes"`
		}
		if err := decodeJSON(r, &req); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		if strings.TrimSpace(req.Name) == "" {
			jsonError(w, 400, "name required")
			return
		}
		c, err := s.store.CreateCircle(strings.TrimSpace(req.Name), req.Color, req.Notes)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonCreated(w, c)
	default:
		methodNotAllowed(w)
	}
}

// handleCircleForMember lists the circles that directly contain a member.
// GET /api/v2/circles/for-member?type=group|contact&ref=JID
func (s *Server) handleCircleForMember(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	mt := r.URL.Query().Get("type")
	ref := r.URL.Query().Get("ref")
	if mt == "" || ref == "" {
		jsonError(w, 400, "type and ref required")
		return
	}
	circles, err := s.store.GetCirclesForMember(mt, ref)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	if circles == nil {
		circles = []db.Circle{}
	}
	jsonOK(w, circles)
}

// handleCircleByID handles /api/v2/circles/{id} and sub-paths {id}/members, {id}/chats.
func (s *Server) handleCircleByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v2/circles/")
	parts := strings.SplitN(path, "/", 2)
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		jsonError(w, 400, "invalid circle id")
		return
	}
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}

	switch sub {
	case "members":
		s.handleCircleMembers(w, r, id)
	case "chats":
		s.handleCircleChats(w, r, id)
	case "suggestions":
		s.handleCircleSuggestions(w, r, id)
	case "contacts":
		s.handleCircleContacts(w, r, id)
	case "extract":
		s.handleCircleExtract(w, r, id)
	default:
		s.handleCircleEntity(w, r, id)
	}
}

// handleCircleContacts returns the circle's contact members enriched (group
// count, admin flag, tags), admins first.
// GET /api/v2/circles/{id}/contacts
func (s *Server) handleCircleContacts(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	cc, err := s.store.GetCircleContacts(id)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, cc)
}

// ownPhone returns the linked account's phone number (without device suffix).
func (s *Server) ownPhone() string {
	if wa := s.client.GetWhatsmeowClient(); wa != nil && wa.Store != nil && wa.Store.ID != nil {
		return wa.Store.ID.User
	}
	return ""
}

// handleCircleSuggestions returns keyword-matched groups/contacts not yet in the circle.
// GET /api/v2/circles/{id}/suggestions
func (s *Server) handleCircleSuggestions(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	sugg, context, err := s.store.SuggestForCircle(id)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, map[string]interface{}{"context": context, "suggestions": sugg})
}

// handleCircleEntity: GET details (+members), PUT update, DELETE remove.
func (s *Server) handleCircleEntity(w http.ResponseWriter, r *http.Request, id int64) {
	switch r.Method {
	case http.MethodGet:
		c, err := s.store.GetCircle(id)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		if c == nil {
			jsonError(w, 404, "circle not found")
			return
		}
		members, _ := s.store.GetCircleMembers(id)
		if members == nil {
			members = []db.CircleMember{}
		}
		jsonOK(w, map[string]interface{}{"circle": c, "members": members})
	case http.MethodPut:
		var req struct {
			Name     string   `json:"name"`
			Color    string   `json:"color"`
			Notes    string   `json:"notes"`
			Keywords []string `json:"keywords"`
		}
		if err := decodeJSON(r, &req); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		if strings.TrimSpace(req.Name) == "" {
			jsonError(w, 400, "name required")
			return
		}
		// normalize keywords: trim, drop empties, dedupe
		seen := map[string]bool{}
		kws := []string{}
		for _, k := range req.Keywords {
			k = strings.TrimSpace(k)
			if k == "" || seen[strings.ToLower(k)] {
				continue
			}
			seen[strings.ToLower(k)] = true
			kws = append(kws, k)
		}
		if err := s.store.UpdateCircle(id, strings.TrimSpace(req.Name), req.Color, req.Notes, kws); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		c, _ := s.store.GetCircle(id)
		jsonOK(w, c)
	case http.MethodDelete:
		if err := s.store.DeleteCircle(id); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, map[string]bool{"success": true})
	default:
		methodNotAllowed(w)
	}
}

// handleCircleMembers: GET list, POST add, DELETE remove.
// Add/remove body: {"member_type": "group|contact|circle", "member_ref": "..."}
func (s *Server) handleCircleMembers(w http.ResponseWriter, r *http.Request, id int64) {
	switch r.Method {
	case http.MethodGet:
		members, err := s.store.GetCircleMembers(id)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		if members == nil {
			members = []db.CircleMember{}
		}
		jsonOK(w, members)
	case http.MethodPost, http.MethodDelete:
		var req struct {
			MemberType string `json:"member_type"`
			MemberRef  string `json:"member_ref"`
		}
		if err := decodeJSON(r, &req); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		if req.MemberType == "" || req.MemberRef == "" {
			jsonError(w, 400, "member_type and member_ref required")
			return
		}
		if r.Method == http.MethodPost {
			if err := s.store.AddCircleMember(id, req.MemberType, req.MemberRef); err != nil {
				jsonError(w, 400, err.Error())
				return
			}
			// Adding a group automatically pulls its participants in as contacts.
			if req.MemberType == "group" {
				s.store.AddGroupParticipantsAsContacts(id, req.MemberRef, s.ownPhone())
			}
		} else {
			if err := s.store.RemoveCircleMember(id, req.MemberType, req.MemberRef); err != nil {
				jsonError(w, 500, err.Error())
				return
			}
		}
		jsonOK(w, map[string]bool{"success": true})
	default:
		methodNotAllowed(w)
	}
}

// handleCircleChats returns the flattened chat JIDs in a circle (incl nested).
func (s *Server) handleCircleChats(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	jids, err := s.store.FlattenCircleChats(id)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, map[string]interface{}{"chat_jids": jids})
}
