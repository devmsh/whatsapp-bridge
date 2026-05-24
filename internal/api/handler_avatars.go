package api

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// avatarLocks dedupes concurrent fetches of the same JID. A second request
// for an in-flight JID waits on the same channel.
var avatarLocks = struct {
	mu sync.Mutex
	m  map[string]chan struct{}
}{m: map[string]chan struct{}{}}

// avatarBackoff records a "no avatar / fetch failed" marker per JID with a
// short cooldown so we don't hammer whatsmeow when a contact simply has no pic
// (or our connection is offline). Pure in-memory.
var avatarBackoff = struct {
	mu sync.Mutex
	m  map[string]time.Time
}{m: map[string]time.Time{}}

const (
	avatarNopicCooldown = 6 * time.Hour
	avatarFetchTimeout  = 15 * time.Second
)

// handleAvatar serves the profile picture for a contact or group, fetching it
// from WhatsApp on first request and caching the JPEG under <mediaDir>/avatars.
// A 404 means "no picture available" — the UI falls back to the letter avatar.
//
// GET /api/v2/avatars/{jid}        full-size cached image (404 if none)
// GET /api/v2/avatars/{jid}?refresh=1   force a fresh fetch
func (s *Server) handleAvatar(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	rawJID := strings.TrimPrefix(r.URL.Path, "/api/v2/avatars/")
	rawJID = strings.TrimSuffix(rawJID, "/")
	if rawJID == "" || strings.ContainsAny(rawJID, "/\\") {
		http.NotFound(w, r)
		return
	}
	// We refuse to fetch for hidden chats — keep hidden content fully offline.
	if s.store.IsChatHidden(rawJID) && !s.isUnlocked(r) {
		http.NotFound(w, r)
		return
	}
	refresh := r.URL.Query().Get("refresh") == "1"

	avPath := s.avatarPath(rawJID)
	// Fast path: served from disk cache.
	if !refresh {
		if fi, err := os.Stat(avPath); err == nil && fi.Size() > 0 {
			serveCachedAvatar(w, r, avPath, fi)
			return
		}
		// Honor short cooldown for known "no avatar" results.
		avatarBackoff.mu.Lock()
		until, ok := avatarBackoff.m[rawJID]
		avatarBackoff.mu.Unlock()
		if ok && time.Now().Before(until) {
			http.NotFound(w, r)
			return
		}
	}

	// Dedupe concurrent fetches for the same JID.
	avatarLocks.mu.Lock()
	wait, busy := avatarLocks.m[rawJID]
	if busy {
		avatarLocks.mu.Unlock()
		<-wait
		// Try the cache again after the other fetch finished.
		if fi, err := os.Stat(avPath); err == nil && fi.Size() > 0 {
			serveCachedAvatar(w, r, avPath, fi)
			return
		}
		http.NotFound(w, r)
		return
	}
	done := make(chan struct{})
	avatarLocks.m[rawJID] = done
	avatarLocks.mu.Unlock()

	defer func() {
		avatarLocks.mu.Lock()
		delete(avatarLocks.m, rawJID)
		avatarLocks.mu.Unlock()
		close(done)
	}()

	parsedJID, err := types.ParseJID(rawJID)
	if err != nil {
		http.Error(w, "invalid jid", 400)
		return
	}
	wmclient := s.client.GetWhatsmeowClient()
	if wmclient == nil || !wmclient.IsConnected() {
		http.NotFound(w, r)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), avatarFetchTimeout)
	defer cancel()

	params := &whatsmeow.GetProfilePictureParams{
		Preview:     false,
		IsCommunity: false,
	}
	info, err := wmclient.GetProfilePictureInfo(ctx, parsedJID, params)
	if err != nil || info == nil || info.URL == "" {
		rememberNoAvatar(rawJID)
		http.NotFound(w, r)
		return
	}

	body, err := downloadBytes(ctx, info.URL)
	if err != nil || len(body) == 0 {
		rememberNoAvatar(rawJID)
		http.NotFound(w, r)
		return
	}

	if err := writeAvatarFile(avPath, body); err != nil {
		http.Error(w, "save failed", 500)
		return
	}
	w.Header().Set("Content-Type", detectImageType(body))
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(body)
}

// avatarPath returns the on-disk cache path for a JID. The JID is sanitized
// since it contains `@` and `:` which are OK on macOS but ugly to look at.
func (s *Server) avatarPath(jid string) string {
	safe := strings.NewReplacer("@", "_at_", ":", "_", "/", "_", "\\", "_").Replace(jid)
	return filepath.Join(s.cfg.MediaDir, "avatars", safe+".bin")
}

func rememberNoAvatar(jid string) {
	avatarBackoff.mu.Lock()
	avatarBackoff.m[jid] = time.Now().Add(avatarNopicCooldown)
	avatarBackoff.mu.Unlock()
}

func writeAvatarFile(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func serveCachedAvatar(w http.ResponseWriter, r *http.Request, path string, fi os.FileInfo) {
	body, err := os.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", detectImageType(body))
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_ = fi // (intentionally unused; ETag/Last-Modified omitted for now)
	w.Write(body)
}

// detectImageType returns a content-type by sniffing the first bytes. Falls
// back to image/jpeg, which covers WhatsApp's profile photos.
func detectImageType(b []byte) string {
	if len(b) >= 4 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G' {
		return "image/png"
	}
	if len(b) >= 4 && b[0] == 'G' && b[1] == 'I' && b[2] == 'F' {
		return "image/gif"
	}
	if len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP" {
		return "image/webp"
	}
	return "image/jpeg"
}

func downloadBytes(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, &avatarStatusErr{status: resp.StatusCode}
	}
	return io.ReadAll(resp.Body)
}

type avatarStatusErr struct{ status int }

func (e *avatarStatusErr) Error() string {
	return "avatar fetch HTTP " + intToString(e.status)
}

func intToString(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [12]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}
