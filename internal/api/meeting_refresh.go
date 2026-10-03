package api

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"whatsapp-bridge-v2/internal/db"
	"whatsapp-bridge-v2/internal/extract"
)

// meetingModelSlot makes sure only one meeting job — the finder or the
// refresher — asks the local model at a time.
//
// The finder and the refresher used to check each other's own "running" flag,
// each under its own lock: the finder peeked at the refresher's flag, and the
// refresher peeked at the finder's. That is two different lock orders reached
// from two different places, which is exactly how a deadlock gets introduced
// later without anyone noticing. One slot with one lock removes the question
// entirely — neither struct needs to know anything about the other, only
// whether the slot is free.
type meetingModelSlot struct {
	mu    sync.Mutex
	owner string // "" when free, else "finder" or "refresh"
}

// claim takes the slot for owner in one lock. Returns false, changing
// nothing, when somebody already holds it.
func (l *meetingModelSlot) claim(owner string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.owner != "" {
		return false
	}
	l.owner = owner
	return true
}

// release frees the slot, but only when owner is the one holding it. A
// stray release from a job that never actually got the slot (or already
// lost it) must not kick out whoever holds it now.
func (l *meetingModelSlot) release(owner string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.owner == owner {
		l.owner = ""
	}
}

// heldBy names the current owner, or "" when the slot is free.
func (l *meetingModelSlot) heldBy() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.owner
}

// maxRefreshPasses bounds how many 600-line passes one chat gets in one run.
// Fifty passes is thirty thousand lines, more than any chat here holds in the
// window a meeting is followed for.
const maxRefreshPasses = 50

// recentMeetingWindow is how far back the first, fast round of a refresh looks.
const recentMeetingWindow = 14 * 24 * 3600

// MeetingRefresher is what keeps a found meeting up to date.
//
// meeting_scan.go finds a meeting the first time it is mentioned. After that,
// nothing read the chat again — so a meeting that moved, gained an agenda
// point, or got cancelled just sat there wrong (see
// docs/superpowers/specs/2026-09-18-live-meetings-design.md). This type is
// the second read: for each chat with an open meeting due a check, it asks
// the model "what changed?", applies whatever survives verification, and
// then lets code close meetings that quietly died on their own.
//
// It shares the local engine with the finder, coordinated through
// meetingModelSlot on the Server, so the two never run at the same time.
type MeetingRefresher struct {
	s  *Server
	mu sync.Mutex

	running   bool
	current   *Run
	lastRunID string
	last      meetingRefreshSummary

	// updaterFn overrides how the "what changed?" engine is built. Left nil
	// in production, where doRun falls back to Server.meetingUpdater(); a
	// test sets it to hand doRun a fake extract.MeetingUpdater so a refresh
	// can run end to end without a real model.
	updaterFn func() (extract.MeetingUpdater, error)
}

// meetingRefreshSummary is what the last finished run did, shown on the
// status endpoint so the UI does not have to wait for a new run to know
// whether refreshing is working at all. finished_at has no omitempty: the
// API contract fixes this shape, and an absent key would read as
// "undefined" on the web side instead of "never run".
type meetingRefreshSummary struct {
	Chats      int   `json:"chats"`
	Calls      int   `json:"calls"`
	Changes    int   `json:"changes"`
	Lapsed     int   `json:"lapsed"`
	FinishedAt int64 `json:"finished_at"`
}

func newMeetingRefresher(s *Server) *MeetingRefresher { return &MeetingRefresher{s: s} }

// busy reports whether a refresh is going right now.
func (m *MeetingRefresher) busy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// status is what the GET endpoint shows.
func (m *MeetingRefresher) status() (running bool, runID string, last meetingRefreshSummary) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running, m.lastRunID, m.last
}

// run starts one refresh pass over chats, in order. It refuses (ok=false)
// when the model slot is already taken — by another refresh, or by the
// finder — since only one model call happens at a time on the local engine.
// The caller (the endpoint, or the scanner's tick) decides what to tell the
// person.
func (m *MeetingRefresher) run(trigger string, chats []string) (*Run, bool) {
	if !m.s.meetingModel.claim("refresh") {
		return nil, false
	}

	subject := "all"
	label := fmt.Sprintf("%d chat(s)", len(chats))
	if len(chats) == 1 {
		subject = chats[0]
		label = chatLabel(m.s.store, chats[0])
	}
	run, ctx := m.s.runs.StartTagged("meeting-refresh", subject, label, trigger)

	m.mu.Lock()
	m.running = true
	m.current = run
	m.lastRunID = run.ID
	m.mu.Unlock()

	fmt.Printf("meeting-refresh: %s run=%s\n", label, run.ID)

	go m.doRun(ctx, run, chats)
	return run, true
}

// updater returns the engine doRun asks "what changed?" — the override if a
// test set one, otherwise the real engine picked by the extract_engine
// setting.
func (m *MeetingRefresher) updater() (extract.MeetingUpdater, error) {
	if m.updaterFn != nil {
		return m.updaterFn()
	}
	return m.s.meetingUpdater()
}

// doRun is the refresh itself: one chat at a time, oldest-first, then the
// rule pass that lets dead meetings close without ever asking the model.
func (m *MeetingRefresher) doRun(ctx context.Context, run *Run, chats []string) {
	defer func() {
		// A panic here must still free the slot and close the run — leaving
		// either stuck would lock out every future meeting job for good.
		if p := recover(); p != nil {
			run.Finish(RunFailed, "", "", 0, fmt.Sprintf("panic: %v", p))
		}
		m.mu.Lock()
		m.running = false
		m.current = nil
		m.mu.Unlock()
		m.s.meetingModel.release("refresh")
	}()
	run.SetRunning()

	updater, err := m.updater()
	if err != nil {
		run.Finish(RunFailed, "", "", 0, err.Error())
		return
	}
	deps := extract.UpdateDeps{Store: m.s.store, Updater: updater, Loc: extractLocation()}

	var calls, changes int
	// Two rounds over the same chats. The first reads only the meetings whose
	// unread messages start in the last two weeks: those are the ones a person
	// is looking at, and a chat is read oldest first, so without this a
	// meeting from this week waits behind months of old ones. The second round
	// reads everything that is left.
	for _, recentSince := range []int64{time.Now().Unix() - recentMeetingWindow, 0} {
		if ctx.Err() != nil {
			break
		}
		deps.RecentSince = recentSince
		for _, chatJID := range chats {
			if ctx.Err() != nil {
				run.AddEvent(RunEvent{Kind: "info", Text: "cancelled"})
				break
			}
			// One pass reads at most 600 lines, oldest first. A chat with months
			// of open meetings behind it (one DM here holds 64, going back to
			// June) needs many passes to reach today, and a meeting from this week
			// would otherwise wait for dozens of ticks. So a chat is read until
			// nothing in it is due. The cap stops a chat whose watermark cannot
			// move from holding the run forever.
			var chatCalls, chatChanges, passes int
			for passes = 0; passes < maxRefreshPasses && ctx.Err() == nil; passes++ {
				res, err := extract.RefreshChatMeetings(ctx, deps, chatJID, run.ID,
					func(msg string) { run.AddEvent(RunEvent{Kind: "info", Text: msg}) })
				if err != nil {
					run.AddEvent(RunEvent{Kind: "info", Text: fmt.Sprintf("%s: %v", chatLabel(m.s.store, chatJID), err)})
					break
				}
				chatCalls += res.Calls
				chatChanges += res.Changes
				if res.Meetings == 0 || res.FailedChunks > 0 {
					break
				}
			}
			calls += chatCalls
			changes += chatChanges
			if chatCalls > 0 {
				run.AddEvent(RunEvent{Kind: "info", Text: fmt.Sprintf("%s: %d call(s), %d change(s)",
					chatLabel(m.s.store, chatJID), chatCalls, chatChanges)})
			}
		}
	}

	if ctx.Err() == context.Canceled {
		run.Finish(RunCancelled, "", "Cancelled.", changes, "")
		return
	}

	// The rule pass costs no model call, so it runs every time, even when
	// every chat above failed or found nothing to change.
	closed, err := m.s.store.CloseStaleMeetings(time.Now().Unix())
	if err != nil {
		run.AddEvent(RunEvent{Kind: "info", Text: fmt.Sprintf("closing stale meetings: %v", err)})
	}
	lapsed := 0
	for _, c := range closed {
		if c.Field == "status" && (c.NewValue == db.MeetingLapsed || c.NewValue == db.MeetingHeld) {
			lapsed++
		}
	}

	summary := fmt.Sprintf("%s: %d chat(s), %d call(s), %d change(s); %d closed by rule",
		updater.Name(), len(chats), calls, changes, lapsed)

	m.mu.Lock()
	m.last = meetingRefreshSummary{
		Chats: len(chats), Calls: calls, Changes: changes, Lapsed: lapsed,
		FinishedAt: time.Now().Unix(),
	}
	m.mu.Unlock()

	run.Finish(RunDone, "", summary, changes, "")
}

// meetingUpdater builds the engine that answers "what changed?", honouring
// the same engine setting the finder and the task extractor use.
func (s *Server) meetingUpdater() (extract.MeetingUpdater, error) {
	ex, err := s.extractorFor("", "")
	if err != nil {
		return nil, err
	}
	updater, ok := ex.(extract.MeetingUpdater)
	if !ok {
		return nil, fmt.Errorf("the %s engine cannot update meetings", ex.Name())
	}
	return updater, nil
}

// aiExcludedChatMessage is the one message every meeting-refresh path uses
// when a hidden, archived or deleted chat is asked for — one string, so a
// person sees the same reason everywhere they hit it.
const aiExcludedChatMessage = "AI features are disabled for hidden and archived chats"

// handleMeetingRefresh is the refresher's own endpoint.
//
//	GET  /api/v2/meetings/refresh   -> status + what the last run did
//	POST /api/v2/meetings/refresh   {"all": true} | {"chat_jid": "..."} | {"meeting_id": N}
//	                                 -> starts one background run
func (s *Server) handleMeetingRefresh(w http.ResponseWriter, r *http.Request) {
	if s.meetingRefresh == nil {
		jsonError(w, 503, "meeting refresher not initialised")
		return
	}
	m := s.meetingRefresh

	switch r.Method {
	case http.MethodGet:
		running, runID, last := m.status()
		waiting, err := s.store.ChatsWithMeetingFollowUps(time.Now().Unix(), 500)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, map[string]any{
			"running":       running,
			"run_id":        runID,
			"waiting_chats": len(waiting),
			"last":          last,
		})

	case http.MethodPost:
		var req struct {
			All       bool   `json:"all"`
			ChatJID   string `json:"chat_jid"`
			MeetingID int64  `json:"meeting_id"`
		}
		if err := decodeJSON(r, &req); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}

		var chats []string
		switch {
		case req.MeetingID != 0:
			mt, err := s.store.GetMeeting(req.MeetingID)
			if err != nil || mt == nil {
				jsonError(w, 404, "meeting not found")
				return
			}
			// A hidden or archived chat must never be read for this either —
			// same rule the chat_jid and all branches already follow below.
			seen := map[string]bool{}
			for _, msg := range mt.Messages {
				if msg.ChatJID == "" || seen[msg.ChatJID] || s.store.IsChatExcludedFromAI(msg.ChatJID) {
					continue
				}
				seen[msg.ChatJID] = true
				chats = append(chats, msg.ChatJID)
			}
			if len(chats) == 0 {
				jsonError(w, 403, aiExcludedChatMessage)
				return
			}
		case req.ChatJID != "":
			if s.store.IsChatExcludedFromAI(req.ChatJID) {
				jsonError(w, 403, aiExcludedChatMessage)
				return
			}
			chats = []string{req.ChatJID}
		case req.All:
			var err error
			chats, err = s.store.ChatsWithMeetingFollowUps(time.Now().Unix(), 500)
			if err != nil {
				jsonError(w, 500, err.Error())
				return
			}
		default:
			jsonError(w, 400, "all, chat_jid or meeting_id required")
			return
		}

		run, ok := m.run("manual", chats)
		if !ok {
			jsonError(w, 409, "a meeting check is already running")
			return
		}
		jsonOK(w, map[string]any{"run_id": run.ID, "chats": len(chats)})

	default:
		methodNotAllowed(w)
	}
}
