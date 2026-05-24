// Package reco builds smart suggestions to help users grow their circles.
// It mines the signals that actually indicate a shared venture/company/project:
// distinctive tokens shared across group names, people who co-occur across
// those groups, and activity. It deliberately avoids weak attribute buckets
// (phone prefix, surname) that produce large, low-value lists.
package reco

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"whatsapp-bridge-v2/internal/db"
)

// chatStat holds per-chat message volume and recency.
type chatStat struct {
	count int
	last  int64
}

// recencyScore rewards recent activity; stale clusters get nothing.
func recencyScore(days int64) float64 {
	switch {
	case days < 7:
		return 10
	case days < 30:
		return 7
	case days < 60:
		return 4
	case days < 120:
		return 2
	default:
		return 0
	}
}

// recencyPhrase describes how recently a cluster was active.
func recencyPhrase(days int64) string {
	switch {
	case days < 1:
		return "Active today."
	case days < 7:
		return fmt.Sprintf("Active %d day(s) ago.", days)
	case days < 30:
		return fmt.Sprintf("Last active %d days ago.", days)
	default:
		return fmt.Sprintf("Last active %d weeks ago.", days/7)
	}
}

// RecMember is a suggested member of a circle.
type RecMember struct {
	Type  string `json:"type"` // "group" | "contact"
	Ref   string `json:"ref"`
	Label string `json:"label"`
}

// Recommendation is one actionable suggestion.
type Recommendation struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"` // "new_circle" | "add_to_circle"
	Title    string      `json:"title"`
	Reason   string      `json:"reason"`
	Score    float64     `json:"score"`
	Name     string      `json:"name,omitempty"`      // proposed name (new_circle)
	Color    string      `json:"color,omitempty"`     // proposed color (new_circle)
	CircleID int64       `json:"circle_id,omitempty"` // target (add_to_circle)
	Members  []RecMember `json:"members"`
}

var palette = []string{"#22c55e", "#3b82f6", "#a855f7", "#ef4444", "#eab308", "#14b8a6", "#f97316", "#ec4899"}

var splitRe = regexp.MustCompile(`[^\p{L}\p{N}]+`)
var digitsRe = regexp.MustCompile(`^[0-9]+$`)

// stop holds generic words (Latin + Arabic) that don't identify a venture.
var stop = map[string]bool{}

func init() {
	words := []string{
		// generic English
		"group", "groups", "team", "teams", "official", "chat", "chats", "the", "and", "for", "with",
		"vip", "main", "general", "all", "new", "info", "news", "update", "updates", "support", "help",
		"club", "members", "member", "community", "network", "channel", "broadcast", "test", "private",
		"public", "family", "friends", "work", "project", "projects", "company", "office", "staff",
		"daily", "weekly", "meeting", "meetings", "room", "discussion", "announcements", "announcement",
		// generic business descriptors that span unrelated ventures
		"platform", "app", "apps", "application", "tech", "technology", "brand", "branding",
		"marketing", "sales", "product", "products", "management", "mgmt", "leadership", "silo",
		"silos", "demo", "label", "whitelabel", "structure", "process", "handover", "labs", "lab",
		"holding", "ventures", "venture", "solutions", "services", "service", "consulting", "agency",
		"media", "digital", "global", "international", "gold", "hr", "finance", "ops", "operations",
		"design", "uat", "prod", "production", "staging", "internal", "external", "client", "clients",
		"customer", "customers", "partner", "partners", "partnership", "one", "onboarding", "feedback",
		"العامة", "العام", "الجديد", "الجديدة", "تجريبي", "تجريبية",
		// common filler / time words that aren't venture names
		"later", "today", "tomorrow", "tonight", "now", "soon", "asap", "urgent",
		"important", "reminder", "reminders", "misc", "other", "others", "random",
		"stuff", "todo", "note", "notes", "list", "lists", "follow", "followup",
		// generic Arabic
		"مجموعة", "جروب", "فريق", "العمل", "عائلة", "العائلة", "اصدقاء", "الأصدقاء", "الرسمية", "عام",
		"قروب", "شباب", "اعضاء", "الأعضاء", "تحديثات", "اعلانات", "نقاش", "اجتماع",
		// Arabic function/stop words (not venture names)
		"في", "من", "على", "الى", "إلى", "عن", "مع", "هذا", "هذه", "ذلك", "التي", "الذي",
		"ما", "لا", "او", "أو", "ثم", "كل", "بعد", "قبل", "عند", "انا", "أنا", "نحن", "هو",
		"هي", "انت", "أنت", "يا", "ايضا", "أيضا", "فقط", "جدا", "كان", "هناك", "حول", "بين",
		"خلال", "الان", "الآن", "غير", "دون", "كما", "حتى", "اي", "أي", "به", "له", "لها",
	}
	for _, w := range words {
		stop[w] = true
	}
}

// brandable reports whether a token looks like a coined brand/name rather than a
// common word: an acronym (BIV, NEO), camelCase (xSpace, FamCare), or a
// letter+digit mix (Reson8, 2Pass). These are strong venture identifiers.
func brandable(s string) bool {
	rs := []rune(s)
	letters, upper, lower, digit := 0, 0, 0, 0
	camel := false
	for i, r := range rs {
		switch {
		case unicode.IsUpper(r):
			upper++
			letters++
			if i > 0 && unicode.IsLower(rs[i-1]) {
				camel = true
			}
		case unicode.IsLower(r):
			lower++
			letters++
		case unicode.IsDigit(r):
			digit++
		}
	}
	if letters == 0 {
		return false
	}
	if camel {
		return true
	}
	if upper >= 2 && lower == 0 {
		return true // acronym
	}
	if digit > 0 && letters > 0 {
		return true // letter+digit mix
	}
	return false
}

func tokenize(name string) []string {
	parts := splitRe.Split(name, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if utf8.RuneCountInString(p) < 2 {
			continue
		}
		if digitsRe.MatchString(p) {
			continue // pure numbers (years, counters) aren't venture names
		}
		out = append(out, p)
	}
	return out
}

// Engine produces recommendations from the store.
type Engine struct{ s *db.Store }

func New(s *db.Store) *Engine { return &Engine{s: s} }

type groupRec struct {
	jid    string
	name   string
	tokens map[string]bool   // lowercased token keys
	parts  map[string]bool   // canonical participant jids
}

// Recommend returns all candidate suggestions, best first (capped). The caller
// filters dismissed ones and trims to its own limit. ownPhone is excluded from
// "core people" since you are in all your own groups.
func (e *Engine) Recommend(ownPhone string) ([]Recommendation, error) {
	groups, err := e.loadGroups()
	if err != nil {
		return nil, err
	}
	contactName := e.loadContactNames()
	activity := e.loadActivity()
	circledGroups, circleGroupSets, circleContactSets, circleNames := e.loadCircles()

	// origForm: a representative original-case spelling per token key.
	origForm := map[string]string{}
	tokenGroups := map[string][]string{}
	partLabel := map[string]string{} // canonical part jid -> display label
	partNamed := map[string]bool{}

	for _, g := range groups {
		for key := range g.tokens {
			tokenGroups[key] = append(tokenGroups[key], g.jid)
		}
	}
	// representative original spelling
	for _, g := range groups {
		for _, raw := range tokenize(g.name) {
			key := strings.ToLower(raw)
			if _, ok := origForm[key]; !ok {
				origForm[key] = raw
			}
		}
	}

	// participant labels
	for _, g := range groups {
		for p := range g.parts {
			if _, ok := partLabel[p]; ok {
				continue
			}
			if n := contactName[p]; n != "" {
				partLabel[p] = n
				partNamed[p] = true
			} else {
				partLabel[p] = "+" + jidUser(p)
			}
		}
	}

	groupByJID := map[string]groupRec{}
	for _, g := range groups {
		groupByJID[g.jid] = g
	}

	var recs []Recommendation
	recs = append(recs, e.keywordRecs()...)
	recs = append(recs, e.newCircleRecs(tokenGroups, origForm, groupByJID, activity, partLabel, partNamed, circledGroups, ownPhone, contactName)...)
	recs = append(recs, e.fillCircleRecs(circleGroupSets, circleContactSets, circleNames, groupByJID, activity, partLabel, partNamed, contactName, ownPhone)...)

	sort.SliceStable(recs, func(i, j int) bool { return recs[i].Score > recs[j].Score })

	// keep a sensible number of candidates and give each new-circle a color
	if len(recs) > 30 {
		recs = recs[:30]
	}
	for i := range recs {
		if recs[i].Type == "new_circle" {
			recs[i].Color = palette[i%len(palette)]
		}
	}
	return recs, nil
}

// keywordRecs turns each circle's saved keywords into a high-priority
// "add matches" suggestion (explicit user intent).
func (e *Engine) keywordRecs() []Recommendation {
	var out []Recommendation
	circles, _ := e.s.ListCircles()
	for _, c := range circles {
		if len(c.Keywords) == 0 {
			continue
		}
		sugg, _, _ := e.s.SuggestForCircle(c.ID)
		if len(sugg) == 0 {
			continue
		}
		members := make([]RecMember, 0, len(sugg))
		for i, sg := range sugg {
			if i >= 12 {
				break
			}
			members = append(members, RecMember{Type: sg.Type, Ref: sg.Ref, Label: sg.Label})
		}
		out = append(out, Recommendation{
			ID:       "kw:" + strconv.FormatInt(c.ID, 10),
			Type:     "add_to_circle",
			CircleID: c.ID,
			Title:    "Add keyword matches to “" + c.Name + "”",
			Reason:   fmt.Sprintf("%d match this circle's keywords (%s).", len(sugg), strings.Join(c.Keywords, ", ")),
			Score:    70 + float64(min(len(sugg), 10)),
			Members:  members,
		})
	}
	return out
}

// newCircleRecs clusters groups by distinctive shared name tokens and proposes
// a circle of those groups + the people who co-occur across them.
func (e *Engine) newCircleRecs(
	tokenGroups map[string][]string,
	origForm map[string]string,
	groupByJID map[string]groupRec,
	activity map[string]chatStat,
	partLabel map[string]string,
	partNamed map[string]bool,
	circledGroups map[string]bool,
	ownPhone string,
	contactName map[string]string,
) []Recommendation {
	var out []Recommendation
	seenClusters := []map[string]bool{}

	for key, gjids := range tokenGroups {
		if stop[key] {
			continue
		}
		n := len(gjids)
		if n < 2 || n > 10 {
			continue // need a small, focused cluster (not 1, not generic)
		}
		// skip if most of these groups are already in circles
		already := 0
		for _, gj := range gjids {
			if circledGroups[gj] {
				already++
			}
		}
		if float64(already) > 0.6*float64(n) {
			continue
		}
		// dedupe overlapping clusters (same/near-same group set)
		gset := toSet(gjids)
		dup := false
		for _, prev := range seenClusters {
			if jaccard(gset, prev) > 0.6 {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		seenClusters = append(seenClusters, gset)

		// core people: participants present in >=2 of the cluster's groups
		coreCount := map[string]int{}
		for _, gj := range gjids {
			for p := range groupByJID[gj].parts {
				if ownPhone != "" && p == ownPhone+"@s.whatsapp.net" {
					continue
				}
				coreCount[p]++
			}
		}
		type pc struct {
			ref   string
			count int
			named bool
		}
		var core []pc
		for p, c := range coreCount {
			if c >= 2 {
				core = append(core, pc{p, c, partNamed[p]})
			}
		}
		sort.SliceStable(core, func(i, j int) bool {
			if core[i].named != core[j].named {
				return core[i].named // named first
			}
			return core[i].count > core[j].count
		})
		if len(core) > 8 {
			core = core[:8]
		}

		// recency: only recommend clusters that are still alive, and rank the
		// most-recently-active ones first.
		now := time.Now().Unix()
		var maxLast int64
		activeGroups := 0
		for _, gj := range gjids {
			l := activity[gj].last
			if l > maxLast {
				maxLast = l
			}
			if l > 0 && now-l < 90*86400 {
				activeGroups++
			}
		}
		if maxLast == 0 {
			continue // no messages at all — not actionable
		}
		days := (now - maxLast) / 86400
		if days > 180 {
			continue // dead for half a year — skip
		}

		// build members + score
		var members []RecMember
		act := 0
		for _, gj := range gjids {
			g := groupByJID[gj]
			members = append(members, RecMember{Type: "group", Ref: gj, Label: g.name})
			act += activity[gj].count
		}
		namedCore := 0
		for _, c := range core {
			if c.named {
				namedCore++
			}
			members = append(members, RecMember{Type: "contact", Ref: c.ref, Label: partLabel[c.ref]})
		}

		distinct := 0.0
		if n >= 2 && n <= 5 {
			distinct = 3
		}
		name := origForm[key]
		brandBonus := 0.0
		if brandable(name) {
			brandBonus = 5
		}
		// Recency dominates; raw message volume is a minor tiebreak.
		score := recencyScore(days) + 2*float64(activeGroups) + brandBonus +
			3*math.Min(float64(n), 6) + math.Log1p(float64(act)) +
			1.5*float64(len(core)) + 2*float64(namedCore) + distinct
		reason := fmt.Sprintf("%d groups share “%s”.", n, name)
		if len(core) > 0 {
			reason += fmt.Sprintf(" %d shared people.", len(core))
		}
		reason += " " + recencyPhrase(days)
		out = append(out, Recommendation{
			ID:      "new:" + key,
			Type:    "new_circle",
			Title:   "Create circle “" + name + "”",
			Reason:  reason,
			Score:   score,
			Name:    name,
			Members: members,
		})
	}
	return out
}

// fillCircleRecs suggests adding key people (present in most of a circle's
// groups) and related groups (sharing distinctive tokens) to existing circles.
func (e *Engine) fillCircleRecs(
	circleGroupSets map[int64]map[string]bool,
	circleContactSets map[int64]map[string]bool,
	circleNames map[int64]string,
	groupByJID map[string]groupRec,
	activity map[string]chatStat,
	partLabel map[string]string,
	partNamed map[string]bool,
	contactName map[string]string,
	ownPhone string,
) []Recommendation {
	var out []Recommendation
	now := time.Now().Unix()

	for cid, gset := range circleGroupSets {
		if len(gset) < 2 {
			continue
		}
		ng := len(gset)
		need := int(math.Ceil(0.6 * float64(ng)))
		if need < 2 {
			need = 2
		}
		contactSet := circleContactSets[cid]

		// key people across the circle's groups
		count := map[string]int{}
		for gj := range gset {
			for p := range groupByJID[gj].parts {
				if ownPhone != "" && p == ownPhone+"@s.whatsapp.net" {
					continue
				}
				if contactSet[p] {
					continue // already a member
				}
				count[p]++
			}
		}
		type pc struct {
			ref   string
			count int
			named bool
		}
		var people []pc
		for p, c := range count {
			if c >= need {
				people = append(people, pc{p, c, partNamed[p]})
			}
		}
		sort.SliceStable(people, func(i, j int) bool {
			if people[i].named != people[j].named {
				return people[i].named
			}
			return people[i].count > people[j].count
		})
		if len(people) > 8 {
			people = people[:8]
		}
		if len(people) >= 2 {
			var members []RecMember
			named := 0
			for _, p := range people {
				if p.named {
					named++
				}
				members = append(members, RecMember{Type: "contact", Ref: p.ref, Label: partLabel[p.ref]})
			}
			score := 2*float64(len(people)) + 2*float64(named) + 1
			out = append(out, Recommendation{
				ID:       "people:" + strconv.FormatInt(cid, 10),
				Type:     "add_to_circle",
				CircleID: cid,
				Title:    "Add key people to “" + circleNames[cid] + "”",
				Reason:   fmt.Sprintf("%d people are in most of this circle's groups but aren't members yet.", len(people)),
				Score:    score,
				Members:  members,
			})
		}

		// related groups: share a distinctive token with the circle's groups
		circleTokens := map[string]bool{}
		for gj := range gset {
			for t := range groupByJID[gj].tokens {
				if !stop[t] {
					circleTokens[t] = true
				}
			}
		}
		var relGroups []RecMember
		for gj, g := range groupByJID {
			if gset[gj] {
				continue
			}
			if l := activity[gj].last; l == 0 || now-l > 180*86400 {
				continue // skip dead groups
			}
			shared := ""
			for t := range g.tokens {
				if circleTokens[t] && !stop[t] {
					shared = t
					break
				}
			}
			if shared != "" {
				relGroups = append(relGroups, RecMember{Type: "group", Ref: gj, Label: g.name})
			}
		}
		if len(relGroups) >= 1 {
			if len(relGroups) > 6 {
				relGroups = relGroups[:6]
			}
			out = append(out, Recommendation{
				ID:       "groups:" + strconv.FormatInt(cid, 10),
				Type:     "add_to_circle",
				CircleID: cid,
				Title:    "Add related groups to “" + circleNames[cid] + "”",
				Reason:   fmt.Sprintf("%d more groups share a name with this circle's groups.", len(relGroups)),
				Score:    2.5 * float64(len(relGroups)),
				Members:  relGroups,
			})
		}
	}
	return out
}

// --- loaders ---

func (e *Engine) loadGroups() ([]groupRec, error) {
	rows, err := e.s.DB.Query(`SELECT jid, name FROM groups`)
	if err != nil {
		return nil, err
	}
	byJID := map[string]*groupRec{}
	var order []string
	for rows.Next() {
		var jid, name string
		if rows.Scan(&jid, &name) != nil {
			continue
		}
		g := &groupRec{jid: jid, name: name, tokens: map[string]bool{}, parts: map[string]bool{}}
		for _, raw := range tokenize(name) {
			g.tokens[strings.ToLower(raw)] = true
		}
		byJID[jid] = g
		order = append(order, jid)
	}
	rows.Close()

	prows, err := e.s.DB.Query(`SELECT group_jid, jid, phone FROM group_participants`)
	if err == nil {
		for prows.Next() {
			var gj, pj, phone string
			if prows.Scan(&gj, &pj, &phone) != nil {
				continue
			}
			if g, ok := byJID[gj]; ok {
				g.parts[canonicalPart(pj, phone)] = true
			}
		}
		prows.Close()
	}

	out := make([]groupRec, 0, len(order))
	for _, jid := range order {
		out = append(out, *byJID[jid])
	}
	return out, nil
}

func (e *Engine) loadContactNames() map[string]string {
	m := map[string]string{}
	rows, err := e.s.DB.Query(`SELECT jid, phone, name, push_name, business_name FROM contacts`)
	if err != nil {
		return m
	}
	defer rows.Close()
	for rows.Next() {
		var jid, phone, name, push, biz string
		if rows.Scan(&jid, &phone, &name, &push, &biz) != nil {
			continue
		}
		best := firstNonEmpty(name, biz, push)
		if best == "" {
			continue
		}
		if jid != "" {
			m[jid] = best
		}
		if phone != "" {
			m[phone+"@s.whatsapp.net"] = best
		}
	}
	return m
}

func (e *Engine) loadActivity() map[string]chatStat {
	m := map[string]chatStat{}
	rows, err := e.s.DB.Query(`SELECT chat_jid, COUNT(*), MAX(timestamp) FROM messages GROUP BY chat_jid`)
	if err != nil {
		return m
	}
	defer rows.Close()
	for rows.Next() {
		var jid string
		var c int
		var last int64
		if rows.Scan(&jid, &c, &last) == nil {
			m[jid] = chatStat{count: c, last: last}
		}
	}
	return m
}

// loadCircles returns: groups already in any circle; per-circle group set and
// contact set; and circle names.
func (e *Engine) loadCircles() (map[string]bool, map[int64]map[string]bool, map[int64]map[string]bool, map[int64]string) {
	circled := map[string]bool{}
	groupSets := map[int64]map[string]bool{}
	contactSets := map[int64]map[string]bool{}
	names := map[int64]string{}

	circles, _ := e.s.ListCircles()
	for _, c := range circles {
		names[c.ID] = c.Name
		groupSets[c.ID] = map[string]bool{}
		contactSets[c.ID] = map[string]bool{}
	}
	rows, err := e.s.DB.Query(`SELECT circle_id, member_type, member_ref FROM circle_members`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var cid int64
			var mt, ref string
			if rows.Scan(&cid, &mt, &ref) != nil {
				continue
			}
			switch mt {
			case "group":
				circled[ref] = true
				if groupSets[cid] != nil {
					groupSets[cid][ref] = true
				}
			case "contact":
				if contactSets[cid] != nil {
					contactSets[cid][ref] = true
				}
			}
		}
	}
	return circled, groupSets, contactSets, names
}

// --- helpers ---

func canonicalPart(jid, phone string) string {
	if phone != "" {
		return phone + "@s.whatsapp.net"
	}
	return jid
}

func jidUser(jid string) string {
	if i := strings.IndexByte(jid, '@'); i >= 0 {
		return jid[:i]
	}
	return jid
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, i := range items {
		s[i] = true
	}
	return s
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
