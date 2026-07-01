package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"whatsapp-bridge-v2/internal/db"
)

// circleDigestInFlight tracks which circles currently have a background
// regeneration running. It is package-level (not a Server field) because the
// digest route is registered inside handleCircleByID's switch, matching how
// handler_briefings.go adds no Server state. Keyed per-circle so concurrent
// requests for the SAME circle don't spawn duplicate sidecar runs, while
// DIFFERENT circles regenerate concurrently.
var circleDigestInFlight = struct {
	mu  sync.Mutex
	ids map[int64]bool
}{ids: map[int64]bool{}}

// handleCircleDigest returns the cached circle digest FAST — it never blocks
// on the LLM sidecar. When the cached row is missing or stale (existing ==
// nil, or >=10 new messages have arrived since the watermark), it kicks off
// regeneration in a background goroutine (guarded by circleDigestInFlight)
// and responds immediately with the (possibly stale or null) cached data plus
// refreshing:true. The frontend polls again shortly while refreshing is true.
// GET /api/v2/circles/{id}/digest
func (s *Server) handleCircleDigest(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	existing, err := s.store.GetCircleDigest(id)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}

	jids, err := s.store.FlattenCircleChats(id)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}

	var watermark int64
	if existing != nil {
		watermark = existing.LastMsgTS
	}
	newCount := s.circleMessageCountSince(jids, watermark)
	needsRefresh := existing == nil || newCount >= 10

	if needsRefresh {
		circleDigestInFlight.mu.Lock()
		alreadyRunning := circleDigestInFlight.ids[id]
		if !alreadyRunning {
			circleDigestInFlight.ids[id] = true
		}
		circleDigestInFlight.mu.Unlock()

		if !alreadyRunning {
			go func() {
				defer func() {
					circleDigestInFlight.mu.Lock()
					delete(circleDigestInFlight.ids, id)
					circleDigestInFlight.mu.Unlock()
				}()
				s.regenerateCircleDigest(id, jids, existing)
			}()
		}
	}

	resp := map[string]any{"refreshing": needsRefresh}
	if existing != nil && existing.Data != "" {
		resp["digest"] = json.RawMessage(existing.Data)
	} else {
		resp["digest"] = nil
	}
	jsonOK(w, resp)
}

// circleMessageCountSince counts messages across a circle's flattened chats
// newer than watermark. Placeholder-building mirrors TasksForCircle
// (tasks.go:281-288). Returns 0 when jids is empty.
func (s *Server) circleMessageCountSince(jids []string, watermark int64) int {
	if len(jids) == 0 {
		return 0
	}
	placeholders := make([]string, len(jids))
	args := make([]any, 0, len(jids)+1)
	for i, jid := range jids {
		placeholders[i] = "?"
		args = append(args, jid)
	}
	args = append(args, watermark)
	var n int
	s.store.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE chat_jid IN (`+
		strings.Join(placeholders, ",")+`) AND timestamp > ?`, args...).Scan(&n)
	return n
}

// regenerateCircleDigest is the background-goroutine body kicked off by
// handleCircleDigest. It gathers facts scoped to one circle's flattened
// chats, calls the circle-digest sidecar with the previous summary (for an
// incremental update rather than a from-scratch rewrite), and upserts the
// fresh row + new watermark. Best-effort: it logs errors and never panics —
// there is no HTTP request left to respond to by the time this runs.
func (s *Server) regenerateCircleDigest(id int64, jids []string, existing *db.CircleDigest) {
	defer func() {
		if rec := recover(); rec != nil {
			fmt.Printf("circle-digest: recovered panic for circle %d: %v\n", id, rec)
		}
	}()

	circle, err := s.store.GetCircle(id)
	if err != nil || circle == nil {
		fmt.Printf("circle-digest: circle %d not found: %v\n", id, err)
		return
	}

	var watermark int64
	var previousSummary string
	if existing != nil {
		watermark = existing.LastMsgTS
		previousSummary = existing.Summary
	}
	newCount := s.circleMessageCountSince(jids, watermark)

	now := time.Now()
	weekAgo := now.Add(-7 * 24 * time.Hour).Unix()

	// ── tasks: accepted + open/in_progress, scoped to this circle ─────
	tasks, err := s.store.TasksForCircle(id)
	if err != nil {
		fmt.Printf("circle-digest: TasksForCircle(%d) failed: %v\n", id, err)
	}
	var accepted []db.Task
	for _, t := range tasks {
		if t.ReviewStatus == db.ReviewAccepted && (t.Status == db.TaskOpen || t.Status == db.TaskInProgress) {
			accepted = append(accepted, t)
		}
	}

	overdue := make([]db.Task, 0, len(accepted))
	for _, t := range accepted {
		if t.DueAt > 0 && t.DueAt < now.Unix() {
			overdue = append(overdue, t)
		}
	}
	sort.Slice(overdue, func(i, j int) bool { return overdue[i].DueAt < overdue[j].DueAt })
	if len(overdue) > 10 {
		overdue = overdue[:10]
	}

	today := make([]db.Task, len(accepted))
	copy(today, accepted)
	priorityRank := func(p string) int {
		switch p {
		case "high":
			return 0
		case "normal":
			return 1
		default:
			return 2
		}
	}
	sort.Slice(today, func(i, j int) bool {
		pi, pj := priorityRank(today[i].Priority), priorityRank(today[j].Priority)
		if pi != pj {
			return pi < pj
		}
		return today[i].UpdatedAt > today[j].UpdatedAt
	})
	if len(today) > 5 {
		today = today[:5]
	}

	toBriefingTask := func(t db.Task) briefingTask {
		return briefingTask{
			ID:          t.ID,
			Title:       t.Title,
			Priority:    t.Priority,
			Status:      t.Status,
			AssigneeJID: t.AssigneeJID,
			DueAt:       t.DueAt,
			UpdatedAt:   t.UpdatedAt,
		}
	}
	overdueBT := make([]briefingTask, len(overdue))
	for i, t := range overdue {
		overdueBT[i] = toBriefingTask(t)
	}
	todayBT := make([]briefingTask, len(today))
	for i, t := range today {
		todayBT[i] = toBriefingTask(t)
	}

	// ── signal chats: circle-scoped, watermark-floored (NOT a fixed
	// 24h/7d window — a circle can go quiet for days between count-based
	// regenerations, so a hardcoded floor would silently return an empty
	// digest despite real content existing) ──────────────────────────
	signalChats := []briefingChat{}
	if len(jids) > 0 {
		placeholders := make([]string, len(jids))
		args := make([]any, 0, len(jids)+1)
		for i, jid := range jids {
			placeholders[i] = "?"
			args = append(args, jid)
		}
		args = append(args, watermark)
		q := `
			SELECT m.chat_jid,
			       MAX(m.timestamp) AS last_ts,
			       COUNT(*) AS n,
			       COALESCE(NULLIF(g.name,''), NULLIF(c.name,''),
			                NULLIF(c.push_name,''), NULLIF(c.business_name,''),
			                m.chat_jid) AS name
			FROM messages m
			LEFT JOIN groups g ON g.jid = m.chat_jid
			LEFT JOIN contacts c ON c.jid = m.chat_jid
			WHERE m.chat_jid IN (` + strings.Join(placeholders, ",") + `)
			  AND m.timestamp > ?
			  AND m.chat_jid NOT LIKE '%@newsletter'
			  AND m.chat_jid != 'status@broadcast'
			  AND m.chat_jid NOT IN (SELECT chat_jid FROM hidden_chats)
			  AND COALESCE(m.content,'') != ''
			GROUP BY m.chat_jid
			HAVING n >= 3
			ORDER BY n DESC LIMIT 8`
		if rows, err := s.store.DB.Query(q, args...); err == nil {
			for rows.Next() {
				var bc briefingChat
				if rows.Scan(&bc.JID, &bc.LastActiveAt, &bc.NewMessages, &bc.Name) == nil {
					signalChats = append(signalChats, bc)
				}
			}
			rows.Close()
		} else {
			fmt.Printf("circle-digest: signal-chats query failed for circle %d: %v\n", id, err)
		}
	}

	// ── awaiting reply: DMs only, circle-scoped, floor =
	// max(watermark, weekAgo) so a circle quiet for months doesn't dump
	// excessive history ────────────────────────────────────────────────
	floor := watermark
	if floor < weekAgo {
		floor = weekAgo
	}
	awaiting := []briefingAwaiting{}
	if len(jids) > 0 {
		placeholders := make([]string, len(jids))
		args := make([]any, 0, len(jids)+1)
		for i, jid := range jids {
			placeholders[i] = "?"
			args = append(args, jid)
		}
		args = append(args, floor)
		q := `
			WITH last_msgs AS (
			  SELECT m.chat_jid, MAX(m.timestamp) AS ts
			  FROM messages m
			  WHERE m.chat_jid LIKE '%@s.whatsapp.net'
			    AND m.chat_jid IN (` + strings.Join(placeholders, ",") + `)
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
			LIMIT 8`
		if rows, err := s.store.DB.Query(q, args...); err == nil {
			for rows.Next() {
				var bw briefingAwaiting
				if rows.Scan(&bw.JID, &bw.LastMessageAt, &bw.LastFromName, &bw.Name, &bw.Preview) == nil {
					awaiting = append(awaiting, bw)
				}
			}
			rows.Close()
		} else {
			fmt.Printf("circle-digest: awaiting-reply query failed for circle %d: %v\n", id, err)
		}
	}

	// ── per-signal-chat samples: last 15 by count, no time floor ──────
	type signalChatCtx struct {
		ChatJID  string   `json:"chat_jid"`
		Name     string   `json:"name"`
		Sample   []string `json:"sample"`
		Messages int      `json:"messages"`
	}
	var signalCtx []signalChatCtx
	for _, sc := range signalChats {
		sample := s.recentChatLinesByCount(sc.JID, 15)
		signalCtx = append(signalCtx, signalChatCtx{
			ChatJID: sc.JID, Name: sc.Name, Sample: sample, Messages: sc.NewMessages,
		})
	}

	input := map[string]any{
		"circle_name":       circle.Name,
		"previous_summary":  previousSummary,
		"new_message_count": newCount,
		"tasks_top":         todayBT,
		"tasks_overdue":     overdueBT,
		"awaiting_reply":    awaiting,
		"signal_chats":      signalCtx,
	}
	inputJSON, _ := json.Marshal(input)

	out, err := s.runAgentInput(3*time.Minute, string(inputJSON), "circle-digest.mjs")
	if err != nil {
		fmt.Printf("circle-digest sidecar failed for circle %d: %v\n", id, err)
	}
	var res struct {
		OK              bool              `json:"ok"`
		Summary         string            `json:"summary"`
		SignalSummaries map[string]string `json:"signal_summaries"`
	}
	if line := lastJSONLine(out); line != nil {
		json.Unmarshal(line, &res)
	}
	for i := range signalChats {
		if n, ok := res.SignalSummaries[signalChats[i].JID]; ok {
			signalChats[i].Narrative = strings.TrimSpace(n)
		}
	}

	payload := &briefingPayload{
		ForDate:        now.Format("2006-01-02"),
		GeneratedAt:    now.Unix(),
		Summary:        res.Summary,
		Today:          todayBT,
		Overdue:        overdueBT,
		SignalChats:    signalChats,
		AwaitingReply:  awaiting,
		StatsTasksOpen: len(accepted),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		fmt.Printf("circle-digest: marshal failed for circle %d: %v\n", id, err)
		return
	}

	// ── new watermark: max message ts across the circle's chats, now ──
	var newWatermark int64
	if len(jids) > 0 {
		placeholders := make([]string, len(jids))
		args := make([]any, len(jids))
		for i, jid := range jids {
			placeholders[i] = "?"
			args[i] = jid
		}
		s.store.DB.QueryRow(`SELECT COALESCE(MAX(timestamp),0) FROM messages WHERE chat_jid IN (`+
			strings.Join(placeholders, ",")+`)`, args...).Scan(&newWatermark)
	}

	if err := s.store.SaveCircleDigest(id, payload.Summary, string(payloadJSON), newWatermark); err != nil {
		fmt.Printf("circle-digest: SaveCircleDigest failed for circle %d: %v\n", id, err)
	}
}

// recentChatLinesByCount is recentChatLines (handler_briefings.go:297-338)
// minus the `AND timestamp >= ?` time floor: it samples the last n messages
// by count instead of by a fixed 24h window, because a circle can go quiet
// for days between count-based regenerations and a time floor would return
// nothing despite real recent content. Returned chronologically (oldest
// first), same as recentChatLines.
func (s *Server) recentChatLinesByCount(jid string, n int) []string {
	rows, err := s.store.DB.Query(`SELECT
		COALESCE(NULLIF(sender_name,''), NULLIF(push_name,''), '') AS who,
		is_from_me,
		SUBSTR(COALESCE(content,''), 1, 240) AS body,
		COALESCE(media_type,'') AS media
		FROM messages
		WHERE chat_jid = ?
		ORDER BY timestamp DESC LIMIT ?`, jid, n)
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
