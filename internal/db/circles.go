package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Member type values for circle_members.member_type.
const (
	MemberGroup   = "group"
	MemberContact = "contact"
	MemberCircle  = "circle"
)

// Circle is a user-defined cluster of groups, contacts, and other circles.
type Circle struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Color       string   `json:"color"`
	Notes       string   `json:"notes,omitempty"`
	Keywords    []string `json:"keywords"` // saved terms that keep suggesting matching members
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
	MemberCount int      `json:"member_count"` // computed
}

// MemberSuggestion is a group/contact the keywords matched but isn't a member.
type MemberSuggestion struct {
	Type    string `json:"type"`
	Ref     string `json:"ref"`
	Label   string `json:"label"`
	Keyword string `json:"keyword"`
}

func parseKeywords(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	var kw []string
	if json.Unmarshal([]byte(s), &kw) != nil {
		return []string{}
	}
	return kw
}

func encodeKeywords(kw []string) string {
	if len(kw) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(kw)
	return string(b)
}

// CircleMember is one membership edge (a group/contact/circle in a circle).
type CircleMember struct {
	CircleID   int64  `json:"circle_id"`
	MemberType string `json:"member_type"`
	MemberRef  string `json:"member_ref"`
	AddedAt    int64  `json:"added_at"`
}

// ListCircles returns all circles with their direct member counts, by name.
func (s *Store) ListCircles() ([]Circle, error) {
	rows, err := s.DB.Query(`SELECT c.id, c.name, c.color, c.notes, c.keywords, c.created_at, c.updated_at,
		(SELECT COUNT(*) FROM circle_members m WHERE m.circle_id = c.id) AS cnt
		FROM circles c ORDER BY c.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Circle{}
	for rows.Next() {
		var c Circle
		var kw string
		if err := rows.Scan(&c.ID, &c.Name, &c.Color, &c.Notes, &kw, &c.CreatedAt, &c.UpdatedAt, &c.MemberCount); err != nil {
			return out, err
		}
		c.Keywords = parseKeywords(kw)
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCircle returns one circle by id.
func (s *Store) GetCircle(id int64) (*Circle, error) {
	c := &Circle{}
	var kw string
	err := s.DB.QueryRow(`SELECT c.id, c.name, c.color, c.notes, c.keywords, c.created_at, c.updated_at,
		(SELECT COUNT(*) FROM circle_members m WHERE m.circle_id = c.id) AS cnt
		FROM circles c WHERE c.id = ?`, id).
		Scan(&c.ID, &c.Name, &c.Color, &c.Notes, &kw, &c.CreatedAt, &c.UpdatedAt, &c.MemberCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	c.Keywords = parseKeywords(kw)
	return c, err
}

// CreateCircle inserts a new circle and returns it.
func (s *Store) CreateCircle(name, color, notes string) (*Circle, error) {
	now := time.Now().Unix()
	res, err := s.DB.Exec(`INSERT INTO circles (name, color, notes, created_at, updated_at)
		VALUES (?,?,?,?,?)`, name, color, notes, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Circle{ID: id, Name: name, Color: color, Notes: notes, CreatedAt: now, UpdatedAt: now}, nil
}

// UpdateCircle updates a circle's name/color/notes/keywords.
func (s *Store) UpdateCircle(id int64, name, color, notes string, keywords []string) error {
	_, err := s.DB.Exec(`UPDATE circles SET name = ?, color = ?, notes = ?, keywords = ?, updated_at = ? WHERE id = ?`,
		name, color, notes, encodeKeywords(keywords), time.Now().Unix(), id)
	return err
}

var nonAlnum = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// normKey lowercases and strips all non-alphanumeric characters, so "Neo Later",
// "neo-later", "NeoLater" and "neolater" all normalize to the same "neolater".
func normKey(s string) string {
	return strings.ToLower(nonAlnum.ReplaceAllString(s, ""))
}

// circleTerms returns a circle's match terms: its explicit keywords if set,
// otherwise its name used as an implicit keyword. Returns normalized forms and
// a human label.
func circleTerms(c *Circle) (norms []string, label string) {
	raw := c.Keywords
	if len(raw) == 0 {
		raw = []string{c.Name}
	}
	for _, t := range raw {
		if n := normKey(t); n != "" {
			norms = append(norms, n)
		}
	}
	return norms, strings.Join(raw, "/")
}

// parentCircles returns the ids of circles that directly contain circle id.
func (s *Store) parentCircles(id int64) []int64 {
	var out []int64
	rows, err := s.DB.Query(`SELECT circle_id FROM circle_members WHERE member_type = ? AND member_ref = ?`,
		MemberCircle, strconv.FormatInt(id, 10))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var pid int64
		if rows.Scan(&pid) == nil {
			out = append(out, pid)
		}
	}
	return out
}

// ancestorCircles returns all circles that transitively contain circle id,
// nearest parent first.
func (s *Store) ancestorCircles(id int64) []int64 {
	var out []int64
	seen := map[int64]bool{}
	queue := s.parentCircles(id)
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if seen[p] || p == id {
			continue
		}
		seen[p] = true
		out = append(out, p)
		queue = append(queue, s.parentCircles(p)...)
	}
	return out
}

// SuggestForCircle returns groups/contacts that match the circle's context and
// aren't already members. The context is the circle's own terms (its keywords,
// or its name if none) AND every ancestor circle's terms. So "Finance" nested
// under "Neo" suggests items whose name contains both "neo" and "finance".
// Matching is strict per term: the whole term must appear as a contiguous run
// after ignoring case and separators.
func (s *Store) SuggestForCircle(id int64) ([]MemberSuggestion, string, error) {
	c, err := s.GetCircle(id)
	if err != nil || c == nil {
		return []MemberSuggestion{}, "", err
	}

	// Build levels: a candidate must match ANY term within each level, and ALL
	// levels (AND across levels).
	//
	// Rule: an EXPLICIT keyword means exactly what it says, so we match only the
	// circle's own keywords and ignore ancestors. With NO keyword we fall back to
	// the circle's name and inherit ancestor context (the "smart" default for an
	// unconfigured sub-circle).
	type level struct{ norms []string }
	var levels []level
	var labels []string
	if len(c.Keywords) == 0 {
		ancestors := s.ancestorCircles(id)
		for i := len(ancestors) - 1; i >= 0; i-- {
			if a, _ := s.GetCircle(ancestors[i]); a != nil {
				if n, lbl := circleTerms(a); len(n) > 0 {
					levels = append(levels, level{n})
					labels = append(labels, lbl)
				}
			}
		}
	}
	if n, lbl := circleTerms(c); len(n) > 0 {
		levels = append(levels, level{n})
		labels = append(labels, lbl)
	}
	context := strings.Join(labels, " + ")
	if len(levels) == 0 {
		return []MemberSuggestion{}, context, nil
	}

	matchesAll := func(hay string) bool {
		for _, lv := range levels {
			ok := false
			for _, term := range lv.norms {
				if strings.Contains(hay, term) {
					ok = true
					break
				}
			}
			if !ok {
				return false
			}
		}
		return true
	}

	members, _ := s.GetCircleMembers(id)
	has := map[string]bool{}
	for _, m := range members {
		has[m.MemberType+":"+m.MemberRef] = true
	}

	out := []MemberSuggestion{}

	if grows, gerr := s.DB.Query(`SELECT jid, name FROM groups`); gerr == nil {
		for grows.Next() {
			var jid, name string
			if grows.Scan(&jid, &name) != nil {
				continue
			}
			if matchesAll(normKey(name)) {
				if key := "group:" + jid; !has[key] {
					out = append(out, MemberSuggestion{Type: "group", Ref: jid, Label: name, Keyword: context})
				}
			}
		}
		grows.Close()
	}

	if crows, cerr := s.DB.Query(`SELECT jid, phone, name, push_name, business_name FROM contacts`); cerr == nil {
		for crows.Next() {
			var jid, phone, name, push, biz string
			if crows.Scan(&jid, &phone, &name, &push, &biz) != nil {
				continue
			}
			hay := normKey(name) + "\x00" + normKey(push) + "\x00" + normKey(biz)
			if matchesAll(hay) {
				label := name
				if label == "" {
					label = biz
				}
				if label == "" {
					label = push
				}
				if label == "" {
					label = "+" + phone
				}
				if key := "contact:" + jid; !has[key] {
					out = append(out, MemberSuggestion{Type: "contact", Ref: jid, Label: label, Keyword: context})
				}
			}
		}
		crows.Close()
	}
	return out, context, nil
}

// addParticipantsAsContacts adds one group's participants as contact members of
// a circle, skipping the account owner and anything already in `has`. The `has`
// set is updated so callers can batch multiple groups without duplicates.
func (s *Store) addParticipantsAsContacts(circleID int64, groupJID, ownPhone string, has map[string]bool) int {
	rows, err := s.DB.Query(`SELECT jid, phone FROM group_participants WHERE group_jid = ?`, groupJID)
	if err != nil {
		return 0
	}
	defer rows.Close()
	ownRef := ""
	if ownPhone != "" {
		ownRef = ownPhone + "@s.whatsapp.net"
	}
	now := time.Now().Unix()
	added := 0
	for rows.Next() {
		var jid, phone string
		if rows.Scan(&jid, &phone) != nil {
			continue
		}
		ref := jid
		if phone != "" {
			ref = phone + "@s.whatsapp.net"
		}
		if ref == "" || ref == ownRef || has[ref] {
			continue
		}
		has[ref] = true
		if _, e := s.DB.Exec(`INSERT OR IGNORE INTO circle_members (circle_id, member_type, member_ref, added_at)
			VALUES (?,?,?,?)`, circleID, MemberContact, ref, now); e == nil {
			added++
		}
	}
	return added
}

// AddGroupParticipantsAsContacts auto-imports a single group's participants into
// a circle. Called whenever a group is added to a circle.
func (s *Store) AddGroupParticipantsAsContacts(circleID int64, groupJID, ownPhone string) int {
	members, _ := s.GetCircleMembers(circleID)
	has := map[string]bool{}
	for _, m := range members {
		if m.MemberType == MemberContact {
			has[m.MemberRef] = true
		}
	}
	return s.addParticipantsAsContacts(circleID, groupJID, ownPhone, has)
}

// BackfillGroupContacts imports participants for every group already present in
// any circle. Used once for groups added before auto-import existed.
func (s *Store) BackfillGroupContacts(ownPhone string) int {
	circles, _ := s.ListCircles()
	total := 0
	for _, c := range circles {
		members, _ := s.GetCircleMembers(c.ID)
		has := map[string]bool{}
		var groups []string
		for _, m := range members {
			switch m.MemberType {
			case MemberContact:
				has[m.MemberRef] = true
			case MemberGroup:
				groups = append(groups, m.MemberRef)
			}
		}
		for _, g := range groups {
			total += s.addParticipantsAsContacts(c.ID, g, ownPhone, has)
		}
	}
	return total
}

// CircleContact is a circle's contact member enriched with how many of the
// circle's groups they're in, whether they admin any, and their tags.
type CircleContact struct {
	JID        string `json:"jid"`
	GroupCount int    `json:"group_count"`
	IsAdmin    bool   `json:"is_admin"`
	Tags       []Tag  `json:"tags"`
}

// GetCircleContacts returns the circle's contact members enriched and sorted
// with admins first, then by how many of the circle's groups they're in.
func (s *Store) GetCircleContacts(id int64) ([]CircleContact, error) {
	members, _ := s.GetCircleMembers(id)
	var refs []string
	for _, m := range members {
		if m.MemberType == MemberContact {
			refs = append(refs, m.MemberRef)
		}
	}
	if len(refs) == 0 {
		return []CircleContact{}, nil
	}

	jids, _ := s.FlattenCircleChats(id)
	count := map[string]int{}
	admin := map[string]bool{}
	for _, g := range jids {
		if !strings.HasSuffix(g, "@g.us") {
			continue
		}
		rows, err := s.DB.Query(`SELECT jid, phone, is_admin, is_super_admin FROM group_participants WHERE group_jid = ?`, g)
		if err != nil {
			continue
		}
		for rows.Next() {
			var jid, phone string
			var isAdmin, isSuper bool
			if rows.Scan(&jid, &phone, &isAdmin, &isSuper) != nil {
				continue
			}
			key := jid
			if phone != "" {
				key = phone + "@s.whatsapp.net"
			}
			count[key]++
			if isAdmin || isSuper {
				admin[key] = true
			}
		}
		rows.Close()
	}

	allTags, _ := s.AllContactTags()
	out := make([]CircleContact, 0, len(refs))
	for _, ref := range refs {
		tags := allTags[ref]
		if tags == nil {
			tags = []Tag{}
		}
		out = append(out, CircleContact{JID: ref, GroupCount: count[ref], IsAdmin: admin[ref], Tags: tags})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsAdmin != out[j].IsAdmin {
			return out[i].IsAdmin
		}
		return out[i].GroupCount > out[j].GroupCount
	})
	return out, nil
}

// DeleteCircle removes a circle, its memberships (as parent, via cascade), and
// any edges where it appears as a nested member of other circles.
func (s *Store) DeleteCircle(id int64) error {
	if _, err := s.DB.Exec(`DELETE FROM circle_members WHERE member_type = ? AND member_ref = ?`,
		MemberCircle, strconv.FormatInt(id, 10)); err != nil {
		return err
	}
	_, err := s.DB.Exec(`DELETE FROM circles WHERE id = ?`, id)
	return err
}

// AddCircleMember adds a group/contact/circle to a circle. For nested circles it
// rejects edges that would create a loop.
func (s *Store) AddCircleMember(circleID int64, memberType, memberRef string) error {
	switch memberType {
	case MemberGroup, MemberContact, MemberCircle:
	default:
		return fmt.Errorf("invalid member type %q", memberType)
	}
	if memberType == MemberCircle {
		childID, err := strconv.ParseInt(memberRef, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid circle ref %q", memberRef)
		}
		if childID == circleID {
			return fmt.Errorf("a circle cannot contain itself")
		}
		// Adding circleID -> childID creates a loop iff childID can already
		// reach circleID by following contains-edges.
		if s.circleReaches(childID, circleID) {
			return fmt.Errorf("that would create a loop between circles")
		}
	}
	_, err := s.DB.Exec(`INSERT OR IGNORE INTO circle_members (circle_id, member_type, member_ref, added_at)
		VALUES (?,?,?,?)`, circleID, memberType, memberRef, time.Now().Unix())
	return err
}

// RemoveCircleMember removes one membership edge.
func (s *Store) RemoveCircleMember(circleID int64, memberType, memberRef string) error {
	_, err := s.DB.Exec(`DELETE FROM circle_members WHERE circle_id = ? AND member_type = ? AND member_ref = ?`,
		circleID, memberType, memberRef)
	return err
}

// GetCircleMembers returns the direct members of a circle.
func (s *Store) GetCircleMembers(circleID int64) ([]CircleMember, error) {
	rows, err := s.DB.Query(`SELECT circle_id, member_type, member_ref, added_at
		FROM circle_members WHERE circle_id = ? ORDER BY member_type, added_at`, circleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CircleMember{}
	for rows.Next() {
		var m CircleMember
		if err := rows.Scan(&m.CircleID, &m.MemberType, &m.MemberRef, &m.AddedAt); err != nil {
			return out, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetCirclesForMember returns the circles that directly contain the given member.
func (s *Store) GetCirclesForMember(memberType, memberRef string) ([]Circle, error) {
	rows, err := s.DB.Query(`SELECT c.id, c.name, c.color, c.notes, c.created_at, c.updated_at, 0
		FROM circles c JOIN circle_members m ON m.circle_id = c.id
		WHERE m.member_type = ? AND m.member_ref = ? ORDER BY c.name COLLATE NOCASE`,
		memberType, memberRef)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Circle{}
	for rows.Next() {
		var c Circle
		if err := rows.Scan(&c.ID, &c.Name, &c.Color, &c.Notes, &c.CreatedAt, &c.UpdatedAt, &c.MemberCount); err != nil {
			return out, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// circleReaches reports whether `from` can reach `to` by following nested-circle
// edges (from contains ... contains to).
func (s *Store) circleReaches(from, to int64) bool {
	visited := map[int64]bool{}
	stack := []int64{from}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == to {
			return true
		}
		if visited[cur] {
			continue
		}
		visited[cur] = true
		rows, err := s.DB.Query(`SELECT member_ref FROM circle_members WHERE circle_id = ? AND member_type = ?`,
			cur, MemberCircle)
		if err != nil {
			continue
		}
		for rows.Next() {
			var ref string
			if rows.Scan(&ref) == nil {
				if childID, err := strconv.ParseInt(ref, 10, 64); err == nil {
					stack = append(stack, childID)
				}
			}
		}
		rows.Close()
	}
	return false
}

// FlattenCircleChats returns the de-duplicated chat JIDs (groups + contacts)
// reachable from a circle, descending into nested circles. Loop-safe.
func (s *Store) FlattenCircleChats(circleID int64) ([]string, error) {
	jids := []string{}
	seenJID := map[string]bool{}
	visited := map[int64]bool{}
	stack := []int64{circleID}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		members, err := s.GetCircleMembers(cur)
		if err != nil {
			return jids, err
		}
		for _, m := range members {
			switch m.MemberType {
			case MemberGroup, MemberContact:
				if !seenJID[m.MemberRef] {
					seenJID[m.MemberRef] = true
					jids = append(jids, m.MemberRef)
				}
			case MemberCircle:
				if childID, err := strconv.ParseInt(m.MemberRef, 10, 64); err == nil {
					stack = append(stack, childID)
				}
			}
		}
	}
	return jids, nil
}
