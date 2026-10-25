package db

import (
	"fmt"
	"strings"
	"time"
)

// Mention is one self-mention hit: the message itself, plus its rowid (needed
// to fetch the messages right before/after it — see GetMessagesAround).
type Mention struct {
	Message
	RowID int64 `json:"-"`
}

// ListSelfMentions returns messages where the user was @-mentioned, newest
// first, across every chat. selfPats comes from Client.SelfMentionPatterns —
// the same JID substrings the chat-list "@" badge already matches against
// (UnreadMentionCounts below), so this page and that badge always agree.
//
// Unlike UnreadMentionCounts (bounded per-chat by unread_count, count-only),
// this is a real cross-chat feed, so the self-pattern match has to happen in
// SQL rather than after fetching — otherwise a hard LIMIT could cut off
// genuine matches in favor of unrelated recent messages.
func (s *Store) ListSelfMentions(selfPats []string, limit int) ([]Mention, error) {
	if len(selfPats) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}
	clauses := make([]string, len(selfPats))
	args := make([]interface{}, 0, len(selfPats)+1)
	for i, pat := range selfPats {
		clauses[i] = "mentions LIKE ?"
		args = append(args, "%"+pat+"%")
	}
	args = append(args, limit)

	query := fmt.Sprintf(`SELECT rowid, %s
		FROM messages
		WHERE is_from_me = 0 AND is_deleted = 0 AND (%s)
		ORDER BY timestamp DESC LIMIT ?`,
		messageColumns, strings.Join(clauses, " OR "))

	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Mention
	for rows.Next() {
		var m Mention
		if err := rows.Scan(
			&m.RowID,
			&m.ID, &m.ChatJID, &m.Sender, &m.SenderName, &m.PushName, &m.Content, &m.Timestamp,
			&m.IsFromMe, &m.IsGroup, &m.MessageType, &m.DeviceID,
			&m.IsEphemeral, &m.IsViewOnce, &m.IsForwarded, &m.ForwardScore,
			&m.IsEdit, &m.EditTimestamp, &m.OriginalID,
			&m.IsDeleted, &m.DeletedAt, &m.DeletedBy,
			&m.MediaType, &m.MediaPath, &m.MediaMime, &m.MediaSize, &m.MediaCaption, &m.MediaFilename, &m.ThumbnailPath,
			&m.ReplyToID, &m.ReplyToSender, &m.ReplyToContent, &m.Mentions,
			&m.Latitude, &m.Longitude, &m.LocationName, &m.LocationAddress,
			&m.VCardName, &m.VCardData, &m.PollID, &m.StickerPack, &m.BroadcastListJID,
		); err != nil {
			return out, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DismissMention marks a mention as dismissed, so it drops off the Mentions
// page without needing a reply. Idempotent, like starring.
func (s *Store) DismissMention(chatJID, messageID string) error {
	_, err := s.DB.Exec(
		`INSERT OR REPLACE INTO dismissed_mentions (chat_jid, message_id, dismissed_at)
		 VALUES (?, ?, ?)`,
		chatJID, messageID, time.Now().Unix(),
	)
	return err
}

// UndismissMention removes the dismissed mark, if any — the same reversible
// toggle shape as UnstarMessage.
func (s *Store) UndismissMention(chatJID, messageID string) error {
	_, err := s.DB.Exec(
		`DELETE FROM dismissed_mentions WHERE chat_jid = ? AND message_id = ?`,
		chatJID, messageID,
	)
	return err
}

// DismissedMentionIDs returns the set of dismissed message IDs, so the
// Mentions list can filter them out without an N+1 query.
func (s *Store) DismissedMentionIDs() (map[string]bool, error) {
	out := map[string]bool{}
	rows, err := s.DB.Query(`SELECT message_id FROM dismissed_mentions`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return out, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// UnreadMentionCounts returns, for each chat in chatJIDs, the number of the
// chat's most-recent incoming messages whose `mentions` column contains any
// substring in selfPats. The "most-recent" window per chat is capped at
// chatUnread[jid] — i.e. we only count messages that are still plausibly
// unread. We don't track a true read marker, so the unread_count from the
// chats table (which appstate sync keeps) is the closest proxy.
//
// Returns counts only when > 0 — callers can treat absent entries as zero.
// Self-pattern match is a plain substring scan: `mentions` is a JSON string
// like `["10000000000101@lid","..."]`, so a substring like `10000000000101@lid`
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
