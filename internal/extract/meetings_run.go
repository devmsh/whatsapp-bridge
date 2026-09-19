package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"whatsapp-bridge-v2/internal/db"
	"whatsapp-bridge-v2/internal/llmlog"
)

// The meetings run: read, chunk, ask, check, write.
//
// The same shape as Run() for tasks, and the same rule about the watermark: it
// moves per chunk, only after that chunk has been written. A run that dies
// half way costs nothing but the work it already did.

// MeetingDeps is what a meetings run needs.
type MeetingDeps struct {
	Store  *db.Store
	Finder MeetingFinder
	Loc    *time.Location
	DryRun bool
}

// MeetingResult is what a run did.
type MeetingResult struct {
	Chunks   int
	Skipped  int
	Proposed int
	Verified int
	Rejected int
	// Attached counts proposals that turned out to be a meeting already known,
	// where the new messages were linked to it instead of making a second one.
	Attached int
	Meetings []db.Meeting
	// Outcomes is every proposal and what became of it. A dry run writes
	// nothing, so this is the only way to see what the model actually
	// answered — which is what the eval command prints.
	Outcomes []MeetingOutcome
	// Rejections says why things were dropped, keyed by reason.
	Rejections map[MeetingReject]int
	// LastMsgTS is the newest message the run actually read, so the caller can
	// move the watermark even when nothing was found.
	LastMsgTS    int64
	FailedChunks int
	LastError    string
	// CappedOut says the run hit MaxMeetingsPerRun and stopped writing. The
	// remaining proposals are not lost — the watermark still moves, so they
	// are simply not recorded, and the summary says so.
	CappedOut bool
}

// MeetingOutcome is one proposal and the decision made about it.
type MeetingOutcome struct {
	ChunkIndex int
	Proposal   ProposedMeeting
	// EvidenceID is the message it was finally anchored to, which can differ
	// from what the model said when the quote was found elsewhere.
	EvidenceID string
	// StartsAt is the date code worked out from the words, 0 when none.
	StartsAt int64
	Kept     bool
	// Reason is empty when Kept. AttachedTo names the meeting it joined when
	// the proposal turned out to be one already known.
	Reason     MeetingReject
	AttachedTo int64
}

// meetingDedupeWindow is how far apart two meetings can be and still be the
// same one. Two days: a meeting discussed on Sunday and again on Monday for
// "الخميس" is one meeting, but next week's weekly is a different one.
const meetingDedupeWindow = 2 * 24 * 3600

// sameMeetingTitle is how close two titles must be to be the same meeting.
// Higher than the tasks threshold — meetings are named more loosely
// ("اجتماع", "لقاء الفريق") and a low bar merges two real ones.
const sameMeetingTitle = 0.7

// FirstLook is how far back a chat that has never been scanned is read.
//
// Without a floor, "no watermark" means "since the beginning of time", and the
// first run on a four-year chat reads all of it. That is not a theory: the
// first live run read one busy conversation back to July and proposed 177
// meetings, of which 58 were written — six weeks of arrangements, nearly all
// of them long since over, dumped into the review queue at once.
//
// A week is what "what is being arranged right now" means. Older meetings are
// history, and history is a deliberate backfill with an explicit Since, not
// something a timer should do on its own the first time it runs.
const FirstLook = 7 * 24 * 3600

// MaxMeetingsPerRun is a safety valve, not a policy.
//
// One run should find a handful of meetings. A run that wants to write fifty
// is not having a busy week — something is wrong, with the prompt, the model
// or the window. Stopping at the cap keeps the review queue usable and puts
// the number in the run summary where somebody will see it.
const MaxMeetingsPerRun = 12

// RunMeetings finds the meetings in one chat.
func RunMeetings(ctx context.Context, d MeetingDeps, spec RunSpec, progress func(string)) (MeetingResult, error) {
	res := MeetingResult{Rejections: map[MeetingReject]int{}}
	say := func(format string, args ...any) {
		if progress != nil {
			progress(fmt.Sprintf(format, args...))
		}
	}
	if d.Loc == nil {
		d.Loc = time.UTC
	}
	if d.Finder == nil {
		return res, fmt.Errorf("this engine cannot find meetings")
	}
	if d.Store.IsChatExcludedFromAI(spec.ChatJID) {
		return res, fmt.Errorf("chat is hidden, archived or deleted")
	}

	since := spec.Since
	if since == 0 {
		since = d.Store.GetMeetingScanState(spec.ChatJID).LastMsgTS
	}
	until := spec.Until
	if until == 0 {
		until = time.Now().Unix()
	}
	// A chat nobody has scanned before starts a week ago, not at its first
	// message. Only when the caller gave no Since of its own — a deliberate
	// backfill says how far back it wants to go and means it.
	if bounded := firstLookFloor(spec.Since, since, time.Now().Unix()); bounded != since {
		since = bounded
		say("first run on this chat — reading the last 7 days only")
	}

	lines, err := FetchLines(d.Store, spec.ChatJID, since, until)
	if err != nil {
		return res, fmt.Errorf("read chat: %w", err)
	}
	if len(lines) == 0 {
		say("nothing new since the last meetings run")
		return res, nil
	}
	res.LastMsgTS = lines[len(lines)-1].TS

	people, ownName, isGroup, err := Roster(d.Store, spec.ChatJID)
	if err != nil {
		return res, fmt.Errorf("roster: %w", err)
	}
	chatName := chatDisplayName(d.Store, spec.ChatJID)
	today := time.Now().In(d.Loc).Format("2006-01-02")

	chunks := Split(lines, spec.ChatJID, DefaultChunkOptions(d.Loc))
	res.Chunks = len(chunks)
	say("%d new messages in %d chunk(s)", len(lines), len(chunks))

	// Titles kept across the whole run, so the same meeting discussed in two
	// chunks is reported once.
	var titles []string

	for _, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return res, err
		}

		// Most chat is not about meetings. Asking the model about a chunk with
		// no meeting word in it anywhere is a model call spent to be told no.
		if !chunkMentionsMeeting(chunk) {
			res.Skipped++
			continue
		}

		callCtx := llmlog.With(ctx, llmlog.Tag{
			RunID: spec.RunID, Service: "meetings", ChatJID: spec.ChatJID,
		})
		started := time.Now()
		out, err := d.Finder.FindMeetings(callCtx, MeetingInput{
			Chunk: chunk, Roster: people, OwnName: ownName,
			ChatName: chatName, IsGroup: isGroup, Today: today,
		})
		if err != nil {
			say("chunk %d failed: %v", chunk.Index, err)
			res.FailedChunks++
			res.LastError = err.Error()
			continue
		}

		kept := 0
		for _, p := range out.Meetings {
			res.Proposed++
			v, reason, ok := VerifyMeeting(chunk, p)
			if !ok {
				res.Rejected++
				res.Rejections[reason]++
				res.Outcomes = append(res.Outcomes, MeetingOutcome{
					ChunkIndex: chunk.Index, Proposal: p, Reason: reason,
					EvidenceID: v.Line.MessageID,
				})
				continue
			}
			if sameMeetingAlready(titles, v.Meeting.Title) {
				res.Rejections[MRejectSameEvent]++
				res.Outcomes = append(res.Outcomes, MeetingOutcome{
					ChunkIndex: chunk.Index, Proposal: p,
					EvidenceID: v.Line.MessageID, Reason: MRejectSameEvent,
				})
				continue
			}

			startsAt := ResolveMeetingStartInChunk(chunk, v, d.Loc)

			// Already known? The join code settles it outright; otherwise a
			// close title at a close time in a chat we already have.
			if existing := findExistingMeeting(d.Store, v, startsAt, spec.ChatJID); existing != nil {
				if !d.DryRun {
					_ = d.Store.LinkMeetingMessage(existing.ID, spec.ChatJID, v.Line.MessageID, "scheduling")
					if existing.Status == db.MeetingLapsed {
						// A lapsed meeting can come back (design decision 8):
						// a new scheduling message means it is being
						// arranged again, not that it is still dead.
						status := db.MeetingProposed
						_, _ = d.Store.ApplyMeetingPatch(existing.ID, db.MeetingPatch{
							Status: &status, Note: "It came up again in the chat.",
							Source: db.ChangeByModel, ChatJID: spec.ChatJID,
							MessageID: v.Line.MessageID, EvidenceTS: v.Line.TS, RunID: spec.RunID,
						})
					}
				}
				res.Attached++
				res.Rejections[MRejectDuplicate]++
				res.Outcomes = append(res.Outcomes, MeetingOutcome{
					ChunkIndex: chunk.Index, Proposal: p, EvidenceID: v.Line.MessageID,
					StartsAt: startsAt, Reason: MRejectDuplicate, AttachedTo: existing.ID,
				})
				titles = append(titles, v.Meeting.Title)
				continue
			}

			if res.Verified >= MaxMeetingsPerRun {
				res.CappedOut = true
				res.Rejections[MRejectTooMany]++
				res.Outcomes = append(res.Outcomes, MeetingOutcome{
					ChunkIndex: chunk.Index, Proposal: p, EvidenceID: v.Line.MessageID,
					StartsAt: startsAt, Reason: MRejectTooMany,
				})
				continue
			}

			titles = append(titles, v.Meeting.Title)
			res.Verified++
			kept++
			res.Outcomes = append(res.Outcomes, MeetingOutcome{
				ChunkIndex: chunk.Index, Proposal: p, EvidenceID: v.Line.MessageID,
				StartsAt: startsAt, Kept: true,
			})
			if d.DryRun {
				continue
			}
			m, err := PersistMeeting(d.Store, spec, v, startsAt, people, d.Finder.Name())
			if err != nil {
				say("could not save %q: %v", v.Meeting.Title, err)
				continue
			}
			res.Meetings = append(res.Meetings, *m)
		}
		say("chunk %d: %d proposed, %d kept", chunk.Index, len(out.Meetings), kept)

		if !d.DryRun {
			RecordCall(d.Store, RunMeta{RunID: spec.RunID, Engine: d.Finder.Name(),
				ChatJID: spec.ChatJID}, "meetings", chunk, time.Since(started),
				len(out.Meetings), kept, len(out.Meetings)-kept)
		}
	}

	return res, nil
}

// firstLookFloor decides where a run starts reading.
//
// askedFor is the caller's Since (0 means "use the watermark"), watermark is
// what the last run reached, and now is the clock. An explicit request is
// always honoured; otherwise a missing or ancient watermark is raised to
// FirstLook so the first run reads a week rather than a lifetime.
func firstLookFloor(askedFor, watermark, now int64) int64 {
	if askedFor > 0 {
		return askedFor
	}
	floor := now - FirstLook
	if watermark < floor {
		return floor
	}
	return watermark
}

// chunkMentionsMeeting is the same cheap filter the scanner uses, applied one
// level down. A chunk with no meeting word, no join link and no invite in any
// non-context line cannot contain a meeting worth a model call.
func chunkMentionsMeeting(c Chunk) bool {
	for _, l := range c.Lines {
		if l.Context {
			continue
		}
		if MentionsMeetingTalk(l.Text) {
			return true
		}
	}
	return false
}

func sameMeetingAlready(titles []string, title string) bool {
	for _, t := range titles {
		if TitleSimilarity(t, title) >= sameMeetingTitle {
			return true
		}
	}
	return false
}

// findExistingMeeting looks for the meeting this proposal is really about.
//
// The join code is proof and needs no judgement: the same Meet link in another
// chat is the same meeting, which is exactly the cross-chat case the old agent
// used a whole tool loop to find. Without a link, a close title at a close
// time in a chat already linked to the meeting is the best code can honestly
// do; anything looser would merge two real meetings.
func findExistingMeeting(store *db.Store, v VerifiedMeeting, startsAt int64, chatJID string) *db.Meeting {
	if code := firstMeetingCode(v.Meeting.Link, v.Line.Text); code != "" {
		if m, err := store.FindMeetingByCode(code); err == nil && m != nil {
			return m
		}
	}
	// This exact message already produced a meeting — a re-run over the same
	// window, which must not double up.
	if m := meetingFromMessage(store, chatJID, v.Line.MessageID); m != nil {
		return m
	}
	if startsAt == 0 {
		// With no time to compare, a title match alone is too weak. Two
		// undated "نجتمع قريب" threads a month apart are two meetings.
		return nil
	}
	candidates, err := store.MeetingsForChat(chatJID, 60)
	if err != nil {
		return nil
	}
	for i := range candidates {
		c := candidates[i]
		if c.StartsAt == 0 || abs64(c.StartsAt-startsAt) > meetingDedupeWindow {
			continue
		}
		if TitleSimilarity(c.Title, v.Meeting.Title) >= sameMeetingTitle {
			return &c
		}
	}
	return nil
}

func meetingFromMessage(store *db.Store, chatJID, messageID string) *db.Meeting {
	var id int64
	err := store.DB.QueryRow(`SELECT meeting_id FROM meeting_messages
		WHERE chat_jid = ? AND message_id = ? LIMIT 1`, chatJID, messageID).Scan(&id)
	if err != nil || id == 0 {
		return nil
	}
	m, err := store.GetMeeting(id)
	if err != nil {
		return nil
	}
	return m
}

// firstMeetingCode reads a join code out of the model's link field, falling
// back to the message text. The model copies links badly; the message does not.
func firstMeetingCode(link, text string) string {
	if code := db.MeetingLinkCode(link); code != "" {
		return code
	}
	return db.MeetingLinkCode(text)
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// PersistMeeting writes one verified meeting, its people and its agenda.
//
// Every meeting written here is pending review. Nothing an engine finds goes
// straight onto the calendar — the review queue is what makes an over-eager
// model safe, exactly as it is for tasks.
func PersistMeeting(store *db.Store, spec RunSpec, v VerifiedMeeting, startsAt int64,
	roster []RosterPerson, engine string) (*db.Meeting, error) {

	p := v.Meeting
	options, _ := json.Marshal(MeetingTimeOptions(p.WhenText, p.TimeText))

	link := strings.TrimSpace(p.Link)
	if link == "" {
		// The model often paraphrases a URL. The message has the real one.
		if code := db.MeetingLinkCode(v.Line.Text); code != "" {
			link = v.Line.Text
		}
	}

	m := &db.Meeting{
		Title:           strings.TrimSpace(p.Title),
		Purpose:         strings.TrimSpace(p.Purpose),
		Status:          meetingStatus(p, startsAt),
		StartsAt:        startsAt,
		TimeOptions:     string(options),
		Mode:            meetingMode(p),
		Location:        strings.TrimSpace(p.Location),
		Link:            link,
		Notes:           meetingNotes(p, startsAt, engine),
		Source:          "auto",
		OriginChatJID:   spec.ChatJID,
		OriginMessageID: v.Line.MessageID,
		Confidence:      p.Confidence,
		ReviewStatus:    db.ReviewPending,
	}
	created, err := store.CreateMeeting(m)
	if err != nil {
		return nil, err
	}

	// CreateMeeting links the origin message as "origin". Link it again under
	// the role that says what it actually was, so the meeting view shows the
	// message that arranged it.
	_ = store.LinkMeetingMessage(created.ID, spec.ChatJID, v.Line.MessageID, "scheduling")

	for _, part := range ResolveAttendees(p.Attendees, roster) {
		_ = store.AddMeetingParticipant(created.ID, part)
	}
	for _, text := range p.Agenda {
		if text = strings.TrimSpace(text); text != "" {
			_, _ = store.AddMeetingItem(&db.MeetingItem{
				MeetingID: created.ID, Kind: db.ItemAgenda, Text: text,
			})
		}
	}
	return created, nil
}

// meetingNotes keeps what could not become a field. When no date could be
// computed, the words that were said are the whole record of the timing, and
// losing them would leave a meeting that says only "sometime".
func meetingNotes(p ProposedMeeting, startsAt int64, engine string) string {
	var parts []string
	if startsAt == 0 {
		if when := strings.TrimSpace(p.WhenText); when != "" {
			parts = append(parts, "Timing as discussed: "+when)
		} else {
			parts = append(parts, "No date named yet.")
		}
	}
	parts = append(parts, "Found by "+engine+".")
	return strings.Join(parts, "\n")
}
