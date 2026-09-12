package extract

import (
	"context"
	"fmt"
	"time"

	"whatsapp-bridge-v2/internal/db"
)

// The run: select, read, chunk, ask, check, write.
//
// The order matters in one particular way. The watermark moves only after a
// chunk has been written successfully, so a run that dies half way costs
// nothing but the work it already did — the next run picks up exactly where
// this one stopped, and re-running is always safe.

type Deps struct {
	Store     *db.Store
	Extractor Extractor
	Loc       *time.Location
	// DryRun reads and asks, but writes nothing. The evaluation command uses it
	// to measure a model without filling the task list with its mistakes.
	DryRun bool
}

type RunSpec struct {
	ChatJID string
	// Since and Until bound the window. Since 0 means "use the stored
	// watermark", which is the normal case.
	Since, Until int64
	RunID        string
}

type Result struct {
	Chunks      int
	Skipped     int
	Proposed    int
	Verified    int
	Rejected    int
	Completions int
	// Tasks are what was written, so a caller can report them without a second
	// query. Empty on a dry run.
	Tasks []db.Task
	// Rejections says why things were dropped, keyed by reason.
	Rejections map[RejectReason]int
	// Outcomes is every proposal and what became of it. A dry run writes
	// nothing, so this is the only way to see what a model actually answered —
	// which is what the evaluation command scores.
	Outcomes []Outcome
	// ChunkMS is how long each extract call took, for the median latency.
	ChunkMS []int64
	// FailedChunks counts model calls that errored. A run that swallows these
	// looks exactly like a model that found nothing, which is how a wrong
	// model name once scored "0 precision" instead of "not installed".
	FailedChunks int
	// LastError is the most recent chunk failure, so a caller can say why.
	LastError string
}

// Outcome is one proposal and the decision made about it.
type Outcome struct {
	ChunkIndex int
	Title      string
	EvidenceID string
	// Evidence is the quote the model gave. Kept so the evaluation command can
	// re-check it against the raw message, by a path that does not run Verify.
	Evidence   string
	Confidence float64
	Kept       bool
	// Reason is empty when Kept. OwnerJID and DueAt are filled only when kept,
	// because they are resolved after the checks pass.
	Reason   RejectReason
	OwnerJID string
	DueAt    int64
	// Resolvable says the evidence message carried a mention or was a reply —
	// something an owner could be read from. Without it, "unknown" is the
	// correct answer, and scoring the owner would be scoring a guess.
	Resolvable bool
}

// forwardWindow is how far to look for the original of a forwarded message.
// A week is generous; the same instruction pushed into another group usually
// arrives within minutes.
const forwardWindow = 7 * 24 * 3600

// dedupeWindow is how far back an open task counts as "the same work said
// again" rather than a fresh ask.
const dedupeWindow = 14 * 24 * 3600

// Run extracts tasks from one chat.
func Run(ctx context.Context, d Deps, spec RunSpec, progress func(string)) (Result, error) {
	res := Result{Rejections: map[RejectReason]int{}}
	say := func(format string, args ...any) {
		if progress != nil {
			progress(fmt.Sprintf(format, args...))
		}
	}
	if d.Loc == nil {
		d.Loc = time.UTC
	}

	// Nothing hidden, archived or deleted is ever read. This is the same gate
	// every other AI feature uses, checked here rather than trusted upstream.
	if d.Store.IsChatExcludedFromAI(spec.ChatJID) {
		return res, fmt.Errorf("chat is hidden, archived or deleted")
	}

	since := spec.Since
	if since == 0 {
		since = watermark(d.Store, spec.ChatJID)
	}
	until := spec.Until
	if until == 0 {
		until = time.Now().Unix()
	}

	lines, err := FetchLines(d.Store, spec.ChatJID, since, until)
	if err != nil {
		return res, fmt.Errorf("read chat: %w", err)
	}
	if len(lines) == 0 {
		say("nothing new since the last run")
		return res, nil
	}

	people, ownName, isGroup, err := Roster(d.Store, spec.ChatJID)
	if err != nil {
		return res, fmt.Errorf("roster: %w", err)
	}
	chatName := chatDisplayName(d.Store, spec.ChatJID)

	open := openTasks(d.Store, spec.ChatJID)
	origins := map[string]bool{}
	for _, t := range open {
		if t.OriginMessageID != "" {
			origins[t.OriginMessageID] = true
		}
	}
	circleIDs := chatCircles(d.Store, spec.ChatJID)

	chunks := Split(lines, spec.ChatJID, DefaultChunkOptions(d.Loc))
	res.Chunks = len(chunks)
	say("%d new messages in %d chunk(s)", len(lines), len(chunks))

	meta := RunMeta{RunID: spec.RunID, Engine: d.Extractor.Name(), ChatJID: spec.ChatJID}

	// A message carried into the next chunk as context can now produce a task,
	// which means the same message can be reported twice in one run. The
	// database catches that between runs; within a run nothing is written yet
	// on a dry run, so the guard has to live here too.
	seen := map[string]bool{}
	// And the same piece of work is often asked for twice in different
	// messages — "the map needs changing" on Monday, and again in Tuesday's
	// list. Titles catch what message ids cannot.
	var titles []string

	for _, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return res, err
		}

		// Completions code can settle on its own: a reply to a task's origin
		// saying "تم" needs no model and admits no doubt.
		for _, c := range DoneReplies(chunk.Lines, open) {
			if !d.DryRun {
				completeTask(d.Store, c, spec.ChatJID)
			}
			res.Completions++
		}

		if Skippable(chunk, origins) {
			res.Skipped++
			say("chunk %d skipped — nothing in it could be work", chunk.Index)
			continue
		}

		started := time.Now()
		out, err := d.Extractor.Extract(ctx, ExtractInput{
			Chunk: chunk, Roster: people, OwnName: ownName,
			ChatName: chatName, IsGroup: isGroup,
		})
		if err != nil {
			// One bad chunk must not cost the whole run, and the watermark has
			// not moved, so the next run will see these messages again.
			say("chunk %d failed: %v", chunk.Index, err)
			res.FailedChunks++
			res.LastError = err.Error()
			continue
		}
		res.ChunkMS = append(res.ChunkMS, time.Since(started).Milliseconds())

		verified, rejected := 0, 0
		// Survivors of every code check, held back for the second opinion.
		type survivor struct {
			p        ProposedTask
			v        Verified
			ownerJID string
			dueAt    int64
			fwdChat  string
			fwdMsg   string
		}
		var survivors []survivor
		for _, p := range out.Tasks {
			res.Proposed++
			v, reason, ok := Verify(chunk, p)
			if !ok {
				rejected++
				res.Rejected++
				res.Rejections[reason]++
				res.Outcomes = append(res.Outcomes, Outcome{
					ChunkIndex: chunk.Index, Title: p.Title, EvidenceID: trimHash(p.EvidenceID),
					Evidence: p.Evidence, Confidence: p.Confidence, Reason: reason,
				})
				if !d.DryRun {
					RecordRejection(d.Store, meta, p, reason)
				}
				continue
			}

			if sameWorkAlready(titles, v.Task.Title) {
				res.Rejections["same_work"]++
				res.Outcomes = append(res.Outcomes, Outcome{
					ChunkIndex: chunk.Index, Title: p.Title, EvidenceID: v.Line.MessageID,
					Evidence: p.Evidence, Confidence: p.Confidence, Reason: "same_work",
				})
				continue
			}
			if seen[v.Line.MessageID] {
				res.Rejections["duplicate"]++
				res.Outcomes = append(res.Outcomes, Outcome{
					ChunkIndex: chunk.Index, Title: p.Title, EvidenceID: v.Line.MessageID,
					Evidence: p.Evidence, Confidence: p.Confidence, Reason: "duplicate",
				})
				continue
			}

			existingID, action := FindExisting(d.Store, circleIDs, v, spec.ChatJID, dedupeWindow)
			switch action {
			case ActionSkip:
				// This very message already produced a task, or one that was
				// rejected. Either way the answer is already known.
				res.Rejections["duplicate"]++
				res.Outcomes = append(res.Outcomes, Outcome{
					ChunkIndex: chunk.Index, Title: p.Title, EvidenceID: v.Line.MessageID,
					Evidence: p.Evidence, Confidence: p.Confidence, Reason: "duplicate",
				})
				continue
			case ActionAttach:
				if !d.DryRun {
					_ = d.Store.LinkTaskMessage(existingID, spec.ChatJID, v.Line.MessageID, "comment")
				}
				res.Rejections["same_work"]++
				res.Outcomes = append(res.Outcomes, Outcome{
					ChunkIndex: chunk.Index, Title: p.Title, EvidenceID: v.Line.MessageID,
					Evidence: p.Evidence, Confidence: p.Confidence, Reason: "same_work",
				})
				continue
			}

			ownerJID, _ := ResolveOwner(v, people)
			dueAt := ResolveDue(v.Task.DueText, v.Line.TS, d.Loc)
			fwdChat, fwdMsg := ForwardedOrigin(d.Store, v.Line, spec.ChatJID, forwardWindow)

			seen[v.Line.MessageID] = true
			titles = append(titles, v.Task.Title)
			survivors = append(survivors, survivor{p, v, ownerJID, dueAt, fwdChat, fwdMsg})
		}

		// The second opinion. Code has checked everything code can check; what
		// is left is the judgement call of whether these are really work.
		items := make([]JudgeItem, 0, len(survivors))
		for _, sv := range survivors {
			items = append(items, JudgeItem{
				Title: sv.v.Task.Title, Evidence: sv.v.Task.Evidence,
				EvidenceID: sv.v.Line.MessageID,
			})
		}
		keep := judgeItems(ctx, d, chunk, items, say)

		for i, sv := range survivors {
			if !keep[i] {
				rejected++
				res.Rejected++
				res.Rejections[RejectNotWork]++
				res.Outcomes = append(res.Outcomes, Outcome{
					ChunkIndex: chunk.Index, Title: sv.v.Task.Title,
					EvidenceID: sv.v.Line.MessageID, Evidence: sv.v.Task.Evidence,
					Confidence: sv.p.Confidence, Reason: RejectNotWork,
				})
				if !d.DryRun {
					RecordRejection(d.Store, meta, sv.p, RejectNotWork)
				}
				continue
			}
			verified++
			res.Verified++
			res.Outcomes = append(res.Outcomes, Outcome{
				ChunkIndex: chunk.Index, Title: sv.v.Task.Title, EvidenceID: sv.v.Line.MessageID,
				Evidence: sv.v.Task.Evidence, Confidence: sv.p.Confidence, Kept: true,
				OwnerJID: sv.ownerJID, DueAt: sv.dueAt,
				Resolvable: len(sv.v.Line.Mentions) > 0 || sv.v.Line.ReplyTo != "",
			})
			if d.DryRun {
				continue
			}
			task, err := Persist(d.Store, meta, sv.v, sv.ownerJID, sv.dueAt, sv.fwdChat, sv.fwdMsg)
			if err != nil {
				say("could not save %q: %v", sv.p.Title, err)
				continue
			}
			res.Tasks = append(res.Tasks, *task)
		}
		if !d.DryRun {
			RecordCall(d.Store, meta, "extract", chunk, time.Since(started),
				len(out.Tasks), verified, rejected)
		}
		say("chunk %d: %d proposed, %d kept", chunk.Index, len(out.Tasks), verified)

		// Ask about completions only when there is something open to complete
		// and the cheap rule did not already settle it.
		if len(open) > 0 {
			started := time.Now()
			cOut, err := d.Extractor.CheckCompletion(ctx, CompletionInput{Chunk: chunk, Open: open})
			if err == nil {
				kept := 0
				for _, c := range cOut.Completions {
					if !verifyCompletion(chunk, c, open) {
						continue
					}
					kept++
					res.Completions++
					if !d.DryRun {
						completeTask(d.Store, c, spec.ChatJID)
					}
				}
				if !d.DryRun {
					RecordCall(d.Store, meta, "completion", chunk, time.Since(started),
						len(cOut.Completions), kept, len(cOut.Completions)-kept)
				}
			}
		}

		// The watermark moves per chunk, not per run. A failure later costs
		// only the chunks after this one.
		if !d.DryRun && len(chunk.Lines) > 0 {
			last := chunk.Lines[len(chunk.Lines)-1]
			setWatermark(d.Store, spec.ChatJID, last.TS)
		}
	}

	return res, nil
}

// verifyCompletion holds a claimed completion to the same standard as a task:
// the words must be in the message named, and the task must be one we asked
// about.
func verifyCompletion(c Chunk, p ProposedCompletion, open []OpenTask) bool {
	if p.Confidence < 0.8 {
		return false
	}
	known := false
	for _, t := range open {
		if t.ID == p.TaskID {
			known = true
			break
		}
	}
	if !known {
		return false
	}
	id := trimHash(p.EvidenceID)
	for _, l := range c.Lines {
		if l.MessageID == id && !l.Context {
			return quoteMatches(l, p.Evidence)
		}
	}
	return false
}

func trimHash(s string) string {
	if len(s) > 0 && s[0] == '#' {
		return s[1:]
	}
	return s
}

func completeTask(store *db.Store, c ProposedCompletion, chatJID string) {
	// The 'completion' role is what marks the task done, including when the
	// news arrives in a different chat from the one that created it.
	_ = store.LinkTaskMessage(c.TaskID, chatJID, trimHash(c.EvidenceID), "completion")
}

func watermark(store *db.Store, chatJID string) int64 {
	var ts int64
	store.DB.QueryRow(`SELECT COALESCE(last_msg_ts, 0) FROM chat_extraction_state
		WHERE chat_jid = ?`, chatJID).Scan(&ts)
	return ts
}

func setWatermark(store *db.Store, chatJID string, ts int64) {
	now := time.Now().Unix()
	store.DB.Exec(`INSERT INTO chat_extraction_state (chat_jid, last_msg_ts, updated_at)
		VALUES (?,?,?)
		ON CONFLICT(chat_jid) DO UPDATE SET
			last_msg_ts = MAX(chat_extraction_state.last_msg_ts, excluded.last_msg_ts),
			updated_at = excluded.updated_at`, chatJID, ts, now)
}

func openTasks(store *db.Store, chatJID string) []OpenTask {
	rows, err := store.DB.Query(`
		SELECT DISTINCT t.id, t.title, COALESCE(c.name, ''),
		       COALESCE(t.origin_chat_jid,''), COALESCE(t.origin_message_id,'')
		FROM tasks t
		LEFT JOIN contacts c ON c.jid = t.assignee_jid
		LEFT JOIN task_circles tc ON tc.task_id = t.id
		WHERE t.status != 'done' AND t.review_status != 'rejected'
		  AND (t.origin_chat_jid = ?1
		       OR tc.circle_id IN (SELECT circle_id FROM circle_members
		                            WHERE member_type = 'group' AND member_ref = ?1))
		ORDER BY t.updated_at DESC
		LIMIT 60`, chatJID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []OpenTask
	for rows.Next() {
		var t OpenTask
		if rows.Scan(&t.ID, &t.Title, &t.OwnerName, &t.OriginChatJID, &t.OriginMessageID) == nil {
			out = append(out, t)
		}
	}
	return out
}

func chatCircles(store *db.Store, chatJID string) []int64 {
	rows, err := store.DB.Query(`SELECT circle_id FROM circle_members
		WHERE member_type = 'group' AND member_ref = ?`, chatJID)
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

func chatDisplayName(store *db.Store, chatJID string) string {
	var name string
	store.DB.QueryRow(`
		SELECT COALESCE(NULLIF(g.name,''), NULLIF(ct.name,''), NULLIF(ct.push_name,''), '')
		FROM chats ch
		LEFT JOIN groups g ON g.jid = ch.jid
		LEFT JOIN contacts ct ON (ct.jid = ch.jid OR ct.lid = ch.jid)
		WHERE ch.jid = ?`, chatJID).Scan(&name)
	return name
}

// sameWorkTitle is how close two titles must be to be the same piece of work.
// The same number the evaluation command uses to match a proposal to a label.
const sameWorkTitle = 0.6

func sameWorkAlready(titles []string, title string) bool {
	for _, t := range titles {
		if TitleSimilarity(t, title) >= sameWorkTitle {
			return true
		}
	}
	return false
}

// judgeItems asks the model whether each surviving proposal is really work,
// and returns one decision per item.
//
// It fails open: if the judge call errors or answers about the wrong things,
// everything is kept. The alternative — dropping work because a second call
// timed out — loses real tasks silently, and the review queue already exists
// to catch what gets through.
func judgeItems(ctx context.Context, d Deps, chunk Chunk, items []JudgeItem,
	say func(string, ...any)) []bool {

	keep := make([]bool, len(items))
	for i := range keep {
		keep[i] = true
	}
	if len(items) == 0 {
		return keep
	}

	out, err := d.Extractor.Judge(ctx, JudgeInput{Chunk: chunk, Items: items})
	if err != nil {
		say("chunk %d: second opinion unavailable, keeping all %d (%v)",
			chunk.Index, len(items), err)
		return keep
	}
	for _, v := range out.Verdicts {
		if v.Index >= 0 && v.Index < len(keep) {
			keep[v.Index] = v.IsTask
		}
	}
	return keep
}
