package api

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"whatsapp-bridge-v2/internal/db"
)

// maxExportMessagesPerChat caps how many messages are pulled per chat for the
// export. The output is text-only so it stays small, but the cap guards memory
// on pathologically long chats. In practice no chat reaches it.
const maxExportMessagesPerChat = 1000000

// handleCircleExport streams a .zip archive of every chat in a circle as
// plain-text transcripts (WhatsApp-style). The zip contains one .txt per chat
// plus a _summary.txt. Nested sub-circles, groups, and contacts are all
// included; hidden chats are included by design (this is a full export).
// GET /api/v2/circles/{id}/export
func (s *Server) handleCircleExport(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	circle, err := s.store.GetCircle(id)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	if circle == nil {
		jsonError(w, 404, "circle not found")
		return
	}

	jids, err := s.store.FlattenCircleChats(id)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}

	now := time.Now()
	folder := fmt.Sprintf("circle-%s-%s", sanitizeFilename(circle.Name), now.Format("2006-01-02"))
	zipName := folder + ".zip"

	// Headers go out before the body; once the zip starts streaming we can no
	// longer send a JSON error, so any per-chat failure below is skipped.
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(
		"attachment; filename=%q; filename*=UTF-8''%s", "circle-export.zip", url.PathEscape(zipName)))

	zw := zip.NewWriter(w)
	defer zw.Close()

	type chatSummary struct {
		name  string
		count int
	}
	var summaries []chatSummary
	usedNames := map[string]bool{} // de-dupe colliding chat filenames

	for _, jid := range jids {
		name := s.exportChatName(jid)
		msgs := s.fetchAllMessages(jid)
		summaries = append(summaries, chatSummary{name: name, count: len(msgs)})
		if len(msgs) == 0 {
			continue // listed in the summary, but no file written
		}
		fileName := uniqueName(usedNames, sanitizeFilename(name)) + ".txt"
		fw, err := zw.Create(folder + "/" + fileName)
		if err != nil {
			continue
		}
		writeChatTranscript(fw, name, msgs)
	}

	// Summary, ordered by message count so the busiest chats are on top.
	sort.SliceStable(summaries, func(i, j int) bool { return summaries[i].count > summaries[j].count })
	if sw, err := zw.Create(folder + "/_summary.txt"); err == nil {
		total := 0
		for _, c := range summaries {
			total += c.count
		}
		fmt.Fprintf(sw, "Circle: %s\n", circle.Name)
		fmt.Fprintf(sw, "Exported: %s\n", now.Format("2006-01-02 15:04:05"))
		fmt.Fprintf(sw, "Chats: %d\n", len(summaries))
		fmt.Fprintf(sw, "Messages: %d\n\n", total)
		for _, c := range summaries {
			fmt.Fprintf(sw, "%s — %d messages\n", c.name, c.count)
		}
	}
}

// fetchAllMessages returns every stored message for a chat in chronological
// order, merging the phone JID and LID for DMs (same as the chat view).
func (s *Server) fetchAllMessages(jid string) []db.Message {
	chatJIDs := []string{jid}
	if s.client != nil {
		if lid := s.client.ResolveLIDForJID(jid); lid != "" {
			chatJIDs = append(chatJIDs, lid)
		} else if pn := s.client.ResolvePhoneForLID(jid); pn != "" {
			chatJIDs = append(chatJIDs, pn)
		}
	}
	msgs, err := s.store.GetMessagesMerged(chatJIDs, 0, maxExportMessagesPerChat)
	if err != nil {
		return nil
	}
	return msgs
}

// exportChatName resolves a human label for a chat JID: the group name for
// groups, the best contact name for DMs, else the bare phone/JID.
func (s *Server) exportChatName(jid string) string {
	if strings.HasSuffix(jid, "@g.us") {
		if c, _ := s.store.GetChat(jid); c != nil && strings.TrimSpace(c.Name) != "" {
			return c.Name
		}
		return jidUser(jid)
	}
	// DM: resolve a contact name across the jid / phone / lid variants.
	var name string
	row := s.store.DB.QueryRow(
		`SELECT COALESCE(NULLIF(name,''), NULLIF(verified_name,''), NULLIF(business_name,''), NULLIF(push_name,''), phone, '')
		 FROM contacts
		 WHERE jid = ? OR phone || '@s.whatsapp.net' = ? OR lid || '@lid' = ?
		 LIMIT 1`, jid, jid, jid)
	if row.Scan(&name) == nil && strings.TrimSpace(name) != "" {
		return name
	}
	if c, _ := s.store.GetChat(jid); c != nil && strings.TrimSpace(c.Name) != "" {
		return c.Name
	}
	return jidUser(jid)
}

// writeChatTranscript writes one chat's messages as readable text lines.
func writeChatTranscript(w io.Writer, chatName string, msgs []db.Message) {
	fmt.Fprintf(w, "Chat: %s\n", chatName)
	fmt.Fprintf(w, "Messages: %d\n", len(msgs))
	fmt.Fprintln(w, strings.Repeat("-", 40))
	for _, m := range msgs {
		body := exportBody(m)
		if body == "" {
			continue // skip empty system messages
		}
		ts := time.Unix(m.Timestamp, 0).Format("2006-01-02 15:04")
		fmt.Fprintf(w, "[%s] %s: %s\n", ts, exportSenderName(m), body)
	}
}

// exportSenderName returns the display name for a message's author.
func exportSenderName(m db.Message) string {
	if m.IsFromMe {
		return "You"
	}
	if n := firstNonEmpty(m.SenderName, m.PushName); n != "" {
		return strings.TrimSpace(n)
	}
	if m.Sender != "" {
		return jidUser(m.Sender)
	}
	return "Unknown"
}

// exportBody renders the text body of a single message: the text for plain
// messages, AI transcript/description or a typed placeholder for media, and
// special markers for deleted / location / contact / poll messages.
func exportBody(m db.Message) string {
	if m.IsDeleted {
		return "<message deleted>"
	}
	if strings.TrimSpace(m.MediaType) != "" {
		s := mediaPlaceholder(m)
		if c := strings.TrimSpace(m.MediaCaption); c != "" {
			s += " " + c
		}
		return withEdited(m, s)
	}
	if m.Latitude != 0 || m.Longitude != 0 {
		loc := strings.TrimSpace(m.LocationName)
		if loc == "" {
			loc = fmt.Sprintf("%.5f,%.5f", m.Latitude, m.Longitude)
		}
		return "<location: " + loc + ">"
	}
	if n := strings.TrimSpace(m.VCardName); n != "" {
		return "<contact card: " + n + ">"
	}
	if strings.TrimSpace(m.PollID) != "" && strings.TrimSpace(m.Content) == "" {
		return "<poll>"
	}
	return withEdited(m, strings.TrimSpace(m.Content))
}

// mediaPlaceholder picks the line for a media message, preferring AI-derived
// text (voice transcript, image description) where the bridge already has it.
func mediaPlaceholder(m db.Message) string {
	mt := strings.ToLower(m.MediaType)
	switch {
	case strings.Contains(mt, "audio") || mt == "ptt":
		if t := strings.TrimSpace(m.Transcript); t != "" {
			return fmt.Sprintf("‎<voice, transcript: %q>", t)
		}
		return "‎<voice message omitted>"
	case strings.Contains(mt, "image"):
		if d := strings.TrimSpace(m.MediaDescription); d != "" {
			return "‎<image: " + d + ">"
		}
		return "‎<image omitted>"
	case strings.Contains(mt, "video"):
		return "‎<video omitted>"
	case strings.Contains(mt, "document"):
		if fn := firstNonEmpty(m.MediaFilename, m.MediaCaption); fn != "" {
			return "‎<document: " + strings.TrimSpace(fn) + ">"
		}
		return "‎<document omitted>"
	case strings.Contains(mt, "sticker"):
		return "‎<sticker>"
	default:
		return "‎<media omitted>"
	}
}

// withEdited appends an "(edited)" marker to a non-empty body for edited messages.
func withEdited(m db.Message, s string) string {
	if s != "" && m.IsEdit {
		return s + " (edited)"
	}
	return s
}

// sanitizeFilename makes a chat name safe to use as a zip entry name.
func sanitizeFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		if r < 0x20 {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if r := []rune(name); len(r) > 60 {
		name = strings.TrimSpace(string(r[:60]))
	}
	if name == "" {
		return "chat"
	}
	return name
}

// uniqueName returns base, or "base (2)", "base (3)", … if base is already used.
// Comparison is case-insensitive so the names work on case-insensitive filesystems.
func uniqueName(used map[string]bool, base string) string {
	name := base
	for i := 2; used[strings.ToLower(name)]; i++ {
		name = fmt.Sprintf("%s (%d)", base, i)
	}
	used[strings.ToLower(name)] = true
	return name
}
