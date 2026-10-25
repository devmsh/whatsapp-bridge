package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"whatsapp-bridge-v2/internal/db"
	"whatsapp-bridge-v2/internal/extract"
)

// MeetingScanner is what closes the meetings gap.
//
// Until this existed, a meeting arranged in a new message was never picked up
// by anything. The task sweep saw it and threw it away on purpose (meetings
// have their own module), and the meetings module only ran when somebody
// called the endpoint by hand — which nothing in the UI did. So new meetings
// were simply missed.
//
// It runs in two steps, and the split is the whole point:
//
//  1. A keyword pass over new messages. Pure string matching, no model, a few
//     milliseconds per chat. It answers "could there be a meeting in here".
//  2. A model call, but only for the chats step 1 flagged, one at a time, and
//     never more than once per cooldown for the same chat.
//
// Most chat is not about meetings, so step 1 throws away almost everything and
// step 2 stays cheap enough to run on a timer. Since the finder now runs on
// the local engine, a run costs no quota at all — which is what makes running
// it automatically reasonable in the first place.
type MeetingScanner struct {
	s  *Server
	mu sync.Mutex

	running      bool
	current      *Run
	lastRunID    string
	lastTickedAt int64
	lastScanned  int // chats looked at on the last keyword pass
	lastFlagged  int // of those, how many had meeting talk
	// refreshStreak counts consecutive ticks that gave the refresh the turn
	// instead of the finder. See refreshTurn.
	refreshStreak int
}

const (
	meetingScanEnabledKey  = "meeting_scan_enabled"
	meetingScanCooldownKey = "meeting_scan_cooldown_hours"
	// A chat is not read again inside this window, however much meeting talk
	// arrives. Six hours means a busy group is looked at a few times a day,
	// and a thread being argued over all morning produces one run, not twenty.
	meetingScanDefaultCooldownHrs = 6
	// How often the timer fires. The same cadence as auto-extract.
	meetingScanTick = 10 * time.Minute
	// A chat needs at least this many meeting-ish messages waiting before a
	// model reads it. One is enough: a single "نجتمع الخميس الساعة ٥" is a
	// whole meeting, and waiting for a second mention would miss it.
	meetingScanMinHits = 1
	// How far back the very first scan of a chat looks. Without this, a chat
	// with four years of history would hand its entire past to the model on
	// the first tick. Meetings older than this are backfilled on demand.
	meetingScanFirstLook = 30 * 24 * 3600
)

func newMeetingScanner(s *Server) *MeetingScanner { return &MeetingScanner{s: s} }

// enabled defaults to ON. The scanner only costs local model time, and a
// meetings module that silently never runs is the bug this replaces.
//
// This switch covers all AUTOMATIC model work for meetings: the finder's own
// tick, and the automatic refresh started from it. The rule pass
// (CloseStaleMeetings) costs no model call, so it runs every tick regardless
// of the switch. The manual endpoints — this endpoint's run_now, and POST
// /api/v2/meetings/refresh — also ignore the switch: turning the timer off
// stops new work from starting on its own, not a person asking for a check
// by hand.
func (m *MeetingScanner) enabled() bool {
	v, _, _ := m.s.store.GetSyncState(meetingScanEnabledKey)
	return v != "0"
}

func (m *MeetingScanner) cooldownHours() int {
	v, _, _ := m.s.store.GetSyncState(meetingScanCooldownKey)
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return meetingScanDefaultCooldownHrs
	}
	return n
}

// Start launches the timer. First tick is delayed so it does not compete with
// the boot-time sync, and offset from the task sweep so the two are not asking
// the same model at the same moment.
func (m *MeetingScanner) Start() {
	go func() {
		time.Sleep(90 * time.Second)
		t := time.NewTicker(meetingScanTick)
		defer t.Stop()
		m.tick()
		for range t.C {
			m.tick()
		}
	}()
}

func (m *MeetingScanner) tick() {
	m.mu.Lock()
	m.lastTickedAt = time.Now().Unix()
	busy := m.running
	m.mu.Unlock()
	if busy {
		return
	}

	// CloseStaleMeetings costs no model call, so it runs on every tick, on or
	// off, no matter which job (if any) currently holds the model slot below
	// — except we skip it while a job is actually running, so a rule patch
	// never races that job's own writes to the same meeting.
	if m.s.meetingModel.heldBy() == "" {
		m.s.store.CloseStaleMeetings(time.Now().Unix())
	}

	if !m.enabled() {
		return
	}
	m.s.rememberOwnJID()

	// Step 1 runs even when nothing will be extracted, because the flags are
	// worth having on their own: the debug screen shows which chats are
	// waiting, and a person can run one by hand.
	scanned, flagged := m.scanForMeetingTalk()
	m.mu.Lock()
	m.lastScanned, m.lastFlagged = scanned, flagged
	m.mu.Unlock()

	// A meeting already found still needs to be kept up to date, and that
	// takes the same one model slot the finder below uses (meetingModelSlot
	// on the Server). Refresh gets the turn first, but not forever: two
	// ticks in a row that start a refresh spend the turn, and the third
	// always goes to the finder instead. Without this cap, a chat whose
	// model call keeps failing never advances its checked_ts (see
	// RefreshChatMeetings), so it would keep "something is due" true on
	// every tick and lock the finder out for good.
	if m.refreshTurn() {
		now := time.Now().Unix()
		chats, err := m.s.store.ChatsWithMeetingFollowUps(now, 25)
		if err == nil && len(chats) > 0 {
			if _, ok := m.s.meetingRefresh.run("auto", chats); ok {
				m.markRefreshStarted()
				return
			}
			// The slot was already taken by something else (most likely a
			// manual refresh) — fall through and let the finder try; it will
			// meet the same refusal below and simply wait for the next tick.
		} else {
			m.markNothingToRefresh()
		}
	}

	chatJID, label, ok := m.pickDueChat()
	if !ok {
		return
	}
	m.startRun(chatJID, label, "auto")
}

// refreshTurn reports whether this tick may try a refresh before the finder.
// Two refreshes in a row use up the turn; the next tick always skips
// straight to the finder, and the streak starts over from there.
func (m *MeetingScanner) refreshTurn() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.refreshStreak >= 2 {
		m.refreshStreak = 0
		return false
	}
	return true
}

// markRefreshStarted records that this tick spent its turn on a refresh.
func (m *MeetingScanner) markRefreshStarted() {
	m.mu.Lock()
	m.refreshStreak++
	m.mu.Unlock()
}

// markNothingToRefresh resets the streak: a tick that found nothing due
// never counts against the finder's turn.
func (m *MeetingScanner) markNothingToRefresh() {
	m.mu.Lock()
	m.refreshStreak = 0
	m.mu.Unlock()
}

// scanForMeetingTalk is step 1: read the new messages of every live chat and
// count the ones that look like meeting talk. No model is involved.
func (m *MeetingScanner) scanForMeetingTalk() (scanned, flagged int) {
	excluded := m.s.store.AIExcludedJIDs()
	cutoff := time.Now().Unix() - meetingScanFirstLook

	rows, err := m.s.store.DB.Query(`
		SELECT c.jid, COALESCE(MAX(msg.timestamp), 0)
		FROM chats c
		JOIN messages msg ON msg.chat_jid = c.jid
		WHERE c.is_archived = 0 AND COALESCE(c.deleted_at, 0) = 0
		GROUP BY c.jid
		HAVING MAX(msg.timestamp) > 0`)
	if err != nil {
		return 0, 0
	}
	type candidate struct {
		jid   string
		maxTS int64
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if rows.Scan(&c.jid, &c.maxTS) == nil && !excluded[c.jid] {
			candidates = append(candidates, c)
		}
	}
	rows.Close()

	for _, c := range candidates {
		st := m.s.store.GetMeetingScanState(c.jid)
		since := st.LastMsgTS
		if since == 0 {
			// Never scanned. Look at the last month only.
			since = cutoff
		}
		if c.maxTS <= since {
			continue // nothing new
		}
		scanned++

		hits, lastHit := m.countMeetingTalk(c.jid, since)
		if hits == 0 {
			// Nothing meeting-shaped in the new messages, so the model has no
			// reason to read them — ever. Move the watermark past them now,
			// or every future tick re-reads the same chatter.
			m.s.store.AdvanceMeetingScan(c.jid, c.maxTS, "", 0)
			continue
		}
		flagged++
		m.s.store.SetMeetingScanHits(c.jid, hits, lastHit)
	}
	return scanned, flagged
}

// countMeetingTalk counts new messages in one chat that read like meeting
// talk, and returns the newest such message's time.
func (m *MeetingScanner) countMeetingTalk(chatJID string, since int64) (int, int64) {
	// content plus the caption: a meeting is often arranged under a shared
	// photo of a schedule. Voice-note transcripts live in content too, once
	// the media worker has filled them in.
	rows, err := m.s.store.DB.Query(`
		SELECT COALESCE(content, ''), COALESCE(media_caption, ''), COALESCE(timestamp, 0)
		FROM messages
		WHERE chat_jid = ? AND timestamp > ? AND is_deleted = 0
		ORDER BY timestamp ASC LIMIT 2000`, chatJID, since)
	if err != nil {
		return 0, 0
	}
	defer rows.Close()

	hits := 0
	var lastHit int64
	for rows.Next() {
		var text, caption string
		var ts int64
		if rows.Scan(&text, &caption, &ts) != nil {
			continue
		}
		if extract.MentionsMeetingTalk(text) || extract.MentionsMeetingTalk(caption) {
			hits++
			lastHit = ts
		}
	}
	return hits, lastHit
}

// pickDueChat returns the chat most worth reading now: the most waiting
// meeting talk, among chats past their cooldown.
func (m *MeetingScanner) pickDueChat() (string, string, bool) {
	states, err := m.s.store.ListMeetingScanState(200)
	if err != nil {
		return "", "", false
	}
	cutoff := time.Now().Unix() - int64(m.cooldownHours())*3600
	for _, st := range states {
		if st.Hits < meetingScanMinHits {
			continue // the list is sorted by hits, so nothing below this matters
		}
		if st.LastRunAt > cutoff {
			continue
		}
		if m.s.store.IsChatExcludedFromAI(st.ChatJID) {
			continue
		}
		return st.ChatJID, chatLabel(m.s.store, st.ChatJID), true
	}
	return "", "", false
}

// startRun kicks off one meeting extraction and tracks it. It returns nil,
// changing nothing, when the model slot is already taken — by a refresh, or
// by another finder run — so a caller must always check for nil rather than
// assume a run started.
func (m *MeetingScanner) startRun(chatJID, label, trigger string) *Run {
	if !m.s.meetingModel.claim("finder") {
		return nil
	}

	run, ctx := m.s.runs.StartTagged("meetings", chatJID, label, trigger)

	m.mu.Lock()
	m.running = true
	m.current = run
	m.lastRunID = run.ID
	m.mu.Unlock()

	fmt.Printf("meeting-scan: %s (%s) run=%s\n", label, chatJID, run.ID)

	go func() {
		defer func() {
			m.mu.Lock()
			m.running = false
			m.current = nil
			m.mu.Unlock()
			m.s.meetingModel.release("finder")
		}()
		m.s.runMeetingExtraction(ctx, run, chatJID)
	}()
	return run
}

// stop cancels an in-flight run, so switching the scanner off means "stop
// now", not "stop in forty minutes".
func (m *MeetingScanner) stop() {
	m.mu.Lock()
	run := m.current
	m.mu.Unlock()
	if run != nil {
		run.Cancel()
	}
}

// runMeetingExtraction is step 2: the local engine reads one chat and writes
// whatever meetings it finds, all pending review.
func (s *Server) runMeetingExtraction(ctx context.Context, run *Run, chatJID string) {
	run.SetRunning()

	finder, err := s.meetingFinder()
	if err != nil {
		run.Finish(RunFailed, "", "", 0, err.Error())
		return
	}

	deps := extract.MeetingDeps{Store: s.store, Finder: finder, Loc: extractLocation()}
	res, err := extract.RunMeetings(ctx, deps,
		extract.RunSpec{ChatJID: chatJID, RunID: run.ID},
		func(msg string) { run.AddEvent(RunEvent{Kind: "info", Text: msg}) })

	if ctx.Err() == context.Canceled {
		run.Finish(RunCancelled, "", "Cancelled.", len(res.Meetings), "")
		return
	}
	if err != nil {
		run.Finish(RunFailed, "", "", 0, err.Error())
		return
	}

	// The watermark moves even when nothing was found: the model has read
	// these messages and said no, and asking it again would give the same
	// answer at the same cost.
	if res.LastMsgTS > 0 {
		s.store.AdvanceMeetingScan(chatJID, res.LastMsgTS, run.ID, len(res.Meetings))
	}
	run.Finish(RunDone, "", summariseMeetings(finder.Name(), res), len(res.Meetings), "")
}

func summariseMeetings(engine string, r extract.MeetingResult) string {
	out := fmt.Sprintf("%s: %d proposed, %d kept across %d chunk(s)",
		engine, r.Proposed, r.Verified, r.Chunks)
	if r.Attached > 0 {
		out += fmt.Sprintf("; %d attached to a meeting already known", r.Attached)
	}
	if r.Skipped > 0 {
		out += fmt.Sprintf("; %d chunk(s) had no meeting talk", r.Skipped)
	}
	if r.CappedOut {
		out += fmt.Sprintf("; STOPPED at the %d-meeting cap — something is off, check the queue",
			extract.MaxMeetingsPerRun)
	}
	if r.FailedChunks > 0 {
		out += fmt.Sprintf("; %d chunk(s) failed", r.FailedChunks)
	}
	return out
}

// meetingFinder builds the engine that finds meetings, honouring the same
// engine setting the task extractor uses.
func (s *Server) meetingFinder() (extract.MeetingFinder, error) {
	ex, err := s.extractorFor("", "")
	if err != nil {
		return nil, err
	}
	finder, ok := ex.(extract.MeetingFinder)
	if !ok {
		return nil, fmt.Errorf("the %s engine cannot find meetings", ex.Name())
	}
	return finder, nil
}

// meetingScanStatus is what the debug screen shows.
type meetingScanStatus struct {
	Enabled       bool   `json:"enabled"`
	CooldownHours int    `json:"cooldown_hours"`
	TickSeconds   int    `json:"tick_seconds"`
	Running       bool   `json:"running"`
	LastRunID     string `json:"last_run_id,omitempty"`
	LastTickedAt  int64  `json:"last_ticked_at,omitempty"`
	NextTickAt    int64  `json:"next_tick_at,omitempty"`
	LastScanned   int    `json:"last_scanned"`
	LastFlagged   int    `json:"last_flagged"`
	Waiting       int    `json:"waiting"` // chats with meeting talk not yet read
}

func (m *MeetingScanner) status() meetingScanStatus {
	m.mu.Lock()
	st := meetingScanStatus{
		Enabled:       m.enabled(),
		CooldownHours: m.cooldownHours(),
		TickSeconds:   int(meetingScanTick / time.Second),
		Running:       m.running,
		LastRunID:     m.lastRunID,
		LastTickedAt:  m.lastTickedAt,
		LastScanned:   m.lastScanned,
		LastFlagged:   m.lastFlagged,
	}
	m.mu.Unlock()
	if st.LastTickedAt > 0 {
		st.NextTickAt = st.LastTickedAt + int64(st.TickSeconds)
	}
	m.s.store.DB.QueryRow(`SELECT COUNT(*) FROM meeting_scan_state WHERE hits > 0`).
		Scan(&st.Waiting)
	return st
}

// handleMeetingScan is the scanner's own endpoint.
//
//	GET  /api/v2/meetings/scan          -> status + the waiting queue
//	POST /api/v2/meetings/scan          -> {enabled?, cooldown_hours?, run_now?, chat_jid?}
func (s *Server) handleMeetingScan(w http.ResponseWriter, r *http.Request) {
	if s.meetingScan == nil {
		jsonError(w, 503, "meeting scanner not initialised")
		return
	}
	m := s.meetingScan

	switch r.Method {
	case http.MethodGet:
		queue, _ := s.store.ListMeetingScanState(50)
		jsonOK(w, map[string]any{
			"status": m.status(),
			"queue":  withChatNames(s.store, queue),
		})

	case http.MethodPost:
		var req struct {
			Enabled       *bool  `json:"enabled,omitempty"`
			CooldownHours *int   `json:"cooldown_hours,omitempty"`
			RunNow        bool   `json:"run_now,omitempty"`
			ChatJID       string `json:"chat_jid,omitempty"`
		}
		decodeJSON(r, &req)

		if req.Enabled != nil {
			val := "0"
			if *req.Enabled {
				val = "1"
			}
			s.store.PutSyncState(meetingScanEnabledKey, val)
			if !*req.Enabled {
				m.stop()
			}
		}
		if req.CooldownHours != nil && *req.CooldownHours >= 1 {
			s.store.PutSyncState(meetingScanCooldownKey, strconv.Itoa(*req.CooldownHours))
		}

		// run_now with a chat reads that chat regardless of hits or cooldown —
		// this is the manual button. Without a chat it just forces a tick.
		if req.RunNow {
			m.mu.Lock()
			busy := m.running
			m.mu.Unlock()
			if busy {
				jsonError(w, 409, "a meeting run is already going")
				return
			}
			if req.ChatJID != "" {
				if s.store.IsChatExcludedFromAI(req.ChatJID) {
					jsonError(w, 403, aiExcludedChatMessage)
					return
				}
				run := m.startRun(req.ChatJID, chatLabel(s.store, req.ChatJID), "manual")
				if run == nil {
					// The model slot was taken — most likely a refresh in
					// flight. Same wording as the busy check just above:
					// from the caller's side both mean "try again shortly".
					jsonError(w, 409, "a meeting run is already going")
					return
				}
				jsonOK(w, map[string]any{"run_id": run.ID, "status": m.status()})
				return
			}
			go m.tick()
		}
		jsonOK(w, map[string]any{"status": m.status()})

	default:
		methodNotAllowed(w)
	}
}

// withChatNames turns the queue into something readable, since a JID on its
// own tells nobody which conversation it is.
func withChatNames(store *db.Store, queue []db.MeetingScanState) []map[string]any {
	out := make([]map[string]any, 0, len(queue))
	for _, st := range queue {
		row, _ := json.Marshal(st)
		var asMap map[string]any
		json.Unmarshal(row, &asMap)
		asMap["chat_name"] = chatLabel(store, st.ChatJID)
		out = append(out, asMap)
	}
	return out
}
