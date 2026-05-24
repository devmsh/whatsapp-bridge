package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// ── Tool definitions ────────────────────────────────────────────────

func toolCreateTask() mcp.Tool {
	return mcp.NewTool("wa_create_task",
		mcp.WithDescription("Create a task extracted from WhatsApp content. Returns the created task as JSON including its numeric id (use that id with wa_link_task_message to attach more messages). The origin message is linked automatically."),
		mcp.WithString("title", mcp.Required(), mcp.Description("Short task title")),
		mcp.WithString("description", mcp.Description("Longer description / context")),
		mcp.WithString("assignee_jid", mcp.Description("JID of the person the task is assigned to (e.g. 966535435254@s.whatsapp.net). Resolve names/mentions with wa_find_contact or wa_group_info first.")),
		mcp.WithString("priority", mcp.Description("low | normal | high")),
		mcp.WithNumber("due_at", mcp.Description("Due date as a Unix epoch timestamp (seconds), or omit for none")),
		mcp.WithString("origin_chat_jid", mcp.Description("Chat JID where the task started")),
		mcp.WithString("origin_message_id", mcp.Description("Message id where the task started (in origin_chat_jid)")),
		mcp.WithNumber("circle_id", mcp.Description("Optional numeric circle id to pin this task to (use the circle you are analyzing)")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{ReadOnlyHint: mcp.ToBoolPtr(false), DestructiveHint: mcp.ToBoolPtr(false)}),
	)
}

func toolLinkTaskMessage() mcp.Tool {
	return mcp.NewTool("wa_link_task_message",
		mcp.WithDescription("Link a WhatsApp message to an existing task. Use role 'completion' when the message shows the task was done (auto-marks the task done, even if in a different chat), 'comment' for related discussion/updates, 'attachment' for files, or 'related'. This is how a task spans multiple chats/groups."),
		mcp.WithNumber("task_id", mcp.Required(), mcp.Description("Numeric task id (from wa_create_task or wa_list_tasks)")),
		mcp.WithString("chat_jid", mcp.Required(), mcp.Description("Chat JID of the message")),
		mcp.WithString("message_id", mcp.Description("Message id (omit to link the whole chat)")),
		mcp.WithString("role", mcp.Description("origin | completion | comment | attachment | related (default related)")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{ReadOnlyHint: mcp.ToBoolPtr(false), DestructiveHint: mcp.ToBoolPtr(false)}),
	)
}

func toolListTasks() mcp.Tool {
	return mcp.NewTool("wa_list_tasks",
		mcp.WithDescription("List existing tasks (id, title, status, assignee, due). Use before creating to avoid duplicates and to find a task to link a message to. Optionally filter to tasks linked to a chat."),
		mcp.WithString("chat_jid", mcp.Description("Optional: only tasks linked to this chat")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{ReadOnlyHint: mcp.ToBoolPtr(true)}),
	)
}

// ── Handlers ────────────────────────────────────────────────────────

func (s *Server) handleCreateTask(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	title, _ := args["title"].(string)
	if title == "" {
		return mcp.NewToolResultError("title is required"), nil
	}
	payload := map[string]any{"title": title}
	if v, _ := args["description"].(string); v != "" {
		payload["description"] = v
	}
	if v, _ := args["assignee_jid"].(string); v != "" {
		payload["assignee_jid"] = v
	}
	if v, _ := args["priority"].(string); v != "" {
		payload["priority"] = v
	}
	if v, ok := args["due_at"].(float64); ok && v > 0 {
		payload["due_at"] = int64(v)
	}
	if v, _ := args["origin_chat_jid"].(string); v != "" {
		payload["origin_chat_jid"] = v
	}
	if v, _ := args["origin_message_id"].(string); v != "" {
		payload["origin_message_id"] = v
	}
	if v, ok := args["circle_id"].(float64); ok && v > 0 {
		payload["circle_id"] = int64(v)
	}
	// AI-created tasks land in the review queue by default — the user accepts
	// or rejects each one in the triage inbox.
	payload["review_status"] = "pending_review"
	return s.postAPIAny("/tasks", payload)
}

func (s *Server) handleLinkTaskMessage(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	idF, ok := args["task_id"].(float64)
	if !ok || idF <= 0 {
		return mcp.NewToolResultError("task_id is required"), nil
	}
	chatJID, _ := args["chat_jid"].(string)
	if chatJID == "" {
		return mcp.NewToolResultError("chat_jid is required"), nil
	}
	payload := map[string]any{"chat_jid": chatJID}
	if v, _ := args["message_id"].(string); v != "" {
		payload["message_id"] = v
	}
	if v, _ := args["role"].(string); v != "" {
		payload["role"] = v
	}
	return s.postAPIAny(fmt.Sprintf("/tasks/%d/messages", int64(idF)), payload)
}

func (s *Server) handleListTasks(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	chatJID, _ := args["chat_jid"].(string)

	query := `SELECT id, title, status, priority, assignee_jid, due_at, completed_at FROM tasks`
	var queryArgs []any
	if chatJID != "" {
		query += ` WHERE id IN (SELECT task_id FROM task_messages WHERE chat_jid = ?)`
		queryArgs = append(queryArgs, chatJID)
	}
	query += ` ORDER BY updated_at DESC LIMIT 200`

	rows, err := s.db.Query(query, queryArgs...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query failed: %v", err)), nil
	}
	defer rows.Close()

	type task struct {
		ID          int64  `json:"id"`
		Title       string `json:"title"`
		Status      string `json:"status"`
		Priority    string `json:"priority"`
		AssigneeJID string `json:"assignee_jid,omitempty"`
		DueAt       int64  `json:"due_at,omitempty"`
		Due         string `json:"due,omitempty"`
		CompletedAt int64  `json:"completed_at,omitempty"`
	}
	out := []task{}
	for rows.Next() {
		var t task
		if rows.Scan(&t.ID, &t.Title, &t.Status, &t.Priority, &t.AssigneeJID, &t.DueAt, &t.CompletedAt) != nil {
			continue
		}
		if t.DueAt > 0 {
			t.Due = time.Unix(t.DueAt, 0).Format("2006-01-02")
		}
		out = append(out, t)
	}
	b, _ := json.Marshal(map[string]any{"count": len(out), "tasks": out})
	return mcp.NewToolResultText(string(b)), nil
}
