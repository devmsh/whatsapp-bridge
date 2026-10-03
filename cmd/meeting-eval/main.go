// Command meeting-eval runs the meeting finder over real chats and prints
// what it found, without writing anything.
//
// The twin of extract-eval, for the other half of the engine. "It looks like
// it works" is not a thing anybody can check later; this replays real
// conversations through the real pipeline and prints every proposal, every
// rejection and the reason, so a prompt change can be measured rather than
// hoped about.
//
//	go run ./cmd/meeting-eval -chat "Ali/Sharif/Hassan" -today
//	go run ./cmd/meeting-eval -chat 120363...@g.us -from 2026-09-01
//	go run ./cmd/meeting-eval -chat "Some group" -today -write   (actually saves)
//	go run ./cmd/meeting-eval -chat "Some group" -today -keywords-only
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"whatsapp-bridge-v2/internal/db"
	"whatsapp-bridge-v2/internal/extract"
	"whatsapp-bridge-v2/internal/extract/adapters"
)

func main() {
	var (
		dbPath   = flag.String("db", envOr("WA_DB_PATH", "store/messages.db"), "database path")
		chatArg  = flag.String("chat", "", "chat JID, or part of the chat name")
		from     = flag.String("from", "", "start date YYYY-MM-DD")
		to       = flag.String("to", "", "end date YYYY-MM-DD (inclusive)")
		today    = flag.Bool("today", false, "just today")
		days     = flag.Int("days", 0, "the last N days")
		model    = flag.String("model", envOr("EXTRACT_MODEL", "qwen3:30b-a3b-instruct-2507-q4_K_M"), "ollama model")
		ollama   = flag.String("ollama", envOr("OLLAMA_URL", ""), "ollama base url")
		write    = flag.Bool("write", false, "actually save the meetings (default is a dry run)")
		keysOnly = flag.Bool("keywords-only", false, "only run the keyword pass, no model")
		tz       = flag.String("tz", envOr("EXTRACT_TZ", "Asia/Riyadh"), "timezone")
	)
	flag.Parse()

	if *chatArg == "" {
		fail("give -chat: a JID, or part of the chat name")
	}
	loc, err := time.LoadLocation(*tz)
	if err != nil {
		loc = time.UTC
	}

	store, err := db.NewStore(*dbPath)
	if err != nil {
		fail("open database: %v", err)
	}
	defer store.Close()

	jid, name := resolveChat(store, *chatArg)
	if jid == "" {
		fail("no chat matches %q", *chatArg)
	}

	since, until := window(*from, *to, *today, *days, loc)
	fmt.Printf("Chat:    %s  (%s)\n", name, jid)
	fmt.Printf("Window:  %s → %s\n", stamp(since, loc), stamp(until, loc))

	// Step 1: the keyword pass, exactly as the scanner runs it. This is what
	// decides whether a model is asked at all, so it is worth seeing on its
	// own — a meeting missed here is never found.
	lines, err := extract.FetchLines(store, jid, since, until)
	if err != nil {
		fail("read chat: %v", err)
	}
	fmt.Printf("Messages: %d\n\n", len(lines))
	if len(lines) == 0 {
		fmt.Println("Nothing in this window.")
		return
	}

	fmt.Println("── keyword pass ──────────────────────────────────────────────")
	hits := 0
	for _, l := range lines {
		if extract.MentionsMeetingTalk(l.Text) {
			hits++
			fmt.Printf("  HIT  %s  %s: %s\n", stamp(l.TS, loc), l.Sender, oneLine(l.Text, 90))
		}
	}
	fmt.Printf("  %d of %d messages read like meeting talk.\n", hits, len(lines))
	if hits == 0 {
		fmt.Println("\n  The scanner would NOT ask the model about this chat.")
	}
	if *keysOnly {
		return
	}

	fmt.Println("\n── model pass ────────────────────────────────────────────────")
	finder := adapters.NewOllama(*ollama, *model)
	fmt.Printf("  engine: %s\n\n", finder.Name())

	started := time.Now()
	res, err := extract.RunMeetings(context.Background(),
		extract.MeetingDeps{Store: store, Finder: finder, Loc: loc, DryRun: !*write},
		extract.RunSpec{ChatJID: jid, Since: since, Until: until, RunID: "eval"},
		func(msg string) { fmt.Printf("  · %s\n", msg) })
	if err != nil {
		fail("run: %v", err)
	}

	fmt.Printf("\n── result (%s) ───────────────────────────────────────────\n",
		time.Since(started).Round(time.Second))
	fmt.Printf("  chunks %d, skipped %d (no meeting talk), proposed %d, kept %d, attached %d\n",
		res.Chunks, res.Skipped, res.Proposed, res.Verified, res.Attached)
	if res.FailedChunks > 0 {
		fmt.Printf("  FAILED chunks: %d — %s\n", res.FailedChunks, res.LastError)
	}
	if len(res.Rejections) > 0 {
		fmt.Print("  dropped: ")
		var parts []string
		for reason, n := range res.Rejections {
			parts = append(parts, fmt.Sprintf("%s×%d", reason, n))
		}
		fmt.Println(strings.Join(parts, ", "))
	}

	// Every proposal, kept or dropped. A dry run writes nothing, so this is
	// the only way to see what the model actually said.
	for _, o := range res.Outcomes {
		printOutcome(o, loc)
	}
	if len(res.Outcomes) == 0 {
		fmt.Println("\n  The model proposed nothing.")
	}
	for i := range res.Meetings {
		fmt.Printf("\n  saved as meeting #%d:", res.Meetings[i].ID)
		printMeeting(res.Meetings[i], loc)
	}
	if !*write {
		fmt.Println("\n  Dry run — nothing was saved. Add -write to keep these.")
	}
}

func printOutcome(o extract.MeetingOutcome, loc *time.Location) {
	p := o.Proposal
	mark := "KEPT   "
	if !o.Kept {
		mark = "dropped"
		if o.AttachedTo > 0 {
			mark = fmt.Sprintf("joined #%d", o.AttachedTo)
		}
	}
	fmt.Printf("\n  [%s] %s\n", mark, p.Title)
	if o.Reason != "" {
		fmt.Printf("     why not:  %s\n", o.Reason)
	}
	if p.Purpose != "" {
		fmt.Printf("     purpose:  %s\n", p.Purpose)
	}
	fmt.Printf("     status:   %s   confidence %.2f\n", p.Status, p.Confidence)
	when := "(no date named)"
	if o.StartsAt > 0 {
		when = stamp(o.StartsAt, loc)
	}
	fmt.Printf("     starts:   %s\n", when)
	if p.WhenText != "" || p.TimeText != "" {
		fmt.Printf("     said:     when=%q time=%q\n", p.WhenText, p.TimeText)
	}
	if p.Mode != "" {
		fmt.Printf("     mode:     %s\n", p.Mode)
	}
	if p.Location != "" {
		fmt.Printf("     where:    %s\n", p.Location)
	}
	if p.Link != "" {
		fmt.Printf("     link:     %s\n", oneLine(p.Link, 80))
	}
	if len(p.Attendees) > 0 {
		fmt.Printf("     people:   %s\n", strings.Join(p.Attendees, ", "))
	}
	for _, a := range p.Agenda {
		fmt.Printf("     agenda:   %s\n", oneLine(a, 90))
	}
	id := o.EvidenceID
	if id == "" {
		id = p.EvidenceID + " (not found)"
	}
	fmt.Printf("     evidence: [%s] %s\n", id, oneLine(p.Evidence, 90))
}

// printMeeting shows a meeting that was really written.
func printMeeting(m db.Meeting, loc *time.Location) {
	fmt.Printf("\n  ── %s\n", m.Title)
	if m.Purpose != "" {
		fmt.Printf("     purpose:  %s\n", m.Purpose)
	}
	fmt.Printf("     status:   %s   confidence %.2f\n", m.Status, m.Confidence)
	if m.StartsAt > 0 {
		fmt.Printf("     starts:   %s\n", stamp(m.StartsAt, loc))
	} else {
		fmt.Printf("     starts:   (no date named)\n")
	}
	if m.TimeOptions != "" && m.TimeOptions != "[]" {
		fmt.Printf("     timing:   %s\n", m.TimeOptions)
	}
	if m.Mode != "" {
		fmt.Printf("     mode:     %s\n", m.Mode)
	}
	if m.Location != "" {
		fmt.Printf("     where:    %s\n", m.Location)
	}
	if m.Link != "" {
		fmt.Printf("     link:     %s\n", oneLine(m.Link, 80))
	}
	fmt.Printf("     evidence: %s\n", m.OriginMessageID)
	fmt.Printf("     review:   %s\n", m.ReviewStatus)
}

// resolveChat takes a JID or a piece of a name and returns the one chat meant.
func resolveChat(store *db.Store, arg string) (string, string) {
	if strings.Contains(arg, "@") {
		var name string
		store.DB.QueryRow(`
			SELECT COALESCE(NULLIF(g.name,''), NULLIF(ct.name,''), NULLIF(ct.push_name,''), ch.jid)
			FROM chats ch
			LEFT JOIN groups g ON g.jid = ch.jid
			LEFT JOIN contacts ct ON (ct.jid = ch.jid OR ct.lid = ch.jid)
			WHERE ch.jid = ?`, arg).Scan(&name)
		return arg, name
	}
	rows, err := store.DB.Query(`
		SELECT jid, cname FROM (
			SELECT ch.jid AS jid,
			       COALESCE(NULLIF(g.name,''), NULLIF(ct.name,''), NULLIF(ct.push_name,''), ch.jid) AS cname
			FROM chats ch
			LEFT JOIN groups g ON g.jid = ch.jid
			LEFT JOIN contacts ct ON (ct.jid = ch.jid OR ct.lid = ch.jid)
		) WHERE cname LIKE ? LIMIT 10`, "%"+arg+"%")
	if err != nil {
		return "", ""
	}
	defer rows.Close()
	type hit struct{ jid, name string }
	var hits []hit
	for rows.Next() {
		var h hit
		if rows.Scan(&h.jid, &h.name) == nil {
			hits = append(hits, h)
		}
	}
	switch len(hits) {
	case 0:
		return "", ""
	case 1:
		return hits[0].jid, hits[0].name
	}
	fmt.Fprintln(os.Stderr, "More than one chat matches. Pick one:")
	for _, h := range hits {
		fmt.Fprintf(os.Stderr, "  %-32s %s\n", h.jid, h.name)
	}
	os.Exit(2)
	return "", ""
}

// window turns the date flags into a start and an end.
func window(from, to string, today bool, days int, loc *time.Location) (int64, int64) {
	now := time.Now().In(loc)
	end := now.Unix()

	switch {
	case today:
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		return start.Unix(), end
	case days > 0:
		return now.AddDate(0, 0, -days).Unix(), end
	}

	start := int64(0)
	if from != "" {
		if t, err := time.ParseInLocation("2006-01-02", from, loc); err == nil {
			start = t.Unix()
		}
	}
	if to != "" {
		if t, err := time.ParseInLocation("2006-01-02", to, loc); err == nil {
			end = t.AddDate(0, 0, 1).Unix() - 1
		}
	}
	if start == 0 {
		// No dates at all: the last week, which is the useful default.
		start = now.AddDate(0, 0, -7).Unix()
	}
	return start, end
}

func stamp(ts int64, loc *time.Location) string {
	if ts == 0 {
		return "—"
	}
	return time.Unix(ts, 0).In(loc).Format("2006-01-02 15:04")
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
