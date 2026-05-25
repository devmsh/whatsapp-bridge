package api

import (
	"context"
	"encoding/json"
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
func jsonUnmarshalSafe(b []byte, v any) { _ = json.Unmarshal(b, v) }

// MediaUnderstandingManager runs background workers that transcribe voice notes
// (whisper-cli) and describe images (Claude vision via a sidecar). Both kinds
// are gated behind toggles so they don't run by default.
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

func newMediaManager(s *Server) *MediaUnderstandingManager {
	return &MediaUnderstandingManager{s: s, running: map[string]bool{}}
}

// Start launches both kinds of workers. They idle unless enabled.
func (m *MediaUnderstandingManager) Start() {
	m.audioBin = detectWhisperBinary()
	go m.workerLoop("audio", 2)  // 2 parallel transcribes
	go m.workerLoop("image", 3)  // 3 parallel image descriptions
}

func (m *MediaUnderstandingManager) enabledFor(kind string) bool {
	key := audioEnabledKey
	if kind == "image" {
		key = imageEnabledKey
	}
	v, _, _ := m.s.store.GetSyncState(key)
	return v == "1"
}

func (m *MediaUnderstandingManager) setEnabled(kind string, enabled bool) {
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
			muRow.Status = db.MUOK
			muRow.Content = strings.TrimSpace(text)
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

// transcribeAudio runs whisper-cli on a file. Whisper requires a model file
// via -m; without one it exits with code 3. We look in standard places (env
// var WHISPER_MODEL, then a handful of brew/conventional locations) and emit
// a clear error if nothing is found.
func (m *MediaUnderstandingManager) transcribeAudio(path string) (string, error) {
	if m.audioBin == "" {
		return "", fmt.Errorf("no whisper binary detected; install whisper.cpp (brew install whisper-cpp)")
	}
	model := findWhisperModel()
	if model == "" {
		return "", fmt.Errorf("no whisper model found. Set WHISPER_MODEL=<path-to-ggml-*.bin> in .env, " +
			"or place a model at ./models/ggml-base.en.bin. Download a small one: " +
			"curl -L -o ./models/ggml-base.en.bin https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin")
	}
	// whisper-cli flags: -m <model> -f <audio> -otxt (write <path>.txt) -np
	// (no progress) -nt (no timestamps). Stay quiet on stdout.
	args := []string{"-m", model, "-f", path, "-otxt", "-np", "-nt"}
	ctx, cancel := newTimeoutCtx(5 * time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.audioBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("whisper failed: %v: %s", err, snippet(string(out), 240))
	}
	// Prefer the .txt sidecar whisper-cli writes; fall back to stdout.
	txt := path + ".txt"
	if b, e := readFileBytes(txt); e == nil {
		os.Remove(txt) // we have the text; don't pollute the media dir
		return strings.TrimSpace(string(b)), nil
	}
	return strings.TrimSpace(string(out)), nil
}

// findWhisperModel returns the path to a usable whisper.cpp model, or "".
// Checked in priority order:
//   1. $WHISPER_MODEL
//   2. ./models/ggml-*.bin under the bridge cwd
//   3. /opt/homebrew/share/whisper-cpp/models/ggml-*.bin (brew default)
//   4. ~/.cache/whisper/ggml-*.bin
func findWhisperModel() string {
	if v := envOr("WHISPER_MODEL", ""); v != "" {
		if _, err := os.Stat(v); err == nil {
			return v
		}
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		"./models/ggml-base.en.bin",
		"./models/ggml-small.en.bin",
		"./models/ggml-medium.en.bin",
		"/opt/homebrew/share/whisper-cpp/models/ggml-base.en.bin",
		"/opt/homebrew/share/whisper-cpp/models/ggml-small.en.bin",
		filepath.Join(home, ".cache/whisper/ggml-base.en.bin"),
		filepath.Join(home, ".cache/whisper/ggml-small.en.bin"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Last resort: glob ./models for any ggml-*.bin
	if matches, err := filepath.Glob("./models/ggml-*.bin"); err == nil && len(matches) > 0 {
		return matches[0]
	}
	return ""
}

// describeImage runs the image sidecar (Claude vision) on a single file.
func (m *MediaUnderstandingManager) describeImage(path string) (string, error) {
	out, err := m.s.runAgentInput(2*time.Minute, "", "describe-image.mjs", path)
	if err != nil {
		return "", fmt.Errorf("image sidecar failed: %v", err)
	}
	if line := lastJSONLine(out); line != nil {
		var res struct {
			OK          bool   `json:"ok"`
			Description string `json:"description"`
		}
		jsonUnmarshalSafe(line, &res)
		if res.OK {
			return res.Description, nil
		}
		return "", fmt.Errorf("vision: %s", res.Description)
	}
	return "", fmt.Errorf("no result")
}

// ── HTTP handler ────────────────────────────────────────────────────

type mediaStatusBody struct {
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
