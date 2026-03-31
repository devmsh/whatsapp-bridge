package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// handleScanMessages returns all messages since a timestamp as CSV.
// GET /api/v2/scan/messages?since=EPOCH[&chat=JID]
func (s *Server) handleScanMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	sinceStr := r.URL.Query().Get("since")
	if sinceStr == "" {
		jsonError(w, 400, "since parameter required (unix epoch)")
		return
	}
	since, err := strconv.ParseInt(sinceStr, 10, 64)
	if err != nil {
		jsonError(w, 400, "since must be unix epoch integer")
		return
	}

	chatFilter := r.URL.Query().Get("chat")
	excludeParam := r.URL.Query().Get("exclude")
	var excludeJIDs []string
	if excludeParam != "" {
		for _, jid := range strings.Split(excludeParam, ",") {
			excludeJIDs = append(excludeJIDs, strings.TrimSpace(jid))
		}
	}
	limitStr := r.URL.Query().Get("limit")
	limit := 5000
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	// Determine own JID for self-message detection
	selfJID := ""
	if s.client != nil && s.client.WA != nil && s.client.WA.Store.ID != nil {
		selfJID = s.client.WA.Store.ID.User + "@s.whatsapp.net"
	}

	// Build query with enriched joins
	query := `
		SELECT
			m.timestamp,
			m.chat_jid,
			COALESCE(c.name, m.chat_jid) as chat_name,
			CASE
				WHEN m.chat_jid LIKE '%@g.us' THEN 'group'
				WHEN ? != '' AND m.chat_jid = ? THEN 'self'
				WHEN m.chat_jid LIKE '%@s.whatsapp.net' THEN 'individual'
				WHEN m.chat_jid LIKE '%@lid' THEN 'individual'
				WHEN m.chat_jid LIKE '%@newsletter' THEN 'newsletter'
				ELSE 'unknown'
			END as chat_type,
			COALESCE(ct.phone, m.sender, '') as sender_phone,
			COALESCE(m.sender_name, ct.push_name, m.sender, '') as sender_name,
			m.is_from_me,
			COALESCE(m.message_type, 'text') as message_type,
			REPLACE(REPLACE(SUBSTR(COALESCE(m.content, ''), 1, 500), char(10), ' '), char(13), '') as content,
			COALESCE(m.is_forwarded, 0) as is_forwarded,
			COALESCE(m.forward_score, 0) as forward_score,
			CASE WHEN m.reply_to_id != '' AND m.reply_to_id IS NOT NULL THEN 1 ELSE 0 END as has_reply,
			CASE WHEN m.media_type != '' AND m.media_type IS NOT NULL THEN 1 ELSE 0 END as has_media,
			COALESCE(m.media_type, '') as media_type,
			COALESCE(m.mentions, '') as mentions,
			m.id as message_id
		FROM messages m
		LEFT JOIN chats c ON m.chat_jid = c.jid
		LEFT JOIN contacts ct ON ct.jid = (
			SELECT jid FROM contacts
			WHERE jid = m.sender OR phone || '@s.whatsapp.net' = m.sender OR lid || '@lid' = m.sender
			LIMIT 1
		)
		WHERE m.timestamp > ?`

	args := []interface{}{selfJID, selfJID, since}

	if chatFilter != "" {
		query += " AND m.chat_jid = ?"
		args = append(args, chatFilter)
	}

	for _, ej := range excludeJIDs {
		query += " AND m.chat_jid != ?"
		args = append(args, ej)
	}

	query += " ORDER BY m.timestamp ASC LIMIT ?"
	args = append(args, limit)

	rows, err := s.store.DB.Query(query, args...)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline")

	// Header
	fmt.Fprintln(w, "timestamp,chat_jid,chat_name,chat_type,sender_phone,sender_name,is_from_me,message_type,content,is_forwarded,forward_score,has_reply,has_media,media_type,mentions,message_id")

	for rows.Next() {
		var ts int64
		var chatJID, chatName, chatType, senderPhone, senderName string
		var isFromMe int
		var msgType, content string
		var isForwarded, forwardScore, hasReply, hasMedia int
		var mediaType, mentions, messageID string

		if err := rows.Scan(&ts, &chatJID, &chatName, &chatType,
			&senderPhone, &senderName, &isFromMe,
			&msgType, &content, &isForwarded, &forwardScore,
			&hasReply, &hasMedia, &mediaType, &mentions, &messageID); err != nil {
			continue
		}

		// Format timestamp as human-readable
		tsStr := time.Unix(ts, 0).Format("2006-01-02 15:04:05")

		// CSV-escape content (quote if contains comma, quote, or newline)
		content = csvEscape(content)
		chatName = csvEscape(chatName)
		senderName = csvEscape(senderName)
		mentions = csvEscape(mentions)

		fmt.Fprintf(w, "%s,%s,%s,%s,%s,%s,%d,%s,%s,%d,%d,%d,%d,%s,%s,%s\n",
			tsStr, chatJID, chatName, chatType,
			senderPhone, senderName, isFromMe,
			msgType, content, isForwarded, forwardScore,
			hasReply, hasMedia, mediaType, mentions, messageID)
	}
}

// handleScanGroups returns groups created since a timestamp as CSV.
// GET /api/v2/scan/groups?since=EPOCH
func (s *Server) handleScanGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	sinceStr := r.URL.Query().Get("since")
	since := int64(0)
	if sinceStr != "" {
		since, _ = strconv.ParseInt(sinceStr, 10, 64)
	}

	// Accept tracked JIDs and exclude JIDs
	trackedParam := r.URL.Query().Get("tracked")
	trackedMap := make(map[string]bool)
	if trackedParam != "" {
		for _, jid := range strings.Split(trackedParam, ",") {
			trackedMap[strings.TrimSpace(jid)] = true
		}
	}
	excludeParam := r.URL.Query().Get("exclude")
	var excludeJIDs []string
	if excludeParam != "" {
		for _, jid := range strings.Split(excludeParam, ",") {
			excludeJIDs = append(excludeJIDs, strings.TrimSpace(jid))
		}
	}

	query := `
		SELECT
			g.jid,
			COALESCE(g.name, '') as name,
			g.group_created,
			COALESCE(g.owner_jid, '') as owner_jid,
			COALESCE(gp.pcnt, 0) as participant_count,
			COALESCE(msg.cnt, 0) as message_count,
			COALESCE(msg.last_ts, 0) as last_message_at,
			g.is_announce,
			g.is_locked,
			g.is_parent,
			g.suspended,
			COALESCE(g.linked_parent_jid, '') as parent_jid
		FROM groups g
		LEFT JOIN (
			SELECT chat_jid, COUNT(*) as cnt, MAX(timestamp) as last_ts
			FROM messages WHERE chat_jid LIKE '%@g.us'
			GROUP BY chat_jid
		) msg ON g.jid = msg.chat_jid
		LEFT JOIN (
			SELECT group_jid, COUNT(*) as pcnt
			FROM group_participants
			GROUP BY group_jid
		) gp ON g.jid = gp.group_jid
		WHERE g.group_created > ?`

	args := []interface{}{since}
	for _, ej := range excludeJIDs {
		query += " AND g.jid != ?"
		args = append(args, ej)
	}
	query += " ORDER BY g.group_created DESC"

	rows, err := s.store.DB.Query(query, args...)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline")

	// Header
	fmt.Fprintln(w, "created,jid,name,owner_jid,members,messages,last_message_at,is_announce,is_locked,is_community,suspended,parent_jid,tracked")

	for rows.Next() {
		var jid, name, ownerJID, parentJID string
		var groupCreated, lastMessageAt int64
		var participantCount, messageCount int
		var isAnnounce, isLocked, isParent, isSuspended bool

		if err := rows.Scan(&jid, &name, &groupCreated, &ownerJID,
			&participantCount, &messageCount, &lastMessageAt,
			&isAnnounce, &isLocked, &isParent, &isSuspended, &parentJID); err != nil {
			continue
		}

		createdStr := time.Unix(groupCreated, 0).Format("2006-01-02 15:04:05")
		lastMsgStr := ""
		if lastMessageAt > 0 {
			lastMsgStr = time.Unix(lastMessageAt, 0).Format("2006-01-02 15:04:05")
		}

		tracked := 0
		if trackedMap[jid] {
			tracked = 1
		}

		name = csvEscape(name)

		fmt.Fprintf(w, "%s,%s,%s,%s,%d,%d,%s,%v,%v,%v,%v,%s,%d\n",
			createdStr, jid, name, ownerJID,
			participantCount, messageCount, lastMsgStr,
			boolToInt(isAnnounce), boolToInt(isLocked), boolToInt(isParent), boolToInt(isSuspended),
			parentJID, tracked)
	}
}

func csvEscape(s string) string {
	if strings.ContainsAny(s, ",\"\n\r") {
		return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
