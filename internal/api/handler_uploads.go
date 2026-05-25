package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// handleUpload accepts a single multipart file (field name "file") from the
// web UI and writes it under <MediaDir>/uploads/<yyyymm>/<random>.<ext>. The
// path returned in the JSON response is the relative form the existing /send
// and /reply endpoints already understand as media_path, so the UI can do:
//
//	const { path } = await api.upload(file)
//	await api.send(jid, caption, { media_path: path })
//
// 25 MiB cap matches WhatsApp's own per-file limit for documents/media in the
// web client; oversize uploads are rejected before they hit disk.
const uploadMaxBytes = 25 * 1024 * 1024

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	// MaxBytesReader bounds the multipart parser too — once the limit is hit
	// the next read errors out, so we never buffer a huge file in memory.
	r.Body = http.MaxBytesReader(w, r.Body, uploadMaxBytes)
	if err := r.ParseMultipartForm(uploadMaxBytes); err != nil {
		jsonError(w, 413, "upload too large or malformed: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		jsonError(w, 400, "missing 'file' part")
		return
	}
	defer file.Close()

	// Pick a destination path under <MediaDir>/uploads/<yyyymm>/<random>.<ext>.
	// Random name avoids collisions and stops the browser-supplied filename
	// from leaking onto disk verbatim (could contain path separators / unicode
	// the FS doesn't like).
	ext := filepath.Ext(header.Filename)
	if len(ext) > 10 {
		// guard against ridiculous extensions
		ext = ""
	}
	ext = strings.ToLower(ext)
	month := time.Now().Format("200601")
	relDir := filepath.Join("uploads", month)
	absDir := filepath.Join(s.mediaDir, relDir)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		jsonError(w, 500, "mkdir: "+err.Error())
		return
	}
	name, err := randomName()
	if err != nil {
		jsonError(w, 500, "random: "+err.Error())
		return
	}
	relPath := filepath.Join(relDir, name+ext)
	absPath := filepath.Join(s.mediaDir, relPath)

	out, err := os.Create(absPath)
	if err != nil {
		jsonError(w, 500, "create: "+err.Error())
		return
	}
	n, err := io.Copy(out, file)
	closeErr := out.Close()
	if err != nil {
		// Clean up the partial file so a failed upload doesn't leave litter.
		_ = os.Remove(absPath)
		jsonError(w, 500, "write: "+err.Error())
		return
	}
	if closeErr != nil {
		_ = os.Remove(absPath)
		jsonError(w, 500, "close: "+closeErr.Error())
		return
	}

	// /send and /reply both call os.ReadFile(media_path) directly, so we
	// hand back an absolute path that resolves regardless of the bridge's
	// CWD. The UI passes it back verbatim — it should not need to know the
	// server's filesystem layout.
	absResolved, _ := filepath.Abs(absPath)
	jsonOK(w, map[string]interface{}{
		"path":     absResolved,
		"size":     n,
		"mime":     header.Header.Get("Content-Type"),
		"filename": header.Filename,
	})
}

func randomName() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return hex.EncodeToString(b), nil
}
