package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"

	"whatsapp-bridge-v2/internal/db"
)

// Meeting tools for the extraction sidecar.
//
// A meeting is deliberately harder to create than a task. Most Meet links in a
// chat are someone saying "jump on" — a call, not a meeting. The tool
// descriptions carry that rule, because the model reads them as the spec.

// ── Tool definitions ────────────────────────────────────────────────

func toolCreateMeeting() mcp.Tool {
	return mcp.NewTool("wa_create_meeting",
		mcp.WithDescription(
			"Create a PREPARED meeting found in chat. Returns JSON including the numeric id.\n\n"+
				"ONLY create a meeting when it was arranged in advance. Strong signals: a day or "+
				"time named before the event ('الخميس الساعة ٥', 'bukra 4pm'), a stated purpose or "+
				"agenda, a request to confirm attendance, preparation talk, or a Google Calendar "+
				"invite ('has invited you to join a video meeting').\n\n"+
				"Do NOT create one for a bare link with no plan ('https://meet.google.com/xxx', "+
				"'يلا بانتظارك') — that is an ad-hoc call. When unsure, skip it.\n\n"+
				"ALWAYS call wa_find_meeting FIRST. The same meeting is often arranged across "+
				"several chats, and must become ONE meeting with messages linked from each.",
		),
		mcp.WithString("title", mcp.Required(), mcp.Description("Short title, e.g. 'IC weekly review' or 'MinX platform kickoff'")),
		mcp.WithString("purpose", mcp.Description("Why this meeting is happening, in one or two lines")),
		mcp.WithString("status", mcp.Description("proposed (a time is still being agreed) | confirmed | held | cancelled. Default proposed.")),
		mcp.WithNumber("starts_at", mcp.Description("Start as a Unix epoch timestamp (seconds). Omit when no time has been agreed yet.")),
		mcp.WithNumber("ends_at", mcp.Description("End as a Unix epoch timestamp (seconds), if known")),
		mcp.WithString("time_options", mcp.Description(`JSON array of slots still on the table when nothing is fixed, e.g. ["Sat 17:00","Sun 20:30"]`)),
		mcp.WithString("mode", mcp.Description("online | in_person | hybrid")),
		mcp.WithString("location", mcp.Description("Place in words for an in-person meeting, e.g. 'مكتبنا بجاده ٣٠'. Include any Google Maps link that was shared — it is picked up automatically.")),
		mcp.WithString("location_url", mcp.Description("Google Maps link if one was shared separately (maps.app.goo.gl/... )")),
		mcp.WithNumber("location_lat", mcp.Description("Latitude, when someone shared a WhatsApp location pin")),
		mcp.WithNumber("location_lng", mcp.Description("Longitude, when someone shared a WhatsApp location pin")),
		mcp.WithString("link", mcp.Description("Join link (Google Meet / Zoom). The join code is derived automatically and is what ties the same meeting across chats.")),
		mcp.WithString("recurrence", mcp.Description("Repeat rule in the words used, e.g. 'اجتماعنا الاسبوعي السبت ٥ م، ويستثنى السبت القادم'")),
		mcp.WithNumber("prepares_meeting_id", mcp.Description("Id of the meeting THIS one is preparing for, when it is a pre-meeting ('نجلس الأحد قبل اجتماع orbit')")),
		mcp.WithString("origin_chat_jid", mcp.Required(), mcp.Description("Chat where the meeting was first arranged")),
		mcp.WithString("origin_message_id", mcp.Description("Message id that started it, in origin_chat_jid")),
		mcp.WithNumber("confidence", mcp.Description("0..1 — how sure you are this is a real prepared meeting")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{ReadOnlyHint: mcp.ToBoolPtr(false), DestructiveHint: mcp.ToBoolPtr(false)}),
	)
}

func toolFindMeeting() mcp.Tool {
	return mcp.NewTool("wa_find_meeting",
		mcp.WithDescription(
			"Look for a meeting that already exists before creating one. Search by join link "+
				"(strongest — the same link in another chat is the SAME meeting) or by words from "+
				"the title. Returns matching meetings as JSON, or an empty list.",
		),
		mcp.WithString("link", mcp.Description("A Google Meet / Zoom link seen in the chat")),
		mcp.WithString("query", mcp.Description("Words from the title or purpose")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{ReadOnlyHint: mcp.ToBoolPtr(true)}),
	)
}

func toolLinkMeetingMessage() mcp.Tool {
	return mcp.NewTool("wa_link_meeting_message",
		mcp.WithDescription(
			"Attach a message to a meeting. This is how one meeting spans several chats: link "+
				"the invite from each DM, the prep talk, and the follow-up, wherever they were said.",
		),
		mcp.WithNumber("meeting_id", mcp.Required(), mcp.Description("Numeric meeting id")),
		mcp.WithString("chat_jid", mcp.Required(), mcp.Description("Chat JID of the message")),
		mcp.WithString("message_id", mcp.Required(), mcp.Description("Message id within that chat")),
		mcp.WithString("role", mcp.Description("origin | scheduling | agenda | requirement | prep | invite | note | next_step | related")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{ReadOnlyHint: mcp.ToBoolPtr(false), DestructiveHint: mcp.ToBoolPtr(false)}),
	)
}

func toolAddMeetingParticipant() mcp.Tool {
	return mcp.NewTool("wa_add_meeting_participant",
		mcp.WithDescription(
			"Add someone to a meeting. Include people invited through OTHER chats — a meeting "+
				"often has no group, and the participants are only discoverable across DMs. "+
				"Resolve names to JIDs with wa_find_contact or wa_group_info first.",
		),
		mcp.WithNumber("meeting_id", mcp.Required(), mcp.Description("Numeric meeting id")),
		mcp.WithString("jid", mcp.Required(), mcp.Description("Participant JID")),
		mcp.WithString("name", mcp.Description("Display name as seen in chat")),
		mcp.WithString("role", mcp.Description("organizer | required | optional | external")),
		mcp.WithString("rsvp", mcp.Description("unknown | yes | no | maybe — set when someone confirmed or declined")),
		mcp.WithString("note", mcp.Description("Anything said about their attendance, e.g. 'عنده ميتنج مع الفاوندرز'")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{ReadOnlyHint: mcp.ToBoolPtr(false), DestructiveHint: mcp.ToBoolPtr(false)}),
	)
}

func toolAddMeetingItem() mcp.Tool {
	return mcp.NewTool("wa_add_meeting_item",
		mcp.WithDescription(
			"Add one agenda point, entry requirement, or next step to a meeting.\n\n"+
				"agenda      — what will be discussed.\n"+
				"requirement — something that must be true BEFORE the meeting, which often decides "+
				"whether it happens at all ('اي فريق بيحقق ٨٠٪+ تحديث على flow os').\n"+
				"next_step   — an action agreed afterwards. These can later become tasks.",
		),
		mcp.WithNumber("meeting_id", mcp.Required(), mcp.Description("Numeric meeting id")),
		mcp.WithString("kind", mcp.Required(), mcp.Description("agenda | requirement | next_step")),
		mcp.WithString("text", mcp.Required(), mcp.Description("The item, in one line")),
		mcp.WithString("owner_jid", mcp.Description("JID of whoever owns it, if named")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{ReadOnlyHint: mcp.ToBoolPtr(false), DestructiveHint: mcp.ToBoolPtr(false)}),
	)
}

// ── Handlers ────────────────────────────────────────────────────────

func (s *Server) handleCreateMeeting(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	title, _ := args["title"].(string)
	if title == "" {
		return mcp.NewToolResultError("title is required"), nil
	}
	originChat, _ := args["origin_chat_jid"].(string)
	if originChat == "" {
		return mcp.NewToolResultError("origin_chat_jid is required"), nil
	}
	payload := map[string]any{"title": title, "origin_chat_jid": originChat}
	for _, k := range []string{
		"purpose", "status", "time_options", "mode", "location", "location_url",
		"link", "recurrence", "origin_message_id",
	} {
		if v, _ := args[k].(string); v != "" {
			payload[k] = v
		}
	}
	for _, k := range []string{"starts_at", "ends_at", "prepares_meeting_id"} {
		if v, ok := args[k].(float64); ok && v > 0 {
			payload[k] = int64(v)
		}
	}
	for _, k := range []string{"location_lat", "location_lng", "confidence"} {
		if v, ok := args[k].(float64); ok && v != 0 {
			payload[k] = v
		}
	}
	// AI-found meetings land in the review queue. A wrong meeting is worse than
	// a wrong task — it carries people and a time — so none go straight in.
	payload["review_status"] = "pending_review"
	return s.postAPIAny("/meetings", payload)
}

func (s *Server) handleFindMeeting(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	link, _ := args["link"].(string)
	query, _ := args["query"].(string)

	sql := `SELECT id, title, purpose, status, starts_at, link, link_code, location
	        FROM meetings WHERE review_status != 'rejected'`
	var sqlArgs []any
	switch {
	case link != "":
		// The join code is the reliable key; the raw URL varies (?authuser=0).
		code := db.MeetingLinkCode(link)
		if code == "" {
			return mcp.NewToolResultText("[]"), nil
		}
		sql += ` AND link_code = ?`
		sqlArgs = append(sqlArgs, code)
	case query != "":
		sql += ` AND (title LIKE ? OR purpose LIKE ?)`
		like := "%" + query + "%"
		sqlArgs = append(sqlArgs, like, like)
	default:
		return mcp.NewToolResultError("pass link or query"), nil
	}
	sql += ` ORDER BY updated_at DESC LIMIT 20`

	rows, err := s.db.Query(sql, sqlArgs...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query failed: %v", err)), nil
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, starts int64
		var title, purpose, status, lnk, code, loc string
		if rows.Scan(&id, &title, &purpose, &status, &starts, &lnk, &code, &loc) != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "title": title, "purpose": purpose, "status": status,
			"starts_at": starts, "link": lnk, "link_code": code, "location": loc,
		})
	}
	b, _ := json.Marshal(out)
	return mcp.NewToolResultText(string(b)), nil
}

func (s *Server) handleLinkMeetingMessage(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	idF, ok := args["meeting_id"].(float64)
	if !ok || idF <= 0 {
		return mcp.NewToolResultError("meeting_id is required"), nil
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
	return s.postAPIAny(fmt.Sprintf("/meetings/%d/messages", int64(idF)), payload)
}

func (s *Server) handleAddMeetingParticipant(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	idF, ok := args["meeting_id"].(float64)
	if !ok || idF <= 0 {
		return mcp.NewToolResultError("meeting_id is required"), nil
	}
	jid, _ := args["jid"].(string)
	if jid == "" {
		return mcp.NewToolResultError("jid is required"), nil
	}
	payload := map[string]any{"jid": jid}
	for _, k := range []string{"name", "role", "rsvp", "note"} {
		if v, _ := args[k].(string); v != "" {
			payload[k] = v
		}
	}
	return s.postAPIAny(fmt.Sprintf("/meetings/%d/participants", int64(idF)), payload)
}

func (s *Server) handleAddMeetingItem(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	idF, ok := args["meeting_id"].(float64)
	if !ok || idF <= 0 {
		return mcp.NewToolResultError("meeting_id is required"), nil
	}
	kind, _ := args["kind"].(string)
	text, _ := args["text"].(string)
	if kind == "" || text == "" {
		return mcp.NewToolResultError("kind and text are required"), nil
	}
	switch kind {
	case "agenda", "requirement", "next_step":
	default:
		return mcp.NewToolResultError("kind must be agenda, requirement or next_step"), nil
	}
	payload := map[string]any{"kind": kind, "text": text}
	if v, _ := args["owner_jid"].(string); v != "" {
		payload["owner_jid"] = v
	}
	return s.postAPIAny(fmt.Sprintf("/meetings/%d/items", int64(idF)), payload)
}
