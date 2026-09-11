package extract

import (
	"strings"

	"whatsapp-bridge-v2/internal/db"
)

// Not creating the same task twice.
//
// Three things cause duplicates, and only one of them is the model's fault.
// A re-run covers messages already read. A chunk carries context from its
// neighbour. And the same instruction gets repeated in a conversation, or
// forwarded into another group. All of them are answerable from the data, so
// none of them is left to the model.

// Action says what to do with a proposal that matched something existing.
type Action string

const (
	ActionNew    Action = "new"    // nothing like it exists
	ActionSkip   Action = "skip"   // this exact message already produced a task
	ActionAttach Action = "attach" // the same work, said again — link, do not duplicate
)

// TitleOverlapThreshold is how alike two titles must be to count as the same
// work. Deliberately high: attaching to the wrong task hides a real one.
const TitleOverlapThreshold = 0.6

// FindExisting decides whether a verified proposal is new work.
//
// Skipping on the origin message is what makes a re-run safe, and it is also
// what makes a rejection stick: a task the user threw away keeps its row with
// review_status 'rejected', so its origin message is never offered again.
func FindExisting(store *db.Store, circleIDs []int64, v Verified, chatJID string, within int64) (int64, Action) {
	messageID := v.Line.MessageID

	var existingID int64
	err := store.DB.QueryRow(
		`SELECT id FROM tasks WHERE origin_chat_jid = ? AND origin_message_id = ? LIMIT 1`,
		chatJID, messageID).Scan(&existingID)
	if err == nil {
		return existingID, ActionSkip
	}

	// The same work said again, in this chat or another one in the same
	// circle. Only open tasks: finishing something and being asked again is a
	// new task, not a duplicate.
	rows, qErr := store.DB.Query(`
		SELECT DISTINCT t.id, t.title
		FROM tasks t
		LEFT JOIN task_circles tc ON tc.task_id = t.id
		WHERE t.status != 'done'
		  AND t.review_status != 'rejected'
		  AND t.updated_at >= ?
		  AND (t.origin_chat_jid = ? OR tc.circle_id IN (`+placeholders(len(circleIDs))+`))`,
		append([]any{v.Line.TS - within, chatJID}, toAny(circleIDs)...)...)
	if qErr != nil {
		return 0, ActionNew
	}
	defer rows.Close()

	best, bestScore := int64(0), 0.0
	for rows.Next() {
		var id int64
		var title string
		if rows.Scan(&id, &title) != nil {
			continue
		}
		if s := titleOverlap(v.Task.Title, title); s > bestScore {
			best, bestScore = id, s
		}
	}
	if bestScore >= TitleOverlapThreshold {
		return best, ActionAttach
	}
	return 0, ActionNew
}

// titleOverlap is the share of the shorter title's distinctive words found in
// the longer one. Short words are ignored: "في", "the" and "and" would make
// everything look alike.
func titleOverlap(a, b string) float64 {
	wa, wb := distinctive(a), distinctive(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	if len(wa) > len(wb) {
		wa, wb = wb, wa
	}
	set := make(map[string]bool, len(wb))
	for _, w := range wb {
		set[w] = true
	}
	hit := 0
	for _, w := range wa {
		if set[w] {
			hit++
		}
	}
	return float64(hit) / float64(len(wa))
}

func distinctive(s string) []string {
	var out []string
	for _, w := range strings.Fields(normalise(s)) {
		if len([]rune(w)) < 3 {
			continue
		}
		out = append(out, strings.TrimPrefix(w, "ال"))
	}
	return out
}

func placeholders(n int) string {
	if n == 0 {
		return "NULL"
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func toAny(ids []int64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// ForwardedOrigin finds where a forwarded message first appeared.
//
// 877 messages since July are forwards. The same instruction pushed into three
// groups is one piece of work, and without this it becomes three tasks that
// each look real. Matching is on the text itself, because a forward carries no
// pointer back to what it came from.
func ForwardedOrigin(store *db.Store, l Line, chatJID string, window int64) (string, string) {
	if !l.Forwarded || strings.TrimSpace(l.Text) == "" {
		return "", ""
	}
	var oc, om string
	err := store.DB.QueryRow(`
		SELECT m.chat_jid, m.id
		FROM messages m
		LEFT JOIN chats c ON c.jid = m.chat_jid
		WHERE m.content = ?
		  AND m.chat_jid != ?
		  AND m.timestamp BETWEEN ? AND ?
		  AND COALESCE(m.is_deleted, 0) = 0
		  AND COALESCE(c.is_archived, 0) = 0
		  AND COALESCE(c.deleted_at, 0) = 0
		  AND m.chat_jid NOT IN (SELECT chat_jid FROM hidden_chats)
		ORDER BY m.timestamp
		LIMIT 1`,
		l.Text, chatJID, l.TS-window, l.TS+window).Scan(&oc, &om)
	if err != nil {
		return "", ""
	}
	return oc, om
}

// Done markers, for the completion rule code can decide on its own.
var doneMarkers = []string{
	"✅", "✔", "☑", "تم", "خلصت", "خلصنا", "انجزت", "أنجزت", "جاهز", "رفعته",
	"بعتها", "بعتته", "done", "finished", "completed", "sent it", "ready",
}

// DoneReplies finds completions without asking the model anything.
//
// A reply to a task's own origin message saying "تم" is unambiguous, and
// spending a model call on it would be a waste — worse, it would introduce
// doubt where there is none.
func DoneReplies(lines []Line, open []OpenTask) []ProposedCompletion {
	byOrigin := map[string]OpenTask{}
	for _, t := range open {
		if t.OriginMessageID != "" {
			byOrigin[t.OriginMessageID] = t
		}
	}
	if len(byOrigin) == 0 {
		return nil
	}

	var out []ProposedCompletion
	for _, l := range lines {
		if l.Context || l.ReplyTo == "" {
			continue
		}
		task, ok := byOrigin[l.ReplyTo]
		if !ok {
			continue
		}
		text := normalise(l.Text)
		for _, m := range doneMarkers {
			if !strings.Contains(text, m) {
				continue
			}
			out = append(out, ProposedCompletion{
				TaskID: task.ID, EvidenceID: l.MessageID,
				Evidence: l.Text, Confidence: 1.0,
			})
			break
		}
	}
	return out
}
