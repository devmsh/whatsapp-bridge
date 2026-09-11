package db

import "regexp"

// Intro chats: people you met recently, where the conversation is still just an
// introduction — a name exchanged, a number shared, "let's meet properly soon".
//
// This is a different axis from circles. A circle says which venture someone
// belongs to; being a new introduction says how far the relationship has got,
// and the two are independent — a new contact can already belong to a circle.
// Labels carry the lasting answer; this view is how you find the candidates.
//
// The signals below were read off real chats rather than guessed. The opening
// messages of a genuine intro nearly always carry one of: a self-introduction
// ("معك غياث العاني"), a pleasantry about having just met ("تشرفت بمعرفتك"), a
// number being handed over ("هادا رقمي سجله عندك"), a referral ("عبدالله عيطه
// أعطاني رقمك"), or a promise to meet properly ("نرتب لقاء").

// IntroChat is one candidate: a recently-started chat that reads like an
// introduction.
type IntroChat struct {
	JID           string `json:"jid"`
	Name          string `json:"name"`
	MessageCount  int    `json:"message_count"`
	FirstMessage  int64  `json:"first_message_at"`
	LastMessageAt int64  `json:"last_message_at,omitempty"`
	Score         int    `json:"score"`
	// Signals names the reasons, so the UI can explain itself rather than
	// asking you to trust a number.
	Signals []string `json:"signals"`
	Tagged  bool     `json:"tagged"`
}

type introSignal struct {
	name   string
	weight int
	re     *regexp.Regexp
}

var introSignals = []introSignal{
	{"self-intro", 3, regexp.MustCompile(`(?i)مع[اك]\s+\S+|أنا\s+\S+\s+(مهندس|من)|انا\s+\S+\s+(مهندس|من)|this is \w+|i'm \w+ from|my name is`)},
	{"pleased-to-meet", 3, regexp.MustCompile(`(?i)تشرفت|تشرفنا|شرفتنا|سعدت بلقائك|nice (to meet|meeting) you|good to meet`)},
	{"shared-number", 3, regexp.MustCompile(`(?i)ه[اذ]ا رقمي|هي رقمي|رقمي صار عندك|سجله عندك|my number|save my (number|contact)`)},
	{"referred-by", 3, regexp.MustCompile(`(?i)أعطاني رقمك|اعطاني رقمك|وصلني رقمك|طلبت رقمك|أخذت رقمك|gave me your number|got your number`)},
	{"meet-soon", 2, regexp.MustCompile(`(?i)نرتب لقاء|نلتقي قريب|لقاء قريب|نرتبها|نتقابل|meet soon|catch up|set up a (call|meeting)`)},
	{"role/company", 1, regexp.MustCompile(`(?i)\bfrom [A-Z]\w+|من شركة|CEO|founder|مؤسس`)},
}

// IntroChats finds recently-started direct chats that read like introductions.
//
// sinceTS bounds "recently met"; maxMessages keeps it to conversations that
// have not yet turned into a working relationship. Hidden, archived and deleted
// chats never appear. Highest score first.
func (s *Store) IntroChats(sinceTS int64, maxMessages int, tagID int64) ([]IntroChat, error) {
	if maxMessages <= 0 {
		maxMessages = 40
	}
	rows, err := s.DB.Query(`
		SELECT c.jid,
		       COALESCE(NULLIF(ct.name,''), NULLIF(ct.push_name,''),
		                NULLIF(ct.business_name,''), c.jid) AS name,
		       COUNT(m.id)        AS total,
		       MIN(m.timestamp)   AS first_ts,
		       MAX(m.timestamp)   AS last_ts
		FROM chats c
		JOIN messages m ON m.chat_jid = c.jid AND COALESCE(m.is_deleted,0) = 0
		LEFT JOIN contacts ct ON (ct.jid = c.jid OR ct.lid = c.jid)
		WHERE (c.jid LIKE '%@s.whatsapp.net' OR c.jid LIKE '%@lid')
		  AND COALESCE(c.is_archived,0) = 0
		  AND COALESCE(c.deleted_at,0) = 0
		  AND c.jid NOT IN (SELECT chat_jid FROM hidden_chats)
		GROUP BY c.jid
		-- The comparison must be against an integer. min(timestamp) is an
		-- aggregate and carries no column affinity, so comparing it to the
		-- string strftime returns does a TEXT comparison and silently matches
		-- nothing at all.
		HAVING first_ts >= CAST(? AS INTEGER) AND total < ?`,
		sinceTS, maxMessages)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type row struct {
		jid, name       string
		total           int
		firstTS, lastTS int64
	}
	var candidates []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.jid, &r.name, &r.total, &r.firstTS, &r.lastTS) == nil {
			candidates = append(candidates, r)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := []IntroChat{}
	for _, c := range candidates {
		// Only the opening of the conversation is scored. Later messages are
		// the actual relationship, and "تشرفنا" said in passing months in does
		// not make a chat an introduction.
		opening, err := s.DB.Query(`SELECT COALESCE(content,'') FROM messages
			WHERE chat_jid = ? AND content != '' ORDER BY timestamp LIMIT 6`, c.jid)
		if err != nil {
			continue
		}
		blob := ""
		for opening.Next() {
			var t string
			if opening.Scan(&t) == nil {
				blob += " " + t
			}
		}
		opening.Close()

		score := 0
		var signals []string
		for _, sig := range introSignals {
			if sig.re.MatchString(blob) {
				score += sig.weight
				signals = append(signals, sig.name)
			}
		}
		if score == 0 {
			continue
		}
		ic := IntroChat{
			JID: c.jid, Name: c.name, MessageCount: c.total,
			FirstMessage: c.firstTS, LastMessageAt: c.lastTS,
			Score: score, Signals: signals,
		}
		if tagID > 0 {
			var n int
			s.DB.QueryRow(`SELECT 1 FROM contact_tags ct
				LEFT JOIN contacts c ON (c.jid = ? OR c.lid = ?)
				WHERE ct.tag_id = ? AND ct.contact_jid IN (?, COALESCE(c.jid,''), COALESCE(c.lid,''))
				LIMIT 1`, c.jid, c.jid, tagID, c.jid).Scan(&n)
			ic.Tagged = n == 1
		}
		out = append(out, ic)
	}

	// Strongest signal first, then the most recently met.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			a, b := out[j-1], out[j]
			if b.Score > a.Score || (b.Score == a.Score && b.FirstMessage > a.FirstMessage) {
				out[j-1], out[j] = b, a
				continue
			}
			break
		}
	}
	return out, nil
}
