package db

import (
	"encoding/json"
	"strings"
	"time"

	"whatsapp-bridge-v2/internal/llmlog"
)

// Reading and writing the three debugging tables.
//
// Everything here is write-and-forget on the hot path: a failed insert is
// dropped, never returned. A model call must not fail because the debug log
// could not be written.

// LLMCall is one recorded model call as it comes back out of the database.
type LLMCall struct {
	ID           int64  `json:"id"`
	RunID        string `json:"run_id,omitempty"`
	Service      string `json:"service"`
	Engine       string `json:"engine"`
	Model        string `json:"model"`
	Kind         string `json:"kind"`
	ChatJID      string `json:"chat_jid,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	Prompt       string `json:"prompt,omitempty"`
	Response     string `json:"response,omitempty"`
	PromptChars  int    `json:"prompt_chars"`
	ReplyChars   int    `json:"reply_chars"`
	LatencyMS    int64  `json:"latency_ms"`
	Attempt      int    `json:"attempt"`
	OK           bool   `json:"ok"`
	Error        string `json:"error,omitempty"`
	CreatedAt    int64  `json:"created_at"`
}

// RecordLLMCall implements llmlog.Sink. The API layer wires the store in at
// startup; before that, adapters record into nothing.
func (s *Store) RecordLLMCall(c llmlog.Call) {
	okFlag := 1
	errText := ""
	if c.Err != nil {
		okFlag = 0
		errText = c.Err.Error()
	}
	s.DB.Exec(`INSERT INTO llm_calls
		(run_id, service, engine, model, kind, chat_jid, system_prompt, prompt, response,
		 prompt_chars, reply_chars, latency_ms, attempt, ok, error, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.RunID, c.Service, c.Engine, c.Model, c.Kind, c.ChatJID,
		c.System, c.Prompt, c.Response,
		len(c.Prompt)+len(c.System), len(c.Response),
		c.Latency.Milliseconds(), c.Attempt, okFlag, errText, time.Now().Unix())
}

// LLMCallFilter narrows the list. Every field is optional.
type LLMCallFilter struct {
	RunID   string
	Service string
	Engine  string
	ChatJID string
	// OnlyFailed keeps just the calls that errored — the usual first question.
	OnlyFailed bool
	Since      int64
	Limit      int
}

// ListLLMCalls returns recent calls, newest first, WITHOUT the prompt and
// answer text. A list of two hundred calls each carrying 64KB of prompt would
// be 12MB of JSON for a table nobody has expanded yet; the text arrives when
// one row is opened.
func (s *Store) ListLLMCalls(f LLMCallFilter) ([]LLMCall, error) {
	q := `SELECT id, run_id, service, engine, model, kind, chat_jid,
	             prompt_chars, reply_chars, latency_ms, attempt, ok, error, created_at
	      FROM llm_calls WHERE 1=1`
	var args []any
	if f.RunID != "" {
		q += ` AND run_id = ?`
		args = append(args, f.RunID)
	}
	if f.Service != "" {
		q += ` AND service = ?`
		args = append(args, f.Service)
	}
	if f.Engine != "" {
		q += ` AND engine = ?`
		args = append(args, f.Engine)
	}
	if f.ChatJID != "" {
		q += ` AND chat_jid = ?`
		args = append(args, f.ChatJID)
	}
	if f.OnlyFailed {
		q += ` AND ok = 0`
	}
	if f.Since > 0 {
		q += ` AND created_at >= ?`
		args = append(args, f.Since)
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []LLMCall{}
	for rows.Next() {
		var c LLMCall
		var ok int
		if rows.Scan(&c.ID, &c.RunID, &c.Service, &c.Engine, &c.Model, &c.Kind, &c.ChatJID,
			&c.PromptChars, &c.ReplyChars, &c.LatencyMS, &c.Attempt, &ok, &c.Error, &c.CreatedAt) != nil {
			continue
		}
		c.OK = ok == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetLLMCall returns one call with its full prompt and answer.
func (s *Store) GetLLMCall(id int64) (*LLMCall, error) {
	var c LLMCall
	var ok int
	err := s.DB.QueryRow(`SELECT id, run_id, service, engine, model, kind, chat_jid,
		system_prompt, prompt, response, prompt_chars, reply_chars, latency_ms,
		attempt, ok, error, created_at FROM llm_calls WHERE id = ?`, id).
		Scan(&c.ID, &c.RunID, &c.Service, &c.Engine, &c.Model, &c.Kind, &c.ChatJID,
			&c.SystemPrompt, &c.Prompt, &c.Response, &c.PromptChars, &c.ReplyChars,
			&c.LatencyMS, &c.Attempt, &ok, &c.Error, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	c.OK = ok == 1
	return &c, nil
}

// LLMRetention is how long a recorded call is kept.
const LLMRetention = 14 * 24 * time.Hour

// PruneLLMCalls deletes calls older than LLMRetention and returns how many
// went. Run daily.
func (s *Store) PruneLLMCalls() int64 {
	cutoff := time.Now().Add(-LLMRetention).Unix()
	res, err := s.DB.Exec(`DELETE FROM llm_calls WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return n
}

// LLMUsage is the size of the log, for the debug screen.
type LLMUsage struct {
	Rows     int64 `json:"rows"`
	Bytes    int64 `json:"bytes"`
	OldestAt int64 `json:"oldest_at,omitempty"`
	Failed   int64 `json:"failed"`
}

// LLMLogUsage measures the table so the screen can say what the log costs.
func (s *Store) LLMLogUsage() LLMUsage {
	var u LLMUsage
	s.DB.QueryRow(`SELECT COUNT(*), COALESCE(SUM(LENGTH(prompt)+LENGTH(response)+LENGTH(system_prompt)),0),
		COALESCE(MIN(created_at),0), COALESCE(SUM(CASE WHEN ok = 0 THEN 1 ELSE 0 END),0)
		FROM llm_calls`).Scan(&u.Rows, &u.Bytes, &u.OldestAt, &u.Failed)
	return u
}

// ServiceRun is one run of a background service, as stored.
type ServiceRun struct {
	ID        string          `json:"id"`
	Service   string          `json:"service"`
	Kind      string          `json:"kind"`
	Subject   string          `json:"subject,omitempty"`
	Label     string          `json:"label"`
	Trigger   string          `json:"trigger"`
	Status    string          `json:"status"`
	StartedAt int64           `json:"started_at"`
	EndedAt   int64           `json:"ended_at,omitempty"`
	Created   int             `json:"created"`
	Summary   string          `json:"summary,omitempty"`
	Error     string          `json:"error,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Events    json.RawMessage `json:"events,omitempty"`
}

// SaveServiceRun writes or updates one run. Called when a run starts and again
// when it finishes, so a run that never finishes still leaves a row saying so.
func (s *Store) SaveServiceRun(r ServiceRun) {
	events := ""
	if len(r.Events) > 0 {
		events = string(r.Events)
	}
	s.DB.Exec(`INSERT INTO service_runs
		(id, service, kind, subject, label, trigger, status, started_at, ended_at,
		 created, summary, error, session_id, events)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			ended_at = excluded.ended_at,
			created = excluded.created,
			summary = excluded.summary,
			error = excluded.error,
			session_id = excluded.session_id,
			events = CASE WHEN excluded.events != '' THEN excluded.events ELSE service_runs.events END`,
		r.ID, r.Service, r.Kind, r.Subject, r.Label, r.Trigger, r.Status,
		r.StartedAt, r.EndedAt, r.Created, r.Summary, r.Error, r.SessionID, events)
}

// ListServiceRuns returns run history, newest first, without the event log.
func (s *Store) ListServiceRuns(service, status string, limit int) ([]ServiceRun, error) {
	q := `SELECT id, service, kind, subject, label, trigger, status, started_at,
	             ended_at, created, summary, error, session_id
	      FROM service_runs WHERE 1=1`
	var args []any
	if service != "" {
		q += ` AND service = ?`
		args = append(args, service)
	}
	if status != "" {
		q += ` AND status = ?`
		args = append(args, status)
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q += ` ORDER BY started_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ServiceRun{}
	for rows.Next() {
		var r ServiceRun
		if rows.Scan(&r.ID, &r.Service, &r.Kind, &r.Subject, &r.Label, &r.Trigger,
			&r.Status, &r.StartedAt, &r.EndedAt, &r.Created, &r.Summary,
			&r.Error, &r.SessionID) != nil {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetServiceRun returns one run with its events.
func (s *Store) GetServiceRun(id string) (*ServiceRun, error) {
	var r ServiceRun
	var events string
	err := s.DB.QueryRow(`SELECT id, service, kind, subject, label, trigger, status,
		started_at, ended_at, created, summary, error, session_id, events
		FROM service_runs WHERE id = ?`, id).
		Scan(&r.ID, &r.Service, &r.Kind, &r.Subject, &r.Label, &r.Trigger, &r.Status,
			&r.StartedAt, &r.EndedAt, &r.Created, &r.Summary, &r.Error, &r.SessionID, &events)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(strings.TrimSpace(events), "[") {
		r.Events = json.RawMessage(events)
	}
	return &r, nil
}

// RunRetention is how long run history is kept. Longer than the LLM log
// because a row is small and "when did this last succeed" is a question people
// ask about last month.
const RunRetention = 60 * 24 * time.Hour

// PruneServiceRuns drops old run rows and returns how many went.
func (s *Store) PruneServiceRuns() int64 {
	cutoff := time.Now().Add(-RunRetention).Unix()
	res, err := s.DB.Exec(`DELETE FROM service_runs WHERE started_at < ?`, cutoff)
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return n
}

// MarkStaleRunsInterrupted closes out runs left "running" by a crash or a
// restart. Called once at startup: the process that owned them is gone, so
// they can never finish, and a row stuck at "running" forever is a lie.
func (s *Store) MarkStaleRunsInterrupted() int64 {
	res, err := s.DB.Exec(`UPDATE service_runs
		SET status = 'interrupted', ended_at = ?, error = 'the bridge restarted while this was running'
		WHERE status IN ('starting','running')`, time.Now().Unix())
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return n
}

// MeetingScanState is the meeting watermark for one chat.
type MeetingScanState struct {
	ChatJID   string `json:"chat_jid"`
	LastMsgTS int64  `json:"last_msg_ts"`
	LastRunAt int64  `json:"last_run_at"`
	LastRunID string `json:"last_run_id,omitempty"`
	Hits      int    `json:"hits"`
	LastHitTS int64  `json:"last_hit_ts"`
	Found     int    `json:"found"`
	UpdatedAt int64  `json:"updated_at"`
}

// GetMeetingScanState returns the row for a chat, zero-valued when there is
// none. A chat that has never been scanned reads as "watermark 0", which means
// the first scan reads its whole history — correct, and only happens once.
func (s *Store) GetMeetingScanState(chatJID string) MeetingScanState {
	st := MeetingScanState{ChatJID: chatJID}
	s.DB.QueryRow(`SELECT last_msg_ts, last_run_at, last_run_id, hits, last_hit_ts, found, updated_at
		FROM meeting_scan_state WHERE chat_jid = ?`, chatJID).
		Scan(&st.LastMsgTS, &st.LastRunAt, &st.LastRunID, &st.Hits, &st.LastHitTS, &st.Found, &st.UpdatedAt)
	return st
}

// SetMeetingScanHits records what the keyword pass found, without touching the
// watermark. The watermark only moves when a model has actually read the
// messages.
func (s *Store) SetMeetingScanHits(chatJID string, hits int, lastHitTS int64) {
	now := time.Now().Unix()
	s.DB.Exec(`INSERT INTO meeting_scan_state (chat_jid, hits, last_hit_ts, updated_at)
		VALUES (?,?,?,?)
		ON CONFLICT(chat_jid) DO UPDATE SET
			hits = excluded.hits,
			last_hit_ts = MAX(meeting_scan_state.last_hit_ts, excluded.last_hit_ts),
			updated_at = excluded.updated_at`, chatJID, hits, lastHitTS, now)
}

// AdvanceMeetingScan moves the watermark after a model has read the chat, and
// clears the pending hits. found adds to the running total of meetings this
// chat has produced.
func (s *Store) AdvanceMeetingScan(chatJID string, upTo int64, runID string, found int) {
	now := time.Now().Unix()
	s.DB.Exec(`INSERT INTO meeting_scan_state
		(chat_jid, last_msg_ts, last_run_at, last_run_id, hits, found, updated_at)
		VALUES (?,?,?,?,0,?,?)
		ON CONFLICT(chat_jid) DO UPDATE SET
			last_msg_ts = MAX(meeting_scan_state.last_msg_ts, excluded.last_msg_ts),
			last_run_at = excluded.last_run_at,
			last_run_id = excluded.last_run_id,
			hits = 0,
			found = meeting_scan_state.found + excluded.found,
			updated_at = excluded.updated_at`,
		chatJID, upTo, now, runID, found, now)
}

// ListMeetingScanState returns every chat the meeting scanner knows about,
// the ones with waiting hits first. This is the queue, as the debug screen
// shows it.
func (s *Store) ListMeetingScanState(limit int) ([]MeetingScanState, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.DB.Query(`SELECT chat_jid, last_msg_ts, last_run_at, last_run_id,
		hits, last_hit_ts, found, updated_at FROM meeting_scan_state
		ORDER BY hits DESC, last_hit_ts DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MeetingScanState{}
	for rows.Next() {
		var st MeetingScanState
		if rows.Scan(&st.ChatJID, &st.LastMsgTS, &st.LastRunAt, &st.LastRunID,
			&st.Hits, &st.LastHitTS, &st.Found, &st.UpdatedAt) != nil {
			continue
		}
		out = append(out, st)
	}
	return out, rows.Err()
}
