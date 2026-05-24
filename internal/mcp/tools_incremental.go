package mcp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// ── Tool definitions ────────────────────────────────────────────────

func toolChatSince() mcp.Tool {
	return mcp.NewTool("wa_chat_since",
		mcp.WithDescription("Return the 'since' Unix timestamp to use with wa_scan for THIS chat in THIS extraction run. First-ever run: returns 1 (full history). After a previous successful run: returns the watermark, so wa_scan only returns NEW messages. ALWAYS call this before wa_scan to keep runs cheap."),
		mcp.WithString("chat_jid", mcp.Required(), mcp.Description("Chat JID (group or DM)")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{ReadOnlyHint: mcp.ToBoolPtr(true)}),
	)
}

func toolMarkExtracted() mcp.Tool {
	return mcp.NewTool("wa_mark_extracted",
		mcp.WithDescription("Call AFTER you have finished scanning all pages of a chat. It advances the per-chat watermark to the chat's current max message timestamp so the next extraction run only sees newer messages. Optional session_id is recorded for audit."),
		mcp.WithString("chat_jid", mcp.Required(), mcp.Description("Chat JID (group or DM)")),
		mcp.WithString("session_id", mcp.Description("Optional Claude session id of this extraction run")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{ReadOnlyHint: mcp.ToBoolPtr(false), DestructiveHint: mcp.ToBoolPtr(false)}),
	)
}

// ── Handlers ────────────────────────────────────────────────────────

func (s *Server) handleChatSince(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	jid, _ := args["chat_jid"].(string)
	if jid == "" {
		return mcp.NewToolResultError("chat_jid is required"), nil
	}
	var ts int64
	s.db.QueryRow(`SELECT last_msg_ts FROM chat_extraction_state WHERE chat_jid = ?`, jid).Scan(&ts)
	since := ts
	if since <= 0 {
		since = 1 // never extracted → full history
	}
	b, _ := json.Marshal(map[string]any{
		"chat_jid":   jid,
		"since":      since,
		"first_run":  ts == 0,
		"since_time": time.Unix(since, 0).UTC().Format("2006-01-02 15:04:05"),
	})
	return mcp.NewToolResultText(string(b)), nil
}

// handleMarkExtracted writes via the bridge REST API: the MCP server opens
// SQLite read-only, so all writes go through the bridge (same pattern as
// wa_create_task / wa_link_task_message).
func (s *Server) handleMarkExtracted(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	jid, _ := args["chat_jid"].(string)
	if jid == "" {
		return mcp.NewToolResultError("chat_jid is required"), nil
	}
	sessionID, _ := args["session_id"].(string)
	return s.postAPIAny("/extractions/mark", map[string]any{
		"chat_jid":   jid,
		"session_id": sessionID,
	})
}
