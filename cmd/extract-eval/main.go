// Command extract-eval measures the task extractor against tasks a person
// labelled by hand.
//
// Why it exists: "the local model looks good enough" is not a decision anybody
// can defend six months later, and a model upgrade that quietly loses recall
// is invisible without numbers. This prints the same numbers for every engine
// and model, over the same conversations, so switching is a measured choice.
//
// Nothing here writes. The pipeline runs in dry mode, so a bad model cannot
// fill the task list with the mistakes it is being measured for.
//
//	go run ./cmd/extract-eval                      score every case
//	go run ./cmd/extract-eval -engines a,b         compare engines
//	go run ./cmd/extract-eval -label "Group name" -from 2026-07-26 -to 2026-07-29
//	                                               print messages to label
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"whatsapp-bridge-v2/internal/db"
	"whatsapp-bridge-v2/internal/extract"
	"whatsapp-bridge-v2/internal/extract/adapters"
)

// The bar from the design. Printed as pass or fail so the answer is not a
// matter of reading a table hopefully.
const (
	barPrecision = 0.85
	barRecall    = 0.75
	barOwner     = 0.90
	barP50MS     = 30000
)

// matchTitle is how close two titles must be to count as the same task when
// the evidence message ids differ.
const matchTitle = 0.6

// expectedTask is one task a person says is really in the conversation.
type expectedTask struct {
	Title    string `json:"title"`
	OwnerJID string `json:"owner_jid"`
	// EvidenceID names the message that proves the task. EvidenceIDs is for
	// work asked for more than once — "change the map" said on Monday and
	// again in Tuesday's list of four items. Either message is a right answer,
	// and pinning the label to one of them measured the golden set, not the
	// model.
	EvidenceID  string   `json:"evidence_id"`
	EvidenceIDs []string `json:"evidence_ids,omitempty"`
}

func (e expectedTask) ids() []string {
	out := e.EvidenceIDs
	if e.EvidenceID != "" {
		out = append([]string{e.EvidenceID}, out...)
	}
	for i, id := range out {
		out[i] = strings.TrimPrefix(id, "#")
	}
	return out
}

// evalCase is one labelled slice of one chat.
type evalCase struct {
	Name     string         `json:"name"`
	ChatJID  string         `json:"chat_jid"`
	Since    flexTime       `json:"since"`
	Until    flexTime       `json:"until"`
	Expected []expectedTask `json:"expected"`

	file string
}

// flexTime accepts either a unix second or a "2026-07-26" date, because these
// files are written by hand and a date is much easier to check by eye.
type flexTime int64

func (f *flexTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*f = flexTime(n)
		return nil
	}
	for _, layout := range []string{"2006-01-02", "2006-01-02 15:04", time.RFC3339} {
		if t, err := time.ParseInLocation(layout, s, riyadh()); err == nil {
			*f = flexTime(t.Unix())
			return nil
		}
	}
	return fmt.Errorf("cannot read %q as a date", s)
}

func riyadh() *time.Location {
	if loc, err := time.LoadLocation("Asia/Riyadh"); err == nil {
		return loc
	}
	return time.UTC
}

func main() {
	var (
		dbPath  = flag.String("db", "store/messages.db", "message database")
		dir     = flag.String("dir", "store/eval", "folder of labelled cases")
		engines = flag.String("engines", "ollama:qwen2.5:14b", "comma separated engine:model")
		verbose = flag.Bool("v", false, "show every proposal and why it was dropped")
		label   = flag.String("label", "", "print one chat's messages so it can be labelled")
		from    = flag.String("from", "", "start date for -label, e.g. 2026-07-26")
		to      = flag.String("to", "", "end date for -label")
	)
	flag.Parse()

	store, err := db.NewStore(*dbPath)
	if err != nil {
		die("cannot open %s: %v", *dbPath, err)
	}

	if *label != "" {
		if err := printForLabelling(store, *label, *from, *to, *dir); err != nil {
			die("%v", err)
		}
		return
	}

	cases, err := loadCases(*dir)
	if err != nil {
		die("%v", err)
	}
	if len(cases) == 0 {
		fmt.Printf("No labelled cases in %s/.\n\n", *dir)
		fmt.Println("Make one like this:")
		fmt.Printf("  go run ./cmd/extract-eval -label \"The 3 committee\" -from 2026-07-26 -to 2026-07-29\n")
		fmt.Println("Then fill in the 'expected' list in the file it writes.")
		return
	}

	rows := []scoreRow{}
	for _, spec := range strings.Split(*engines, ",") {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		ex, err := buildExtractor(spec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skipping %s: %v\n", spec, err)
			continue
		}
		rows = append(rows, score(store, ex, cases, *verbose))
	}
	printTable(rows)
}

// ---------------------------------------------------------------- scoring

type scoreRow struct {
	Engine string
	Cases  int
	Chunks int

	Expected int // tasks a person labelled
	Kept     int // tasks that survived checking
	Matched  int // kept tasks that a person also labelled
	Found    int // labelled tasks the model found

	// Owner is scored only where the evidence message carried a mention or was
	// a reply. Elsewhere the chat does not say who must act, so "unknown" is
	// the right answer and a score would be measuring luck.
	OwnerAsked int
	OwnerRight int

	BadQuotes    int // model invented a quote (caught by verify)
	Unverifiable int // kept task whose quote is not in the raw message
	Failed       int // chats that could not be read at all

	MS []int64
}

func (r scoreRow) precision() float64 {
	if r.Kept == 0 {
		return 0
	}
	return float64(r.Matched) / float64(r.Kept)
}

func (r scoreRow) recall() float64 {
	if r.Expected == 0 {
		return 0
	}
	return float64(r.Found) / float64(r.Expected)
}

func (r scoreRow) ownerOK() float64 {
	if r.OwnerAsked == 0 {
		return 1
	}
	return float64(r.OwnerRight) / float64(r.OwnerAsked)
}

func (r scoreRow) p50() int64 {
	if len(r.MS) == 0 {
		return 0
	}
	s := append([]int64{}, r.MS...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

func (r scoreRow) passes() bool {
	return r.precision() >= barPrecision && r.recall() >= barRecall &&
		r.ownerOK() >= barOwner && r.Unverifiable == 0 && r.p50() <= barP50MS
}

func score(store *db.Store, ex extract.Extractor, cases []evalCase, verbose bool) scoreRow {
	row := scoreRow{Engine: ex.Name(), Cases: len(cases)}
	deps := extract.Deps{Store: store, Extractor: ex, Loc: riyadh(), DryRun: true}

	for _, c := range cases {
		fmt.Fprintf(os.Stderr, "  %s · %s\n", ex.Name(), c.Name)
		res, err := extract.Run(context.Background(), deps,
			extract.RunSpec{ChatJID: c.ChatJID, Since: int64(c.Since),
				Until: int64(c.Until), RunID: "eval"}, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "    could not read: %v\n", err)
			row.Failed++
			continue
		}

		row.Chunks += res.Chunks
		row.MS = append(row.MS, res.ChunkMS...)
		row.Expected += len(c.Expected)
		row.BadQuotes += res.Rejections[extract.RejectBadQuote]

		// Each labelled task may be found at most once, so a model cannot
		// score twice for proposing the same thing in two shapes.
		taken := make([]bool, len(c.Expected))

		for _, o := range res.Outcomes {
			if !o.Kept {
				if verbose {
					fmt.Fprintf(os.Stderr, "    drop [%s] %s\n      cited %s: %s\n",
						o.Reason, o.Title, o.EvidenceID, o.Evidence)
				}
				continue
			}
			row.Kept++
			if !quoteIsReal(store, c.ChatJID, o.EvidenceID, o.Evidence) {
				row.Unverifiable++
				fmt.Fprintf(os.Stderr, "    !! quote not in message %s: %q\n", o.EvidenceID, o.Evidence)
			}

			idx := matchExpected(c.Expected, taken, o)
			if idx < 0 {
				if verbose {
					fmt.Fprintf(os.Stderr, "    extra  (%.1f) %s\n      quoted: %s\n",
						o.Confidence, o.Title, o.Evidence)
				}
				continue
			}
			taken[idx] = true
			row.Matched++
			row.Found++
			if want := c.Expected[idx].OwnerJID; want != "" && o.Resolvable {
				row.OwnerAsked++
				if sameJID(want, o.OwnerJID) {
					row.OwnerRight++
				} else if verbose {
					fmt.Fprintf(os.Stderr, "    owner  %s: got %s want %s\n",
						o.Title, o.OwnerJID, want)
				}
			}
		}
		if verbose {
			for i, e := range c.Expected {
				if !taken[i] {
					fmt.Fprintf(os.Stderr, "    MISS   %s\n", e.Title)
				}
			}
		}
	}
	return row
}

// matchExpected finds the labelled task a proposal answers, or -1.
//
// Same evidence message is the strong signal. Titles are the fallback, because
// two people writing the same task write it in two different ways.
func matchExpected(want []expectedTask, taken []bool, got extract.Outcome) int {
	// One message can carry two asks ("do markdown, and fix the RTL"), so
	// among the labels sharing this evidence id, take the closest title.
	best, bestScore := -1, -1.0
	for i, e := range want {
		if taken[i] {
			continue
		}
		hit := false
		for _, id := range e.ids() {
			if id == got.EvidenceID {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		if s := extract.TitleSimilarity(e.Title, got.Title); s > bestScore {
			best, bestScore = i, s
		}
	}
	if best >= 0 {
		return best
	}

	// No evidence match. Then the titles alone have to carry it.
	best, bestScore = -1, matchTitle
	for i, e := range want {
		if taken[i] {
			continue
		}
		if s := extract.TitleSimilarity(e.Title, got.Title); s >= bestScore {
			best, bestScore = i, s
		}
	}
	return best
}

// quoteIsReal re-checks the evidence against the stored message, without going
// through the verifier. Checking the verifier with the verifier would prove
// nothing; this reads the database row directly.
func quoteIsReal(store *db.Store, chatJID, msgID, quote string) bool {
	if quote == "" {
		return false
	}
	var content, caption string
	err := store.DB.QueryRow(`SELECT COALESCE(content,''), COALESCE(media_caption,'')
		FROM messages WHERE id = ? AND chat_jid = ?`, msgID, chatJID).Scan(&content, &caption)
	if err != nil {
		return false
	}
	// A voice note has no text of its own; its words live in the transcript.
	parts := []string{content, caption}
	if rows, err := store.DB.Query(`SELECT content FROM media_understanding
		WHERE chat_jid = ? AND message_id = ? AND status = 'ok'`, chatJID, msgID); err == nil {
		for rows.Next() {
			var c string
			if rows.Scan(&c) == nil {
				parts = append(parts, c)
			}
		}
		rows.Close()
	}
	hay := strings.Join(parts, " ")
	// The same rule the verifier uses for a quote: most of its words must be
	// there. Written again here on purpose, so the two can disagree.
	var words []string
	for _, w := range strings.Fields(quote) {
		// An @mention is a name in the rendered line and a number in the
		// stored message. It is put there by the renderer, so checking it
		// would only measure the renderer.
		if strings.HasPrefix(w, "@") {
			continue
		}
		words = append(words, w)
	}
	if len(words) == 0 {
		return false
	}
	hit := 0
	for _, w := range words {
		if strings.Contains(hay, strings.Trim(w, ".,!?:;،؟…")) {
			hit++
		}
	}
	return float64(hit)/float64(len(words)) >= 0.8
}

// sameJID compares identities by their digits, so a LID and a phone form of
// the same person do not read as two people.
func sameJID(a, b string) bool {
	if a == b {
		return true
	}
	return digits(a) != "" && digits(a) == digits(b)
}

func digits(jid string) string {
	if i := strings.IndexAny(jid, "@:"); i >= 0 {
		jid = jid[:i]
	}
	var b strings.Builder
	for _, r := range jid {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func printTable(rows []scoreRow) {
	if len(rows) == 0 {
		return
	}
	fmt.Println()
	fmt.Printf("%-24s %6s %8s %6s %10s %7s %9s %11s %13s %8s  %s\n",
		"engine", "chunks", "expected", "kept", "precision", "recall",
		"owner_ok", "bad_quotes", "unverifiable", "p50_ms", "bar")
	for _, r := range rows {
		verdict := "FAIL"
		if r.passes() {
			verdict = "pass"
		}
		fmt.Printf("%-24s %6d %8d %6d %10.2f %7.2f %9.2f %11d %13d %8d  %s\n",
			r.Engine, r.Chunks, r.Expected, r.Kept, r.precision(), r.recall(),
			r.ownerOK(), r.BadQuotes, r.Unverifiable, r.p50(), verdict)
	}
	fmt.Printf("\nbar: precision >= %.2f, recall >= %.2f, owner >= %.2f, unverifiable = 0, p50 <= %d ms\n",
		barPrecision, barRecall, barOwner, barP50MS)
	for _, r := range rows {
		if r.Failed > 0 {
			fmt.Printf("note: %s could not read %d case(s)\n", r.Engine, r.Failed)
		}
	}
}

// ---------------------------------------------------------------- engines

func buildExtractor(spec string) (extract.Extractor, error) {
	engine, model, _ := strings.Cut(spec, ":")
	switch engine {
	case "ollama":
		if model == "" {
			return nil, fmt.Errorf("ollama needs a model name")
		}
		return adapters.NewOllama(os.Getenv("OLLAMA_URL"), model), nil
	case "claude":
		return adapters.NewClaude(runSidecar, model), nil
	default:
		return nil, fmt.Errorf("unknown engine %q", engine)
	}
}

func loadCases(dir string) ([]evalCase, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var out []evalCase
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var c evalCase
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		if c.ChatJID == "" {
			return nil, fmt.Errorf("%s: chat_jid is missing", filepath.Base(p))
		}
		c.file = p
		if c.Name == "" {
			c.Name = strings.TrimSuffix(filepath.Base(p), ".json")
		}
		out = append(out, c)
	}
	return out, nil
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
