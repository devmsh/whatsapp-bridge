package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"whatsapp-bridge-v2/internal/db"
	"whatsapp-bridge-v2/internal/extract"
)

// Most tests here never drive an actual model: RefreshChatMeetings only calls
// the engine when a chat actually has a meeting due a re-read, and even the
// two tests that set one up use fakeMeetingUpdater instead of a real engine.
// What the model itself decides is covered in internal/extract
// (meetings_update_test.go); these tests check the plumbing around that
// call — the model slot, fairness, request handling, and the endpoint's
// response shapes.

// newTestMeetingRefresher builds a MeetingRefresher, the scanner it must
// coordinate with, and the shared model slot, backed by a throwaway store.
func newTestMeetingRefresher(t *testing.T) (*MeetingRefresher, *Server, *db.Store) {
	t.Helper()
	st, err := db.NewStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s := &Server{store: st, runs: newRunManager(st), meetingModel: &meetingModelSlot{}}
	s.meetingScan = newMeetingScanner(s)
	s.meetingRefresh = newMeetingRefresher(s)
	return s.meetingRefresh, s, st
}

// waitForRefreshIdle polls until the refresher is no longer running, so a
// test does not return (and the store does not close) while its goroutine
// is still mid-run.
func waitForRefreshIdle(t *testing.T, m *MeetingRefresher) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !m.busy() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("refresh did not finish in time")
}

// fakeMeetingUpdater lets a refresh run end to end without a real model —
// these tests only need to prove the plumbing works, not what a model
// decides.
type fakeMeetingUpdater struct{}

func (f *fakeMeetingUpdater) Name() string { return "fake:api-test" }

func (f *fakeMeetingUpdater) UpdateMeetings(_ context.Context, _ extract.MeetingUpdateInput) (extract.MeetingUpdateOutput, error) {
	return extract.MeetingUpdateOutput{}, nil
}

// --- the model slot ---------------------------------------------------

func TestMeetingModelSlotClaimAndRelease(t *testing.T) {
	var slot meetingModelSlot
	if !slot.claim("finder") {
		t.Fatal("first claim should succeed")
	}
	if slot.claim("refresh") {
		t.Fatal("second claim should fail while another owner holds the slot")
	}
	if got := slot.heldBy(); got != "finder" {
		t.Fatalf("heldBy = %q, want finder", got)
	}

	// A release from the wrong owner must not free it.
	slot.release("refresh")
	if got := slot.heldBy(); got != "finder" {
		t.Fatalf("release by a non-owner freed the slot, heldBy = %q", got)
	}

	slot.release("finder")
	if got := slot.heldBy(); got != "" {
		t.Fatalf("release by the real owner should free the slot, heldBy = %q", got)
	}
	if !slot.claim("refresh") {
		t.Fatal("claim should succeed once the slot is free again")
	}
}

// --- refusal rules, through the slot, not a forced flag ----------------

func TestMeetingRefreshRunRefusesWhenSlotTaken(t *testing.T) {
	m, s, _ := newTestMeetingRefresher(t)
	if !s.meetingModel.claim("finder") {
		t.Fatal("setup: claim should succeed")
	}
	if run, ok := m.run("manual", nil); ok || run != nil {
		t.Fatal("run() should refuse while the model slot is taken")
	}
}

func TestMeetingScannerStartRunRefusesWhenSlotTaken(t *testing.T) {
	_, s, _ := newTestMeetingRefresher(t)
	if !s.meetingModel.claim("refresh") {
		t.Fatal("setup: claim should succeed")
	}
	if run := s.meetingScan.startRun("120363000000000099@g.us", "label", "manual"); run != nil {
		t.Fatal("startRun should refuse while the model slot is taken")
	}
}

// TestMeetingRefreshRunEmptyChatsFinishes exercises the real doRun path with
// zero chats: the engine is built (local, no network call at construction)
// but there is nothing to refresh, so it finishes immediately without ever
// reaching the model. Also checks the slot is released afterwards.
func TestMeetingRefreshRunEmptyChatsFinishes(t *testing.T) {
	m, s, _ := newTestMeetingRefresher(t)
	run, ok := m.run("manual", nil)
	if !ok || run == nil {
		t.Fatal("run() should accept when nothing else is going")
	}
	waitForRefreshIdle(t, m)
	if run.Status != RunDone {
		t.Fatalf("expected RunDone, got %s (error=%q)", run.Status, run.Error)
	}
	if got := s.meetingModel.heldBy(); got != "" {
		t.Fatalf("model slot still held after the run finished: %q", got)
	}
}

// TestMeetingRefreshDoRunRecoversFromPanic checks the defer in doRun frees
// the slot and marks the run failed even when something inside it panics —
// a stuck slot would lock out every future meeting job for good.
func TestMeetingRefreshDoRunRecoversFromPanic(t *testing.T) {
	m, s, _ := newTestMeetingRefresher(t)
	m.updaterFn = func() (extract.MeetingUpdater, error) { panic("boom") }

	run, ok := m.run("manual", nil)
	if !ok || run == nil {
		t.Fatal("run() should accept when nothing else is going")
	}
	waitForRefreshIdle(t, m)
	if run.Status != RunFailed {
		t.Fatalf("expected RunFailed after a panic, got %s", run.Status)
	}
	if got := s.meetingModel.heldBy(); got != "" {
		t.Fatalf("model slot still held after a panic: %q", got)
	}
}

// --- scanner fairness ---------------------------------------------------

func TestMeetingScannerRefreshTurnFairness(t *testing.T) {
	_, s, _ := newTestMeetingRefresher(t)
	sc := s.meetingScan

	if !sc.refreshTurn() {
		t.Fatal("first turn should go to the refresh")
	}
	sc.markRefreshStarted()
	if !sc.refreshTurn() {
		t.Fatal("second consecutive turn should still go to the refresh")
	}
	sc.markRefreshStarted()
	if sc.refreshTurn() {
		t.Fatal("third consecutive turn should be forced to the finder")
	}
	// Forcing the finder's turn resets the streak, so refresh gets a turn
	// again on the next tick.
	if !sc.refreshTurn() {
		t.Fatal("streak should have reset after the forced finder turn")
	}
}

func TestMeetingScannerRefreshTurnResetsWhenNothingDue(t *testing.T) {
	_, s, _ := newTestMeetingRefresher(t)
	sc := s.meetingScan

	sc.markRefreshStarted()
	sc.markRefreshStarted() // streak = 2, would force the finder next time
	sc.markNothingToRefresh()
	if !sc.refreshTurn() {
		t.Fatal("a tick with nothing due should reset the streak")
	}
}

// --- GET /api/v2/meetings/refresh ---------------------------------------

func TestHandleMeetingRefreshGetEmptyStore(t *testing.T) {
	_, s, _ := newTestMeetingRefresher(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/meetings/refresh", nil)
	w := httptest.NewRecorder()
	s.handleMeetingRefresh(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var body struct {
		Running      bool `json:"running"`
		WaitingChats int  `json:"waiting_chats"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Running {
		t.Fatal("running should be false on an idle refresher")
	}
	if body.WaitingChats != 0 {
		t.Fatalf("waiting_chats = %d, want 0 on an empty store", body.WaitingChats)
	}
}

// TestHandleMeetingRefreshGetAlwaysHasFinishedAt checks the API contract:
// "last.finished_at" is a fixed field, always present, even before any run.
func TestHandleMeetingRefreshGetAlwaysHasFinishedAt(t *testing.T) {
	_, s, _ := newTestMeetingRefresher(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/meetings/refresh", nil)
	w := httptest.NewRecorder()
	s.handleMeetingRefresh(w, req)

	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	last, ok := raw["last"].(map[string]any)
	if !ok {
		t.Fatalf("last should be an object, got %v", raw["last"])
	}
	if _, ok := last["finished_at"]; !ok {
		t.Fatal("finished_at must always be present, even before any run")
	}
}

// --- POST /api/v2/meetings/refresh: validation --------------------------

func TestHandleMeetingRefreshPostRequiresOneField(t *testing.T) {
	_, s, _ := newTestMeetingRefresher(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/meetings/refresh", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	s.handleMeetingRefresh(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
}

func TestHandleMeetingRefreshPostChatExcluded(t *testing.T) {
	_, s, st := newTestMeetingRefresher(t)
	const jid = "120363000000000009@g.us"
	if err := st.StoreChat(&db.Chat{JID: jid, Name: jid, ChatType: "group", IsArchived: true}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	body := `{"chat_jid":"` + jid + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/meetings/refresh", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleMeetingRefresh(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", w.Code, w.Body.String())
	}
}

func TestHandleMeetingRefreshPostMeetingNotFound(t *testing.T) {
	_, s, _ := newTestMeetingRefresher(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/meetings/refresh", strings.NewReader(`{"meeting_id":999}`))
	w := httptest.NewRecorder()
	s.handleMeetingRefresh(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", w.Code, w.Body.String())
	}
}

// TestHandleMeetingRefreshPost409WhenSlotTaken drives the refusal through
// the real model slot, not a hand-set flag — this is what actually stops a
// second run from starting.
func TestHandleMeetingRefreshPost409WhenSlotTaken(t *testing.T) {
	_, s, _ := newTestMeetingRefresher(t)
	if !s.meetingModel.claim("finder") {
		t.Fatal("setup: claim should succeed")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v2/meetings/refresh", strings.NewReader(`{"all":true}`))
	w := httptest.NewRecorder()
	s.handleMeetingRefresh(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", w.Code, w.Body.String())
	}
}

func TestHandleMeetingRefreshMethodNotAllowed(t *testing.T) {
	_, s, _ := newTestMeetingRefresher(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v2/meetings/refresh", nil)
	w := httptest.NewRecorder()
	s.handleMeetingRefresh(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405; body = %s", w.Code, w.Body.String())
	}
}

// --- POST /api/v2/meetings/refresh: success, with a real follow-up ------

// seedFollowUpMeeting creates one open meeting in chatJID, with a real
// message linked as its origin and a newer message after it — exactly the
// shape MeetingFollowUpsForChat looks for, so the chat is genuinely due a
// re-read (not just accepted blindly by the endpoint).
func seedFollowUpMeeting(t *testing.T, st *db.Store, chatJID string) *db.Meeting {
	t.Helper()
	if err := st.StoreChat(&db.Chat{JID: chatJID, Name: chatJID, ChatType: "group"}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	now := time.Now().Unix()
	if err := st.StoreMessage(&db.Message{
		ID: "origin", ChatJID: chatJID, Content: "نرتب اجتماع الخميس",
		Timestamp: now - 3600, MessageType: "text",
	}); err != nil {
		t.Fatalf("StoreMessage origin: %v", err)
	}
	mt, err := st.CreateMeeting(&db.Meeting{
		Title: "Team sync", Status: db.MeetingProposed,
		OriginChatJID: chatJID, OriginMessageID: "origin",
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	if err := st.LinkMeetingMessage(mt.ID, chatJID, "origin", "scheduling"); err != nil {
		t.Fatalf("LinkMeetingMessage: %v", err)
	}
	// A message newer than the meeting's last check is what makes it due.
	if err := st.StoreMessage(&db.Message{
		ID: "later", ChatJID: chatJID, Content: "تمام نلتقي وقتها",
		Timestamp: now - 60, MessageType: "text",
	}); err != nil {
		t.Fatalf("StoreMessage later: %v", err)
	}
	return mt
}

func TestHandleMeetingRefreshPostSuccessBody(t *testing.T) {
	m, s, st := newTestMeetingRefresher(t)
	m.updaterFn = func() (extract.MeetingUpdater, error) { return &fakeMeetingUpdater{}, nil }
	seedFollowUpMeeting(t, st, "120363000000000010@g.us")

	req := httptest.NewRequest(http.MethodPost, "/api/v2/meetings/refresh", strings.NewReader(`{"all":true}`))
	w := httptest.NewRecorder()
	s.handleMeetingRefresh(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var body struct {
		RunID string `json:"run_id"`
		Chats int    `json:"chats"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.RunID == "" {
		t.Fatal("run_id should not be empty")
	}
	if body.Chats != 1 {
		t.Fatalf("chats = %d, want 1 (the seeded chat has a real follow-up due)", body.Chats)
	}
	waitForRefreshIdle(t, m)
}

// --- POST /api/v2/meetings/refresh: meeting_id ---------------------------

func TestHandleMeetingRefreshPostMeetingIDHappyPath(t *testing.T) {
	m, s, st := newTestMeetingRefresher(t)
	m.updaterFn = func() (extract.MeetingUpdater, error) { return &fakeMeetingUpdater{}, nil }
	mt := seedFollowUpMeeting(t, st, "120363000000000011@g.us")

	req := httptest.NewRequest(http.MethodPost, "/api/v2/meetings/refresh",
		strings.NewReader(fmt.Sprintf(`{"meeting_id":%d}`, mt.ID)))
	w := httptest.NewRecorder()
	s.handleMeetingRefresh(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var body struct {
		RunID string `json:"run_id"`
		Chats int    `json:"chats"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Chats != 1 {
		t.Fatalf("chats = %d, want 1", body.Chats)
	}
	waitForRefreshIdle(t, m)
}

func TestHandleMeetingRefreshPostMeetingIDAllChatsExcluded(t *testing.T) {
	_, s, st := newTestMeetingRefresher(t)
	const jid = "120363000000000012@g.us"
	if err := st.StoreChat(&db.Chat{JID: jid, Name: jid, ChatType: "group", IsArchived: true}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	mt, err := st.CreateMeeting(&db.Meeting{Title: "Old kickoff", Status: db.MeetingProposed})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	if err := st.LinkMeetingMessage(mt.ID, jid, "origin", "scheduling"); err != nil {
		t.Fatalf("LinkMeetingMessage: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v2/meetings/refresh",
		strings.NewReader(fmt.Sprintf(`{"meeting_id":%d}`, mt.ID)))
	w := httptest.NewRecorder()
	s.handleMeetingRefresh(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", w.Code, w.Body.String())
	}
}
