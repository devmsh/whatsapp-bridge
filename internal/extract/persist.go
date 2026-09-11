package extract

import (
	"time"

	"whatsapp-bridge-v2/internal/db"
)

// Writing results down, and recording what it cost.
//
// Nothing reaches the task list without review. An extracted task is a
// proposal with a quote attached, and the quote is what makes reviewing it a
// two-second decision rather than a trip back into the conversation.

// RunMeta identifies one run for the record.
type RunMeta struct {
	RunID   string
	Engine  string
	ChatJID string
}

// Persist writes one verified task, its links and its circles.
//
// Circles are inherited, never chosen: a task takes the circles of the chat it
// came from and of the person who has to do it. The same rule meetings use —
// you already filed the group, so the work in it is filed too.
func Persist(store *db.Store, meta RunMeta, v Verified, ownerJID string, dueAt int64,
	forwardChat, forwardMsg string) (*db.Task, error) {

	t := &db.Task{
		Title:           v.Task.Title,
		Description:     describe(v, dueAt),
		Priority:        priority(v.Task.PriorityHint),
		AssigneeJID:     ownerJID,
		CreatorJID:      v.Line.SenderJID,
		DueAt:           dueAt,
		OriginChatJID:   meta.ChatJID,
		OriginMessageID: v.Line.MessageID,
		ReviewStatus:    db.ReviewPending,
		Evidence:        v.Task.Evidence,
		Engine:          meta.Engine,
		Confidence:      v.Task.Confidence,
	}
	created, err := store.CreateTask(t)
	if err != nil {
		return nil, err
	}

	// The origin link is what the reviewer clicks to see the conversation.
	_ = store.LinkTaskMessage(created.ID, meta.ChatJID, v.Line.MessageID, "origin")

	// A forwarded instruction is one piece of work seen twice. Linking the
	// earlier copy keeps both routes to it without making a second task.
	if forwardChat != "" && forwardMsg != "" {
		_ = store.LinkTaskMessage(created.ID, forwardChat, forwardMsg, "related")
	}

	for _, id := range inheritedCircles(store, meta.ChatJID, ownerJID) {
		_ = store.AddTaskCircle(created.ID, id)
	}
	return created, nil
}

// describe keeps what could not be turned into a field: the timing words when
// they named no date, so "بعد السفر" is still on the task even though no due
// date could be computed from it.
func describe(v Verified, dueAt int64) string {
	if v.Task.DueText != "" && dueAt == 0 {
		return "Timing as discussed: " + v.Task.DueText
	}
	return ""
}

func priority(hint string) string {
	switch hint {
	case "low", "high":
		return hint
	default:
		return "normal"
	}
}

// inheritedCircles returns the circles of the chat and of the assignee.
func inheritedCircles(store *db.Store, chatJID, ownerJID string) []int64 {
	rows, err := store.DB.Query(`
		SELECT DISTINCT cm.circle_id
		FROM circle_members cm
		WHERE (cm.member_type = 'group' AND cm.member_ref = ?1)
		   OR (cm.member_type = 'contact' AND cm.member_ref IN (
		         ?1, ?2,
		         COALESCE((SELECT c.jid FROM contacts c WHERE c.jid = ?2 OR c.lid = ?2), ''),
		         COALESCE((SELECT c.lid FROM contacts c WHERE c.jid = ?2 OR c.lid = ?2), '')))`,
		chatJID, ownerJID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// RecordCall stores what one model call did. Cost and quality are invisible
// without it.
func RecordCall(store *db.Store, meta RunMeta, kind string, chunk Chunk,
	latency time.Duration, proposed, verified, rejected int) {
	store.DB.Exec(`INSERT INTO extraction_calls
		(run_id, chat_jid, chunk_index, engine, kind, messages, chars,
		 latency_ms, proposed, verified, rejected, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		meta.RunID, meta.ChatJID, chunk.Index, meta.Engine, kind,
		len(chunk.Lines), len(chunk.Rendered), latency.Milliseconds(),
		proposed, verified, rejected, time.Now().Unix())
}

// RecordRejection stores why a proposal was dropped. A model that starts
// fabricating shows up here as bad_quote climbing, long before anyone notices
// the task list has gone strange.
func RecordRejection(store *db.Store, meta RunMeta, p ProposedTask, reason RejectReason) {
	store.DB.Exec(`INSERT INTO extraction_rejections
		(run_id, chat_jid, evidence_id, title, reason, created_at)
		VALUES (?,?,?,?,?,?)`,
		meta.RunID, meta.ChatJID, p.EvidenceID, p.Title, string(reason), time.Now().Unix())
}
