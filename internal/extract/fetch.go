package extract

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"whatsapp-bridge-v2/internal/db"
)

// Reading a conversation into the shape the model sees.
//
// Two things matter here more than they look. Every line carries its message
// id, because that is what makes the model's evidence checkable rather than
// merely plausible. And mentions are resolved to names before the model reads
// them, because a model asked to interpret "@200291672703053@lid" will invent
// somebody.

// FetchLines reads a window of a chat, enriched and ordered oldest first.
//
// since is exclusive and until inclusive, so a watermark can be handed
// straight back in without re-reading its last message.
func FetchLines(store *db.Store, chatJID string, since, until int64) ([]Line, error) {
	rows, err := store.DB.Query(`
		SELECT m.id, m.timestamp,
		       COALESCE(m.sender, ''), COALESCE(m.sender_name, ''), m.is_from_me,
		       COALESCE(m.content, ''), COALESCE(m.media_type, ''),
		       COALESCE(m.mentions, ''), COALESCE(m.reply_to_id, ''),
		       COALESCE(m.is_forwarded, 0)
		FROM messages m
		WHERE m.chat_jid = ?
		  AND m.timestamp > ? AND m.timestamp <= ?
		  AND COALESCE(m.is_deleted, 0) = 0
		ORDER BY m.timestamp, m.id`, chatJID, since, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type raw struct {
		id        string
		ts        int64
		sender    string
		senderNm  string
		fromMe    bool
		content   string
		mediaType string
		mentions  string
		replyTo   string
		forwarded bool
	}
	var all []raw
	for rows.Next() {
		var r raw
		if err := rows.Scan(&r.id, &r.ts, &r.sender, &r.senderNm, &r.fromMe,
			&r.content, &r.mediaType, &r.mentions, &r.replyTo, &r.forwarded); err != nil {
			return nil, err
		}
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return []Line{}, nil
	}

	// One batch query for transcripts and image descriptions, rather than one
	// per message.
	refs := make([]db.MediaRef, 0, len(all))
	for _, r := range all {
		if r.mediaType != "" {
			refs = append(refs, db.MediaRef{ChatJID: chatJID, MessageID: r.id})
		}
	}
	media := store.LoadMediaUnderstanding(refs)

	names := newNameResolver(store)
	out := make([]Line, 0, len(all))
	for _, r := range all {
		text := db.MergeAIText(r.content, r.mediaType, media[chatJID+"|"+r.id])

		var mentions []string
		if r.mentions != "" && r.mentions != "[]" {
			var raw []string
			if json.Unmarshal([]byte(r.mentions), &raw) == nil {
				for _, m := range raw {
					// Two different JIDs are needed here. The text contains the
					// digits of whichever form WhatsApp used — usually the @lid
					// — so the replacement has to use that. What downstream
					// stores is the phone form, so one person stays one person.
					if n := names.display(names.canonical(m)); n != "" {
						text = replaceMention(text, m, n)
					}
					mentions = append(mentions, names.canonical(m))
				}
			}
		}

		sender := r.senderNm
		if r.fromMe {
			sender = names.ownName()
		} else if n := names.display(r.sender); n != "" {
			sender = n
		}
		if sender == "" {
			sender = shortJID(r.sender)
		}

		out = append(out, Line{
			MessageID: r.id,
			TS:        r.ts,
			Sender:    sender,
			SenderJID: names.canonical(r.sender),
			Text:      strings.TrimSpace(text),
			Mentions:  mentions,
			ReplyTo:   r.replyTo,
			Forwarded: r.forwarded,
			MediaType: r.mediaType,
		})
	}
	return out, nil
}

// replaceMention swaps every form of a mention for the display name. WhatsApp
// writes them as "@<digits>", so the digits are what appear in the text.
func replaceMention(text, jid, name string) string {
	digits := jid
	if i := strings.IndexByte(digits, '@'); i > 0 {
		digits = digits[:i]
	}
	if digits == "" {
		return text
	}
	return strings.ReplaceAll(text, "@"+digits, "@"+name)
}

func shortJID(jid string) string {
	if i := strings.IndexByte(jid, '@'); i > 0 {
		return "+" + jid[:i]
	}
	if jid == "" {
		return "someone"
	}
	return jid
}

// nameResolver answers "who is this JID" without a query per message, and
// collapses the LID/phone split so one person is one person.
type nameResolver struct {
	store *db.Store
	// byJID maps every identity form to a display name.
	byJID map[string]string
	// canon maps any form to the phone form when one is known. Everything the
	// pipeline stores uses that form, because the rest of the app does.
	canon map[string]string
	own   string
	ready bool
}

func newNameResolver(store *db.Store) *nameResolver {
	return &nameResolver{store: store, byJID: map[string]string{}, canon: map[string]string{}}
}

func (n *nameResolver) load() {
	if n.ready {
		return
	}
	n.ready = true

	type row struct{ jid, lid, phone, display string }
	var rows_ []row
	// Digit strings known to be LIDs. Collected first, because the same digits
	// often ALSO exist as a "<digits>@s.whatsapp.net" row — a sync artefact,
	// 129 of them in this database — and that row must never be mistaken for
	// somebody's phone number.
	lidDigits := map[string]bool{}

	rows, err := n.store.DB.Query(`SELECT jid, COALESCE(lid,''), COALESCE(phone,''),
		COALESCE(name,''), COALESCE(push_name,''), COALESCE(business_name,'')
		FROM contacts`)
	if err != nil {
		return
	}
	for rows.Next() {
		var jid, lid, phone, name, push, biz string
		if rows.Scan(&jid, &lid, &phone, &name, &push, &biz) != nil {
			continue
		}
		if d := digitsOf(lid); d != "" {
			lidDigits[d] = true
		}
		if strings.HasSuffix(jid, "@lid") {
			lidDigits[digitsOf(jid)] = true
		}
		rows_ = append(rows_, row{jid, lid, phone, firstNonEmpty(name, push, biz)})
	}
	rows.Close()

	isPhone := func(jid string) bool {
		return strings.HasSuffix(jid, "@s.whatsapp.net") && !lidDigits[digitsOf(jid)]
	}

	for _, r := range rows_ {
		// The identity to file this person under: a real number when one is
		// known, otherwise whatever we have.
		phoneJID := ""
		for _, cand := range []string{r.jid, r.phone + "@s.whatsapp.net"} {
			if isPhone(cand) {
				phoneJID = cand
				break
			}
		}
		if phoneJID == "" {
			phoneJID = r.jid
		}

		lidJID := r.lid
		if lidJID != "" && !strings.Contains(lidJID, "@") {
			lidJID += "@lid"
		}
		for _, form := range []string{r.jid, r.lid, lidJID, r.phone + "@s.whatsapp.net"} {
			if form == "" || form == "@s.whatsapp.net" {
				continue
			}
			if r.display != "" {
				if _, taken := n.byJID[form]; !taken {
					n.byJID[form] = r.display
				}
			}
			// A mapping that reaches a real number is always the better
			// answer, so one that does not must never replace it.
			if prev, ok := n.canon[form]; ok && isPhone(prev) && !isPhone(phoneJID) {
				continue
			}
			n.canon[form] = phoneJID
		}
	}

	n.own, _, _ = n.store.GetSyncState("intro_own_name")
	if n.own == "" {
		n.own = "Me"
	}
}

// digitsOf returns the user part of a JID, or the string itself when it has no
// server — the lid column stores bare digits.
func digitsOf(jid string) string {
	if i := strings.IndexByte(jid, '@'); i >= 0 {
		return jid[:i]
	}
	return jid
}

func (n *nameResolver) display(jid string) string {
	n.load()
	return n.byJID[jid]
}

// canonical returns the phone form of an identity when one is known, so a
// person filed under their number and mentioned by their @lid are the same
// person everywhere downstream.
func (n *nameResolver) canonical(jid string) string {
	n.load()
	if c := n.canon[jid]; c != "" {
		return c
	}
	return jid
}

func (n *nameResolver) ownName() string {
	n.load()
	return n.own
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// Render builds exactly what the model reads.
//
// The message id leads every line on purpose: the model is asked to quote it
// back, and an id it cannot have invented is what turns "the model says so"
// into something code can check.
func Render(c Chunk, loc *time.Location) string {
	var b strings.Builder
	byID := make(map[string]Line, len(c.Lines))
	for _, l := range c.Lines {
		byID[l.MessageID] = l
	}
	for _, l := range c.Lines {
		if l.Context {
			b.WriteString("[context] ")
		}
		b.WriteString(fmt.Sprintf("[#%s] [%s] %s:",
			l.MessageID, time.Unix(l.TS, 0).In(loc).Format("Mon 02 Jan 15:04"), l.Sender))
		if l.Forwarded {
			b.WriteString(" [forwarded]")
		}
		if l.ReplyTo != "" {
			snippet := ""
			if r, ok := byID[l.ReplyTo]; ok {
				snippet = firstRunes(r.Text, 60)
			}
			b.WriteString(fmt.Sprintf(" ↳ replying to #%s (%s)", l.ReplyTo, snippet))
		}
		b.WriteString(" ")
		b.WriteString(l.Text)
		b.WriteString("\n")
	}
	return b.String()
}

func firstRunes(s string, n int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
