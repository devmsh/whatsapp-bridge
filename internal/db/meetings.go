package db

import (
	"strings"
	"time"
)

// Meeting status. A meeting is usually born "proposed" — a time is being
// argued about — and only becomes "confirmed" once someone pins it down.
const (
	MeetingProposed  = "proposed"
	MeetingConfirmed = "confirmed"
	MeetingHeld      = "held"
	MeetingCancelled = "cancelled"
)

// Kinds of meeting_items. They share a table because they share a shape: a
// line of text, an owner, and whether it is done.
const (
	ItemAgenda      = "agenda"
	ItemRequirement = "requirement"
	ItemNextStep    = "next_step"
)

// Meeting is a real meeting: something arranged in advance, with people,
// a purpose and a life before and after it. See schema.go for why this is not
// modelled as a task with a date.
type Meeting struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Purpose  string `json:"purpose,omitempty"`
	Status   string `json:"status"`
	StartsAt int64  `json:"starts_at,omitempty"`
	EndsAt   int64  `json:"ends_at,omitempty"`
	TZ       string `json:"tz,omitempty"`
	// TimeOptions holds the slots still being argued over, as a JSON array.
	TimeOptions       string  `json:"time_options,omitempty"`
	Mode              string  `json:"mode,omitempty"`
	Location          string  `json:"location,omitempty"`
	LocationURL       string  `json:"location_url,omitempty"`
	LocationLat       float64 `json:"location_lat,omitempty"`
	LocationLng       float64 `json:"location_lng,omitempty"`
	Link              string  `json:"link,omitempty"`
	LinkCode          string  `json:"link_code,omitempty"`
	Notes             string  `json:"notes,omitempty"`
	PreparesMeetingID *int64  `json:"prepares_meeting_id,omitempty"`
	Recurrence        string  `json:"recurrence,omitempty"`
	Source            string  `json:"source"`
	ExternalID        string  `json:"external_id,omitempty"`
	OriginChatJID     string  `json:"origin_chat_jid,omitempty"`
	OriginMessageID   string  `json:"origin_message_id,omitempty"`
	Confidence        float64 `json:"confidence,omitempty"`
	ReviewStatus      string  `json:"review_status"`
	CreatedAt         int64   `json:"created_at"`
	UpdatedAt         int64   `json:"updated_at"`

	// Loaded on demand by GetMeeting / ListMeetings.
	Participants []MeetingParticipant `json:"participants,omitempty"`
	Items        []MeetingItem        `json:"items,omitempty"`
	Messages     []MeetingMessage     `json:"messages,omitempty"`
	Circles      []Circle             `json:"circles,omitempty"`
}

type MeetingParticipant struct {
	JID     string `json:"jid"`
	Name    string `json:"name,omitempty"`
	Role    string `json:"role"`
	RSVP    string `json:"rsvp"`
	Note    string `json:"note,omitempty"`
	AddedAt int64  `json:"added_at,omitempty"`
}

type MeetingItem struct {
	ID        int64  `json:"id"`
	MeetingID int64  `json:"meeting_id"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	Done      bool   `json:"done"`
	OwnerJID  string `json:"owner_jid,omitempty"`
	TaskID    *int64 `json:"task_id,omitempty"`
	Position  int    `json:"position"`
	CreatedAt int64  `json:"created_at"`
}

type MeetingMessage struct {
	ChatJID   string `json:"chat_jid"`
	MessageID string `json:"message_id"`
	Role      string `json:"role"`
	AddedAt   int64  `json:"added_at,omitempty"`
}

// MeetingLinkCode pulls the join code out of a meeting URL. The code is what
// proves two chats are discussing ONE meeting: the same Meet link turns up in
// three separate DMs with no group anywhere, and the code is the only thing
// they have in common. Returns "" when the text carries no known link.
func MeetingLinkCode(text string) string {
	lower := strings.ToLower(text)
	if i := strings.Index(lower, "meet.google.com/"); i >= 0 {
		rest := lower[i+len("meet.google.com/"):]
		code := cutMeetingCode(rest)
		if len(code) >= 10 { // xxx-xxxx-xxx
			return code
		}
		return ""
	}
	if i := strings.Index(lower, "zoom.us/j/"); i >= 0 {
		code := cutMeetingCode(lower[i+len("zoom.us/j/"):])
		if code != "" {
			return "zoom:" + code
		}
	}
	return ""
}

// cutMeetingCode takes the code off the front of a URL tail, stopping at the
// first character that cannot be part of one.
func cutMeetingCode(s string) string {
	end := 0
	for end < len(s) {
		c := s[end]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			end++
			continue
		}
		break
	}
	return strings.Trim(s[:end], "-")
}

// MeetingMapLink pulls a Google Maps link out of message text.
//
// In the real chats this is almost always a maps.app.goo.gl short link — there
// is not a single long-form google.com/maps URL — so the short form is what
// must work. The link is kept whole and never resolved: expanding it would
// mean calling Google on the user's behalf, and the link opens correctly as it
// is. Returns "" when there is no maps link.
func MeetingMapLink(text string) string {
	for _, host := range []string{"maps.app.goo.gl/", "goo.gl/maps/", "google.com/maps", "maps.google.com"} {
		i := strings.Index(strings.ToLower(text), host)
		if i < 0 {
			continue
		}
		// Take everything up to the first space or newline: query strings like
		// ?g_st=iw are part of the link and must survive.
		tail := text[i:]
		if j := strings.IndexAny(tail, " \n\t\r"); j >= 0 {
			tail = tail[:j]
		}
		url := strings.TrimRight(tail, ".,;)")
		// Put the scheme back when the sender omitted it ("maps.app.goo.gl/x").
		if !strings.HasPrefix(strings.ToLower(url), "http") {
			url = "https://" + url
		}
		return url
	}
	return ""
}

// CreateMeeting inserts a meeting and links its origin message, mirroring how
// CreateTask behaves. A link in the text fills link_code automatically, since
// that is what later joins this meeting to the same one seen in another chat.
func (s *Store) CreateMeeting(m *Meeting) (*Meeting, error) {
	now := time.Now().Unix()
	m.CreatedAt, m.UpdatedAt = now, now
	if m.Status == "" {
		m.Status = MeetingProposed
	}
	if m.Source == "" {
		m.Source = "whatsapp"
	}
	if m.ReviewStatus == "" {
		m.ReviewStatus = ReviewAccepted // manual creates skip review
	}
	if m.LinkCode == "" && m.Link != "" {
		m.LinkCode = MeetingLinkCode(m.Link)
	}
	// A maps link pasted into the location text is the location URL.
	if m.LocationURL == "" && m.Location != "" {
		m.LocationURL = MeetingMapLink(m.Location)
	}
	res, err := s.DB.Exec(`INSERT INTO meetings
		(title, purpose, status, starts_at, ends_at, tz, time_options, mode, location,
		 location_url, location_lat, location_lng,
		 link, link_code, notes, prepares_meeting_id, recurrence, source, external_id,
		 origin_chat_jid, origin_message_id, confidence, review_status, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.Title, m.Purpose, m.Status, m.StartsAt, m.EndsAt, m.TZ, m.TimeOptions, m.Mode, m.Location,
		m.LocationURL, m.LocationLat, m.LocationLng,
		m.Link, m.LinkCode, m.Notes, m.PreparesMeetingID, m.Recurrence, m.Source, m.ExternalID,
		m.OriginChatJID, m.OriginMessageID, m.Confidence, m.ReviewStatus, m.CreatedAt, m.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	m.ID = id
	if m.OriginChatJID != "" && m.OriginMessageID != "" {
		_ = s.LinkMeetingMessage(id, m.OriginChatJID, m.OriginMessageID, "origin")
	}
	// Even with no origin message, the chat itself may place the meeting.
	_ = s.SyncMeetingCircles(id)
	return m, nil
}

// FindMeetingByCode returns the meeting already holding this join code, or nil.
// This is the de-duplication step: when the same link shows up in a second
// chat, it must attach to the existing meeting instead of making a new one.
func (s *Store) FindMeetingByCode(code string) (*Meeting, error) {
	if code == "" {
		return nil, nil
	}
	row := s.DB.QueryRow(`SELECT id FROM meetings WHERE link_code = ? ORDER BY id LIMIT 1`, code)
	var id int64
	if err := row.Scan(&id); err != nil {
		return nil, nil
	}
	return s.GetMeeting(id)
}

const meetingCols = `id, title, purpose, status, starts_at, ends_at, tz, time_options, mode,
	location, location_url, location_lat, location_lng,
	link, link_code, notes, prepares_meeting_id, recurrence, source, external_id,
	origin_chat_jid, origin_message_id, confidence, review_status, created_at, updated_at`

func scanMeeting(sc interface{ Scan(...any) error }) (*Meeting, error) {
	m := &Meeting{}
	err := sc.Scan(&m.ID, &m.Title, &m.Purpose, &m.Status, &m.StartsAt, &m.EndsAt, &m.TZ,
		&m.TimeOptions, &m.Mode, &m.Location, &m.LocationURL, &m.LocationLat, &m.LocationLng,
		&m.Link, &m.LinkCode, &m.Notes,
		&m.PreparesMeetingID, &m.Recurrence, &m.Source, &m.ExternalID,
		&m.OriginChatJID, &m.OriginMessageID, &m.Confidence, &m.ReviewStatus,
		&m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// GetMeeting returns one meeting with everything hanging off it.
func (s *Store) GetMeeting(id int64) (*Meeting, error) {
	m, err := scanMeeting(s.DB.QueryRow(`SELECT `+meetingCols+` FROM meetings WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	m.Participants, _ = s.MeetingParticipants(id)
	m.Items, _ = s.MeetingItems(id)
	m.Messages, _ = s.MeetingMessages(id)
	m.Circles, _ = s.MeetingCircles(id)
	return m, nil
}

// MeetingFilter narrows ListMeetings.
type MeetingFilter struct {
	Status       string
	ReviewStatus string
	// Upcoming limits to meetings that have not happened yet, plus any still
	// being negotiated (no agreed time), which are the ones needing attention.
	Upcoming bool
	Limit    int
}

// ListMeetings returns meetings newest-relevant first: the ones with a time
// ordered by that time, and the undated (still being argued about) first,
// because those are the ones waiting on a decision.
func (s *Store) ListMeetings(f MeetingFilter) ([]Meeting, error) {
	q := `SELECT ` + meetingCols + ` FROM meetings WHERE 1=1`
	args := []any{}
	if f.Status != "" {
		q += ` AND status = ?`
		args = append(args, f.Status)
	}
	if f.ReviewStatus != "" {
		q += ` AND review_status = ?`
		args = append(args, f.ReviewStatus)
	}
	if f.Upcoming {
		// A meeting you rejected is not ahead of you — it was never a meeting.
		q += ` AND review_status != 'rejected'
		       AND status NOT IN ('held','cancelled')
		       AND (starts_at = 0 OR starts_at >= ?)`
		args = append(args, time.Now().Unix()-12*3600)
	}
	q += ` ORDER BY (starts_at = 0) DESC, starts_at ASC, updated_at DESC`
	if f.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Meeting{}
	for rows.Next() {
		m, err := scanMeeting(rows)
		if err != nil {
			return out, err
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	// Fill the children in a second pass. The list view needs participants and
	// items to show "3 people · 2 requirements open" without another request.
	for i := range out {
		out[i].Participants, _ = s.MeetingParticipants(out[i].ID)
		out[i].Items, _ = s.MeetingItems(out[i].ID)
		out[i].Circles, _ = s.MeetingCircles(out[i].ID)
	}
	return out, nil
}

// MeetingsForChat returns the meetings this chat is involved in — either it
// carries part of the arrangement trail, or the person you are talking to is a
// participant. The second half matters because a meeting is often set up in
// one chat and then only mentioned in another.
//
// withinDays bounds it to meetings near today, which is what the in-chat bar
// wants: the next one coming up, or the one just held. Meetings with no agreed
// time are always included while they are still open, since those are exactly
// the ones the bar should nag about.
func (s *Store) MeetingsForChat(chatJID string, withinDays int) ([]Meeting, error) {
	now := time.Now().Unix()
	window := int64(withinDays) * 86400
	rows, err := s.DB.Query(`SELECT `+meetingCols+` FROM meetings m
		WHERE m.review_status != 'rejected'
		  AND (
		    EXISTS (SELECT 1 FROM meeting_messages mm
		             WHERE mm.meeting_id = m.id AND mm.chat_jid = ?)
		 OR EXISTS (SELECT 1 FROM meeting_participants mp
		             WHERE mp.meeting_id = m.id AND mp.jid = ?)
		  )
		  AND (
		    (m.starts_at > 0 AND m.starts_at BETWEEN ? AND ?)
		 OR (m.starts_at = 0 AND m.status NOT IN ('held','cancelled'))
		  )
		ORDER BY (m.starts_at = 0) DESC, ABS(m.starts_at - ?) ASC`,
		chatJID, chatJID, now-window, now+window, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Meeting{}
	for rows.Next() {
		m, err := scanMeeting(rows)
		if err != nil {
			return out, err
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	for i := range out {
		out[i].Participants, _ = s.MeetingParticipants(out[i].ID)
	}
	return out, nil
}

// SyncMeetingCircles recomputes which circles a meeting belongs to, from the
// chats it was arranged in and the people in it.
//
// A meeting is never filed by hand: it inherits the circles of its group chats
// and its participants. That is the useful direction — you already sorted the
// group into "OneStudio", so a meeting held there is OneStudio work without
// anyone saying so.
//
// The contact lookup goes through every identity form, because a person can be
// filed under their phone JID while the meeting names their @lid (or the
// reverse), and matching on one form alone would silently drop the circle.
//
// It replaces the set rather than adding to it, so removing a chat or a person
// from the meeting also drops a circle that no longer applies.
func (s *Store) SyncMeetingCircles(meetingID int64) error {
	rows, err := s.DB.Query(`
		SELECT DISTINCT cm.circle_id
		FROM circle_members cm
		WHERE
		  -- groups the meeting was arranged in
		  (cm.member_type = 'group' AND cm.member_ref IN (
		      SELECT chat_jid FROM meeting_messages WHERE meeting_id = ?1))
		  OR
		  -- people in the meeting, or whose DM carried it, in any identity form
		  (cm.member_type = 'contact' AND cm.member_ref IN (
		      SELECT p.jid FROM meeting_participants p WHERE p.meeting_id = ?1
		      UNION
		      SELECT mm.chat_jid FROM meeting_messages mm
		       WHERE mm.meeting_id = ?1 AND mm.chat_jid NOT LIKE '%@g.us'
		      UNION
		      SELECT c.jid FROM contacts c WHERE c.jid != '' AND (
		          c.jid IN (SELECT p.jid FROM meeting_participants p WHERE p.meeting_id = ?1)
		       OR c.lid IN (SELECT p.jid FROM meeting_participants p WHERE p.meeting_id = ?1))
		      UNION
		      SELECT c.lid FROM contacts c WHERE c.lid != '' AND (
		          c.jid IN (SELECT p.jid FROM meeting_participants p WHERE p.meeting_id = ?1)
		       OR c.lid IN (SELECT p.jid FROM meeting_participants p WHERE p.meeting_id = ?1))))`,
		meetingID)
	if err != nil {
		return err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if _, err := s.DB.Exec(`DELETE FROM meeting_circles WHERE meeting_id = ?`, meetingID); err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, id := range ids {
		if _, err := s.DB.Exec(`INSERT OR IGNORE INTO meeting_circles
			(meeting_id, circle_id, added_at) VALUES (?,?,?)`, meetingID, id, now); err != nil {
			return err
		}
	}
	return nil
}

// MeetingCircles returns the circles a meeting belongs to, newest first.
func (s *Store) MeetingCircles(meetingID int64) ([]Circle, error) {
	rows, err := s.DB.Query(`SELECT c.id, c.name, c.color
		FROM meeting_circles mc JOIN circles c ON c.id = mc.circle_id
		WHERE mc.meeting_id = ? ORDER BY c.name`, meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Circle{}
	for rows.Next() {
		var c Circle
		if rows.Scan(&c.ID, &c.Name, &c.Color) == nil {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

// UpcomingMeetingChatJIDs returns the chats that have a meeting still ahead:
// every chat that carries part of the arrangement, plus the direct chat of each
// person attending. The second half matters because a meeting is often agreed
// in one place and the people involved are reachable somewhere else entirely.
//
// "Upcoming" includes meetings with no agreed time yet, as long as they are
// still open — those are the ones needing a decision, so a chat list filtered
// to meetings should surface them rather than hide them.
func (s *Store) UpcomingMeetingChatJIDs() (map[string]bool, error) {
	now := time.Now().Unix()
	rows, err := s.DB.Query(`
		SELECT DISTINCT jid FROM (
		    SELECT mm.chat_jid AS jid
		      FROM meeting_messages mm JOIN meetings m ON m.id = mm.meeting_id
		     WHERE m.review_status != 'rejected'
		       AND m.status NOT IN ('held','cancelled')
		       AND (m.starts_at = 0 OR m.starts_at >= ?)
		    UNION
		    SELECT mp.jid AS jid
		      FROM meeting_participants mp JOIN meetings m ON m.id = mp.meeting_id
		     WHERE m.review_status != 'rejected'
		       AND m.status NOT IN ('held','cancelled')
		       AND (m.starts_at = 0 OR m.starts_at >= ?)
		)`, now-12*3600, now-12*3600)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var jid string
		if rows.Scan(&jid) == nil && jid != "" {
			out[jid] = true
		}
	}
	return out, rows.Err()
}

// UpdateMeeting writes the editable fields of a meeting.
func (s *Store) UpdateMeeting(m *Meeting) error {
	m.UpdatedAt = time.Now().Unix()
	if m.LinkCode == "" && m.Link != "" {
		m.LinkCode = MeetingLinkCode(m.Link)
	}
	if m.LocationURL == "" && m.Location != "" {
		m.LocationURL = MeetingMapLink(m.Location)
	}
	_, err := s.DB.Exec(`UPDATE meetings SET
		title=?, purpose=?, status=?, starts_at=?, ends_at=?, tz=?, time_options=?, mode=?,
		location=?, location_url=?, location_lat=?, location_lng=?,
		link=?, link_code=?, notes=?, prepares_meeting_id=?, recurrence=?,
		source=?, external_id=?, review_status=?, updated_at=?
		WHERE id=?`,
		m.Title, m.Purpose, m.Status, m.StartsAt, m.EndsAt, m.TZ, m.TimeOptions, m.Mode,
		m.Location, m.LocationURL, m.LocationLat, m.LocationLng,
		m.Link, m.LinkCode, m.Notes, m.PreparesMeetingID, m.Recurrence,
		m.Source, m.ExternalID, m.ReviewStatus, m.UpdatedAt, m.ID)
	return err
}

func (s *Store) DeleteMeeting(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM meetings WHERE id = ?`, id)
	return err
}

// SetMeetingReview accepts or rejects an AI-found meeting.
func (s *Store) SetMeetingReview(id int64, status string) error {
	_, err := s.DB.Exec(`UPDATE meetings SET review_status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now().Unix(), id)
	return err
}

// --- participants ------------------------------------------------------------

// AddMeetingParticipant adds or updates someone on the meeting. Re-adding the
// same person keeps whatever is already known and only fills the blanks, so a
// second mention in another chat cannot wipe an RSVP.
func (s *Store) AddMeetingParticipant(meetingID int64, p MeetingParticipant) error {
	if p.Role == "" {
		p.Role = "required"
	}
	if p.RSVP == "" {
		p.RSVP = "unknown"
	}
	_, err := s.DB.Exec(`INSERT INTO meeting_participants
		(meeting_id, jid, name, role, rsvp, note, added_at) VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(meeting_id, jid) DO UPDATE SET
			name = CASE WHEN excluded.name != '' THEN excluded.name ELSE meeting_participants.name END,
			role = CASE WHEN excluded.role != 'required' THEN excluded.role ELSE meeting_participants.role END,
			rsvp = CASE WHEN excluded.rsvp != 'unknown' THEN excluded.rsvp ELSE meeting_participants.rsvp END,
			note = CASE WHEN excluded.note != '' THEN excluded.note ELSE meeting_participants.note END`,
		meetingID, p.JID, p.Name, p.Role, p.RSVP, p.Note, time.Now().Unix())
	if err != nil {
		return err
	}
	// A person carries their circles into the meeting.
	return s.SyncMeetingCircles(meetingID)
}

func (s *Store) RemoveMeetingParticipant(meetingID int64, jid string) error {
	if _, err := s.DB.Exec(`DELETE FROM meeting_participants WHERE meeting_id = ? AND jid = ?`,
		meetingID, jid); err != nil {
		return err
	}
	return s.SyncMeetingCircles(meetingID)
}

func (s *Store) MeetingParticipants(meetingID int64) ([]MeetingParticipant, error) {
	rows, err := s.DB.Query(`SELECT jid, name, role, rsvp, note, added_at
		FROM meeting_participants WHERE meeting_id = ? ORDER BY role, name, jid`, meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MeetingParticipant{}
	for rows.Next() {
		var p MeetingParticipant
		if rows.Scan(&p.JID, &p.Name, &p.Role, &p.RSVP, &p.Note, &p.AddedAt) == nil {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

// --- the cross-chat message trail -------------------------------------------

func (s *Store) LinkMeetingMessage(meetingID int64, chatJID, messageID, role string) error {
	if role == "" {
		role = "related"
	}
	_, err := s.DB.Exec(`INSERT INTO meeting_messages
		(meeting_id, chat_jid, message_id, role, added_at) VALUES (?,?,?,?,?)
		ON CONFLICT(meeting_id, chat_jid, message_id) DO UPDATE SET role = excluded.role`,
		meetingID, chatJID, messageID, role, time.Now().Unix())
	if err != nil {
		return err
	}
	// A new chat in the trail can bring a new circle with it.
	return s.SyncMeetingCircles(meetingID)
}

func (s *Store) MeetingMessages(meetingID int64) ([]MeetingMessage, error) {
	rows, err := s.DB.Query(`SELECT chat_jid, message_id, role, added_at
		FROM meeting_messages WHERE meeting_id = ? ORDER BY added_at`, meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MeetingMessage{}
	for rows.Next() {
		var m MeetingMessage
		if rows.Scan(&m.ChatJID, &m.MessageID, &m.Role, &m.AddedAt) == nil {
			out = append(out, m)
		}
	}
	return out, rows.Err()
}

// MeetingChatJIDs lists the distinct chats a meeting was arranged across. More
// than one means the meeting really is cross-chat, which is the case a calendar
// cannot represent at all.
func (s *Store) MeetingChatJIDs(meetingID int64) ([]string, error) {
	rows, err := s.DB.Query(`SELECT DISTINCT chat_jid FROM meeting_messages
		WHERE meeting_id = ? ORDER BY chat_jid`, meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var j string
		if rows.Scan(&j) == nil {
			out = append(out, j)
		}
	}
	return out, rows.Err()
}

// --- agenda / requirements / next steps --------------------------------------

func (s *Store) AddMeetingItem(it *MeetingItem) (*MeetingItem, error) {
	it.CreatedAt = time.Now().Unix()
	if it.Position == 0 {
		s.DB.QueryRow(`SELECT COALESCE(MAX(position),0)+1 FROM meeting_items
			WHERE meeting_id = ? AND kind = ?`, it.MeetingID, it.Kind).Scan(&it.Position)
	}
	res, err := s.DB.Exec(`INSERT INTO meeting_items
		(meeting_id, kind, text, done, owner_jid, task_id, position, created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		it.MeetingID, it.Kind, it.Text, it.Done, it.OwnerJID, it.TaskID, it.Position, it.CreatedAt)
	if err != nil {
		return nil, err
	}
	it.ID, _ = res.LastInsertId()
	return it, nil
}

func (s *Store) UpdateMeetingItem(it *MeetingItem) error {
	_, err := s.DB.Exec(`UPDATE meeting_items SET text=?, done=?, owner_jid=?, task_id=?, position=?
		WHERE id=?`, it.Text, it.Done, it.OwnerJID, it.TaskID, it.Position, it.ID)
	return err
}

func (s *Store) DeleteMeetingItem(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM meeting_items WHERE id = ?`, id)
	return err
}

func (s *Store) MeetingItems(meetingID int64) ([]MeetingItem, error) {
	rows, err := s.DB.Query(`SELECT id, meeting_id, kind, text, done, owner_jid, task_id,
		position, created_at FROM meeting_items WHERE meeting_id = ?
		ORDER BY kind, position, id`, meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MeetingItem{}
	for rows.Next() {
		var it MeetingItem
		var done int
		if rows.Scan(&it.ID, &it.MeetingID, &it.Kind, &it.Text, &done, &it.OwnerJID,
			&it.TaskID, &it.Position, &it.CreatedAt) == nil {
			it.Done = done == 1
			out = append(out, it)
		}
	}
	return out, rows.Err()
}
