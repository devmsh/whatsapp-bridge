package db

import (
	"strings"
)

// UnreadMentionCounts returns, for each chat in chatJIDs, the number of the
// chat's most-recent incoming messages whose `mentions` column contains any
// substring in selfPats. The "most-recent" window per chat is capped at
// chatUnread[jid] — i.e. we only count messages that are still plausibly
// unread. We don't track a true read marker, so the unread_count from the
// chats table (which appstate sync keeps) is the closest proxy.
//
// Returns counts only when > 0 — callers can treat absent entries as zero.
// Self-pattern match is a plain substring scan: `mentions` is a JSON string
// like `["63840813367480@lid","..."]`, so a substring like `63840813367480@lid`
// is unambiguous and avoids the cost of JSON parsing per row.
func (s *Store) UnreadMentionCounts(
	chatUnread map[string]int,
	selfPats []string,
	maxPerChat int,
) (map[string]int, error) {
	out := map[string]int{}
	if len(chatUnread) == 0 || len(selfPats) == 0 {
		return out, nil
	}

	// Cap how deep we scan per chat. The chats table can have a large
	// unread_count after a long offline period; we'd rather under-report a
	// stale ping than block the chat list on a giant query.
	if maxPerChat <= 0 {
		maxPerChat = 100
	}

	for jid, unread := range chatUnread {
		if unread <= 0 {
			continue
		}
		limit := unread
		if limit > maxPerChat {
			limit = maxPerChat
		}
		rows, err := s.DB.Query(
			`SELECT mentions FROM messages
			 WHERE chat_jid = ? AND is_from_me = 0
			 ORDER BY timestamp DESC LIMIT ?`,
			jid, limit,
		)
		if err != nil {
			// Don't fail the whole batch for one chat's query — just skip.
			continue
		}
		count := 0
		for rows.Next() {
			var mentions string
			if rows.Scan(&mentions) != nil {
				continue
			}
			if mentions == "" {
				continue
			}
			for _, pat := range selfPats {
				if pat != "" && strings.Contains(mentions, pat) {
					count++
					break
				}
			}
		}
		rows.Close()
		if count > 0 {
			out[jid] = count
		}
	}
	return out, nil
}
