package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"whatsapp-bridge-v2/internal/db"
	"whatsapp-bridge-v2/internal/extract"
	"whatsapp-bridge-v2/internal/extract/adapters"
)

// Choosing and driving the extraction engine.
//
// The engine is a setting rather than a build-time choice, so switching from
// the local model back to Claude is a toggle and not a deploy — which is what
// makes the switch reversible if a model upgrade goes wrong.

const (
	settingEngine = "extract_engine"
	settingModel  = "extract_model"

	engineOllama = "ollama"
	engineClaude = "claude"

	// The default is local: no quota, no network, and every answer is checked
	// afterwards anyway.
	defaultEngine = engineOllama
	// Chosen by measurement, not by size. Against the hand-labelled set this
	// model scores 0.65 precision and 0.54 recall where qwen2.5:14b scores
	// 0.54 and 0.51 — and it is twice as fast, because only 3B of its 30B
	// parameters are active per token. See cmd/extract-eval.
	defaultModel = "qwen3:30b-a3b-instruct-2507-q4_K_M"
)

// extractorFor builds the engine named in settings, unless the caller asked
// for something else.
func (s *Server) extractorFor(engine, model string) (extract.Extractor, error) {
	if engine == "" {
		engine, _, _ = s.store.GetSyncState(settingEngine)
	}
	if engine == "" {
		engine = defaultEngine
	}
	if model == "" {
		model, _, _ = s.store.GetSyncState(settingModel)
	}
	if model == "" {
		model = defaultModel
	}

	switch engine {
	case engineOllama:
		return adapters.NewOllama(envOr("OLLAMA_URL", ""), model), nil
	case engineClaude:
		// The sidecar is spawned by the API layer, which knows where the
		// scripts and binaries live; the adapter only needs a way to call it.
		runner := func(ctx context.Context, stdin, script string, args ...string) (string, error) {
			return s.runAgentTracked(ctx, 5*time.Minute, stdin, nil, script, args...)
		}
		return adapters.NewClaude(runner, model), nil
	default:
		return nil, fmt.Errorf("unknown extraction engine %q", engine)
	}
}

// extractLocation is the timezone dates are resolved in. Every "الخميس" in
// these chats means a Thursday here, not in UTC.
func extractLocation() *time.Location {
	if loc, err := time.LoadLocation("Asia/Riyadh"); err == nil {
		return loc
	}
	return time.UTC
}

// runExtraction drives the engine over one chat and reports through the run,
// so the UI sees the same kind of progress the old sidecar produced.
func (s *Server) runExtraction(ctx context.Context, run *Run, chatJID string, since int64) {
	run.SetRunning()

	ex, err := s.extractorFor("", "")
	if err != nil {
		run.Finish(RunFailed, "", "", 0, err.Error())
		return
	}

	res, err := extract.Run(ctx,
		extract.Deps{Store: s.store, Extractor: ex, Loc: extractLocation()},
		extract.RunSpec{ChatJID: chatJID, Since: since, RunID: run.ID},
		func(msg string) { run.AddEvent(RunEvent{Kind: "info", Text: msg}) })
	if err != nil {
		run.Finish(RunFailed, "", "", 0, err.Error())
		return
	}

	run.Finish(RunDone, "", summarise(ex.Name(), res), res.Verified, "")
}

// runCircleExtraction walks every live chat in a circle, sharing one engine.
func (s *Server) runCircleExtraction(ctx context.Context, run *Run, circleID int64) {
	run.SetRunning()

	ex, err := s.extractorFor("", "")
	if err != nil {
		run.Finish(RunFailed, "", "", 0, err.Error())
		return
	}

	// FlattenCircleChats already drops hidden, archived and deleted chats.
	jids, err := s.store.FlattenCircleChats(circleID)
	if err != nil {
		run.Finish(RunFailed, "", "", 0, err.Error())
		return
	}

	deps := extract.Deps{Store: s.store, Extractor: ex, Loc: extractLocation()}
	total := extract.Result{Rejections: map[extract.RejectReason]int{}}
	for _, jid := range jids {
		if ctx.Err() != nil {
			run.Finish(RunCancelled, "", summarise(ex.Name(), total), total.Verified, "")
			return
		}
		name := chatLabel(s.store, jid)
		run.AddEvent(RunEvent{Kind: "info", Text: "reading " + name})

		res, err := extract.Run(ctx, deps,
			extract.RunSpec{ChatJID: jid, RunID: run.ID},
			func(msg string) { run.AddEvent(RunEvent{Kind: "info", Text: name + ": " + msg}) })
		if err != nil {
			// One unreadable chat should not end a circle run.
			run.AddEvent(RunEvent{Kind: "info", Text: name + " skipped: " + err.Error()})
			continue
		}
		total.Chunks += res.Chunks
		total.Skipped += res.Skipped
		total.Proposed += res.Proposed
		total.Verified += res.Verified
		total.Rejected += res.Rejected
		total.Completions += res.Completions
		for reason, n := range res.Rejections {
			total.Rejections[reason] += n
		}
	}
	run.Finish(RunDone, "", summarise(ex.Name(), total), total.Verified, "")
}

func summarise(engine string, r extract.Result) string {
	// Proposed = kept + dropped + deduped. Leaving the last one out made the
	// line look like it had lost count.
	deduped := r.Rejections["duplicate"] + r.Rejections["same_work"]
	out := fmt.Sprintf("%s: %d proposed, %d kept, %d dropped across %d chunk(s)",
		engine, r.Proposed, r.Verified, r.Rejected, r.Chunks)
	if deduped > 0 {
		out += fmt.Sprintf("; %d already said elsewhere", deduped)
	}
	if r.Completions > 0 {
		out += fmt.Sprintf("; %d marked done", r.Completions)
	}
	if r.Skipped > 0 {
		out += fmt.Sprintf("; %d chunk(s) had nothing in them", r.Skipped)
	}
	return out
}

func chatLabel(store *db.Store, jid string) string {
	var name string
	store.DB.QueryRow(`
		SELECT COALESCE(NULLIF(g.name,''), NULLIF(ct.name,''), NULLIF(ct.push_name,''), ch.jid)
		FROM chats ch
		LEFT JOIN groups g ON g.jid = ch.jid
		LEFT JOIN contacts ct ON (ct.jid = ch.jid OR ct.lid = ch.jid)
		WHERE ch.jid = ?`, jid).Scan(&name)
	if name == "" {
		return jid
	}
	return name
}

// handleExtractionStats reports what the engine has been doing: how long calls
// take, how much it proposes, and how much of that survives checking.
//
// GET /api/v2/extractions/stats?days=7
func (s *Server) handleExtractionStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		}
	}
	since := time.Now().AddDate(0, 0, -days).Unix()

	type engineStat struct {
		Engine    string  `json:"engine"`
		Calls     int     `json:"calls"`
		Proposed  int     `json:"proposed"`
		Verified  int     `json:"verified"`
		Rejected  int     `json:"rejected"`
		MedianMS  int     `json:"median_ms"`
		Precision float64 `json:"precision"`
	}

	rows, err := s.store.DB.Query(`
		SELECT engine, COUNT(*), COALESCE(SUM(proposed),0), COALESCE(SUM(verified),0),
		       COALESCE(SUM(rejected),0), COALESCE(AVG(latency_ms),0)
		FROM extraction_calls WHERE created_at >= ? GROUP BY engine`, since)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	engines := []engineStat{}
	for rows.Next() {
		var e engineStat
		var avg float64
		if rows.Scan(&e.Engine, &e.Calls, &e.Proposed, &e.Verified, &e.Rejected, &avg) != nil {
			continue
		}
		e.MedianMS = int(avg)
		if e.Proposed > 0 {
			e.Precision = float64(e.Verified) / float64(e.Proposed)
		}
		engines = append(engines, e)
	}

	// Why proposals were dropped. This is the number to watch: bad_quote
	// climbing means a model has started inventing.
	reasons := map[string]int{}
	if rr, err := s.store.DB.Query(`SELECT reason, COUNT(*) FROM extraction_rejections
		WHERE created_at >= ? GROUP BY reason`, since); err == nil {
		for rr.Next() {
			var reason string
			var n int
			if rr.Scan(&reason, &n) == nil {
				reasons[reason] = n
			}
		}
		rr.Close()
	}

	engine, _, _ := s.store.GetSyncState(settingEngine)
	if engine == "" {
		engine = defaultEngine
	}
	model, _, _ := s.store.GetSyncState(settingModel)
	if model == "" {
		model = defaultModel
	}

	jsonOK(w, map[string]any{
		"days": days, "engines": engines, "rejections": reasons,
		"current": map[string]string{"engine": engine, "model": model},
	})
}

// handleExtractionModels lists what the local engine can run, so the settings
// screen offers real choices instead of a text box.
// GET /api/v2/extractions/models
func (s *Server) handleExtractionModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	names, err := adapters.OllamaModels(envOr("OLLAMA_URL", ""))
	if err != nil {
		// Not an error worth failing on: Ollama may simply not be running,
		// and the screen should say so rather than break.
		jsonOK(w, map[string]any{"models": []string{}, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"models": names})
}
