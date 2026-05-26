package api

import (
	"net/http"
	"strconv"
)

// handleRecentStickers returns the most-recent unique sticker paths the
// bridge has seen — both sent (by us) and received (from others). Used by
// the composer's sticker tray as a "Recents" list the user can tap to
// re-send. Sorted newest-first; deduped on media_path so the same sticker
// reused 10 times shows up once.
//
// Limit defaults to 60, capped at 200. Returns []StickerRecent;
// empty array (not 404) when there's nothing yet.
func (s *Server) handleRecentStickers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	limit := 60
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	// `chat_jid` ignored when private mode is off; we surface every sticker
	// across every chat the user can already see (hidden-chat stickers stay
	// hidden because the bridge filters those rows out of /messages too).
	rows, err := s.store.DB.Query(`
		SELECT m.media_path, COALESCE(NULLIF(m.media_mime,''),'image/webp'), MAX(m.timestamp) AS ts
		FROM messages m
		LEFT JOIN hidden_chats h ON h.chat_jid = m.chat_jid
		WHERE m.media_type = 'sticker' AND m.media_path != '' AND h.chat_jid IS NULL
		GROUP BY m.media_path
		ORDER BY ts DESC
		LIMIT ?`, limit)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	type stickerRecent struct {
		Path string `json:"path"`
		Mime string `json:"mime"`
		Ts   int64  `json:"timestamp"`
	}
	out := []stickerRecent{}
	for rows.Next() {
		var s stickerRecent
		if err := rows.Scan(&s.Path, &s.Mime, &s.Ts); err != nil {
			continue
		}
		out = append(out, s)
	}
	jsonOK(w, out)
}
