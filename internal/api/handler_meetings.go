package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"whatsapp-bridge-v2/internal/db"
)

// Meetings REST surface.
//
//	GET    /api/v2/meetings                     list (?status= &review= &upcoming=1 &limit=)
//	POST   /api/v2/meetings                     create
//	GET    /api/v2/meetings/{id}                one, with people, items and the chat trail
//	PUT    /api/v2/meetings/{id}                update
//	DELETE /api/v2/meetings/{id}                delete
//	POST   /api/v2/meetings/{id}/review         {"status":"accepted"|"rejected"}
//	GET|POST|DELETE /api/v2/meetings/{id}/participants
//	GET|POST|DELETE /api/v2/meetings/{id}/messages
//	GET|POST        /api/v2/meetings/{id}/items
//	PUT|DELETE      /api/v2/meetings/{id}/items/{itemID}

func (s *Server) handleMeetings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Per-chat lookup feeds the bar above the composer: the meeting coming
		// up in this conversation, or the one just held.
		if chat := r.URL.Query().Get("chat_jid"); chat != "" {
			within := 7
			if v := r.URL.Query().Get("within"); v != "" {
				if n, err := strconv.Atoi(v); err == nil && n > 0 {
					within = n
				}
			}
			list, err := s.store.MeetingsForChat(chat, within)
			if err != nil {
				jsonError(w, 500, err.Error())
				return
			}
			jsonOK(w, list)
			return
		}
		f := db.MeetingFilter{
			Status:       r.URL.Query().Get("status"),
			ReviewStatus: r.URL.Query().Get("review"),
			Upcoming:     r.URL.Query().Get("upcoming") == "1",
		}
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				f.Limit = n
			}
		}
		list, err := s.store.ListMeetings(f)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, list)
	case http.MethodPost:
		var m db.Meeting
		if err := decodeJSON(r, &m); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		if strings.TrimSpace(m.Title) == "" {
			jsonError(w, 400, "title required")
			return
		}
		created, err := s.store.CreateMeeting(&m)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		full, _ := s.store.GetMeeting(created.ID)
		jsonCreated(w, full)
	default:
		methodNotAllowed(w)
	}
}

// handleMeetingByID routes /api/v2/meetings/{id}[/sub[/subID]].
func (s *Server) handleMeetingByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v2/meetings/"), "/")
	if rest == "" {
		jsonError(w, 400, "meeting id required")
		return
	}
	parts := strings.Split(rest, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		jsonError(w, 400, "invalid meeting id")
		return
	}
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}

	switch sub {
	case "":
		s.meetingEntity(w, r, id)
	case "review":
		s.meetingReview(w, r, id)
	case "participants":
		s.meetingParticipants(w, r, id)
	case "messages":
		s.meetingMessages(w, r, id)
	case "items":
		var itemID int64
		if len(parts) > 2 {
			itemID, _ = strconv.ParseInt(parts[2], 10, 64)
		}
		s.meetingItems(w, r, id, itemID)
	default:
		jsonError(w, 404, "unknown meeting sub-resource: "+sub)
	}
}

func (s *Server) meetingEntity(w http.ResponseWriter, r *http.Request, id int64) {
	switch r.Method {
	case http.MethodGet:
		m, err := s.store.GetMeeting(id)
		if err != nil || m == nil {
			jsonError(w, 404, "meeting not found")
			return
		}
		jsonOK(w, m)
	case http.MethodPut:
		cur, err := s.store.GetMeeting(id)
		if err != nil || cur == nil {
			jsonError(w, 404, "meeting not found")
			return
		}
		// A copy of the meeting before the edit, so the change trail can
		// record what a person actually changed, same as the model does for
		// its own patches (see docs/superpowers/specs/2026-09-18-live-meetings-design.md).
		before := *cur
		// Patch semantics: only the fields present in the body change, so a
		// partial edit from the UI cannot blank the rest of the meeting.
		var req struct {
			Title       *string  `json:"title"`
			Purpose     *string  `json:"purpose"`
			Status      *string  `json:"status"`
			StartsAt    *int64   `json:"starts_at"`
			EndsAt      *int64   `json:"ends_at"`
			TZ          *string  `json:"tz"`
			TimeOptions *string  `json:"time_options"`
			Mode        *string  `json:"mode"`
			Location    *string  `json:"location"`
			LocationURL *string  `json:"location_url"`
			LocationLat *float64 `json:"location_lat"`
			LocationLng *float64 `json:"location_lng"`
			Link        *string  `json:"link"`
			Notes       *string  `json:"notes"`
			Recurrence  *string  `json:"recurrence"`
			Prepares    *int64   `json:"prepares_meeting_id"`
			Source      *string  `json:"source"`
			ExternalID  *string  `json:"external_id"`
		}
		if err := decodeJSON(r, &req); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		set := func(dst *string, v *string) {
			if v != nil {
				*dst = *v
			}
		}
		set(&cur.Title, req.Title)
		set(&cur.Purpose, req.Purpose)
		set(&cur.Status, req.Status)
		set(&cur.TZ, req.TZ)
		set(&cur.TimeOptions, req.TimeOptions)
		set(&cur.Mode, req.Mode)
		set(&cur.Location, req.Location)
		set(&cur.LocationURL, req.LocationURL)
		if req.LocationLat != nil {
			cur.LocationLat = *req.LocationLat
		}
		if req.LocationLng != nil {
			cur.LocationLng = *req.LocationLng
		}
		set(&cur.Notes, req.Notes)
		set(&cur.Recurrence, req.Recurrence)
		set(&cur.Source, req.Source)
		set(&cur.ExternalID, req.ExternalID)
		if req.Link != nil {
			cur.Link = *req.Link
			cur.LinkCode = db.MeetingLinkCode(*req.Link)
		}
		if req.StartsAt != nil {
			cur.StartsAt = *req.StartsAt
		}
		if req.EndsAt != nil {
			cur.EndsAt = *req.EndsAt
		}
		if req.Prepares != nil {
			if *req.Prepares == 0 {
				cur.PreparesMeetingID = nil
			} else {
				cur.PreparesMeetingID = req.Prepares
			}
		}
		if strings.TrimSpace(cur.Title) == "" {
			jsonError(w, 400, "title required")
			return
		}
		if err := s.store.UpdateMeeting(cur); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		// Keep a trail of what the user changed, so a later model patch knows
		// this field was touched by hand and must not be overwritten.
		if err := s.store.RecordMeetingEdit(&before, cur, db.ChangeByUser); err != nil {
			fmt.Printf("meeting edit: record change failed for meeting %d: %v\n", id, err)
		}
		full, _ := s.store.GetMeeting(id)
		jsonOK(w, full)
	case http.MethodDelete:
		if err := s.store.DeleteMeeting(id); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, map[string]bool{"success": true})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) meetingReview(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	if req.Status != db.ReviewAccepted && req.Status != db.ReviewRejected {
		jsonError(w, 400, "status must be accepted or rejected")
		return
	}
	if err := s.store.SetMeetingReview(id, req.Status); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, map[string]any{"id": id, "review_status": req.Status})
}

func (s *Server) meetingParticipants(w http.ResponseWriter, r *http.Request, id int64) {
	switch r.Method {
	case http.MethodGet:
		ps, err := s.store.MeetingParticipants(id)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, ps)
	case http.MethodPost:
		var p db.MeetingParticipant
		if err := decodeJSON(r, &p); err != nil || strings.TrimSpace(p.JID) == "" {
			jsonError(w, 400, "jid required")
			return
		}
		if err := s.store.AddMeetingParticipant(id, p); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, map[string]bool{"success": true})
	case http.MethodDelete:
		var req struct {
			JID string `json:"jid"`
		}
		if err := decodeJSON(r, &req); err != nil || req.JID == "" {
			jsonError(w, 400, "jid required")
			return
		}
		if err := s.store.RemoveMeetingParticipant(id, req.JID); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, map[string]bool{"success": true})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) meetingMessages(w http.ResponseWriter, r *http.Request, id int64) {
	switch r.Method {
	case http.MethodGet:
		msgs, err := s.store.MeetingMessages(id)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, msgs)
	case http.MethodPost:
		var req struct {
			ChatJID   string `json:"chat_jid"`
			MessageID string `json:"message_id"`
			Role      string `json:"role"`
		}
		if err := decodeJSON(r, &req); err != nil || req.ChatJID == "" {
			jsonError(w, 400, "chat_jid required")
			return
		}
		if err := s.store.LinkMeetingMessage(id, req.ChatJID, req.MessageID, req.Role); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, map[string]bool{"success": true})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) meetingItems(w http.ResponseWriter, r *http.Request, id, itemID int64) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.MeetingItems(id)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, items)
	case http.MethodPost:
		var it db.MeetingItem
		if err := decodeJSON(r, &it); err != nil || strings.TrimSpace(it.Text) == "" {
			jsonError(w, 400, "text required")
			return
		}
		switch it.Kind {
		case db.ItemAgenda, db.ItemRequirement, db.ItemNextStep:
		default:
			jsonError(w, 400, "kind must be agenda, requirement or next_step")
			return
		}
		it.MeetingID = id
		created, err := s.store.AddMeetingItem(&it)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonCreated(w, created)
	case http.MethodPut:
		if itemID == 0 {
			jsonError(w, 400, "item id required")
			return
		}
		items, _ := s.store.MeetingItems(id)
		var cur *db.MeetingItem
		for i := range items {
			if items[i].ID == itemID {
				cur = &items[i]
			}
		}
		if cur == nil {
			jsonError(w, 404, "item not found")
			return
		}
		var req struct {
			Text     *string `json:"text"`
			Done     *bool   `json:"done"`
			OwnerJID *string `json:"owner_jid"`
			TaskID   *int64  `json:"task_id"`
			Position *int    `json:"position"`
		}
		if err := decodeJSON(r, &req); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		if req.Text != nil {
			cur.Text = *req.Text
		}
		if req.Done != nil {
			cur.Done = *req.Done
		}
		if req.OwnerJID != nil {
			cur.OwnerJID = *req.OwnerJID
		}
		if req.TaskID != nil {
			if *req.TaskID == 0 {
				cur.TaskID = nil
			} else {
				cur.TaskID = req.TaskID
			}
		}
		if req.Position != nil {
			cur.Position = *req.Position
		}
		if err := s.store.UpdateMeetingItem(cur); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, cur)
	case http.MethodDelete:
		if itemID == 0 {
			jsonError(w, 400, "item id required")
			return
		}
		if err := s.store.DeleteMeetingItem(itemID); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, map[string]bool{"success": true})
	default:
		methodNotAllowed(w)
	}
}

// handleMeetingExtract runs the meeting-finding agent over one chat. Returns a
// run_id immediately; progress streams through the run's SSE endpoint, the same
// as task extraction.
// POST /api/v2/meetings/extract  {"chat_jid","chat_name","since"} -> {"run_id"}
func (s *Server) handleMeetingExtract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		ChatJID  string `json:"chat_jid"`
		ChatName string `json:"chat_name"`
		Since    int64  `json:"since"`
		// Minutes before the sidecar is killed. Optional; see the default below.
		TimeoutMinutes int `json:"timeout_minutes"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.ChatJID) == "" {
		jsonError(w, 400, "chat_jid required")
		return
	}
	if s.store.IsChatExcludedFromAI(req.ChatJID) {
		jsonError(w, 403, "AI features are disabled for hidden and archived chats")
		return
	}

	label := req.ChatName
	if label == "" {
		label = req.ChatJID
	}
	// Meetings need longer than tasks do. Finding one means reading a chat in
	// order, spotting threads that span days, and searching OTHER chats for the
	// same join link — so a busy group runs far past the 15 minutes that suits
	// task extraction. At 15 minutes the three biggest chats were killed
	// mid-run; 45 gives them room, and the caller can raise it further.
	timeout := 45 * time.Minute
	if req.TimeoutMinutes > 0 {
		timeout = time.Duration(req.TimeoutMinutes) * time.Minute
	}
	run, ctx := s.runs.Start("meetings", req.ChatJID, label)
	go s.executeExtraction(ctx, run, timeout, "extract-meetings.mjs",
		req.ChatJID, req.ChatName, strconv.FormatInt(req.Since, 10))

	fmt.Printf("Meeting extraction starting for %s (run=%s)\n", req.ChatJID, run.ID)
	jsonOK(w, map[string]any{"run_id": run.ID})
}

// handleMeetingsResyncCircles re-derives the circles of every meeting.
//
// Circles change after the fact: you file a group into "NorthStudio" today, and
// every meeting ever held in it should belong there too. Rather than watch for
// circle edits from the meeting side, this recomputes the lot on demand — it is
// a cheap query per meeting and there will never be many.
// POST /api/v2/meetings/resync-circles
func (s *Server) handleMeetingsResyncCircles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	s.rememberOwnJID()
	list, err := s.store.ListMeetings(db.MeetingFilter{})
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	n := 0
	for _, m := range list {
		if s.store.SyncMeetingCircles(m.ID) == nil {
			n++
		}
	}
	jsonOK(w, map[string]any{"resynced": n})
}

// handleMeetingChats lists the chats that have an upcoming meeting, so the chat
// list can offer a "Meetings" filter without asking per chat.
// GET /api/v2/meetings/chats -> {"chat_jids": [...]}
func (s *Server) handleMeetingChats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	set, err := s.store.UpcomingMeetingChatJIDs()
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	// Dual-identity expansion, same as the chat list does for hidden chats.
	// Participants are recorded under whichever JID the chat used — often the
	// @lid form taken from group info — while the person's own conversation is
	// stored under their phone JID. Without both forms the filter would silently
	// miss every chat it reached through a participant.
	for jid := range set {
		if alt := s.client.ResolveLIDForJID(jid); alt != "" {
			set[alt] = true
		} else if alt := s.client.ResolvePhoneForLID(jid); alt != "" {
			set[alt] = true
		}
	}
	out := make([]string, 0, len(set))
	for jid := range set {
		out = append(out, jid)
	}
	jsonOK(w, map[string]any{"chat_jids": out})
}
