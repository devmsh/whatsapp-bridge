package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// handleTTSSend generates speech via ElevenLabs TTS and sends it as a WhatsApp voice note.
// POST /api/v2/tts-send
func (s *Server) handleTTSSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	var req struct {
		JID     string `json:"jid"`
		Text    string `json:"text"`
		VoiceID string `json:"voice_id,omitempty"`
		ModelID string `json:"model_id,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	if req.JID == "" {
		jsonError(w, 400, "jid is required")
		return
	}
	if req.Text == "" {
		jsonError(w, 400, "text is required")
		return
	}
	if !s.guardChatAccess(w, r, req.JID) {
		return
	}

	apiKey := s.cfg.ElevenLabsAPIKey
	if apiKey == "" {
		jsonError(w, 500, "ELEVENLABS_API_KEY not configured")
		return
	}
	if req.VoiceID == "" {
		req.VoiceID = s.cfg.ElevenLabsVoiceID
		if req.VoiceID == "" {
			jsonError(w, 400, "voice_id is required (or set ELEVENLABS_VOICE_ID)")
			return
		}
	}
	if req.ModelID == "" {
		req.ModelID = "eleven_multilingual_v2"
	}

	// Generate TTS audio
	audioPath, err := generateTTS(apiKey, req.VoiceID, req.ModelID, req.Text, s.mediaDir)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("TTS generation failed: %v", err))
		return
	}
	defer os.Remove(audioPath)

	// Send as voice note
	recipientJID, err := parseJID(req.JID)
	if err != nil {
		jsonError(w, 400, fmt.Sprintf("invalid JID: %v", err))
		return
	}

	wa := s.client.GetWhatsmeowClient()
	msg := buildAudioMessage(wa, audioPath, true)
	if msg == nil {
		jsonError(w, 500, "failed to build audio message")
		return
	}

	ts, err := wa.SendMessage(r.Context(), recipientJID, msg)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("send failed: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success":    true,
		"message_id": ts.ID,
		"timestamp":  ts.Timestamp.Unix(),
	})
}

// generateTTS calls ElevenLabs TTS API and saves the result to an MP3 file.
func generateTTS(apiKey, voiceID, modelID, text, mediaDir string) (string, error) {
	url := fmt.Sprintf("https://api.elevenlabs.io/v1/text-to-speech/%s", voiceID)

	body := fmt.Sprintf(`{"text":%q,"model_id":%q}`, text, modelID)
	httpReq, err := http.NewRequest("POST", url, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("xi-api-key", apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ElevenLabs API returned %d: %s", resp.StatusCode, string(errBody))
	}

	// Save to temp file in media dir
	filename := fmt.Sprintf("tts-%d.mp3", time.Now().UnixMilli())
	audioPath := filepath.Join(mediaDir, filename)
	f, err := os.Create(audioPath)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(audioPath)
		return "", fmt.Errorf("write audio: %w", err)
	}

	return audioPath, nil
}
