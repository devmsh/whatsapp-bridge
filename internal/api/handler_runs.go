package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// handleRunsRoot routes the /api/v2/extractions/runs[/...] family.
//
//   GET    /api/v2/extractions/runs              -> active runs
//   GET    /api/v2/extractions/runs/{id}         -> full state + events
//   GET    /api/v2/extractions/runs/{id}/stream  -> SSE (live progress)
//   POST   /api/v2/extractions/runs/{id}/cancel  -> cancel
func (s *Server) handleRunsRoot(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v2/extractions/runs")
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		jsonOK(w, map[string]any{"runs": s.runs.Active()})
		return
	}
	parts := strings.SplitN(path, "/", 2)
	runID := parts[0]
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}
	run := s.runs.Get(runID)
	if run == nil {
		jsonError(w, 404, "run not found")
		return
	}
	switch sub {
	case "":
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		jsonOK(w, run)
	case "stream":
		s.handleRunStream(w, r, run)
	case "cancel":
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		run.Cancel()
		jsonOK(w, map[string]any{"cancelled": true})
	default:
		jsonError(w, 404, "unknown subpath")
	}
}

// handleRunStream is an SSE endpoint that emits the run's existing events
// followed by every new event until the run finishes (then closes).
func (s *Server) handleRunStream(w http.ResponseWriter, r *http.Request, run *Run) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		jsonError(w, 500, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	send := func(name string, payload any) {
		b, _ := json.Marshal(payload)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
		flusher.Flush()
	}

	// Snapshot existing events and current state under the same lock,
	// then start subscribing to new events.
	run.mu.Lock()
	snapshot := append([]RunEvent{}, run.Events...)
	status := run.Status
	terminal := status == RunDone || status == RunFailed || status == RunCancelled
	run.mu.Unlock()

	send("state", map[string]any{
		"id":         run.ID,
		"kind":       run.Kind,
		"subject":    run.Subject,
		"label":      run.Label,
		"status":     status,
		"started_at": run.StartedAt,
		"ended_at":   run.EndedAt,
		"session_id": run.SessionID,
		"created":    run.Created,
		"summary":    run.Summary,
		"error":      run.Error,
	})
	for _, e := range snapshot {
		send("event", e)
	}
	if terminal {
		send("end", map[string]any{"status": status})
		return
	}

	ch, unsub := run.Subscribe()
	defer unsub()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-ch:
			if !ok {
				// Channel closed (run finished). Send a final state event.
				run.mu.Lock()
				final := map[string]any{
					"status":     run.Status,
					"session_id": run.SessionID,
					"created":    run.Created,
					"summary":    run.Summary,
					"error":      run.Error,
					"ended_at":   run.EndedAt,
				}
				run.mu.Unlock()
				send("state", final)
				send("end", map[string]any{"status": final["status"]})
				return
			}
			send("event", e)
		}
	}
}
