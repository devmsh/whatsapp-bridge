package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// ── Tool definitions ────────────────────────────────────────────────

func toolHealth() mcp.Tool {
	return mcp.NewTool("wa_health",
		mcp.WithDescription("Check WhatsApp bridge health: connection status, uptime, DB stats (message/chat/contact counts). Call this first to verify the bridge is running."),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			ReadOnlyHint: mcp.ToBoolPtr(true),
		}),
	)
}

func toolReadMessages() mcp.Tool {
	return mcp.NewTool("wa_read_messages",
		mcp.WithDescription("Read messages from a WhatsApp chat. Returns messages with sender names resolved. Use chat_jid from wa_list_chats or wa_find_contact."),
		mcp.WithString("chat_jid", mcp.Required(), mcp.Description("Chat JID (e.g. 966535435254@s.whatsapp.net or 120363406393924600@g.us)")),
		mcp.WithNumber("since", mcp.Description("Unix epoch timestamp — only messages after this time. Default: last 24 hours")),
		mcp.WithNumber("limit", mcp.Description("Max messages to return (default 50, max 500)")),
		mcp.WithString("search", mcp.Description("Filter messages containing this text")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			ReadOnlyHint: mcp.ToBoolPtr(true),
		}),
	)
}

func toolListChats() mcp.Tool {
	return mcp.NewTool("wa_list_chats",
		mcp.WithDescription("List WhatsApp chats ordered by last message time. Optionally filter by name or show only chats with unread messages."),
		mcp.WithBoolean("unread_only", mcp.Description("Only show chats with unread messages")),
		mcp.WithString("search", mcp.Description("Filter chats by name (case-insensitive)")),
		mcp.WithNumber("limit", mcp.Description("Max chats to return (default 50)")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			ReadOnlyHint: mcp.ToBoolPtr(true),
		}),
	)
}

func toolFindGroup() mcp.Tool {
	return mcp.NewTool("wa_find_group",
		mcp.WithDescription("Search WhatsApp groups by name. Returns the group JID, name, participant count, and last activity. Use this to find a group's JID from its name (e.g. 'ID8 Sports', 'One Studio')."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Group name to search for (partial match, case-insensitive)")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			ReadOnlyHint: mcp.ToBoolPtr(true),
		}),
	)
}

func toolSearchMessages() mcp.Tool {
	return mcp.NewTool("wa_search_messages",
		mcp.WithDescription("Search WhatsApp messages across ALL chats by content keyword. Returns matching messages with chat name, sender, and timestamp. Use when you need to find a specific message or conversation without knowing which chat it's in."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Text to search for in message content")),
		mcp.WithString("chat_jid", mcp.Description("Optional: limit search to a specific chat JID")),
		mcp.WithNumber("since", mcp.Description("Optional: only return messages after this Unix timestamp")),
		mcp.WithNumber("limit", mcp.Description("Max results to return (default 50)")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			ReadOnlyHint: mcp.ToBoolPtr(true),
		}),
	)
}

func toolFindContact() mcp.Tool {
	return mcp.NewTool("wa_find_contact",
		mcp.WithDescription("Search WhatsApp contacts by name, phone number, or JID. Returns matching contacts with all known identifiers."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search term — matches against name, push_name, phone, business_name")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			ReadOnlyHint: mcp.ToBoolPtr(true),
		}),
	)
}

func toolGroupInfo() mcp.Tool {
	return mcp.NewTool("wa_group_info",
		mcp.WithDescription("Get WhatsApp group details: name, participants (with admin status), and optionally recent messages."),
		mcp.WithString("jid", mcp.Required(), mcp.Description("Group JID (e.g. 120363406393924600@g.us)")),
		mcp.WithBoolean("include_messages", mcp.Description("Include recent messages (default false)")),
		mcp.WithNumber("message_limit", mcp.Description("Number of recent messages to include (default 20)")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			ReadOnlyHint: mcp.ToBoolPtr(true),
		}),
	)
}

func toolScan() mcp.Tool {
	return mcp.NewTool("wa_scan",
		mcp.WithDescription("Scan for new WhatsApp messages and groups since a timestamp. Returns enriched data for HQ intelligence pipeline. Use for /hq-scan."),
		mcp.WithNumber("since", mcp.Required(), mcp.Description("Unix epoch timestamp — scan messages/groups created after this time")),
		mcp.WithString("chat_jid", mcp.Description("Filter to a specific chat JID")),
		mcp.WithString("exclude", mcp.Description("Comma-separated JIDs to exclude")),
		mcp.WithNumber("limit", mcp.Description("Max messages to return (default 5000)")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			ReadOnlyHint: mcp.ToBoolPtr(true),
		}),
	)
}

// ── Handlers ────────────────────────────────────────────────────────

func (s *Server) handleHealth(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check bridge daemon via REST health endpoint
	bridgeConnected := false
	bridgeUptime := 0.0
	bridgeVersion := "unknown"

	resp, err := http.Get(s.apiURL + "/health")
	if err == nil {
		defer resp.Body.Close()
		var health struct {
			Connected bool    `json:"connected"`
			Uptime    float64 `json:"uptime"`
			Version   string  `json:"version"`
		}
		if json.NewDecoder(resp.Body).Decode(&health) == nil {
			bridgeConnected = health.Connected
			bridgeUptime = health.Uptime
			bridgeVersion = health.Version
		}
	}

	// DB stats from our read-only connection
	var msgCount, chatCount, contactCount int
	s.db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&msgCount)
	s.db.QueryRow("SELECT COUNT(*) FROM chats").Scan(&chatCount)
	s.db.QueryRow("SELECT COUNT(*) FROM contacts").Scan(&contactCount)

	result := map[string]any{
		"bridge_connected": bridgeConnected,
		"bridge_uptime":    bridgeUptime,
		"bridge_version":   bridgeVersion,
		"bridge_reachable": err == nil,
		"db_stats": map[string]int{
			"messages": msgCount,
			"chats":    chatCount,
			"contacts": contactCount,
		},
	}

	return marshalResult(result)
}

func (s *Server) handleReadMessages(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	chatJID, _ := args["chat_jid"].(string)
	if chatJID == "" {
		return mcp.NewToolResultError("chat_jid is required"), nil
	}

	since := time.Now().Add(-24 * time.Hour).Unix()
	if v, ok := args["since"].(float64); ok && v > 0 {
		since = int64(v)
	}

	limit := 50
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
		if limit > 500 {
			limit = 500
		}
	}

	search, _ := args["search"].(string)

	query := `SELECT m.id, m.chat_jid, m.sender, m.sender_name, m.push_name, m.content, m.timestamp,
		m.is_from_me, m.is_group, m.message_type, m.media_type, m.reply_to_id, m.reply_to_content,
		m.is_forwarded, m.mentions,
		COALESCE(ct.name, ct.push_name, ct.business_name, m.sender_name, m.sender, '') as resolved_name,
		COALESCE(ct.phone, '') as resolved_phone
		FROM messages m
		LEFT JOIN contacts ct ON ct.jid = m.sender
		WHERE m.chat_jid = ? AND m.timestamp > ?`
	queryArgs := []any{chatJID, since}

	if search != "" {
		query += " AND m.content LIKE ?"
		queryArgs = append(queryArgs, "%"+search+"%")
	}

	query += " ORDER BY m.timestamp ASC LIMIT ?"
	queryArgs = append(queryArgs, limit)

	rows, err := s.db.Query(query, queryArgs...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query failed: %v", err)), nil
	}
	defer rows.Close()

	type msg struct {
		ID             string `json:"id"`
		ChatJID        string `json:"chat_jid"`
		Sender         string `json:"sender"`
		SenderName     string `json:"sender_name"`
		PushName       string `json:"push_name"`
		Content        string `json:"content"`
		Timestamp      int64  `json:"timestamp"`
		Time           string `json:"time"`
		IsFromMe       bool   `json:"is_from_me"`
		IsGroup        bool   `json:"is_group"`
		MessageType    string `json:"message_type"`
		MediaType      string `json:"media_type,omitempty"`
		ReplyToID      string `json:"reply_to_id,omitempty"`
		ReplyToContent string `json:"reply_to_content,omitempty"`
		IsForwarded    bool   `json:"is_forwarded"`
		Mentions       string `json:"mentions,omitempty"`
		ResolvedName   string `json:"resolved_name"`
		ResolvedPhone  string `json:"resolved_phone,omitempty"`
	}

	var messages []msg
	for rows.Next() {
		var m msg
		if err := rows.Scan(&m.ID, &m.ChatJID, &m.Sender, &m.SenderName, &m.PushName,
			&m.Content, &m.Timestamp, &m.IsFromMe, &m.IsGroup, &m.MessageType,
			&m.MediaType, &m.ReplyToID, &m.ReplyToContent, &m.IsForwarded, &m.Mentions,
			&m.ResolvedName, &m.ResolvedPhone); err != nil {
			continue
		}
		m.Time = time.Unix(m.Timestamp, 0).Format("2006-01-02 15:04:05")
		messages = append(messages, m)
	}

	if messages == nil {
		messages = []msg{}
	}

	return marshalResult(map[string]any{
		"chat_jid": chatJID,
		"count":    len(messages),
		"messages": messages,
	})
}

func (s *Server) handleListChats(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	unreadOnly, _ := args["unread_only"].(bool)
	search, _ := args["search"].(string)
	limit := 50
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}

	// LEFT JOIN groups so group names are resolved even when chats.name is empty
	query := `SELECT c.jid,
		COALESCE(NULLIF(g.name,''), NULLIF(c.name,''), c.jid) as name,
		c.chat_type, c.last_message_at, c.unread_count,
		c.is_archived, c.is_pinned, c.is_muted
		FROM chats c
		LEFT JOIN groups g ON c.jid = g.jid
		WHERE 1=1`
	var queryArgs []any

	if unreadOnly {
		query += " AND c.unread_count > 0"
	}
	if search != "" {
		query += " AND (COALESCE(NULLIF(g.name,''), NULLIF(c.name,''), c.jid) LIKE ?)"
		queryArgs = append(queryArgs, "%"+search+"%")
	}
	query += " ORDER BY c.last_message_at DESC LIMIT ?"
	queryArgs = append(queryArgs, limit)

	rows, err := s.db.Query(query, queryArgs...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query failed: %v", err)), nil
	}
	defer rows.Close()

	type chat struct {
		JID           string `json:"jid"`
		Name          string `json:"name"`
		ChatType      string `json:"chat_type"`
		LastMessageAt int64  `json:"last_message_at"`
		LastMessageTS string `json:"last_message_time"`
		UnreadCount   int    `json:"unread_count"`
		IsArchived    bool   `json:"is_archived"`
		IsPinned      bool   `json:"is_pinned"`
		IsMuted       bool   `json:"is_muted"`
	}

	var chats []chat
	for rows.Next() {
		var c chat
		if err := rows.Scan(&c.JID, &c.Name, &c.ChatType, &c.LastMessageAt,
			&c.UnreadCount, &c.IsArchived, &c.IsPinned, &c.IsMuted); err != nil {
			continue
		}
		if c.LastMessageAt > 0 {
			c.LastMessageTS = time.Unix(c.LastMessageAt, 0).Format("2006-01-02 15:04:05")
		}
		chats = append(chats, c)
	}

	if chats == nil {
		chats = []chat{}
	}

	return marshalResult(map[string]any{
		"count": len(chats),
		"chats": chats,
	})
}

func (s *Server) handleFindContact(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	query, _ := args["query"].(string)
	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}

	q := "%" + query + "%"
	rows, err := s.db.Query(`SELECT jid, lid, phone, name, push_name, business_name,
		is_business, status_text, first_seen, last_seen
		FROM contacts
		WHERE name LIKE ? OR push_name LIKE ? OR phone LIKE ? OR business_name LIKE ? OR jid LIKE ?
		ORDER BY last_seen DESC LIMIT 20`, q, q, q, q, q)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query failed: %v", err)), nil
	}
	defer rows.Close()

	type contact struct {
		JID          string `json:"jid"`
		LID          string `json:"lid,omitempty"`
		Phone        string `json:"phone,omitempty"`
		Name         string `json:"name"`
		PushName     string `json:"push_name,omitempty"`
		BusinessName string `json:"business_name,omitempty"`
		IsBusiness   bool   `json:"is_business"`
		StatusText   string `json:"status_text,omitempty"`
		FirstSeen    int64  `json:"first_seen,omitempty"`
		LastSeen     int64  `json:"last_seen,omitempty"`
	}

	var contacts []contact
	for rows.Next() {
		var c contact
		if err := rows.Scan(&c.JID, &c.LID, &c.Phone, &c.Name, &c.PushName, &c.BusinessName,
			&c.IsBusiness, &c.StatusText, &c.FirstSeen, &c.LastSeen); err != nil {
			continue
		}
		contacts = append(contacts, c)
	}

	if contacts == nil {
		contacts = []contact{}
	}

	return marshalResult(map[string]any{
		"query":    query,
		"count":    len(contacts),
		"contacts": contacts,
	})
}

func (s *Server) handleGroupInfo(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	jid, _ := args["jid"].(string)
	if jid == "" {
		return mcp.NewToolResultError("jid is required"), nil
	}

	includeMessages, _ := args["include_messages"].(bool)
	messageLimit := 20
	if v, ok := args["message_limit"].(float64); ok && v > 0 {
		messageLimit = int(v)
	}

	// Group metadata
	var name, ownerJID, topic string
	var groupCreated int64
	var participantCount int
	var isAnnounce, isLocked, isParent bool

	err := s.db.QueryRow(`SELECT name, owner_jid, topic, group_created, participant_count,
		is_announce, is_locked, is_parent
		FROM groups WHERE jid = ?`, jid).Scan(&name, &ownerJID, &topic, &groupCreated,
		&participantCount, &isAnnounce, &isLocked, &isParent)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("group not found: %v", err)), nil
	}

	// Participants
	type participant struct {
		JID          string `json:"jid"`
		Phone        string `json:"phone,omitempty"`
		DisplayName  string `json:"display_name,omitempty"`
		IsAdmin      bool   `json:"is_admin"`
		IsSuperAdmin bool   `json:"is_super_admin"`
	}

	pRows, err := s.db.Query(`SELECT gp.jid, gp.phone, gp.display_name, gp.is_admin, gp.is_super_admin
		FROM group_participants gp WHERE gp.group_jid = ?`, jid)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("participants query failed: %v", err)), nil
	}
	defer pRows.Close()

	var participants []participant
	for pRows.Next() {
		var p participant
		if err := pRows.Scan(&p.JID, &p.Phone, &p.DisplayName, &p.IsAdmin, &p.IsSuperAdmin); err != nil {
			continue
		}
		participants = append(participants, p)
	}
	if participants == nil {
		participants = []participant{}
	}

	result := map[string]any{
		"jid":               jid,
		"name":              name,
		"owner_jid":         ownerJID,
		"topic":             topic,
		"group_created":     groupCreated,
		"participant_count": participantCount,
		"is_announce":       isAnnounce,
		"is_locked":         isLocked,
		"is_community":      isParent,
		"participants":      participants,
	}

	if includeMessages {
		since := time.Now().Add(-7 * 24 * time.Hour).Unix()
		mRows, err := s.db.Query(`SELECT m.id, m.sender, m.sender_name, m.content, m.timestamp,
			m.is_from_me, m.message_type
			FROM messages m WHERE m.chat_jid = ? AND m.timestamp > ?
			ORDER BY m.timestamp DESC LIMIT ?`, jid, since, messageLimit)
		if err == nil {
			defer mRows.Close()
			type groupMsg struct {
				ID          string `json:"id"`
				Sender      string `json:"sender"`
				SenderName  string `json:"sender_name"`
				Content     string `json:"content"`
				Timestamp   int64  `json:"timestamp"`
				Time        string `json:"time"`
				IsFromMe    bool   `json:"is_from_me"`
				MessageType string `json:"message_type"`
			}
			var msgs []groupMsg
			for mRows.Next() {
				var m groupMsg
				if err := mRows.Scan(&m.ID, &m.Sender, &m.SenderName, &m.Content, &m.Timestamp,
					&m.IsFromMe, &m.MessageType); err != nil {
					continue
				}
				m.Time = time.Unix(m.Timestamp, 0).Format("2006-01-02 15:04:05")
				msgs = append(msgs, m)
			}
			if msgs == nil {
				msgs = []groupMsg{}
			}
			result["recent_messages"] = msgs
		}
	}

	return marshalResult(result)
}

func (s *Server) handleScan(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	sinceF, ok := args["since"].(float64)
	if !ok || sinceF <= 0 {
		return mcp.NewToolResultError("since is required (unix epoch)"), nil
	}
	since := int64(sinceF)

	chatFilter, _ := args["chat_jid"].(string)
	excludeStr, _ := args["exclude"].(string)
	limit := 5000
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}

	var excludeJIDs []string
	if excludeStr != "" {
		for _, jid := range strings.Split(excludeStr, ",") {
			excludeJIDs = append(excludeJIDs, strings.TrimSpace(jid))
		}
	}

	// Messages scan
	msgQuery := `SELECT m.timestamp, m.chat_jid,
		COALESCE(c.name, m.chat_jid) as chat_name,
		CASE
			WHEN m.chat_jid LIKE '%@g.us' THEN 'group'
			WHEN m.chat_jid LIKE '%@s.whatsapp.net' THEN 'individual'
			WHEN m.chat_jid LIKE '%@lid' THEN 'individual'
			WHEN m.chat_jid LIKE '%@newsletter' THEN 'newsletter'
			ELSE 'unknown'
		END as chat_type,
		COALESCE(ct.phone, m.sender, '') as sender_phone,
		COALESCE(m.sender_name, ct.push_name, m.sender, '') as sender_name,
		m.is_from_me,
		COALESCE(m.message_type, 'text') as message_type,
		SUBSTR(COALESCE(m.content, ''), 1, 500) as content,
		COALESCE(m.is_forwarded, 0) as is_forwarded,
		CASE WHEN m.reply_to_id != '' AND m.reply_to_id IS NOT NULL THEN 1 ELSE 0 END as has_reply,
		CASE WHEN m.media_type != '' AND m.media_type IS NOT NULL THEN 1 ELSE 0 END as has_media,
		COALESCE(m.media_type, '') as media_type,
		COALESCE(m.mentions, '') as mentions,
		m.id as message_id
		FROM messages m
		LEFT JOIN chats c ON m.chat_jid = c.jid
		LEFT JOIN contacts ct ON ct.jid = m.sender
		WHERE m.timestamp > ?`
	msgArgs := []any{since}

	if chatFilter != "" {
		msgQuery += " AND m.chat_jid = ?"
		msgArgs = append(msgArgs, chatFilter)
	}
	for _, ej := range excludeJIDs {
		msgQuery += " AND m.chat_jid != ?"
		msgArgs = append(msgArgs, ej)
	}
	msgQuery += " ORDER BY m.timestamp ASC LIMIT ?"
	msgArgs = append(msgArgs, limit)

	rows, err := s.db.Query(msgQuery, msgArgs...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("scan messages failed: %v", err)), nil
	}
	defer rows.Close()

	type scanMsg struct {
		Timestamp   int64  `json:"timestamp"`
		Time        string `json:"time"`
		ChatJID     string `json:"chat_jid"`
		ChatName    string `json:"chat_name"`
		ChatType    string `json:"chat_type"`
		SenderPhone string `json:"sender_phone"`
		SenderName  string `json:"sender_name"`
		IsFromMe    bool   `json:"is_from_me"`
		MessageType string `json:"message_type"`
		Content     string `json:"content"`
		IsForwarded bool   `json:"is_forwarded"`
		HasReply    bool   `json:"has_reply"`
		HasMedia    bool   `json:"has_media"`
		MediaType   string `json:"media_type,omitempty"`
		Mentions    string `json:"mentions,omitempty"`
		MessageID   string `json:"message_id"`
	}

	var messages []scanMsg
	for rows.Next() {
		var m scanMsg
		var isFromMe, isForwarded, hasReply, hasMedia int
		if err := rows.Scan(&m.Timestamp, &m.ChatJID, &m.ChatName, &m.ChatType,
			&m.SenderPhone, &m.SenderName, &isFromMe, &m.MessageType,
			&m.Content, &isForwarded, &hasReply, &hasMedia,
			&m.MediaType, &m.Mentions, &m.MessageID); err != nil {
			continue
		}
		m.IsFromMe = isFromMe != 0
		m.IsForwarded = isForwarded != 0
		m.HasReply = hasReply != 0
		m.HasMedia = hasMedia != 0
		m.Time = time.Unix(m.Timestamp, 0).Format("2006-01-02 15:04:05")
		messages = append(messages, m)
	}
	if messages == nil {
		messages = []scanMsg{}
	}

	// Groups scan
	grpQuery := `SELECT g.jid, COALESCE(g.name, '') as name, g.group_created,
		COALESCE(g.owner_jid, '') as owner_jid,
		COALESCE(gp.pcnt, 0) as participant_count,
		COALESCE(msg.cnt, 0) as message_count,
		COALESCE(msg.last_ts, 0) as last_message_at,
		g.is_announce, g.is_parent, g.suspended
		FROM groups g
		LEFT JOIN (
			SELECT chat_jid, COUNT(*) as cnt, MAX(timestamp) as last_ts
			FROM messages WHERE chat_jid LIKE '%@g.us'
			GROUP BY chat_jid
		) msg ON g.jid = msg.chat_jid
		LEFT JOIN (
			SELECT group_jid, COUNT(*) as pcnt
			FROM group_participants GROUP BY group_jid
		) gp ON g.jid = gp.group_jid
		WHERE g.group_created > ?
		ORDER BY g.group_created DESC`

	grpRows, err := s.db.Query(grpQuery, since)

	type scanGroup struct {
		JID              string `json:"jid"`
		Name             string `json:"name"`
		GroupCreated     int64  `json:"group_created"`
		CreatedTime      string `json:"created_time"`
		OwnerJID         string `json:"owner_jid"`
		ParticipantCount int    `json:"participant_count"`
		MessageCount     int    `json:"message_count"`
		LastMessageAt    int64  `json:"last_message_at,omitempty"`
		IsAnnounce       bool   `json:"is_announce"`
		IsCommunity      bool   `json:"is_community"`
		Suspended        bool   `json:"suspended"`
	}

	var groups []scanGroup
	if err == nil {
		defer grpRows.Close()
		for grpRows.Next() {
			var g scanGroup
			var isAnnounce, isCommunity, suspended bool
			if err := grpRows.Scan(&g.JID, &g.Name, &g.GroupCreated, &g.OwnerJID,
				&g.ParticipantCount, &g.MessageCount, &g.LastMessageAt,
				&isAnnounce, &isCommunity, &suspended); err != nil {
				continue
			}
			g.IsAnnounce = isAnnounce
			g.IsCommunity = isCommunity
			g.Suspended = suspended
			g.CreatedTime = time.Unix(g.GroupCreated, 0).Format("2006-01-02 15:04:05")
			groups = append(groups, g)
		}
	}
	if groups == nil {
		groups = []scanGroup{}
	}

	// Chat summary — unique chats with message counts
	chatSummary := make(map[string]int)
	for _, m := range messages {
		chatSummary[m.ChatJID]++
	}

	return marshalResult(map[string]any{
		"since":         since,
		"since_time":    time.Unix(since, 0).Format("2006-01-02 15:04:05"),
		"message_count": len(messages),
		"group_count":   len(groups),
		"chat_summary":  chatSummary,
		"messages":      messages,
		"new_groups":    groups,
	})
}

func (s *Server) handleFindGroup(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	query, _ := args["query"].(string)
	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}

	q := "%" + query + "%"
	rows, err := s.db.Query(`SELECT g.jid, g.name, COALESCE(g.topic,'') as topic,
		g.participant_count, g.group_created, g.is_announce, g.is_parent,
		COALESCE(msg.last_ts, 0) as last_message_at
		FROM groups g
		LEFT JOIN (
			SELECT chat_jid, MAX(timestamp) as last_ts
			FROM messages WHERE chat_jid LIKE '%@g.us'
			GROUP BY chat_jid
		) msg ON g.jid = msg.chat_jid
		WHERE g.name LIKE ?
		ORDER BY last_message_at DESC
		LIMIT 20`, q)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query failed: %v", err)), nil
	}
	defer rows.Close()

	type group struct {
		JID              string `json:"jid"`
		Name             string `json:"name"`
		Topic            string `json:"topic,omitempty"`
		ParticipantCount int    `json:"participant_count"`
		CreatedTime      string `json:"created_time,omitempty"`
		IsAnnounce       bool   `json:"is_announce"`
		IsCommunity      bool   `json:"is_community"`
		LastMessageAt    int64  `json:"last_message_at,omitempty"`
		LastMessageTime  string `json:"last_message_time,omitempty"`
	}

	var groups []group
	for rows.Next() {
		var g group
		var groupCreated int64
		if err := rows.Scan(&g.JID, &g.Name, &g.Topic, &g.ParticipantCount,
			&groupCreated, &g.IsAnnounce, &g.IsCommunity, &g.LastMessageAt); err != nil {
			continue
		}
		if groupCreated > 0 {
			g.CreatedTime = time.Unix(groupCreated, 0).Format("2006-01-02")
		}
		if g.LastMessageAt > 0 {
			g.LastMessageTime = time.Unix(g.LastMessageAt, 0).Format("2006-01-02 15:04:05")
		}
		groups = append(groups, g)
	}

	if groups == nil {
		groups = []group{}
	}

	return marshalResult(map[string]any{
		"query":  query,
		"count":  len(groups),
		"groups": groups,
	})
}

func (s *Server) handleSearchMessages(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	query, _ := args["query"].(string)
	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}

	chatJID, _ := args["chat_jid"].(string)
	since := int64(0)
	if v, ok := args["since"].(float64); ok && v > 0 {
		since = int64(v)
	}
	limit := 50
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
		if limit > 500 {
			limit = 500
		}
	}

	q := "%" + query + "%"

	searchSQL := `SELECT m.id, m.chat_jid,
		COALESCE(NULLIF(g.name,''), NULLIF(c.name,''), m.chat_jid) as chat_name,
		COALESCE(ct.name, ct.push_name, m.sender_name, m.sender, '') as sender_name,
		m.content, m.timestamp, m.is_from_me, m.message_type,
		COALESCE(m.reply_to_content,'') as reply_to_content
		FROM messages m
		LEFT JOIN chats c ON c.jid = m.chat_jid
		LEFT JOIN groups g ON g.jid = m.chat_jid
		LEFT JOIN contacts ct ON ct.jid = m.sender
		WHERE m.content LIKE ? AND m.timestamp > ?`
	sqlArgs := []any{q, since}

	if chatJID != "" {
		searchSQL += " AND m.chat_jid = ?"
		sqlArgs = append(sqlArgs, chatJID)
	}

	searchSQL += " ORDER BY m.timestamp DESC LIMIT ?"
	sqlArgs = append(sqlArgs, limit)

	rows, err := s.db.Query(searchSQL, sqlArgs...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
	}
	defer rows.Close()

	type result struct {
		ID             string `json:"id"`
		ChatJID        string `json:"chat_jid"`
		ChatName       string `json:"chat_name"`
		SenderName     string `json:"sender_name"`
		Content        string `json:"content"`
		Timestamp      int64  `json:"timestamp"`
		Time           string `json:"time"`
		IsFromMe       bool   `json:"is_from_me"`
		MessageType    string `json:"message_type"`
		ReplyToContent string `json:"reply_to_content,omitempty"`
	}

	var results []result
	for rows.Next() {
		var r result
		if err := rows.Scan(&r.ID, &r.ChatJID, &r.ChatName, &r.SenderName,
			&r.Content, &r.Timestamp, &r.IsFromMe, &r.MessageType,
			&r.ReplyToContent); err != nil {
			continue
		}
		r.Time = time.Unix(r.Timestamp, 0).Format("2006-01-02 15:04:05")
		results = append(results, r)
	}

	if results == nil {
		results = []result{}
	}

	return marshalResult(map[string]any{
		"query":   query,
		"count":   len(results),
		"results": results,
	})
}

// ── Helpers ─────────────────────────────────────────────────────────

func marshalResult(data any) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal error: %v", err)), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

// getInt extracts an int from request args (JSON numbers come as float64).
func getInt(args map[string]any, key string, defaultVal int) int {
	if v, ok := args[key].(float64); ok && v > 0 {
		return int(v)
	}
	if v, ok := args[key].(string); ok {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultVal
}
