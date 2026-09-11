package db

// UnassignedChat is one live chat that belongs to no circle yet — the raw
// material for building circles. Kind is "group" or "contact", matching the
// member_type you pass when filing it.
type UnassignedChat struct {
	Kind          string `json:"kind"`
	JID           string `json:"jid"`
	Name          string `json:"name"`
	MessageCount  int    `json:"message_count"`
	RecentCount   int    `json:"recent_count"`
	LastMessageAt int64  `json:"last_message_at,omitempty"`
	Participants  int    `json:"participants"`
}

// Ordering for both lists: busiest over the last 30 days first.
//
// Total message count would rank a dead five-year group above a conversation
// that started last week, and last-message time alone would put a single
// "thanks" above a thread with 400 messages this month. Recent volume is what
// actually says "this matters now, file it", which is the whole job of the
// Unassigned list. Ties fall back to the most recent message.
const unassignedOrder = ` ORDER BY recent_count DESC, last_message_at DESC, message_count DESC`

// unassignedWindow is the activity window, as a SQL expression.
const unassignedWindow = `CAST(strftime('%s', 'now', '-30 days') AS INTEGER)`

// UnassignedGroups returns every group that belongs to no circle and is still
// worth sorting: you are still a member, it has at least one message, and it
// is not archived, deleted or hidden. A group nobody ever wrote in is noise
// from group sync, not a filing decision.
//
// The same filter feeds the recommendation engine, so suggestions are built
// only from groups that are actually live.
func (s *Store) UnassignedGroups() ([]UnassignedChat, error) {
	return s.queryUnassigned(`
		SELECT 'group' AS kind,
		       g.jid,
		       COALESCE(NULLIF(g.name, ''), g.jid)   AS name,
		       COALESCE(m.n, 0)                      AS message_count,
		       COALESCE(r.n, 0)                      AS recent_count,
		       COALESCE(c.last_message_at, 0)        AS last_message_at,
		       COALESCE(p.n, 0)                      AS participants
		FROM groups g
		LEFT JOIN chats c ON c.jid = g.jid
		LEFT JOIN (SELECT chat_jid, COUNT(*) n FROM messages GROUP BY chat_jid) m
		       ON m.chat_jid = g.jid
		LEFT JOIN (SELECT chat_jid, COUNT(*) n FROM messages
		            WHERE timestamp >= ` + unassignedWindow + ` GROUP BY chat_jid) r
		       ON r.chat_jid = g.jid
		LEFT JOIN (SELECT group_jid, COUNT(*) n FROM group_participants GROUP BY group_jid) p
		       ON p.group_jid = g.jid
		WHERE g.left_at = 0
		  AND COALESCE(c.is_archived, 0) = 0
		  AND COALESCE(c.deleted_at, 0) = 0
		  AND COALESCE(m.n, 0) > 0
		  AND g.jid NOT IN (SELECT chat_jid FROM hidden_chats)
		  AND g.jid NOT IN (SELECT member_ref FROM circle_members WHERE member_type = 'group')` +
		unassignedOrder)
}

// UnassignedPeople returns every person you actually talk to who sits in no
// circle: a direct chat with at least one message, not archived, deleted or
// hidden.
//
// The circle check has to cover every form of the same person. A contact can be
// filed under their phone JID while the chat row is stored under their @lid
// (or the reverse), so testing the chat's own JID alone would list someone as
// unassigned when they are already in a circle. The contacts row supplies both
// forms and all of them are tested.
//
// ownPhone is your own number, so the self-chat — your notes inbox — is not
// offered as a person to file. Pass "" if it is unknown.
func (s *Store) UnassignedPeople(ownPhone string) ([]UnassignedChat, error) {
	return s.queryUnassigned(`
		SELECT 'contact' AS kind,
		       c.jid,
		       COALESCE(NULLIF(c.name, ''), NULLIF(ct.name, ''), NULLIF(ct.business_name, ''),
		                NULLIF(ct.push_name, ''), c.jid) AS name,
		       COALESCE(m.n, 0)               AS message_count,
		       COALESCE(r.n, 0)               AS recent_count,
		       COALESCE(c.last_message_at, 0) AS last_message_at,
		       0                              AS participants
		FROM chats c
		LEFT JOIN contacts ct ON (ct.jid = c.jid OR ct.lid = c.jid)
		LEFT JOIN (SELECT chat_jid, COUNT(*) n FROM messages GROUP BY chat_jid) m
		       ON m.chat_jid = c.jid
		LEFT JOIN (SELECT chat_jid, COUNT(*) n FROM messages
		            WHERE timestamp >= `+unassignedWindow+` GROUP BY chat_jid) r
		       ON r.chat_jid = c.jid
		WHERE (c.jid LIKE '%@s.whatsapp.net' OR c.jid LIKE '%@lid')
		  AND COALESCE(c.is_archived, 0) = 0
		  AND COALESCE(c.deleted_at, 0) = 0
		  AND COALESCE(m.n, 0) > 0
		  AND c.jid NOT IN (SELECT chat_jid FROM hidden_chats)
		  AND (?1 = '' OR c.jid NOT LIKE ?2)
		  AND NOT EXISTS (
		        SELECT 1 FROM circle_members cm
		        WHERE cm.member_type = 'contact'
		          AND cm.member_ref IN (c.jid, COALESCE(ct.jid, ''), COALESCE(ct.lid, ''))
		      )
		GROUP BY c.jid`+unassignedOrder,
		ownPhone, ownPhone+"@%")
}

func (s *Store) queryUnassigned(query string, args ...any) ([]UnassignedChat, error) {
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []UnassignedChat{}
	for rows.Next() {
		var c UnassignedChat
		if err := rows.Scan(&c.Kind, &c.JID, &c.Name, &c.MessageCount,
			&c.RecentCount, &c.LastMessageAt, &c.Participants); err != nil {
			return out, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
