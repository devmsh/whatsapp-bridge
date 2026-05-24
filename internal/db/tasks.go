package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Task statuses and link roles.
const (
	TaskOpen       = "open"
	TaskInProgress = "in_progress"
	TaskDone       = "done"
	TaskCancelled  = "cancelled"

	RoleOrigin     = "origin"
	RoleCompletion = "completion"
	RoleComment    = "comment"
	RoleAttachment = "attachment"
	RoleRelated    = "related"

	ReviewPending  = "pending_review"
	ReviewAccepted = "accepted"
	ReviewRejected = "rejected"
)

// Task is a work item built on WhatsApp content. It may span multiple chats.
type Task struct {
	ID              int64   `json:"id"`
	Title           string  `json:"title"`
	Description     string  `json:"description"`
	Status          string  `json:"status"`
	Priority        string  `json:"priority"`
	AssigneeJID     string  `json:"assignee_jid"`
	CreatorJID      string  `json:"creator_jid"`
	DueAt           int64   `json:"due_at"`
	CompletedAt     int64   `json:"completed_at"`
	OriginChatJID   string  `json:"origin_chat_jid"`
	OriginMessageID string  `json:"origin_message_id"`
	ReviewStatus    string  `json:"review_status"` // pending_review | accepted | rejected
	CreatedAt       int64   `json:"created_at"`
	UpdatedAt       int64   `json:"updated_at"`
	MessageCount    int     `json:"message_count"`         // computed
	CircleIDs       []int64 `json:"circle_ids,omitempty"`  // computed
}

// TaskMessageLink is a linked message, enriched with its content for display.
type TaskMessageLink struct {
	TaskID     int64  `json:"task_id"`
	ChatJID    string `json:"chat_jid"`
	MessageID  string `json:"message_id"`
	Role       string `json:"role"`
	AddedAt    int64  `json:"added_at"`
	Sender     string `json:"sender,omitempty"`
	SenderName string `json:"sender_name,omitempty"`
	PushName   string `json:"push_name,omitempty"`
	Content    string `json:"content,omitempty"`
	Timestamp  int64  `json:"timestamp,omitempty"`
	IsFromMe   bool   `json:"is_from_me,omitempty"`
	IsGroup    bool   `json:"is_group,omitempty"`
	MediaType  string `json:"media_type,omitempty"`
	MediaPath  string `json:"media_path,omitempty"`
}

func taskColumns() string {
	return `id, title, description, status, priority, assignee_jid, creator_jid,
		due_at, completed_at, origin_chat_jid, origin_message_id, review_status, created_at, updated_at`
}

func scanTask(sc scanner, t *Task) error {
	return sc.Scan(&t.ID, &t.Title, &t.Description, &t.Status, &t.Priority, &t.AssigneeJID,
		&t.CreatorJID, &t.DueAt, &t.CompletedAt, &t.OriginChatJID, &t.OriginMessageID,
		&t.ReviewStatus, &t.CreatedAt, &t.UpdatedAt)
}

// CreateTask inserts a task and an origin link if an origin message is given.
func (s *Store) CreateTask(t *Task) (*Task, error) {
	now := time.Now().Unix()
	t.CreatedAt = now
	t.UpdatedAt = now
	if t.Status == "" {
		t.Status = TaskOpen
	}
	if t.Priority == "" {
		t.Priority = "normal"
	}
	if t.Status == TaskDone && t.CompletedAt == 0 {
		t.CompletedAt = now
	}
	if t.ReviewStatus == "" {
		t.ReviewStatus = ReviewAccepted // manual creates skip review by default
	}
	res, err := s.DB.Exec(`INSERT INTO tasks
		(title, description, status, priority, assignee_jid, creator_jid, due_at, completed_at,
		 origin_chat_jid, origin_message_id, review_status, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.Title, t.Description, t.Status, t.Priority, t.AssigneeJID, t.CreatorJID, t.DueAt,
		t.CompletedAt, t.OriginChatJID, t.OriginMessageID, t.ReviewStatus, t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	t.ID, _ = res.LastInsertId()
	if t.OriginChatJID != "" {
		s.LinkTaskMessage(t.ID, t.OriginChatJID, t.OriginMessageID, RoleOrigin)
	}
	return t, nil
}

// GetTask returns a task with its message count and circle ids.
func (s *Store) GetTask(id int64) (*Task, error) {
	t := &Task{}
	err := scanTask(s.DB.QueryRow(`SELECT `+taskColumns()+` FROM tasks WHERE id = ?`, id), t)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.DB.QueryRow(`SELECT COUNT(*) FROM task_messages WHERE task_id = ?`, id).Scan(&t.MessageCount)
	t.CircleIDs = s.taskCircleIDs(id)
	return t, nil
}

func (s *Store) scanTaskList(rows *sql.Rows) ([]Task, error) {
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		var t Task
		if err := scanTask(rows, &t); err != nil {
			return out, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	// enrich counts + circles
	for i := range out {
		s.DB.QueryRow(`SELECT COUNT(*) FROM task_messages WHERE task_id = ?`, out[i].ID).Scan(&out[i].MessageCount)
		out[i].CircleIDs = s.taskCircleIDs(out[i].ID)
	}
	return out, nil
}

// SetTaskReview moves a task to accepted or rejected. Used by the triage UI.
func (s *Store) SetTaskReview(id int64, review string) error {
	switch review {
	case ReviewPending, ReviewAccepted, ReviewRejected:
	default:
		return fmt.Errorf("invalid review status %q", review)
	}
	_, err := s.DB.Exec(`UPDATE tasks SET review_status = ?, updated_at = ? WHERE id = ?`,
		review, time.Now().Unix(), id)
	return err
}

// CountTasksByReview returns a map review_status -> count (only accepted+pending are typically interesting).
func (s *Store) CountTasksByReview() map[string]int {
	out := map[string]int{}
	rows, err := s.DB.Query(`SELECT review_status, COUNT(*) FROM tasks GROUP BY review_status`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if rows.Scan(&k, &n) == nil {
			out[k] = n
		}
	}
	return out
}

// ListTasks returns tasks, optionally filtered by status and/or assignee.
func (s *Store) ListTasks(status, assignee string) ([]Task, error) {
	q := `SELECT ` + taskColumns() + ` FROM tasks`
	var conds []string
	var args []interface{}
	if status != "" {
		conds = append(conds, "status = ?")
		args = append(args, status)
	}
	if assignee != "" {
		conds = append(conds, "assignee_jid = ?")
		args = append(args, assignee)
	}
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY (status='done' OR status='cancelled'), COALESCE(NULLIF(due_at,0), 9999999999), updated_at DESC"
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	return s.scanTaskList(rows)
}

// TasksForChat returns tasks linked to any message in the given chat.
func (s *Store) TasksForChat(chatJID string) ([]Task, error) {
	rows, err := s.DB.Query(`SELECT `+taskColumns()+` FROM tasks WHERE id IN
		(SELECT task_id FROM task_messages WHERE chat_jid = ?) ORDER BY updated_at DESC`, chatJID)
	if err != nil {
		return nil, err
	}
	return s.scanTaskList(rows)
}

// TasksForCircle returns tasks pinned to the circle or whose linked chats belong
// to it (including nested circles).
func (s *Store) TasksForCircle(circleID int64) ([]Task, error) {
	ids := map[int64]bool{}
	// explicitly pinned
	if rows, err := s.DB.Query(`SELECT task_id FROM task_circles WHERE circle_id = ?`, circleID); err == nil {
		for rows.Next() {
			var tid int64
			if rows.Scan(&tid) == nil {
				ids[tid] = true
			}
		}
		rows.Close()
	}
	// derived from linked chats in the circle
	chats, _ := s.FlattenCircleChats(circleID)
	for _, c := range chats {
		if rows, err := s.DB.Query(`SELECT task_id FROM task_messages WHERE chat_jid = ?`, c); err == nil {
			for rows.Next() {
				var tid int64
				if rows.Scan(&tid) == nil {
					ids[tid] = true
				}
			}
			rows.Close()
		}
	}
	if len(ids) == 0 {
		return []Task{}, nil
	}
	placeholders := make([]string, 0, len(ids))
	args := make([]interface{}, 0, len(ids))
	for id := range ids {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	rows, err := s.DB.Query(`SELECT `+taskColumns()+` FROM tasks WHERE id IN (`+
		strings.Join(placeholders, ",")+`) ORDER BY updated_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	return s.scanTaskList(rows)
}

// UpdateTask updates the editable fields. completed_at follows the status.
func (s *Store) UpdateTask(t *Task) error {
	now := time.Now().Unix()
	completedAt := t.CompletedAt
	if t.Status == TaskDone {
		if completedAt == 0 {
			completedAt = now
		}
	} else {
		completedAt = 0
	}
	_, err := s.DB.Exec(`UPDATE tasks SET title=?, description=?, status=?, priority=?,
		assignee_jid=?, due_at=?, completed_at=?, updated_at=? WHERE id=?`,
		t.Title, t.Description, t.Status, t.Priority, t.AssigneeJID, t.DueAt, completedAt, now, t.ID)
	return err
}

// DeleteTask removes a task and its links (cascade).
func (s *Store) DeleteTask(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM tasks WHERE id = ?`, id)
	return err
}

// LinkTaskMessage links a message (or whole chat) to a task with a role. A
// 'completion' link marks the task done.
func (s *Store) LinkTaskMessage(taskID int64, chatJID, messageID, role string) error {
	if role == "" {
		role = RoleRelated
	}
	_, err := s.DB.Exec(`INSERT OR IGNORE INTO task_messages (task_id, chat_jid, message_id, role, added_at)
		VALUES (?,?,?,?,?)`, taskID, chatJID, messageID, role, time.Now().Unix())
	if err != nil {
		return err
	}
	if role == RoleCompletion {
		now := time.Now().Unix()
		s.DB.Exec(`UPDATE tasks SET status=?, completed_at=CASE WHEN completed_at=0 THEN ? ELSE completed_at END, updated_at=? WHERE id=?`,
			TaskDone, now, now, taskID)
	}
	return nil
}

// UnlinkTaskMessage removes a message link.
func (s *Store) UnlinkTaskMessage(taskID int64, chatJID, messageID, role string) error {
	_, err := s.DB.Exec(`DELETE FROM task_messages WHERE task_id=? AND chat_jid=? AND message_id=? AND role=?`,
		taskID, chatJID, messageID, role)
	return err
}

// GetTaskMessages returns a task's linked messages, enriched with content and
// ordered chronologically.
func (s *Store) GetTaskMessages(taskID int64) ([]TaskMessageLink, error) {
	rows, err := s.DB.Query(`SELECT tm.chat_jid, tm.message_id, tm.role, tm.added_at,
		COALESCE(m.sender,''), COALESCE(m.sender_name,''), COALESCE(m.push_name,''),
		COALESCE(m.content,''), COALESCE(m.timestamp,0), COALESCE(m.is_from_me,0),
		COALESCE(m.is_group,0), COALESCE(m.media_type,''), COALESCE(m.media_path,'')
		FROM task_messages tm
		LEFT JOIN messages m ON m.id = tm.message_id AND m.chat_jid = tm.chat_jid
		WHERE tm.task_id = ?
		ORDER BY COALESCE(m.timestamp, tm.added_at)`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskMessageLink{}
	for rows.Next() {
		l := TaskMessageLink{TaskID: taskID}
		if err := rows.Scan(&l.ChatJID, &l.MessageID, &l.Role, &l.AddedAt, &l.Sender, &l.SenderName,
			&l.PushName, &l.Content, &l.Timestamp, &l.IsFromMe, &l.IsGroup, &l.MediaType, &l.MediaPath); err != nil {
			return out, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// --- task <-> circle ---

func (s *Store) taskCircleIDs(taskID int64) []int64 {
	var out []int64
	rows, err := s.DB.Query(`SELECT circle_id FROM task_circles WHERE task_id = ?`, taskID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// AddTaskCircle / RemoveTaskCircle pin or unpin a task to a circle.
func (s *Store) AddTaskCircle(taskID, circleID int64) error {
	_, err := s.DB.Exec(`INSERT OR IGNORE INTO task_circles (task_id, circle_id, added_at) VALUES (?,?,?)`,
		taskID, circleID, time.Now().Unix())
	return err
}

func (s *Store) RemoveTaskCircle(taskID, circleID int64) error {
	_, err := s.DB.Exec(`DELETE FROM task_circles WHERE task_id=? AND circle_id=?`, taskID, circleID)
	return err
}
