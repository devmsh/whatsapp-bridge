package extract_test

import (
	"context"
	"testing"
	"time"

	"whatsapp-bridge-v2/internal/db"
	"whatsapp-bridge-v2/internal/extract"
)

// fakeModel returns whatever it is told to, so the pipeline can be tested
// without a model. This is the payoff of the port: every stage around the
// judgement call is exercised deterministically.
type fakeModel struct {
	tasks       []extract.ProposedTask
	completions []extract.ProposedCompletion
	calls       int
}

func (f *fakeModel) Name() string { return "fake:test" }

func (f *fakeModel) Extract(_ context.Context, _ extract.ExtractInput) (extract.ExtractOutput, error) {
	f.calls++
	return extract.ExtractOutput{Tasks: f.tasks}, nil
}

func (f *fakeModel) CheckCompletion(_ context.Context, _ extract.CompletionInput) (extract.CompletionOutput, error) {
	return extract.CompletionOutput{Completions: f.completions}, nil
}

func seedChat(t *testing.T, st *db.Store, chat string) int64 {
	t.Helper()
	now := time.Now().Unix() - 3600

	if err := st.StoreChat(&db.Chat{JID: chat, Name: "Delivery team"}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	for _, c := range []db.Contact{
		{JID: "966500000001@s.whatsapp.net", Phone: "966500000001", Name: "Sara Haddad"},
		{JID: "966500000002@s.whatsapp.net", Phone: "966500000002", Name: "Omar Nasser"},
	} {
		cc := c
		if err := st.StoreContact(&cc); err != nil {
			t.Fatalf("StoreContact: %v", err)
		}
		if err := st.StoreGroupParticipant(&db.GroupParticipant{
			GroupJID: chat, JID: cc.JID, Phone: cc.Phone,
		}); err != nil {
			t.Fatalf("StoreGroupParticipant: %v", err)
		}
	}

	if err := st.StoreMessage(&db.Message{
		ID: "REQ", ChatJID: chat, Sender: "966500000002@s.whatsapp.net",
		Content:  "@966500000001 جهز العقد قبل الخميس",
		Mentions: `["966500000001@s.whatsapp.net"]`, Timestamp: now,
	}); err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}
	return now
}

// TestRunEndToEnd walks a real request through every stage: the model proposes,
// code verifies the quote, resolves the owner from the mention and the date
// from the message's own timestamp, then writes a task awaiting review.
func TestRunEndToEnd(t *testing.T) {
	st := newStore(t)
	chat := "team@g.us"
	sent := seedChat(t, st, chat)

	model := &fakeModel{tasks: []extract.ProposedTask{{
		Title: "جهز العقد", OwnerText: "unknown",
		EvidenceID: "#REQ", Evidence: "جهز العقد قبل الخميس",
		DueText: "الخميس", PriorityHint: "high", Confidence: 0.9,
	}}}

	res, err := extract.Run(context.Background(),
		extract.Deps{Store: st, Extractor: model, Loc: riyadh(t)},
		extract.RunSpec{ChatJID: chat, RunID: "run-1"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Verified != 1 || len(res.Tasks) != 1 {
		t.Fatalf("expected one task, got verified=%d tasks=%d rejections=%v",
			res.Verified, len(res.Tasks), res.Rejections)
	}

	task := res.Tasks[0]
	if task.ReviewStatus != db.ReviewPending {
		t.Errorf("an extracted task must wait for review, got %q", task.ReviewStatus)
	}
	if task.Evidence == "" {
		t.Errorf("the evidence quote is what makes review quick; it must be stored")
	}
	if task.Engine != "fake:test" {
		t.Errorf("engine = %q, want the extractor's name", task.Engine)
	}
	if task.AssigneeJID != "966500000001@s.whatsapp.net" {
		t.Errorf("owner should come from the mention, got %q", task.AssigneeJID)
	}
	if task.Priority != "high" {
		t.Errorf("priority = %q, want high", task.Priority)
	}
	// "الخميس" is resolved against the message's own date, not today's.
	if task.DueAt == 0 {
		t.Fatalf("a named weekday is a resolvable date")
	}
	if d := time.Unix(task.DueAt, 0).In(riyadh(t)); d.Weekday() != time.Thursday {
		t.Errorf("due %s, want a Thursday", d.Weekday())
	}
	if task.DueAt <= sent {
		t.Errorf("the due date must be after the message that asked for it")
	}

	// The origin message is linked, which is what a reviewer clicks.
	links, err := st.GetTaskMessages(task.ID)
	if err != nil {
		t.Fatalf("GetTaskMessages: %v", err)
	}
	var hasOrigin bool
	for _, l := range links {
		if l.Role == "origin" && l.MessageID == "REQ" {
			hasOrigin = true
		}
	}
	if !hasOrigin {
		t.Errorf("the origin message should be linked, got %+v", links)
	}
}

// TestRunIsIdempotent is the contract that makes a background schedule safe:
// running twice over the same conversation must not create the work twice.
func TestRunIsIdempotent(t *testing.T) {
	st := newStore(t)
	chat := "team@g.us"
	seedChat(t, st, chat)

	model := &fakeModel{tasks: []extract.ProposedTask{{
		Title: "جهز العقد", OwnerText: "Sara Haddad",
		EvidenceID: "#REQ", Evidence: "جهز العقد قبل الخميس",
		Confidence: 0.9,
	}}}
	deps := extract.Deps{Store: st, Extractor: model, Loc: riyadh(t)}

	first, err := extract.Run(context.Background(), deps,
		extract.RunSpec{ChatJID: chat, RunID: "run-1"}, nil)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if first.Verified != 1 {
		t.Fatalf("first run should find the task, got %d", first.Verified)
	}

	// A second run with the watermark in place sees nothing new at all.
	second, err := extract.Run(context.Background(), deps,
		extract.RunSpec{ChatJID: chat, RunID: "run-2"}, nil)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if second.Verified != 0 {
		t.Errorf("second run created %d more task(s)", second.Verified)
	}

	// And even ignoring the watermark, the same message cannot produce a
	// second task — which is what protects a manual re-run over old messages.
	third, err := extract.Run(context.Background(), deps,
		extract.RunSpec{ChatJID: chat, Since: 1, RunID: "run-3"}, nil)
	if err != nil {
		t.Fatalf("third Run: %v", err)
	}
	if third.Verified != 0 {
		t.Errorf("re-reading old messages created %d duplicate(s)", third.Verified)
	}

	all, err := st.ListTasks("", "")
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("ended with %d tasks, want exactly 1", len(all))
	}
}

// TestRunRefusesExcludedChats — hidden, archived and deleted chats are never
// read by anything. Checked here rather than trusted from upstream.
func TestRunRefusesExcludedChats(t *testing.T) {
	st := newStore(t)
	chat := "team@g.us"
	seedChat(t, st, chat)
	if err := st.SetChatArchived(chat, true); err != nil {
		t.Fatalf("SetChatArchived: %v", err)
	}

	_, err := extract.Run(context.Background(),
		extract.Deps{Store: st, Extractor: &fakeModel{}, Loc: riyadh(t)},
		extract.RunSpec{ChatJID: chat}, nil)
	if err == nil {
		t.Fatalf("an archived chat must not be read")
	}
}

// TestDryRunWritesNothing keeps the evaluation command from filling the task
// list with the mistakes it is measuring.
func TestDryRunWritesNothing(t *testing.T) {
	st := newStore(t)
	chat := "team@g.us"
	seedChat(t, st, chat)

	model := &fakeModel{tasks: []extract.ProposedTask{{
		Title: "جهز العقد", EvidenceID: "#REQ",
		Evidence: "جهز العقد قبل الخميس", Confidence: 0.9,
	}}}
	res, err := extract.Run(context.Background(),
		extract.Deps{Store: st, Extractor: model, Loc: riyadh(t), DryRun: true},
		extract.RunSpec{ChatJID: chat}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Verified != 1 {
		t.Errorf("a dry run still measures: verified = %d, want 1", res.Verified)
	}
	tasks, _ := st.ListTasks("", "")
	if len(tasks) != 0 {
		t.Errorf("a dry run wrote %d task(s)", len(tasks))
	}
	// And it leaves the watermark alone, so a real run still covers this.
	res2, _ := extract.Run(context.Background(),
		extract.Deps{Store: st, Extractor: model, Loc: riyadh(t), DryRun: true},
		extract.RunSpec{ChatJID: chat}, nil)
	if res2.Verified != 1 {
		t.Errorf("a dry run must not move the watermark")
	}
}

// TestDoneReplyClosesTaskWithoutAModel — a reply to a task's own origin saying
// "تم" admits no doubt, and spending a model call on it would only introduce
// some.
func TestDoneReplyClosesTaskWithoutAModel(t *testing.T) {
	st := newStore(t)
	chat := "team@g.us"
	now := seedChat(t, st, chat)

	task, err := st.CreateTask(&db.Task{
		Title: "جهز العقد", OriginChatJID: chat, OriginMessageID: "REQ",
		ReviewStatus: db.ReviewAccepted,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := st.StoreMessage(&db.Message{
		ID: "DONE", ChatJID: chat, Sender: "966500000001@s.whatsapp.net",
		Content: "تم ✅", ReplyToID: "REQ", Timestamp: now + 60,
	}); err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}

	model := &fakeModel{}
	res, err := extract.Run(context.Background(),
		extract.Deps{Store: st, Extractor: model, Loc: riyadh(t)},
		extract.RunSpec{ChatJID: chat, Since: now, RunID: "run-1"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Completions != 1 {
		t.Fatalf("the done reply should close the task, got %d completions", res.Completions)
	}

	got, err := st.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != "done" {
		t.Errorf("status = %q, want done", got.Status)
	}
}
