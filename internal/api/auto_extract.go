package api

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// AutoExtractor periodically re-runs incremental circle-level extraction on
// circles that have had new messages since their last watermark. It reuses the
// same RunManager and engine as a manual extraction.
//
// Gated behind a sync_state toggle: nothing happens until the user enables it.
type AutoExtractor struct {
	s            *Server
	mu           sync.Mutex
	running      bool   // true while a sidecar is mid-run
	current      *Run   // the in-flight run, so toggling off can cancel it
	lastRunID    string // most recent run, for debugging
	lastTickedAt int64
}

const (
	autoExtractEnabledKey  = "auto_extract_enabled"
	autoExtractIntervalKey = "auto_extract_interval_hours"
	autoExtractDefaultHrs  = 8
	// Per-circle cooldown stamp: "auto_extract_last:<circle_id>" -> unix seconds.
	// This is the hard floor that makes a runaway loop impossible even if the
	// per-chat watermarks are missing or stale.
	autoExtractLastPrefix = "auto_extract_last:"
)

// lastAutoRun returns the unix time this circle was last picked by the
// scheduler, or 0 if never.
func (a *AutoExtractor) lastAutoRun(circleID int64) int64 {
	v, _, _ := a.s.store.GetSyncState(autoExtractLastPrefix + strconv.FormatInt(circleID, 10))
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// markAutoRun stamps the cooldown for a circle. Called when a run *starts*, not
// when it finishes, so a crashed or task-less run still burns the cooldown.
func (a *AutoExtractor) markAutoRun(circleID int64) {
	a.s.store.PutSyncState(autoExtractLastPrefix+strconv.FormatInt(circleID, 10),
		strconv.FormatInt(time.Now().Unix(), 10))
}

func newAutoExtractor(s *Server) *AutoExtractor { return &AutoExtractor{s: s} }

func (a *AutoExtractor) enabled() bool {
	v, _, _ := a.s.store.GetSyncState(autoExtractEnabledKey)
	return v == "1"
}

func (a *AutoExtractor) intervalHours() int {
	v, _, _ := a.s.store.GetSyncState(autoExtractIntervalKey)
	if v == "" {
		return autoExtractDefaultHrs
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return autoExtractDefaultHrs
	}
	return n
}

// Start launches the scheduler goroutine. It ticks every 10 minutes; on each
// tick, if enabled and no run is in flight, it picks one due circle with new
// messages and starts an extraction.
func (a *AutoExtractor) Start() {
	go func() {
		time.Sleep(30 * time.Second) // let sync settle on boot
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		a.tick()
		for range t.C {
			a.tick()
		}
	}()
}

func (a *AutoExtractor) tick() {
	// Label brand-new introductions on the same cadence. It is a couple of
	// cheap queries and it is independent of task extraction — it must keep
	// working even when auto-extract is switched off, which is why it runs
	// before the enabled() check below.
	if names, err := a.s.store.AutoClassifyIntros(); err == nil && len(names) > 0 {
		fmt.Printf("Intro label applied to %d new chat(s): %v\n", len(names), names)
	}

	a.mu.Lock()
	a.lastTickedAt = time.Now().Unix()
	if a.running || !a.enabled() {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()

	circleID, name, ok := a.pickDueCircle()
	if !ok {
		return
	}
	// Re-check: picking scans every circle and can take a moment, during which
	// the user may have switched the toggle off.
	if !a.enabled() {
		return
	}

	// Burn the cooldown before starting. If the run dies, crashes, or marks no
	// watermarks at all, this circle still waits a full interval before it can
	// be picked again.
	a.markAutoRun(circleID)

	// Mirror the manual handler: start an async run via RunManager.
	run, ctx := a.s.runs.Start("circle", strconv.FormatInt(circleID, 10), name+" (auto)")

	a.mu.Lock()
	a.running = true
	a.current = run
	a.lastRunID = run.ID
	a.mu.Unlock()

	fmt.Printf("auto-extract: circle %d (%s) run=%s\n", circleID, name, run.ID)

	go func() {
		defer func() {
			a.mu.Lock()
			a.running = false
			a.current = nil
			a.mu.Unlock()
		}()
		a.s.runCircleExtraction(ctx, run, circleID)
	}()
}

// stop cancels the in-flight auto run, if any. Called when the toggle is
// switched off so "off" means "stop now", not "stop within 30 minutes".
func (a *AutoExtractor) stop() {
	a.mu.Lock()
	run := a.current
	a.mu.Unlock()
	if run != nil {
		fmt.Printf("auto-extract: disabled, cancelling run=%s\n", run.ID)
		run.Cancel()
	}
}

// pickDueCircle returns the circle that should be extracted next, or ok=false
// if nothing is due.
//
// A circle is due when BOTH hold:
//   - it has at least one chat with messages newer than that chat's watermark, and
//   - it has not been extracted within the last interval.
//
// "Last extracted" is the newest of (a) the per-circle cooldown stamp and
// (b) max(chat_extraction_state.updated_at) over the circle's chats. Both are
// *run* times. Earlier versions compared against min(last_msg_ts) — a *message*
// timestamp — so a single dormant chat with a months-old last message made the
// circle look permanently overdue and it re-ran on every 10-minute tick.
//
// Among due circles, the one with the most recent activity wins. Ranking by
// "oldest watermark" let one dead chat pin a circle to the top forever and
// starve every other circle.
func (a *AutoExtractor) pickDueCircle() (int64, string, bool) {
	circles, err := a.s.store.ListCircles()
	if err != nil {
		return 0, "", false
	}
	intervalSec := int64(a.intervalHours()) * 3600
	nowU := time.Now().Unix()

	var best struct {
		id     int64
		name   string
		newest int64 // newest message in the circle (more recent = higher priority)
		found  bool
	}
	for _, c := range circles {
		jids, _ := a.s.store.FlattenCircleChats(c.ID)
		if len(jids) == 0 {
			continue
		}

		// lastRun = when this circle was last actually extracted.
		lastRun := a.lastAutoRun(c.ID)
		var newestMsg int64
		hasNew := false
		for _, jid := range jids {
			var maxTS, wmTS, updatedAt int64
			a.s.store.DB.QueryRow(`SELECT COALESCE(MAX(timestamp),0) FROM messages WHERE chat_jid = ?`, jid).Scan(&maxTS)
			a.s.store.DB.QueryRow(`SELECT COALESCE(last_msg_ts,0), COALESCE(updated_at,0) FROM chat_extraction_state WHERE chat_jid = ?`, jid).Scan(&wmTS, &updatedAt)
			if maxTS > wmTS {
				hasNew = true
			}
			if maxTS > newestMsg {
				newestMsg = maxTS
			}
			if updatedAt > lastRun {
				lastRun = updatedAt
			}
		}
		if !hasNew {
			continue
		}
		// Cooldown: never re-run a circle inside one interval.
		if lastRun > 0 && nowU-lastRun < intervalSec {
			continue
		}
		if !best.found || newestMsg > best.newest {
			best.id, best.name, best.newest, best.found = c.ID, c.Name, newestMsg, true
		}
	}
	if !best.found {
		return 0, "", false
	}
	return best.id, best.name, true
}

// status returns the auto-extractor's current state for the UI.
type autoStatus struct {
	Enabled       bool   `json:"enabled"`
	IntervalHours int    `json:"interval_hours"`
	Running       bool   `json:"running"`
	LastRunID     string `json:"last_run_id,omitempty"`
	LastTickedAt  int64  `json:"last_ticked_at,omitempty"`
}

// handleAutoExtract returns/updates the auto-extractor status + toggle.
//
//	GET  /api/v2/extractions/auto         -> current status
//	POST /api/v2/extractions/auto         -> {enabled?, interval_hours?}
func (s *Server) handleAutoExtract(w http.ResponseWriter, r *http.Request) {
	if s.autoExtract == nil {
		jsonError(w, 503, "auto-extract not initialised")
		return
	}
	a := s.autoExtract
	switch r.Method {
	case http.MethodGet:
		a.mu.Lock()
		st := autoStatus{
			Enabled:       a.enabled(),
			IntervalHours: a.intervalHours(),
			Running:       a.running,
			LastRunID:     a.lastRunID,
			LastTickedAt:  a.lastTickedAt,
		}
		a.mu.Unlock()
		jsonOK(w, st)
	case http.MethodPost:
		var req struct {
			Enabled       *bool `json:"enabled,omitempty"`
			IntervalHours *int  `json:"interval_hours,omitempty"`
		}
		decodeJSON(r, &req)
		if req.Enabled != nil {
			val := "0"
			if *req.Enabled {
				val = "1"
			}
			s.store.PutSyncState(autoExtractEnabledKey, val)
			if *req.Enabled {
				go a.tick() // give the user immediate feedback
			} else {
				a.stop() // kill any in-flight sidecar immediately
			}
		}
		if req.IntervalHours != nil && *req.IntervalHours >= 1 {
			s.store.PutSyncState(autoExtractIntervalKey, strconv.Itoa(*req.IntervalHours))
		}
		a.mu.Lock()
		st := autoStatus{
			Enabled:       a.enabled(),
			IntervalHours: a.intervalHours(),
			Running:       a.running,
			LastRunID:     a.lastRunID,
			LastTickedAt:  a.lastTickedAt,
		}
		a.mu.Unlock()
		jsonOK(w, st)
	default:
		methodNotAllowed(w)
	}
}
