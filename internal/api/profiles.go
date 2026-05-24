package api

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"whatsapp-bridge-v2/internal/db"
)

// Profile generation is done by a background worker that summarizes each entity
// (group, DM, circle) with the Claude Agent SDK. Chats with no content get an
// instant stub (no model call). Profiles refresh on a 7-working-day cadence.

const (
	profileMsgSample = 80 // recent messages fed to the summarizer
	profileMaxMsgLen = 280 // per-message cap (chars)
	profileQueueCap  = 12000
)

type profileJob struct {
	entityType string
	ref        string
}

// ProfileManager runs the background profiling worker.
type ProfileManager struct {
	s       *Server
	jobs    chan profileJob
	mu      sync.Mutex
	inQueue map[string]bool
	active  string // entity currently being generated, for status display
}

func newProfileManager(s *Server) *ProfileManager {
	return &ProfileManager{s: s, jobs: make(chan profileJob, profileQueueCap), inQueue: map[string]bool{}}
}

// profilesEnabledKey gates the bulk runs. Profiling only auto-scans once the
// user has explicitly enabled it (so a restart never silently spends quota).
const profilesEnabledKey = "profiles_enabled"

func (m *ProfileManager) enabled() bool {
	v, _, _ := m.s.store.GetSyncState(profilesEnabledKey)
	return v == "1"
}

// Enable turns on profiling and kicks an immediate scan.
func (m *ProfileManager) Enable() {
	m.s.store.PutSyncState(profilesEnabledKey, "1")
	go m.EnqueueStale()
}

// Start launches the worker and the daily refresh scan. The worker always runs
// (to service manual regenerate requests); the daily auto-scan only enqueues
// once profiling has been enabled.
func (m *ProfileManager) Start() {
	go m.worker()
	go func() {
		time.Sleep(20 * time.Second)
		if m.enabled() {
			m.EnqueueStale()
		}
		t := time.NewTicker(24 * time.Hour)
		for range t.C {
			if m.enabled() {
				m.EnqueueStale()
			}
		}
	}()
}

func key(entityType, ref string) string { return entityType + ":" + ref }

// enqueue adds a job unless it is already queued.
func (m *ProfileManager) enqueue(entityType, ref string) bool {
	m.mu.Lock()
	k := key(entityType, ref)
	if m.inQueue[k] {
		m.mu.Unlock()
		return false
	}
	m.inQueue[k] = true
	m.mu.Unlock()
	select {
	case m.jobs <- profileJob{entityType, ref}:
		return true
	default:
		m.mu.Lock()
		delete(m.inQueue, k)
		m.mu.Unlock()
		return false
	}
}

func (m *ProfileManager) queued() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.inQueue)
}

// EnqueueStale decides, in bulk, what needs work. It loads message counts and
// existing profiles in two queries, then: chats with no messages are stubbed
// instantly in one transaction (no model, no queue); only chats with real
// content (and circles) that are missing or stale are queued for a model call.
// Manual profiles are left alone.
func (m *ProfileManager) EnqueueStale() {
	cutoff := db.ProfileStaleCutoff()
	store := m.s.store
	counts := store.ChatMessageCounts()
	existing := store.AllProfiles()

	var stubs []db.ProfileRef

	// consider a chat-like entity (group or DM) given its message count.
	consider := func(entityType, ref string) {
		p := existing[entityType+":"+ref]
		if counts[ref] == 0 {
			if p == nil { // empty and unseen → stub once, no model
				stubs = append(stubs, db.ProfileRef{Type: entityType, Ref: ref})
			}
			return
		}
		if p != nil && p.Source == "manual" {
			return
		}
		// Has content: generate if new, was empty before, pending, or time-stale.
		if p == nil || p.Status == db.ProfileEmpty || p.Status == db.ProfilePending || p.GeneratedAt < cutoff {
			m.enqueue(entityType, ref)
		}
	}

	if rows, err := store.DB.Query(`SELECT jid FROM groups`); err == nil {
		for rows.Next() {
			var jid string
			if rows.Scan(&jid) == nil {
				consider(db.ProfileGroup, jid)
			}
		}
		rows.Close()
	}
	if rows, err := store.DB.Query(`SELECT jid FROM contacts`); err == nil {
		for rows.Next() {
			var jid string
			if rows.Scan(&jid) == nil {
				consider(db.ProfileContact, jid)
			}
		}
		rows.Close()
	}

	// All empty chats in one transaction.
	store.StubEmptyProfiles(stubs)

	// Circles summarize from their members, so always queue stale/missing ones.
	if circles, err := store.ListCircles(); err == nil {
		for _, c := range circles {
			ref := strconv.FormatInt(c.ID, 10)
			p := existing["circle:"+ref]
			if p != nil && p.Source == "manual" {
				continue
			}
			if p == nil || p.Status == db.ProfilePending || p.GeneratedAt < cutoff {
				m.enqueue(db.ProfileCircle, ref)
			}
		}
	}
}

// RegenerateNow forces one entity to be (re)generated, bypassing freshness.
func (m *ProfileManager) RegenerateNow(entityType, ref string) {
	m.enqueue(entityType, ref)
}

func (m *ProfileManager) worker() {
	for job := range m.jobs {
		m.mu.Lock()
		m.active = key(job.entityType, job.ref)
		m.mu.Unlock()

		m.generate(job.entityType, job.ref)

		m.mu.Lock()
		delete(m.inQueue, key(job.entityType, job.ref))
		m.active = ""
		m.mu.Unlock()

		time.Sleep(500 * time.Millisecond) // be gentle on the subscription
	}
}

// generate builds the context for one entity, calls the summarizer (or stubs an
// empty chat), and stores the result.
func (m *ProfileManager) generate(entityType, ref string) {
	store := m.s.store
	context, msgCount, empty := m.buildContext(entityType, ref)
	if empty {
		store.SaveProfileResult(entityType, ref, "No conversation yet.", db.ProfileEmpty, "", msgCount)
		return
	}

	out, err := m.s.runAgentInput(3*time.Minute, context, "profile.mjs", entityType)
	var res struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if line := lastJSONLine(out); line != nil {
		json.Unmarshal(line, &res)
	}
	desc := strings.TrimSpace(res.Description)
	if err != nil && desc == "" {
		store.SaveProfileResult(entityType, ref, "", db.ProfileError, err.Error(), msgCount)
		return
	}
	if desc == "" || desc == "INSUFFICIENT" {
		store.SaveProfileResult(entityType, ref, "Not enough activity to summarize.", db.ProfileEmpty, "", msgCount)
		return
	}
	store.SaveProfileResult(entityType, ref, desc, db.ProfileOK, "", msgCount)
}

// buildContext assembles the text to summarize for an entity. Returns the
// context, the message count (for staleness), and whether it is empty (stub).
func (m *ProfileManager) buildContext(entityType, ref string) (string, int, bool) {
	store := m.s.store
	switch entityType {
	case db.ProfileGroup:
		var name, topic string
		var pcount int
		store.DB.QueryRow(`SELECT name, topic, participant_count FROM groups WHERE jid = ?`, ref).Scan(&name, &topic, &pcount)
		sample, n := m.messageSample(ref, true)
		if n == 0 {
			return "", 0, true
		}
		var b strings.Builder
		fmt.Fprintf(&b, "GROUP: %s\n", orDash(name))
		if topic != "" {
			fmt.Fprintf(&b, "Topic: %s\n", topic)
		}
		fmt.Fprintf(&b, "Participants: %d\nMessages stored: %d\n\nRecent messages (oldest→newest):\n%s", pcount, n, sample)
		return b.String(), n, false

	case db.ProfileContact:
		var name, push, biz string
		store.DB.QueryRow(`SELECT name, push_name, business_name FROM contacts WHERE jid = ?`, ref).Scan(&name, &push, &biz)
		sample, n := m.messageSample(ref, false)
		if n == 0 {
			return "", 0, true
		}
		label := firstNonEmpty(name, biz, push, ref)
		var b strings.Builder
		fmt.Fprintf(&b, "DIRECT CHAT with: %s\nMessages stored: %d\n\nRecent messages (oldest→newest, \"Me\" = the account owner):\n%s", label, n, sample)
		return b.String(), n, false

	case db.ProfileCircle:
		id, _ := strconv.ParseInt(ref, 10, 64)
		return m.circleContext(id)
	}
	return "", 0, true
}

// messageSample returns up to profileMsgSample recent non-empty messages for a
// chat, oldest→newest, with sender labels, plus the total stored count.
func (m *ProfileManager) messageSample(jid string, group bool) (string, int) {
	store := m.s.store
	total := store.ChatMessageCount(jid)
	if total == 0 {
		return "", 0
	}
	rows, err := store.DB.Query(`SELECT sender_name, push_name, is_from_me, content, media_type
		FROM messages WHERE chat_jid = ? AND (content != '' OR media_type != '')
		ORDER BY timestamp DESC LIMIT ?`, jid, profileMsgSample)
	if err != nil {
		return "", total
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var sname, pname, content, media string
		var fromMe bool
		if rows.Scan(&sname, &pname, &fromMe, &content, &media) != nil {
			continue
		}
		who := "Me"
		if !fromMe {
			who = firstNonEmpty(sname, pname, "Someone")
		}
		text := strings.TrimSpace(content)
		if text == "" && media != "" {
			text = "[" + media + "]"
		}
		if text == "" {
			continue
		}
		if len(text) > profileMaxMsgLen {
			text = text[:profileMaxMsgLen] + "…"
		}
		text = strings.ReplaceAll(text, "\n", " ")
		lines = append(lines, who+": "+text)
	}
	_ = group
	// rows came newest→oldest; reverse to chronological
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	if len(lines) == 0 {
		return "", total
	}
	return strings.Join(lines, "\n"), total
}

// circleContext builds a circle's summary input from its metadata and the
// (already-generated) profiles of its direct members and sub-circles.
func (m *ProfileManager) circleContext(id int64) (string, int, bool) {
	store := m.s.store
	c, err := store.GetCircle(id)
	if err != nil || c == nil {
		return "", 0, true
	}
	members, _ := store.GetCircleMembers(id)
	var b strings.Builder
	fmt.Fprintf(&b, "CIRCLE: %s\n", c.Name)
	if len(c.Keywords) > 0 {
		fmt.Fprintf(&b, "Keywords: %s\n", strings.Join(c.Keywords, ", "))
	}
	if c.Notes != "" {
		fmt.Fprintf(&b, "Notes: %s\n", c.Notes)
	}

	var groups, contacts, subs []string
	for _, mem := range members {
		switch mem.MemberType {
		case db.MemberGroup:
			name := groupName(store, mem.MemberRef)
			desc := profileLine(store, db.ProfileGroup, mem.MemberRef)
			groups = append(groups, bullet(name, desc))
		case db.MemberContact:
			name := contactName(store, mem.MemberRef)
			desc := profileLine(store, db.ProfileContact, mem.MemberRef)
			contacts = append(contacts, bullet(name, desc))
		case db.MemberCircle:
			cid, _ := strconv.ParseInt(mem.MemberRef, 10, 64)
			if sc, _ := store.GetCircle(cid); sc != nil {
				desc := profileLine(store, db.ProfileCircle, mem.MemberRef)
				subs = append(subs, bullet(sc.Name, desc))
			}
		}
	}
	if len(subs) > 0 {
		fmt.Fprintf(&b, "\nSub-circles:\n%s\n", strings.Join(subs, "\n"))
	}
	if len(groups) > 0 {
		// cap the lists so a huge circle doesn't blow the prompt
		fmt.Fprintf(&b, "\nGroups (%d):\n%s\n", len(groups), strings.Join(capLines(groups, 40), "\n"))
	}
	if len(contacts) > 0 {
		fmt.Fprintf(&b, "\nKey contacts (%d total):\n%s\n", len(contacts), strings.Join(capLines(contacts, 40), "\n"))
	}
	if len(members) == 0 {
		return "", 0, true
	}
	return b.String(), len(members), false
}

// --- small helpers ---

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(unnamed)"
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func bullet(name, desc string) string {
	if desc == "" {
		return "- " + name
	}
	return "- " + name + ": " + desc
}

func capLines(lines []string, max int) []string {
	if len(lines) <= max {
		return lines
	}
	out := append([]string{}, lines[:max]...)
	return append(out, fmt.Sprintf("… and %d more", len(lines)-max))
}

func groupName(store *db.Store, jid string) string {
	var name string
	store.DB.QueryRow(`SELECT name FROM groups WHERE jid = ?`, jid).Scan(&name)
	return firstNonEmpty(name, jid)
}

func contactName(store *db.Store, jid string) string {
	var name, push, biz string
	store.DB.QueryRow(`SELECT name, push_name, business_name FROM contacts WHERE jid = ?`, jid).Scan(&name, &push, &biz)
	return firstNonEmpty(name, biz, push, jid)
}

func profileLine(store *db.Store, entityType, ref string) string {
	p, _ := store.GetProfile(entityType, ref)
	if p == nil || p.Status == db.ProfileEmpty || p.Status == db.ProfilePending {
		return ""
	}
	d := strings.ReplaceAll(p.Description, "\n", " ")
	if len(d) > 200 {
		d = d[:200] + "…"
	}
	return d
}
