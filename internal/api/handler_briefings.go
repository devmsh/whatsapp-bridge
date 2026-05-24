package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// A Briefing is what the UI renders. The bridge precomputes all the structured
// facts via SQL, then a small sidecar adds narrative summaries via Claude.
type briefingTask struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Priority    string `json:"priority"`
	Status      string `json:"status"`
	Assignee    string `json:"assignee,omitempty"`
	AssigneeJID string `json:"assignee_jid,omitempty"`
	DueAt       int64  `json:"due_at,omitempty"`
	CircleName  string `json:"circle_name,omitempty"`
	UpdatedAt   int64  `json:"updated_at,omitempty"`
}

type briefingChat struct {
	JID          string `json:"jid"`
	Name         string `json:"name"`
	LastActiveAt int64  `json:"last_active_at"`
	NewMessages  int    `json:"new_messages"`
	Narrative    string `json:"narrative,omitempty"`
}

type briefingAwaiting struct {
	JID           string `json:"jid"`
	Name          string `json:"name"`
	LastMessageAt int64  `json:"last_message_at"`
	LastFromName  string `json:"last_from_name"`
	Preview       string `json:"preview,omitempty"`
}

type briefingPayload struct {
	ForDate        string             `json:"for_date"`
	GeneratedAt    int64              `json:"generated_at"`
	Summary        string             `json:"summary"`
	Today          []briefingTask     `json:"today"`
	Overdue        []briefingTask     `json:"overdue"`
	SignalChats    []briefingChat     `json:"signal_chats"`
	AwaitingReply  []briefingAwaiting `json:"awaiting_reply"`
	StatsTasksOpen int                `json:"stats_tasks_open"`
}

// handleBriefingsRoot routes /api/v2/briefings[...] paths.
//
//   GET  /api/v2/briefings/today      -> latest briefing for today, or null
//   GET  /api/v2/briefings            -> recent briefings (list)
//   POST /api/v2/briefings/generate   -> build a fresh briefing and store it
func (s *Server) handleBriefingsRoot(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v2/briefings")
	path = strings.TrimPrefix(path, "/")
	switch path {
	case "":
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		list, err := s.store.ListBriefings(30)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, list)
	case "today":
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		today := time.Now().Format("2006-01-02")
		b, err := s.store.LatestBriefing(today)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, b) // may be nil
	case "generate":
		s.handleBriefingsGenerate(w, r)
	default:
		jsonError(w, 404, "unknown briefings path")
	}
}

func (s *Server) handleBriefingsGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	payload, err := s.buildBriefing()
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	// Enrich with narrative summaries via the sidecar (best-effort: if the
	// sidecar fails, we still save the structural briefing without narratives).
	enriched := s.enrichBriefing(payload)
	bts, _ := json.Marshal(enriched)
	saved, err := s.store.SaveBriefing(enriched.ForDate, string(bts))
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, saved)
}

// buildBriefing assembles the structured facts via SQL only (no LLM).
func (s *Server) buildBriefing() (*briefingPayload, error) {
	now := time.Now()
	today := now.Format("2006-01-02")
	dayAgo := now.Add(-24 * time.Hour).Unix()
	weekAgo := now.Add(-7 * 24 * time.Hour).Unix()

	out := &briefingPayload{ForDate: today, GeneratedAt: now.Unix()}

	// ── tasks_open count ─────────────────────────────────────────────
	s.store.DB.QueryRow(`SELECT COUNT(*) FROM tasks
		WHERE review_status='accepted' AND status IN ('open','in_progress')`).Scan(&out.StatsTasksOpen)

	// ── overdue tasks ────────────────────────────────────────────────
	out.Overdue = s.queryBriefingTasks(`
		SELECT t.id, t.title, t.priority, t.status, t.assignee_jid, t.due_at, t.updated_at,
		       COALESCE(NULLIF(c.name,''), NULLIF(c.push_name,''), NULLIF(c.business_name,''), '') AS assignee,
		       COALESCE(NULLIF(circ.name,''), '') AS circle_name
		FROM tasks t
		LEFT JOIN contacts c ON c.jid = t.assignee_jid
		LEFT JOIN task_circles tc ON tc.task_id = t.id
		LEFT JOIN circles circ ON circ.id = tc.circle_id
		WHERE t.review_status='accepted'
		  AND t.status IN ('open','in_progress')
		  AND t.due_at > 0 AND t.due_at < ?
		GROUP BY t.id
		ORDER BY t.due_at ASC LIMIT 10`, now.Unix())

	// ── today's top tasks ────────────────────────────────────────────
	// High priority first; then most-recently-updated. Limit 5.
	out.Today = s.queryBriefingTasks(`
		SELECT t.id, t.title, t.priority, t.status, t.assignee_jid, t.due_at, t.updated_at,
		       COALESCE(NULLIF(c.name,''), NULLIF(c.push_name,''), NULLIF(c.business_name,''), '') AS assignee,
		       COALESCE(NULLIF(circ.name,''), '') AS circle_name
		FROM tasks t
		LEFT JOIN contacts c ON c.jid = t.assignee_jid
		LEFT JOIN task_circles tc ON tc.task_id = t.id
		LEFT JOIN circles circ ON circ.id = tc.circle_id
		WHERE t.review_status='accepted'
		  AND t.status IN ('open','in_progress')
		GROUP BY t.id
		ORDER BY CASE t.priority WHEN 'high' THEN 0 WHEN 'normal' THEN 1 ELSE 2 END,
		         t.updated_at DESC
		LIMIT 5`)

	// ── signal chats (top by new-message count in last 24h) ──────────
	// Skip newsletters / status / hidden chats / under 3 messages.
	if rows, err := s.store.DB.Query(`
		SELECT m.chat_jid,
		       MAX(m.timestamp) AS last_ts,
		       COUNT(*) AS n,
		       COALESCE(NULLIF(g.name,''), NULLIF(c.name,''),
		                NULLIF(c.push_name,''), NULLIF(c.business_name,''),
		                m.chat_jid) AS name
		FROM messages m
		LEFT JOIN groups g ON g.jid = m.chat_jid
		LEFT JOIN contacts c ON c.jid = m.chat_jid
		WHERE m.timestamp >= ?
		  AND m.chat_jid NOT LIKE '%@newsletter'
		  AND m.chat_jid != 'status@broadcast'
		  AND m.chat_jid NOT IN (SELECT chat_jid FROM hidden_chats)
		  AND COALESCE(m.content,'') != ''
		GROUP BY m.chat_jid
		HAVING n >= 3
		ORDER BY n DESC LIMIT 8`, dayAgo); err == nil {
		for rows.Next() {
			var bc briefingChat
			if rows.Scan(&bc.JID, &bc.LastActiveAt, &bc.NewMessages, &bc.Name) == nil {
				out.SignalChats = append(out.SignalChats, bc)
			}
		}
		rows.Close()
	}

	// ── awaiting reply (their last DM ≥ your last) ──────────────────
	// DMs only (no groups). They sent the last message; you didn't reply.
	if rows, err := s.store.DB.Query(`
		WITH last_msgs AS (
		  SELECT m.chat_jid, MAX(m.timestamp) AS ts
		  FROM messages m
		  WHERE m.chat_jid LIKE '%@s.whatsapp.net'
		    AND m.timestamp >= ?
		    AND m.chat_jid NOT IN (SELECT chat_jid FROM hidden_chats)
		  GROUP BY m.chat_jid
		)
		SELECT m.chat_jid, m.timestamp,
		       COALESCE(NULLIF(m.sender_name,''), NULLIF(m.push_name,''), '') AS from_name,
		       COALESCE(NULLIF(c.name,''), NULLIF(c.push_name,''),
		                NULLIF(c.business_name,''), m.chat_jid) AS chat_name,
		       SUBSTR(COALESCE(m.content,''), 1, 140) AS preview
		FROM messages m
		JOIN last_msgs l ON l.chat_jid = m.chat_jid AND l.ts = m.timestamp
		LEFT JOIN contacts c ON c.jid = m.chat_jid
		WHERE m.is_from_me = 0
		ORDER BY m.timestamp DESC
		LIMIT 8`, weekAgo); err == nil {
		for rows.Next() {
			var bw briefingAwaiting
			if rows.Scan(&bw.JID, &bw.LastMessageAt, &bw.LastFromName, &bw.Name, &bw.Preview) == nil {
				out.AwaitingReply = append(out.AwaitingReply, bw)
			}
		}
		rows.Close()
	}

	return out, nil
}

func (s *Server) queryBriefingTasks(q string, args ...any) []briefingTask {
	out := []briefingTask{}
	rows, err := s.store.DB.Query(q, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var t briefingTask
		if err := rows.Scan(&t.ID, &t.Title, &t.Priority, &t.Status, &t.AssigneeJID,
			&t.DueAt, &t.UpdatedAt, &t.Assignee, &t.CircleName); err != nil {
			continue
		}
		out = append(out, t)
	}
	return out
}

// enrichBriefing asks the briefing sidecar to write the top-line and one
// narrative per signal chat. Best-effort: failures fall through with empties.
func (s *Server) enrichBriefing(p *briefingPayload) *briefingPayload {
	if len(p.SignalChats) == 0 && len(p.Today) == 0 && len(p.Overdue) == 0 {
		p.Summary = "Quiet today — no pending tasks or active chats."
		return p
	}

	// Build a context for each signal chat: last ~10 lines, oldest→newest.
	type signalChatCtx struct {
		ChatJID  string   `json:"chat_jid"`
		Name     string   `json:"name"`
		Sample   []string `json:"sample"`
		Messages int      `json:"messages"`
	}
	var signalCtx []signalChatCtx
	for _, sc := range p.SignalChats {
		sample := s.recentChatLines(sc.JID, 12)
		signalCtx = append(signalCtx, signalChatCtx{
			ChatJID: sc.JID, Name: sc.Name, Sample: sample, Messages: sc.NewMessages,
		})
	}

	input := map[string]any{
		"date":           p.ForDate,
		"tasks_open":     p.StatsTasksOpen,
		"tasks_top":      p.Today,
		"tasks_overdue":  p.Overdue,
		"awaiting_reply": p.AwaitingReply,
		"signal_chats":   signalCtx,
	}
	in, _ := json.Marshal(input)

	out, err := s.runAgentInput(3*time.Minute, string(in), "briefing.mjs")
	if err != nil {
		fmt.Printf("briefing sidecar failed: %v\n", err)
	}
	var res struct {
		OK              bool              `json:"ok"`
		Summary         string            `json:"summary"`
		SignalSummaries map[string]string `json:"signal_summaries"`
	}
	if line := lastJSONLine(out); line != nil {
		json.Unmarshal(line, &res)
	}
	p.Summary = res.Summary
	for i := range p.SignalChats {
		if n, ok := res.SignalSummaries[p.SignalChats[i].JID]; ok {
			p.SignalChats[i].Narrative = strings.TrimSpace(n)
		}
	}
	return p
}

// recentChatLines pulls a small chronological sample of recent text messages
// from a chat (last 24h, capped). Used to give the sidecar enough context to
// describe what's going on there.
func (s *Server) recentChatLines(jid string, n int) []string {
	rows, err := s.store.DB.Query(`SELECT
		COALESCE(NULLIF(sender_name,''), NULLIF(push_name,''), '') AS who,
		is_from_me,
		SUBSTR(COALESCE(content,''), 1, 240) AS body,
		COALESCE(media_type,'') AS media
		FROM messages
		WHERE chat_jid = ? AND timestamp >= ?
		ORDER BY timestamp DESC LIMIT ?`, jid, time.Now().Add(-24*time.Hour).Unix(), n)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var who, body, media string
		var fromMe bool
		if rows.Scan(&who, &fromMe, &body, &media) != nil {
			continue
		}
		text := strings.ReplaceAll(strings.TrimSpace(body), "\n", " ")
		if text == "" && media != "" {
			text = "[" + media + "]"
		}
		if text == "" {
			continue
		}
		label := who
		if fromMe {
			label = "Me"
		}
		if label == "" {
			label = "Someone"
		}
		lines = append(lines, label+": "+text)
	}
	// reverse to chronological
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return lines
}

// envHelper for routing constants
var _ = strconv.Itoa
