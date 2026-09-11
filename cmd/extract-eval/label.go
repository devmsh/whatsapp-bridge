package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"whatsapp-bridge-v2/internal/db"
	"whatsapp-bridge-v2/internal/extract"
)

// printForLabelling shows a slice of one chat exactly as the model will see
// it, then writes a skeleton case file.
//
// Labelling by hand is the slow part of evaluation, so the point of this is to
// make it a copy-and-paste job: the message ids are already in the text, and
// the file is already valid JSON with the right chat and dates in it.
func printForLabelling(store *db.Store, name, from, to, dir string) error {
	chatJID, display, err := findChat(store, name)
	if err != nil {
		return err
	}
	loc := riyadh()

	since, err := parseDay(from, loc)
	if err != nil {
		return fmt.Errorf("-from: %w", err)
	}
	until, err := parseDay(to, loc)
	if err != nil {
		return fmt.Errorf("-to: %w", err)
	}
	if until == 0 {
		until = time.Now().Unix()
	}

	lines, err := extract.FetchLines(store, chatJID, since, until)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return fmt.Errorf("no messages in %q between those dates", display)
	}

	chunks := extract.Split(lines, chatJID, extract.DefaultChunkOptions(loc))
	for _, c := range chunks {
		fmt.Printf("\n=== chunk %d — %d message(s) ===\n", c.Index, len(c.Lines))
		fmt.Println(extract.Render(c, loc))
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, slug(display)+".json")
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("\n%s already exists — not overwriting it.\n", path)
		return nil
	}

	skeleton := evalCase{
		Name: display, ChatJID: chatJID,
		Since: flexTime(since), Until: flexTime(until),
		Expected: []expectedTask{{
			Title:      "<the task, in the words you would write it>",
			OwnerJID:   "<who must do it, e.g. 966500000000@s.whatsapp.net, or empty>",
			EvidenceID: "<the [#id] of the message that asks for it>",
		}},
	}
	raw, err := json.MarshalIndent(skeleton, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		return err
	}

	fmt.Printf("\nWrote %s\n", path)
	fmt.Println("Now edit it: one entry per real task above. Delete the example entry.")
	fmt.Println("A slice with no tasks in it is worth keeping too — leave 'expected' empty.")
	return nil
}

// findChat accepts a JID, a group name or a contact name, because nobody
// remembers a JID.
func findChat(store *db.Store, name string) (jid, display string, err error) {
	if strings.Contains(name, "@") {
		return name, chatName(store, name), nil
	}
	store.DB.QueryRow(`SELECT jid, name FROM groups WHERE name = ?`, name).Scan(&jid, &display)
	if jid != "" {
		return jid, display, nil
	}
	store.DB.QueryRow(`SELECT c.jid, COALESCE(NULLIF(ct.name,''), ct.push_name)
		FROM chats c JOIN contacts ct ON (ct.jid = c.jid OR ct.lid = c.jid)
		WHERE ct.name = ? OR ct.push_name = ? LIMIT 1`, name, name).Scan(&jid, &display)
	if jid != "" {
		return jid, display, nil
	}
	// Last try: anything whose name contains the text.
	store.DB.QueryRow(`SELECT jid, name FROM groups WHERE name LIKE ? LIMIT 1`,
		"%"+name+"%").Scan(&jid, &display)
	if jid == "" {
		return "", "", fmt.Errorf("no chat called %q", name)
	}
	return jid, display, nil
}

func chatName(store *db.Store, jid string) string {
	var name string
	store.DB.QueryRow(`SELECT COALESCE(NULLIF(g.name,''), NULLIF(ct.name,''), ?1)
		FROM chats ch LEFT JOIN groups g ON g.jid = ch.jid
		LEFT JOIN contacts ct ON (ct.jid = ch.jid OR ct.lid = ch.jid)
		WHERE ch.jid = ?1`, jid).Scan(&name)
	if name == "" {
		return jid
	}
	return name
}

func parseDay(s string, loc *time.Location) (int64, error) {
	if s == "" {
		return 0, nil
	}
	t, err := time.ParseInLocation("2006-01-02", s, loc)
	if err != nil {
		return 0, fmt.Errorf("use a date like 2026-07-26")
	}
	return t.Unix(), nil
}

var unsafe = regexp.MustCompile(`[^a-zA-Z0-9\x{0600}-\x{06FF}]+`)

func slug(s string) string {
	out := strings.Trim(unsafe.ReplaceAllString(s, "-"), "-")
	if out == "" {
		return "case"
	}
	return strings.ToLower(out)
}
