package api

import (
	"net/http"
	"strings"
)

// searchHit is one result row (any kind).
type searchHit struct {
	Kind     string `json:"kind"` // contact | group | circle | task | message
	ID       string `json:"id"`   // JID, circle id, or task id (as string)
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
	// Private mode: unlocked searches ONLY hidden chats, locked searches only
	// the normal world. The two scopes never mix.
	hidden := s.store.HiddenChatJIDs()
	unlocked := s.isUnlocked(r)
	// `skip[jid]` reports whether a chat should be filtered out of results.
	skip := func(jid string) bool { return hidden[jid] != unlocked }

	hits := []searchHit{}

	// Contacts.
	//
	// One person can hold two contact rows: a phone identity and a @lid one,
	// with nothing joining them. Searching then returned the same name twice,
	// and only the phone row was clickable because the chat lives under it.
	//
	// Two passes fix it. The query prefers the row that actually has messages,
	// so the useful identity wins; then LID rows are dropped when the same
	// person is already present, resolved through whatsmeow's LID mapping and,
	// failing that, by display name. The LID digits are also never shown as a
	// phone number, because they are not one.
	if rows, err := s.store.DB.Query(`SELECT c.jid,
		COALESCE(NULLIF(c.name,''), NULLIF(c.push_name,''), NULLIF(c.business_name,''), ''),
		COALESCE(c.phone,''),
		(SELECT COUNT(*) FROM messages m WHERE m.chat_jid = c.jid) AS msgs
		FROM contacts c
		WHERE (c.name LIKE ? ESCAPE '\' OR c.push_name LIKE ? ESCAPE '\' OR c.business_name LIKE ? ESCAPE '\' OR c.phone LIKE ? ESCAPE '\')
		  -- Drop rows that are a LID wearing a phone server. When the very same
		  -- digits also exist as "<digits>@lid", the phone-form row is a sync
		  -- artefact, not a person: a real number cannot collide with a LID.
		  -- Requiring zero messages too keeps a genuine contact safe if that
		  -- assumption is ever wrong.
		  AND NOT (
		        c.jid LIKE '%@s.whatsapp.net'
		    AND EXISTS (SELECT 1 FROM contacts l
		                 WHERE l.jid = REPLACE(c.jid, '@s.whatsapp.net', '@lid'))
		    AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.chat_jid = c.jid)
		  )
		ORDER BY msgs DESC, (c.jid LIKE '%@lid') ASC
		LIMIT 24`, pat, pat, pat, pat); err == nil {
		seenID := map[string]bool{}
		seenName := map[string]bool{}
		out := 0
		for rows.Next() {
			var jid, name, phone string
			var msgs int
			if rows.Scan(&jid, &name, &phone, &msgs) != nil {
				continue
			}
			if skip(jid) || out >= 12 {
				continue
			}
			isLID := strings.HasSuffix(jid, "@lid")
			// Collapse onto the phone identity when one is known.
			key := jid
			if isLID {
				if pn := s.client.ResolvePhoneForLID(jid); pn != "" {
					key = pn
				}
			}
			if seenID[key] {
				continue
			}
			// Without a mapping, an identical name already shown is the same
			// person in practice. Only a LID row is dropped this way — two real
			// numbers sharing a name are genuinely different contacts.
			if isLID && name != "" && seenName[name] {
				continue
			}
			seenID[key] = true
			if name != "" {
				seenName[name] = true
			}
			if name == "" {
				name = "+" + phone
			}
			sub := phone
			if isLID {
				sub = "" // LID digits are an internal id, not a phone number
			}
			hits = append(hits, searchHit{Kind: "contact", ID: jid, Title: name, Subtitle: sub})
			out++
		}
		rows.Close()
	}

	// Groups.
	if rows, err := s.store.DB.Query(`SELECT jid, COALESCE(name,'') AS name, COALESCE(topic,'') AS topic
		FROM groups WHERE name LIKE ? ESCAPE '\' OR topic LIKE ? ESCAPE '\' LIMIT 12`, pat, pat); err == nil {
		for rows.Next() {
			var jid, name, topic string
			if rows.Scan(&jid, &name, &topic) == nil {
				if skip(jid) {
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
				if skip(chatJID) {
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
