package mcp

import "strings"

// hiddenChatJIDs returns the set of chat JIDs the user has hidden. The MCP
// server uses this to UNCONDITIONALLY filter hidden chats out of every read
// tool (wa_scan, wa_read_messages, wa_search_messages, wa_circle_info,
// wa_get_profile, wa_group_info, wa_list_chats, ...). Hidden chats are never
// exposed to the AI, regardless of any per-session unlock state.
func (s *Server) hiddenChatJIDs() map[string]bool {
	out := map[string]bool{}
	rows, err := s.db.Query(`SELECT chat_jid FROM hidden_chats`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var jid string
		if rows.Scan(&jid) == nil {
			out[jid] = true
		}
	}
	return out
}

// hiddenChatFilter returns " AND <column> NOT IN (?,?,?)" plus matching args,
// for embedding in SQL queries. Empty fragment when nothing is hidden.
func (s *Server) hiddenChatFilter(column string) (string, []any) {
	rows, err := s.db.Query(`SELECT chat_jid FROM hidden_chats`)
	if err != nil {
		return "", nil
	}
	defer rows.Close()
	var jids []string
	for rows.Next() {
		var j string
		if rows.Scan(&j) == nil {
			jids = append(jids, j)
		}
	}
	if len(jids) == 0 {
		return "", nil
	}
	args := make([]any, len(jids))
	for i, j := range jids {
		args[i] = j
	}
	return " AND " + column + " NOT IN (" + strings.Repeat("?,", len(jids)-1) + "?)", args
}
