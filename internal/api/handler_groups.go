package api

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	_ "golang.org/x/image/webp"

	"whatsapp-bridge-v2/internal/db"
)

// handleGroupsDiscover returns all groups with activity stats, marking tracked vs untracked.
// POST /api/v2/groups/discover — body: {"tracked_jids":["jid1","jid2"]}
// GET /api/v2/groups/discover — returns all groups (none marked as tracked)
// Optional query: ?filter=new (only untracked), ?filter=active (has messages), ?filter=dead (no messages)
func (s *Server) handleGroupsDiscover(w http.ResponseWriter, r *http.Request) {
	trackedMap := make(map[string]bool)

	if r.Method == http.MethodPost {
		var req struct {
			TrackedJIDs []string `json:"tracked_jids"`
		}
		if err := decodeJSON(r, &req); err == nil {
			for _, jid := range req.TrackedJIDs {
				trackedMap[jid] = true
			}
		}
	}

	results, err := s.store.GetGroupsDiscovery(trackedMap)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	if results == nil {
		results = []db.GroupDiscovery{}
	}

	// Apply filter
	filter := r.URL.Query().Get("filter")
	if filter != "" {
		var filtered []db.GroupDiscovery
		for _, g := range results {
			switch filter {
			case "new":
				if !g.Tracked {
					filtered = append(filtered, g)
				}
			case "active":
				if g.MessageCount > 0 {
					filtered = append(filtered, g)
				}
			case "dead":
				if g.MessageCount == 0 {
					filtered = append(filtered, g)
				}
			case "community":
				if g.IsParent {
					filtered = append(filtered, g)
				}
			default:
				filtered = append(filtered, g)
			}
		}
		results = filtered
		if results == nil {
			results = []db.GroupDiscovery{}
		}
	}

	jsonOK(w, results)
}

func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		groups, err := s.store.GetGroups()
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		if groups == nil {
			groups = []db.Group{}
		}
		jsonOK(w, groups)
	case http.MethodPost:
		s.handleGroupCreate(w, r)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleGroupCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string   `json:"name"`
		Participants []string `json:"participants"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	if req.Name == "" {
		jsonError(w, 400, "name required")
		return
	}

	wa := s.client.GetWhatsmeowClient()
	var jids []types.JID
	for _, p := range req.Participants {
		j, err := parseJID(p)
		if err == nil {
			jids = append(jids, j)
		}
	}

	info, err := wa.CreateGroup(context.Background(), whatsmeow.ReqCreateGroup{
		Name:         req.Name,
		Participants: jids,
	})
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("create group: %v", err))
		return
	}

	jsonCreated(w, map[string]string{"jid": info.JID.String(), "name": info.Name})
}

func (s *Server) handleGroupJoin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}

	wa := s.client.GetWhatsmeowClient()
	jid, err := wa.JoinGroupWithLink(context.Background(), req.Code)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("join group: %v", err))
		return
	}
	jsonOK(w, map[string]string{"jid": jid.String()})
}

func (s *Server) handleGroupByJID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v2/groups/")
	if path == "join" {
		s.handleGroupJoin(w, r)
		return
	}

	parts := strings.SplitN(path, "/", 2)
	jid := parts[0]
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}

	switch sub {
	case "":
		s.handleGroupGet(w, r, jid)
	case "name":
		s.handleGroupName(w, r, jid)
	case "description":
		s.handleGroupDescription(w, r, jid)
	case "photo":
		s.handleGroupPhoto(w, r, jid)
	case "settings":
		s.handleGroupSettings(w, r, jid)
	case "participants":
		s.handleGroupParticipants(w, r, jid)
	case "invite-link":
		s.handleGroupInviteLink(w, r, jid)
	case "sub-groups":
		s.handleGroupSubGroups(w, r, jid)
	case "requests":
		s.handleGroupRequests(w, r, jid)
	default:
		jsonError(w, 404, "not found")
	}
}

func (s *Server) handleGroupGet(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method == http.MethodDelete {
		parsedJID, err := types.ParseJID(jid)
		if err != nil {
			jsonError(w, 400, "invalid JID")
			return
		}
		wa := s.client.GetWhatsmeowClient()
		err = wa.LeaveGroup(context.Background(), parsedJID)
		if err != nil {
			jsonError(w, 500, fmt.Sprintf("leave group: %v", err))
			return
		}
		jsonOK(w, map[string]bool{"success": true})
		return
	}

	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	group, err := s.store.GetGroup(jid)
	if err != nil {
		jsonError(w, 404, "group not found")
		return
	}
	parts, _ := s.store.GetGroupParticipants(jid)
	type GroupWithParticipants struct {
		*db.Group
		Participants []db.GroupParticipant `json:"participants"`
	}
	if parts == nil {
		parts = []db.GroupParticipant{}
	}
	jsonOK(w, GroupWithParticipants{Group: group, Participants: parts})
}

func (s *Server) handleGroupName(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	var req struct{ Name string `json:"name"` }
	decodeJSON(r, &req)

	parsedJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid JID")
		return
	}
	wa := s.client.GetWhatsmeowClient()
	err = wa.SetGroupName(context.Background(), parsedJID, req.Name)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("set name: %v", err))
		return
	}
	jsonOK(w, map[string]bool{"success": true})
}

func (s *Server) handleGroupDescription(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	var req struct{ Description string `json:"description"` }
	decodeJSON(r, &req)

	parsedJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid JID")
		return
	}
	wa := s.client.GetWhatsmeowClient()
	err = wa.SetGroupTopic(context.Background(), parsedJID, "", "", req.Description)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("set description: %v", err))
		return
	}
	jsonOK(w, map[string]bool{"success": true})
}

// convertToJPEG takes any image format (PNG, WebP, GIF, JPEG) and returns
// a 640x640 JPEG suitable for WhatsApp profile/group photos.
func convertToJPEG(data []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	// Resize to 640x640 (WhatsApp standard)
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	// Crop to square first (center crop)
	var cropImg image.Image = img
	if w != h {
		size := w
		if h < w {
			size = h
		}
		x0 := (w - size) / 2
		y0 := (h - size) / 2
		type subImager interface {
			SubImage(r image.Rectangle) image.Image
		}
		if si, ok := img.(subImager); ok {
			cropImg = si.SubImage(image.Rect(x0, y0, x0+size, y0+size))
		}
	}

	// Resize to 640x640 using nearest-neighbor
	cropBounds := cropImg.Bounds()
	target := 640
	thumb := image.NewRGBA(image.Rect(0, 0, target, target))
	for y := 0; y < target; y++ {
		for x := 0; x < target; x++ {
			srcX := cropBounds.Min.X + x*cropBounds.Dx()/target
			srcY := cropBounds.Min.Y + y*cropBounds.Dy()/target
			thumb.Set(x, y, cropImg.At(srcX, srcY))
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, thumb, &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}

func (s *Server) handleGroupPhoto(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	file, _, err := r.FormFile("photo")
	if err != nil {
		jsonError(w, 400, "photo file required")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		jsonError(w, 400, "failed to read photo")
		return
	}

	// Auto-convert any image format to 640x640 JPEG
	jpegData, err := convertToJPEG(data)
	if err != nil {
		jsonError(w, 400, fmt.Sprintf("invalid image: %v", err))
		return
	}

	parsedJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid JID")
		return
	}

	wa := s.client.GetWhatsmeowClient()
	_, err = wa.SetGroupPhoto(context.Background(), parsedJID, jpegData)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("set photo: %v", err))
		return
	}
	jsonOK(w, map[string]bool{"success": true})
}

func (s *Server) handleGroupSettings(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Locked   *bool `json:"locked,omitempty"`
		Announce *bool `json:"announce,omitempty"`
	}
	decodeJSON(r, &req)

	parsedJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid JID")
		return
	}

	wa := s.client.GetWhatsmeowClient()
	if req.Locked != nil {
		wa.SetGroupLocked(context.Background(), parsedJID, *req.Locked)
	}
	if req.Announce != nil {
		wa.SetGroupAnnounce(context.Background(), parsedJID, *req.Announce)
	}

	jsonOK(w, map[string]bool{"success": true})
}

func (s *Server) handleGroupParticipants(w http.ResponseWriter, r *http.Request, jid string) {
	parsedJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid JID")
		return
	}

	switch r.Method {
	case http.MethodGet:
		parts, err := s.store.GetGroupParticipants(jid)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		if parts == nil {
			parts = []db.GroupParticipant{}
		}
		jsonOK(w, parts)
	case http.MethodPost:
		var req struct {
			JIDs   []string `json:"jids"`
			Action string   `json:"action"`
		}
		decodeJSON(r, &req)

		var memberJIDs []types.JID
		for _, j := range req.JIDs {
			pj, err := parseJID(j)
			if err == nil {
				memberJIDs = append(memberJIDs, pj)
			}
		}

		wa := s.client.GetWhatsmeowClient()
		var action whatsmeow.ParticipantChange
		switch req.Action {
		case "add":
			action = "add"
		case "remove":
			action = "remove"
		case "promote":
			action = "promote"
		case "demote":
			action = "demote"
		default:
			jsonError(w, 400, "action must be add/remove/promote/demote")
			return
		}

		_, opErr := wa.UpdateGroupParticipants(context.Background(), parsedJID, memberJIDs, action)
		if opErr != nil {
			jsonError(w, 500, opErr.Error())
			return
		}
		jsonOK(w, map[string]bool{"success": true})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleGroupInviteLink(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	parsedJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid JID")
		return
	}

	wa := s.client.GetWhatsmeowClient()
	reset := r.URL.Query().Get("reset") == "true"
	link, err := wa.GetGroupInviteLink(context.Background(), parsedJID, reset)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("get invite link: %v", err))
		return
	}
	jsonOK(w, map[string]string{"link": link})
}

func (s *Server) handleGroupSubGroups(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	parsedJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid JID")
		return
	}
	wa := s.client.GetWhatsmeowClient()
	subs, err := wa.GetSubGroups(context.Background(), parsedJID)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("get sub-groups: %v", err))
		return
	}
	jsonOK(w, subs)
}

func (s *Server) handleGroupRequests(w http.ResponseWriter, r *http.Request, jid string) {
	parsedJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid JID")
		return
	}
	wa := s.client.GetWhatsmeowClient()

	switch r.Method {
	case http.MethodGet:
		requests, err := wa.GetGroupRequestParticipants(context.Background(), parsedJID)
		if err != nil {
			jsonError(w, 500, fmt.Sprintf("get requests: %v", err))
			return
		}
		jsonOK(w, requests)
	case http.MethodPost:
		var req struct {
			JIDs   []string `json:"jids"`
			Action string   `json:"action"`
		}
		decodeJSON(r, &req)

		var memberJIDs []types.JID
		for _, j := range req.JIDs {
			pj, err := parseJID(j)
			if err == nil {
				memberJIDs = append(memberJIDs, pj)
			}
		}

		var action whatsmeow.ParticipantChange = "approve"
		if req.Action == "reject" {
			action = "reject"
		}

		_, err := wa.UpdateGroupParticipants(context.Background(), parsedJID, memberJIDs, action)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, map[string]bool{"success": true})
	default:
		methodNotAllowed(w)
	}
}
