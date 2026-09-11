package db

// Two separate rules keep a chat away from the AI, and they mean different
// things:
//
//	hidden   — private. Locked behind Touch ID / PIN. You should not even see
//	           it in the list without unlocking, and the AI must never read it.
//	archived — finished. Anyone with the app open may still read it, but the
//	           work is over: a group you left, a client with no live contract.
//	           It drops out of the active list, and the AI stops spending any
//	           work on it.
//	deleted  — gone. You deleted the chat in WhatsApp, so it is not in the list
//	           at all. The rows stay on disk, but nothing may act on them.
//
// The viewing rules differ (archived needs no unlock, deleted is simply not
// shown), but for every AI feature all three behave the same. So AI code asks
// for this combined set instead of the hidden set alone. Any one of the three
// is enough to stop extraction, profiling, digests, drafts and briefings
// touching a chat.

// AIExcludedJIDs returns every chat the AI must leave alone: hidden, archived
// or deleted.
func (s *Store) AIExcludedJIDs() map[string]bool {
	out := map[string]bool{}
	rows, err := s.DB.Query(`
		SELECT chat_jid FROM hidden_chats
		UNION
		SELECT jid FROM chats WHERE is_archived = 1 OR deleted_at > 0`)
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

// AIExcludedJIDsList returns the same set as a slice, for SQL IN clauses.
func (s *Store) AIExcludedJIDsList() []string {
	rows, err := s.DB.Query(`
		SELECT chat_jid FROM hidden_chats
		UNION
		SELECT jid FROM chats WHERE is_archived = 1 OR deleted_at > 0
		ORDER BY 1`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var jid string
		if rows.Scan(&jid) == nil {
			out = append(out, jid)
		}
	}
	return out
}

// IsChatExcludedFromAI reports whether a chat is hidden, archived or deleted.
// Used by the per-chat AI endpoints, which refuse all three.
func (s *Store) IsChatExcludedFromAI(jid string) bool {
	var n int
	s.DB.QueryRow(`
		SELECT 1 WHERE EXISTS (SELECT 1 FROM hidden_chats WHERE chat_jid = ?)
		            OR EXISTS (SELECT 1 FROM chats WHERE jid = ? AND (is_archived = 1 OR deleted_at > 0))`,
		jid, jid).Scan(&n)
	return n == 1
}
