package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"whatsapp-bridge-v2/internal/db"
)

// small helpers used only by this file
func newTimeoutCtx(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
func readFileBytes(path string) ([]byte, error) { return os.ReadFile(path) }
func snippet(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// MediaUnderstandingManager runs background workers that transcribe voice notes
// (whisper-cli, local) and describe images (Codex vision). Voice-note
// transcripts are also cleaned up by a Codex refine pass. Each kind is gated
// behind a per-kind toggle.
type MediaUnderstandingManager struct {
	s *Server

	mu       sync.Mutex
	running  map[string]bool // entity keys currently being processed
	audioBin string          // detected once on Start
}

const (
	audioEnabledKey = "media_audio_enabled"
	imageEnabledKey = "media_image_enabled"
)

// mediaUnderstandingEnabled is the master switch for the AI media-understanding
// workers: voice-note transcription (local whisper) + transcript refinement
// (Codex) and image description (Codex vision).
//
// It was OFF while these ran on the Anthropic API and drained its rate limit.
// The AI calls now go through the local Codex CLI on the ChatGPT subscription
// (no API billing), and concurrency is capped in codexExec, so the feature is
// back ON. Per-kind UI toggles still apply on top of this (see enabledFor).
const mediaUnderstandingEnabled = true

func newMediaManager(s *Server) *MediaUnderstandingManager {
	return &MediaUnderstandingManager{s: s, running: map[string]bool{}}
}

// Start launches both kinds of workers. They idle unless enabled.
func (m *MediaUnderstandingManager) Start() {
	m.audioBin = detectWhisperBinary()
	if !mediaUnderstandingEnabled {
		fmt.Println("media-understanding: disabled (rate-limit guard) — voice + image AI workers not started")
		return
	}
	go m.workerLoop("audio", 2)  // 2 parallel whisper transcribes (local CPU)
	go m.workerLoop("image", 2)  // 2 parallel Codex image descriptions
	go m.refineBackfillLoop()    // re-refine old raw transcripts in the background
}

// refineBackfillLoop continuously walks transcript rows whose `refined` flag
// is still 0 and runs them through the refiner — without re-running whisper.
// This is how we backfill the refinement onto historical voice notes that
// were transcribed before the refinement layer existed.
//
// Up to refineBackfillParallel refinements run at once. Each refinement is a
// single Codex call. The real cap on concurrent Codex work is enforced in
// codexExec (CODEX_MAX_CONCURRENT, default 2) across image + refine combined,
// so the history backfill stays gentle on the ChatGPT quota.
const refineBackfillParallel = 2

func (m *MediaUnderstandingManager) refineBackfillLoop() {
	time.Sleep(60 * time.Second) // let initial transcription work settle
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		if m.enabledFor("audio") {
			batch := m.s.store.PendingTranscriptsToRefine(30)
			if len(batch) > 0 {
				m.processRefineBatch(batch)
			}
		}
		<-t.C
	}
}

// processRefineBatch fans out the batch to N parallel workers — each pulls a
// target off the channel until the batch drains. Keeps memory bounded and
// gives clean shutdown if the audio toggle flips off mid-batch.
func (m *MediaUnderstandingManager) processRefineBatch(batch []db.RefineTarget) {
	ch := make(chan db.RefineTarget)
	var wg sync.WaitGroup
	for i := 0; i < refineBackfillParallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for target := range ch {
				if !m.enabledFor("audio") {
					return
				}
				refined := m.refineTranscript(target.ChatJID, target.MessageID, target.Raw)
				if strings.TrimSpace(refined) == "" || strings.TrimSpace(refined) == strings.TrimSpace(target.Raw) {
					continue
				}
				m.s.store.SetTranscriptRefined(target.ChatJID, target.MessageID, refined)
			}
		}()
	}
	for _, t := range batch {
		ch <- t
	}
	close(ch)
	wg.Wait()
}

func (m *MediaUnderstandingManager) enabledFor(kind string) bool {
	if !mediaUnderstandingEnabled {
		return false
	}
	key := audioEnabledKey
	if kind == "image" {
		key = imageEnabledKey
	}
	v, _, _ := m.s.store.GetSyncState(key)
	// Default ON when never toggled: only an explicit "0" from the UI disables.
	return v != "0"
}

func (m *MediaUnderstandingManager) setEnabled(kind string, enabled bool) {
	if !mediaUnderstandingEnabled {
		return // feature parked behind the rate-limit guard; ignore toggle writes
	}
	key := audioEnabledKey
	if kind == "image" {
		key = imageEnabledKey
	}
	val := "0"
	if enabled {
		val = "1"
	}
	m.s.store.PutSyncState(key, val)
}

func (m *MediaUnderstandingManager) audioAvailable() bool { return m.audioBin != "" }

// workerLoop polls every 30s for pending media of one kind. When work exists
// and the kind is enabled, it processes a batch with `parallel` concurrent
// jobs and repeats until empty.
func (m *MediaUnderstandingManager) workerLoop(kind string, parallel int) {
	time.Sleep(15 * time.Second)
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		if m.enabledFor(kind) {
			if kind == "audio" && !m.audioAvailable() {
				// nothing to do — binary missing
			} else {
				m.drain(kind, parallel)
			}
		}
		<-t.C
	}
}

func (m *MediaUnderstandingManager) drain(kind string, parallel int) {
	for {
		batch := m.s.store.PendingMedia(kind, parallel*3)
		if len(batch) == 0 {
			return
		}
		// Run `parallel` workers over this batch.
		ch := make(chan db.PendingMediaMessage)
		var wg sync.WaitGroup
		for i := 0; i < parallel; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for p := range ch {
					m.processOne(p)
				}
			}()
		}
		for _, p := range batch {
			ch <- p
		}
		close(ch)
		wg.Wait()
	}
}

func (m *MediaUnderstandingManager) processOne(p db.PendingMediaMessage) {
	key := p.MediaType + ":" + p.ChatJID + ":" + p.MessageID
	m.mu.Lock()
	if m.running[key] {
		m.mu.Unlock()
		return
	}
	m.running[key] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.running, key)
		m.mu.Unlock()
	}()

	cwd, _ := filepath.Abs(".")
	path := p.MediaPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}

	switch p.MediaType {
	case "audio":
		text, err := m.transcribeAudio(path)
		muRow := &db.MediaUnderstanding{
			ChatJID: p.ChatJID, MessageID: p.MessageID, Kind: db.MUTranscript,
		}
		if err != nil {
			muRow.Status = db.MUError
			muRow.Error = err.Error()
		} else if strings.TrimSpace(text) == "" {
			muRow.Status = db.MUSkipped
		} else {
			// Whisper output is verbatim and unpunctuated. Run a small LLM
			// refinement pass with chat context so the stored transcript
			// reads as proper Arabic with punctuation, correct English
			// spelling for tech terms, and (when natural) markdown bullets.
			refined := m.refineTranscript(p.ChatJID, p.MessageID, text)
			muRow.Status = db.MUOK
			muRow.Content = strings.TrimSpace(refined)
			// Mark refined=1 only if the refiner actually changed the text.
			// (If it returned the raw text unchanged because of an API blip,
			// the backfill pass will retry it later.)
			if strings.TrimSpace(refined) != strings.TrimSpace(text) {
				muRow.Refined = 1
			}
		}
		m.s.store.UpsertMU(muRow)

	case "image":
		desc, err := m.describeImage(path)
		muRow := &db.MediaUnderstanding{
			ChatJID: p.ChatJID, MessageID: p.MessageID, Kind: db.MUDescription,
		}
		if err != nil {
			muRow.Status = db.MUError
			muRow.Error = err.Error()
		} else if strings.TrimSpace(desc) == "" {
			muRow.Status = db.MUSkipped
		} else {
			muRow.Status = db.MUOK
			muRow.Content = strings.TrimSpace(desc)
		}
		m.s.store.UpsertMU(muRow)
	}
}

// detectWhisperBinary returns the path to a local whisper CLI, or "" if none.
// Order: WHISPER_BIN env, whisper-cli (whisper.cpp), whisper-cpp, whisper.
func detectWhisperBinary() string {
	if v := envOr("WHISPER_BIN", ""); v != "" {
		if _, err := exec.LookPath(v); err == nil {
			return v
		}
		if _, err := exec.LookPath(filepath.Base(v)); err == nil {
			return v
		}
	}
	for _, name := range []string{"whisper-cli", "whisper-cpp", "whisper"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// transcribeAudio handles a WhatsApp voice note end-to-end:
//   1. ffmpeg-convert the source (typically 48kHz Opus .ogg) to 16kHz mono wav
//      in a temp file — whisper.cpp expects 16kHz mono.
//   2. Run whisper-cli on the wav with the user's preferred language (default
//      Arabic, matching the meeting-scribe convention: -l ar handles mixed
//      AR/EN best for this user's content).
//
// Model + language are overridable via .env:
//   WHISPER_MODEL — full path to a ggml-*.bin file
//   WHISPER_LANG  — language code ("ar", "en", "auto", …). Default: "ar".
func (m *MediaUnderstandingManager) transcribeAudio(path string) (string, error) {
	if m.audioBin == "" {
		return "", fmt.Errorf("no whisper binary detected; install whisper.cpp (brew install whisper-cpp)")
	}
	model := findWhisperModel()
	if model == "" {
		return "", fmt.Errorf("no whisper model found. Set WHISPER_MODEL=<path-to-ggml-*.bin> in .env. " +
			"Recommended: $HOME/whisper-models/ggml-large-v3-turbo.bin (~1.6 GB)")
	}
	lang := envOr("WHISPER_LANG", "ar")

	// 1) ffmpeg -> 16kHz mono wav in a temp file (matches meeting-scribe).
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", fmt.Errorf("ffmpeg not found; install with: brew install ffmpeg")
	}
	tmp, err := os.CreateTemp("", "wa-voice-*.wav")
	if err != nil {
		return "", fmt.Errorf("temp file: %w", err)
	}
	tmp.Close()
	wavPath := tmp.Name()
	defer os.Remove(wavPath)

	ctx, cancel := newTimeoutCtx(5 * time.Minute)
	defer cancel()

	conv := exec.CommandContext(ctx, ffmpeg,
		"-hide_banner", "-loglevel", "error", "-y",
		"-i", path,
		"-ar", "16000", "-ac", "1",
		wavPath)
	if out, err := conv.CombinedOutput(); err != nil {
		return "", fmt.Errorf("ffmpeg convert failed: %v: %s", err, snippet(string(out), 240))
	}

	// 2) whisper-cli on the 16kHz mono wav.
	// Flags mirror meeting-scribe: -m model -f wav -otxt -nt --no-prints -l <lang>.
	// -otxt makes whisper-cli write <wavPath>.txt with the transcript.
	args := []string{
		"-m", model,
		"-f", wavPath,
		"-otxt", "-nt", "--no-prints",
	}
	if lang != "" && lang != "auto" {
		args = append(args, "-l", lang)
	}
	cmd := exec.CommandContext(ctx, m.audioBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("whisper failed: %v: %s", err, snippet(string(out), 240))
	}
	// Prefer the .txt sidecar whisper-cli writes; fall back to stdout.
	txt := wavPath + ".txt"
	if b, e := readFileBytes(txt); e == nil {
		os.Remove(txt)
		return strings.TrimSpace(string(b)), nil
	}
	return strings.TrimSpace(string(out)), nil
}

// findWhisperModel returns the path to a usable whisper.cpp model, or "".
// Priority order:
//   1. $WHISPER_MODEL
//   2. ~/whisper-models/ggml-large-v3{-turbo,}.bin (the meeting-scribe path)
//   3. ./models/ggml-*.bin under the bridge cwd
//   4. /opt/homebrew/share/whisper-cpp/models/ggml-*.bin (brew default)
//   5. ~/.cache/whisper/ggml-*.bin
func findWhisperModel() string {
	if v := envOr("WHISPER_MODEL", ""); v != "" {
		if _, err := os.Stat(v); err == nil {
			return v
		}
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		// Existing models from the meeting-scribe setup — preferred.
		filepath.Join(home, "whisper-models/ggml-large-v3-turbo.bin"),
		filepath.Join(home, "whisper-models/ggml-large-v3.bin"),
		filepath.Join(home, "whisper-models/ggml-medium.bin"),
		// Project-local
		"./models/ggml-large-v3-turbo.bin",
		"./models/ggml-large-v3.bin",
		"./models/ggml-medium.bin",
		"./models/ggml-base.en.bin",
		// Homebrew + whisper.cpp conventions
		"/opt/homebrew/share/whisper-cpp/models/ggml-base.en.bin",
		filepath.Join(home, ".cache/whisper/ggml-base.en.bin"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Last resort: glob whisper-models and ./models for any ggml-*.bin
	for _, pattern := range []string{
		filepath.Join(home, "whisper-models/ggml-*.bin"),
		"./models/ggml-*.bin",
	} {
		if matches, err := filepath.Glob(pattern); err == nil && len(matches) > 0 {
			return matches[0]
		}
	}
	return ""
}

// refineSystemInstructions is the cleaning brief for raw whisper output. Kept
// in sync with the previous Claude sidecar, with a "no tools" guard for Codex.
const refineSystemInstructions = `You are a transcript cleaner for Arabic WhatsApp voice notes.

INPUT
- "RAW TRANSCRIPT": the verbatim output of whisper on one voice note. It is mostly correct but has zero punctuation, may have a few misheard words on technical terms, and sometimes transliterates English words into Arabic letters (e.g. writes "إم سي بي" or "هب سبوت" instead of "MCP" or "HubSpot").
- "CONTEXT": the most recent messages in the same chat (oldest→newest). Use this ONLY to disambiguate names, product names, tech terms, and topics that appear in the voice note. Do not invent content that is not in the raw transcript.

YOUR OUTPUT (the refined transcript)
1. Keep the speaker's words faithfully. Do NOT paraphrase, summarize, or add anything they did not say. Only fix obvious mishearings using the chat context.
2. Add proper punctuation where the speaker paused or finished a thought ("،"/"." for clauses, "؟"/"?" for questions, "!" for exclamations). Break into normal sentences and short paragraphs.
3. Restore English spelling for any English word, brand, product, or acronym the speaker said in English — even if whisper transliterated it to Arabic letters (e.g. "إم سي بي" → "MCP", "هب سبوت" → "HubSpot"). Match what the speaker actually said; do not translate Arabic words to English.
4. Keep Arabic words in Arabic and English words in English. The output is mixed AR/EN, mirroring what was spoken.
5. If the speaker naturally enumerates (lists items, counts off steps), format that part as a markdown list. Otherwise keep prose.
6. No headings, no end summary, no speaker labels, no quotes or code fences, and do not include the original raw text.

If you cannot improve the raw transcript (already clean, or too short), output it unchanged.
Do not run any commands or use any tools. Output ONLY the cleaned transcript text.`

// buildRefinePrompt assembles the single Codex prompt from the cleaning brief,
// the optional recent-chat context, and the raw transcript.
func buildRefinePrompt(rawText string, recent []string) string {
	var b strings.Builder
	b.WriteString(refineSystemInstructions)
	b.WriteString("\n\n")
	if len(recent) > 0 {
		b.WriteString("CONTEXT (recent chat, oldest→newest):\n")
		for _, l := range recent {
			b.WriteString("  " + l + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("RAW TRANSCRIPT:\n\"\"\"\n")
	b.WriteString(rawText)
	b.WriteString("\n\"\"\"\n\nReturn the cleaned transcript text only.")
	return b.String()
}

// refineTranscript runs the Codex refiner on a raw whisper transcript using the
// last 10 messages of the chat as context. Returns the refined text — or the
// raw text if Codex can't be reached or returns nothing (so a refiner failure
// NEVER costs us the transcript).
func (m *MediaUnderstandingManager) refineTranscript(chatJID, msgID, rawText string) string {
	ctx := m.recentChatLinesBefore(chatJID, msgID, 10)
	out, err := codexExec(2*time.Minute, buildRefinePrompt(rawText, ctx))
	if err != nil || strings.TrimSpace(out) == "" {
		return rawText
	}
	return out
}

// recentChatLinesBefore returns up to n recent messages in chatJID with
// content before (but not including) the given message id. Voice notes use
// their already-stored transcript when available (so context is genuinely
// readable, not a string of media placeholders).
func (m *MediaUnderstandingManager) recentChatLinesBefore(chatJID, msgID string, n int) []string {
	var ts int64
	m.s.store.DB.QueryRow(`SELECT timestamp FROM messages WHERE id = ? AND chat_jid = ?`,
		msgID, chatJID).Scan(&ts)
	if ts == 0 {
		return nil
	}
	// Pull a small window of messages and pull each one's transcript/description
	// in one extra query — cheaper than per-row joins for a single small batch.
	rows, err := m.s.store.DB.Query(`SELECT
		m.id,
		COALESCE(NULLIF(m.sender_name,''), NULLIF(m.push_name,''), '') AS who,
		m.is_from_me,
		SUBSTR(COALESCE(m.content,''), 1, 280) AS body,
		COALESCE(m.media_type,'') AS media
		FROM messages m
		WHERE m.chat_jid = ? AND m.timestamp < ?
		ORDER BY m.timestamp DESC LIMIT ?`, chatJID, ts, n)
	if err != nil {
		return nil
	}
	defer rows.Close()
	type row struct {
		id, body, media, who string
		fromMe               bool
	}
	var rs []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.id, &r.who, &r.fromMe, &r.body, &r.media) == nil {
			rs = append(rs, r)
		}
	}
	if len(rs) == 0 {
		return nil
	}
	// Lookup transcripts/descriptions for these messages in one shot.
	ids := make([]any, 0, len(rs))
	for _, r := range rs {
		ids = append(ids, r.id)
	}
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	ai := map[string]string{}
	if muRows, err := m.s.store.DB.Query(
		`SELECT message_id, content FROM media_understanding
		 WHERE chat_jid = ? AND status='ok' AND message_id IN (`+placeholders+`)`,
		append([]any{chatJID}, ids...)...); err == nil {
		for muRows.Next() {
			var id, content string
			if muRows.Scan(&id, &content) == nil {
				ai[id] = content
			}
		}
		muRows.Close()
	}
	// Reverse to chronological + build the labeled lines.
	lines := make([]string, 0, len(rs))
	for i := len(rs) - 1; i >= 0; i-- {
		r := rs[i]
		text := strings.ReplaceAll(strings.TrimSpace(r.body), "\n", " ")
		if text == "" {
			if t, ok := ai[r.id]; ok {
				text = strings.ReplaceAll(strings.TrimSpace(t), "\n", " ")
			} else if r.media != "" {
				text = "[" + r.media + "]"
			}
		}
		if text == "" {
			continue
		}
		who := r.who
		if r.fromMe {
			who = "Me"
		}
		if who == "" {
			who = "Someone"
		}
		lines = append(lines, who+": "+text)
	}
	return lines
}

// imageDescribePrompt instructs Codex to describe one WhatsApp image. Mirrors
// the previous Claude vision sidecar, with an explicit "no tools" guard since
// Codex is otherwise an agentic coding CLI.
const imageDescribePrompt = `Describe this WhatsApp image in 1-3 short sentences. Cover:
- What kind of image it is (screenshot, photo, document, meme, etc.)
- The main subject or any visible text (OCR the readable text)
- Any detail useful to a task-tracking agent (e.g. a receipt for $X, a chart of Y, a UI screenshot of Z)
Be concrete; do not start with "this image shows". No greetings, no markdown.
If the image contains Arabic text, transcribe it in Arabic; keep English in English; mixed AR/EN is fine.
If the image is uninformative (blank, decorative), output exactly: Uninformative.
Do not run any commands or use any tools. Output only the description text.`

// describeImage runs Codex vision on a single image file.
func (m *MediaUnderstandingManager) describeImage(path string) (string, error) {
	out, err := codexExec(2*time.Minute, imageDescribePrompt, path)
	if err != nil {
		return "", fmt.Errorf("codex vision failed: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("codex vision: empty result")
	}
	return out, nil
}

// ── HTTP handler ────────────────────────────────────────────────────

type mediaStatusBody struct {
	Disabled        bool         `json:"disabled"` // feature parked behind the rate-limit guard
	AudioEnabled    bool         `json:"audio_enabled"`
	ImageEnabled    bool         `json:"image_enabled"`
	WhisperDetected bool         `json:"whisper_detected"`
	WhisperBinary   string       `json:"whisper_binary,omitempty"`
	Stats           db.MediaStats `json:"stats"`
}

// handleMediaStatus is the GET status / POST toggle endpoint for media
// understanding (voice + image).
//   GET  /api/v2/media/understanding
//   POST /api/v2/media/understanding  {audio_enabled?, image_enabled?}
func (s *Server) handleMediaUnderstanding(w http.ResponseWriter, r *http.Request) {
	if s.mediaUnderstanding == nil {
		jsonError(w, 503, "media worker not initialised")
		return
	}
	m := s.mediaUnderstanding
	switch r.Method {
	case http.MethodGet:
		jsonOK(w, mediaStatusBody{
			Disabled:        !mediaUnderstandingEnabled,
			AudioEnabled:    m.enabledFor("audio"),
			ImageEnabled:    m.enabledFor("image"),
			WhisperDetected: m.audioAvailable(),
			WhisperBinary:   m.audioBin,
			Stats:           s.store.CountMedia(),
		})
	case http.MethodPost:
		var req struct {
			AudioEnabled *bool `json:"audio_enabled,omitempty"`
			ImageEnabled *bool `json:"image_enabled,omitempty"`
		}
		decodeJSON(r, &req)
		if req.AudioEnabled != nil {
			m.setEnabled("audio", *req.AudioEnabled)
		}
		if req.ImageEnabled != nil {
			m.setEnabled("image", *req.ImageEnabled)
		}
		jsonOK(w, mediaStatusBody{
			Disabled:        !mediaUnderstandingEnabled,
			AudioEnabled:    m.enabledFor("audio"),
			ImageEnabled:    m.enabledFor("image"),
			WhisperDetected: m.audioAvailable(),
			WhisperBinary:   m.audioBin,
			Stats:           s.store.CountMedia(),
		})
	default:
		methodNotAllowed(w)
	}
}
