package api

import (
	"net/http"
	"strconv"
	"strings"

	"whatsapp-bridge-v2/internal/db"
)

// handleTasks: GET list (filters), POST create.
// GET /api/v2/tasks?status=&assignee=&chat=&circle=
func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query()
		var tasks []db.Task
		var err error
		switch {
		case q.Get("chat") != "":
			tasks, err = s.store.TasksForChat(q.Get("chat"))
		case q.Get("circle") != "":
			if id, e := strconv.ParseInt(q.Get("circle"), 10, 64); e == nil {
				tasks, err = s.store.TasksForCircle(id)
			}
		default:
			tasks, err = s.store.ListTasks(q.Get("status"), q.Get("assignee"))
		}
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		if tasks == nil {
			tasks = []db.Task{}
		}
		jsonOK(w, tasks)
	case http.MethodPost:
		var req struct {
			Title           string `json:"title"`
			Description     string `json:"description"`
			Status          string `json:"status"`
			Priority        string `json:"priority"`
			AssigneeJID     string `json:"assignee_jid"`
			DueAt           int64  `json:"due_at"`
			OriginChatJID   string `json:"origin_chat_jid"`
			OriginMessageID string `json:"origin_message_id"`
			CircleID        int64  `json:"circle_id"`
		}
		if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Title) == "" {
			jsonError(w, 400, "title required")
			return
		}
		t, err := s.store.CreateTask(&db.Task{
			Title:           strings.TrimSpace(req.Title),
			Description:     req.Description,
			Status:          req.Status,
			Priority:        req.Priority,
			AssigneeJID:     req.AssigneeJID,
			DueAt:           req.DueAt,
			OriginChatJID:   req.OriginChatJID,
			OriginMessageID: req.OriginMessageID,
		})
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		if req.CircleID != 0 {
			s.store.AddTaskCircle(t.ID, req.CircleID)
		}
		full, _ := s.store.GetTask(t.ID)
		jsonCreated(w, full)
	default:
		methodNotAllowed(w)
	}
}

// handleTaskByID routes /api/v2/tasks/{id} and sub-paths {id}/messages, {id}/circles.
func (s *Server) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v2/tasks/")
	parts := strings.SplitN(path, "/", 2)
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		jsonError(w, 400, "invalid task id")
		return
	}
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}
	switch sub {
	case "messages":
		s.handleTaskMessages(w, r, id)
	case "circles":
		s.handleTaskCircles(w, r, id)
	default:
		s.handleTaskEntity(w, r, id)
	}
}

// handleTaskEntity: GET detail (+messages), PUT update, DELETE.
func (s *Server) handleTaskEntity(w http.ResponseWriter, r *http.Request, id int64) {
	switch r.Method {
	case http.MethodGet:
		t, err := s.store.GetTask(id)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		if t == nil {
			jsonError(w, 404, "task not found")
			return
		}
		msgs, _ := s.store.GetTaskMessages(id)
		if msgs == nil {
			msgs = []db.TaskMessageLink{}
		}
		jsonOK(w, map[string]interface{}{"task": t, "messages": msgs})
	case http.MethodPut:
		t, err := s.store.GetTask(id)
		if err != nil || t == nil {
			jsonError(w, 404, "task not found")
			return
		}
		var req struct {
			Title       *string `json:"title"`
			Description *string `json:"description"`
			Status      *string `json:"status"`
			Priority    *string `json:"priority"`
			AssigneeJID *string `json:"assignee_jid"`
			DueAt       *int64  `json:"due_at"`
		}
		if err := decodeJSON(r, &req); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		if req.Title != nil {
			t.Title = strings.TrimSpace(*req.Title)
		}
		if req.Description != nil {
			t.Description = *req.Description
		}
		if req.Status != nil {
			t.Status = *req.Status
		}
		if req.Priority != nil {
			t.Priority = *req.Priority
		}
		if req.AssigneeJID != nil {
			t.AssigneeJID = *req.AssigneeJID
		}
		if req.DueAt != nil {
			t.DueAt = *req.DueAt
		}
		if t.Title == "" {
			jsonError(w, 400, "title required")
			return
		}
		if err := s.store.UpdateTask(t); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		full, _ := s.store.GetTask(id)
		jsonOK(w, full)
	case http.MethodDelete:
		if err := s.store.DeleteTask(id); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, map[string]bool{"success": true})
	default:
		methodNotAllowed(w)
	}
}

// handleTaskMessages: GET list, POST link, DELETE unlink.
// body: {"chat_jid","message_id","role"}
func (s *Server) handleTaskMessages(w http.ResponseWriter, r *http.Request, id int64) {
	switch r.Method {
	case http.MethodGet:
		msgs, err := s.store.GetTaskMessages(id)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		if msgs == nil {
			msgs = []db.TaskMessageLink{}
		}
		jsonOK(w, msgs)
	case http.MethodPost, http.MethodDelete:
		var req struct {
			ChatJID   string `json:"chat_jid"`
			MessageID string `json:"message_id"`
			Role      string `json:"role"`
		}
		if err := decodeJSON(r, &req); err != nil || req.ChatJID == "" {
			jsonError(w, 400, "chat_jid required")
			return
		}
		if r.Method == http.MethodPost {
			if err := s.store.LinkTaskMessage(id, req.ChatJID, req.MessageID, req.Role); err != nil {
				jsonError(w, 500, err.Error())
				return
			}
		} else {
			if err := s.store.UnlinkTaskMessage(id, req.ChatJID, req.MessageID, req.Role); err != nil {
				jsonError(w, 500, err.Error())
				return
			}
		}
		jsonOK(w, map[string]bool{"success": true})
	default:
		methodNotAllowed(w)
	}
}

// handleTaskCircles: POST add, DELETE remove. body: {"circle_id"}
func (s *Server) handleTaskCircles(w http.ResponseWriter, r *http.Request, id int64) {
	var req struct {
		CircleID int64 `json:"circle_id"`
	}
	if err := decodeJSON(r, &req); err != nil || req.CircleID == 0 {
		jsonError(w, 400, "circle_id required")
		return
	}
	switch r.Method {
	case http.MethodPost:
		if err := s.store.AddTaskCircle(id, req.CircleID); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
	case http.MethodDelete:
		if err := s.store.RemoveTaskCircle(id, req.CircleID); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
	default:
		methodNotAllowed(w)
		return
	}
	jsonOK(w, map[string]bool{"success": true})
}
