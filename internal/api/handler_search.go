package api

import (
	"net/http"
	"strings"
)

// searchHit is one result row (any kind).
type searchHit struct {
	Kind     string `json:"kind"`     // contact | group | circle | task | message
	ID       string `json:"id"`       // JID, circle id, or task id (as string)
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Snippet  string `json:"snippet,omitempty"`
	ChatJID  string `json:"chat_jid,omitempty"`
	TS       int64  `json:"ts,omitempty"`
}

// handleSearch is the universal search endpoint. It fans out across contacts,
// groups, circles, tasks, and message bodies (LIKE — no FTS yet) and returns
// ranked results in one payload.
// GET /api/v2/search?q=<query>
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		jsonOK(w, map[string]any{"q": "", "hits": []searchHit{}})
		return
	}
	pat := "%" + strings.ReplaceAll(strings.ReplaceAll(q, `\`, `\\`), `%`, `\%`) + "%"
	hidden := map[string]bool{}
	if !s.isUnlocked(r) {
		hidden = s.store.HiddenChatJIDs()
	}

	hits := []searchHit{}

	// Contacts.
	if rows, err := s.store.DB.Query(`SELECT jid,
		COALESCE(NULLIF(name,''), NULLIF(push_name,''), NULLIF(business_name,''), ''),
		COALESCE(phone,'')
		FROM contacts
		WHERE name LIKE ? ESCAPE '\' OR push_name LIKE ? ESCAPE '\' OR business_name LIKE ? ESCAPE '\' OR phone LIKE ? ESCAPE '\'
		LIMIT 12`, pat, pat, pat, pat); err == nil {
		for rows.Next() {
			var jid, name, phone string
			if rows.Scan(&jid, &name, &phone) == nil {
				if hidden[jid] {
					continue
				}
				if name == "" {
					name = "+" + phone
				}
				hits = append(hits, searchHit{Kind: "contact", ID: jid, Title: name, Subtitle: phone})
			}
		}
		rows.Close()
	}

	// Groups.
	if rows, err := s.store.DB.Query(`SELECT jid, COALESCE(name,'') AS name, COALESCE(topic,'') AS topic
		FROM groups WHERE name LIKE ? ESCAPE '\' OR topic LIKE ? ESCAPE '\' LIMIT 12`, pat, pat); err == nil {
		for rows.Next() {
			var jid, name, topic string
			if rows.Scan(&jid, &name, &topic) == nil {
				if hidden[jid] {
					continue
				}
				if name == "" {
					name = jid
				}
				hits = append(hits, searchHit{Kind: "group", ID: jid, Title: name, Subtitle: topic})
			}
		}
		rows.Close()
	}

	// Circles.
	if rows, err := s.store.DB.Query(`SELECT id, name FROM circles WHERE name LIKE ? ESCAPE '\' LIMIT 10`, pat); err == nil {
		for rows.Next() {
			var id int64
			var name string
			if rows.Scan(&id, &name) == nil {
				hits = append(hits, searchHit{Kind: "circle", ID: itoa(id), Title: name})
			}
		}
		rows.Close()
	}

	// Tasks.
	if rows, err := s.store.DB.Query(`SELECT id, title, status, COALESCE(description,'') AS description
		FROM tasks WHERE review_status != 'rejected' AND (title LIKE ? ESCAPE '\' OR description LIKE ? ESCAPE '\')
		LIMIT 15`, pat, pat); err == nil {
		for rows.Next() {
			var id int64
			var title, status, desc string
			if rows.Scan(&id, &title, &status, &desc) == nil {
				snip := strings.TrimSpace(strings.ReplaceAll(desc, "\n", " "))
				if len(snip) > 120 {
					snip = snip[:120] + "…"
				}
				hits = append(hits, searchHit{Kind: "task", ID: itoa(id), Title: title, Subtitle: status, Snippet: snip})
			}
		}
		rows.Close()
	}

	// Messages — most-recent text matches.
	if rows, err := s.store.DB.Query(`SELECT m.chat_jid, m.id, m.timestamp,
		SUBSTR(COALESCE(m.content,''), 1, 220) AS snippet,
		COALESCE(NULLIF(g.name,''), NULLIF(c.name,''), NULLIF(c.push_name,''),
		         NULLIF(c.business_name,''), m.chat_jid) AS chat_name
		FROM messages m
		LEFT JOIN groups g ON g.jid = m.chat_jid
		LEFT JOIN contacts c ON c.jid = m.chat_jid
		WHERE m.content LIKE ? ESCAPE '\' AND m.content != ''
		ORDER BY m.timestamp DESC LIMIT 15`, pat); err == nil {
		for rows.Next() {
			var chatJID, msgID, snippet, chatName string
			var ts int64
			if rows.Scan(&chatJID, &msgID, &ts, &snippet, &chatName) == nil {
				if hidden[chatJID] {
					continue
				}
				hits = append(hits, searchHit{
					Kind: "message", ID: msgID, Title: chatName,
					Snippet: strings.ReplaceAll(strings.TrimSpace(snippet), "\n", " "),
					ChatJID: chatJID, TS: ts,
				})
			}
		}
		rows.Close()
	}

	jsonOK(w, map[string]any{"q": q, "hits": hits})
}

func itoa(i int64) string {
	// avoid strconv import collision — small helper
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
