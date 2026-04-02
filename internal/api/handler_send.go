package api

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	_ "golang.org/x/image/webp"

	"whatsapp-bridge-v2/internal/db"
)

// getAudioDuration returns the duration in seconds using ffprobe.
// Returns 0 if ffprobe is unavailable or fails.
func getAudioDuration(path string) uint32 {
	out, err := exec.Command("ffprobe", "-v", "quiet", "-show_entries", "format=duration", "-of", "csv=p=0", path).Output()
	if err != nil {
		return 0
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0
	}
	return uint32(math.Ceil(f))
}

// convertToOpus converts any audio file to OGG Opus format (required for WA voice note transcription).
// Returns the path to the converted file, or the original path if conversion fails/unnecessary.
func convertToOpus(srcPath string) (string, bool) {
	ext := strings.ToLower(srcPath[strings.LastIndex(srcPath, ".")+1:])
	if ext == "ogg" {
		return srcPath, false // already OGG, no temp file created
	}
	dst := srcPath[:strings.LastIndex(srcPath, ".")] + ".ogg"
	err := exec.Command("ffmpeg", "-y", "-i", srcPath, "-c:a", "libopus", "-b:a", "48k", "-vn", dst).Run()
	if err != nil {
		return srcPath, false // fallback to original
	}
	return dst, true
}

// generateWaveform produces a 64-byte waveform from audio using ffmpeg.
// Each byte is an amplitude sample (0-100) that WhatsApp uses for the visual waveform.
func generateWaveform(path string) []byte {
	// Extract raw PCM samples, compute RMS per chunk
	out, err := exec.Command("ffmpeg", "-i", path, "-ac", "1", "-ar", "8000", "-f", "s16le", "-").Output()
	if err != nil || len(out) < 128 {
		return nil
	}
	// 16-bit signed LE samples
	numSamples := len(out) / 2
	chunkSize := numSamples / 64
	if chunkSize < 1 {
		chunkSize = 1
	}
	waveform := make([]byte, 64)
	var maxRMS float64
	rmsValues := make([]float64, 64)
	for i := 0; i < 64; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > numSamples {
			end = numSamples
		}
		var sumSq float64
		for j := start; j < end; j++ {
			idx := j * 2
			if idx+1 >= len(out) {
				break
			}
			sample := int16(out[idx]) | int16(out[idx+1])<<8
			sumSq += float64(sample) * float64(sample)
		}
		rms := math.Sqrt(sumSq / float64(end-start))
		rmsValues[i] = rms
		if rms > maxRMS {
			maxRMS = rms
		}
	}
	if maxRMS > 0 {
		for i := 0; i < 64; i++ {
			waveform[i] = byte(rmsValues[i] / maxRMS * 100)
		}
	}
	return waveform
}

// getImageDimensions decodes image data to get width and height.
func getImageDimensions(data []byte) (int, int, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

// makeJpegThumbnail creates a small JPEG thumbnail from image data.
func makeJpegThumbnail(data []byte) []byte {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	// Resize to max 72px on longest side
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	maxDim := 72
	if w > h {
		h = h * maxDim / w
		w = maxDim
	} else {
		w = w * maxDim / h
		h = maxDim
	}
	// Create thumbnail using simple nearest-neighbor
	thumb := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			srcX := x * bounds.Dx() / w
			srcY := y * bounds.Dy() / h
			thumb.Set(x, y, img.At(bounds.Min.X+srcX, bounds.Min.Y+srcY))
		}
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, thumb, &jpeg.Options{Quality: 50})
	return buf.Bytes()
}

// storeOutgoingMessage saves a sent message to the local DB and updates chat activity.
func (s *Server) storeOutgoingMessage(msgID, chatJID, content, msgType string) {
	wa := s.client.GetWhatsmeowClient()
	now := time.Now().Unix()
	s.store.StoreMessage(&db.Message{
		ID:          msgID,
		ChatJID:     chatJID,
		Sender:      wa.Store.ID.User,
		Content:     content,
		Timestamp:   now,
		IsFromMe:    true,
		MessageType: msgType,
	})
	s.store.UpdateChatLastMessage(chatJID, "", now)
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		JID       string `json:"jid"`
		Message   string `json:"message"`
		MediaPath string `json:"media_path,omitempty"`
		Sticker   bool   `json:"sticker,omitempty"` // true = send .webp as sticker; default = image
		PTT       bool   `json:"ptt,omitempty"`     // true = send audio as voice note (push-to-talk)
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	if req.JID == "" {
		jsonError(w, 400, "jid is required")
		return
	}
	if req.Message == "" && req.MediaPath == "" {
		jsonError(w, 400, "message or media_path required")
		return
	}

	recipientJID, err := parseJID(req.JID)
	if err != nil {
		jsonError(w, 400, fmt.Sprintf("invalid JID: %v", err))
		return
	}

	wa := s.client.GetWhatsmeowClient()
	msg := &waE2E.Message{}

	if req.MediaPath != "" {
		// forceImage=true by default; sticker only when explicitly requested
		if err := buildMediaMessageEx(wa, msg, req.MediaPath, req.Message, !req.Sticker, req.PTT); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
	} else {
		msg.Conversation = proto.String(req.Message)
	}

	resp, err := wa.SendMessage(context.Background(), recipientJID, msg)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("send failed: %v", err))
		return
	}

	msgType := "text"
	if req.MediaPath != "" {
		msgType = "media"
	}
	s.storeOutgoingMessage(resp.ID, recipientJID.String(), req.Message, msgType)

	jsonOK(w, map[string]interface{}{
		"success":    true,
		"message_id": resp.ID,
		"timestamp":  resp.Timestamp.Unix(),
	})
}

func (s *Server) handleReply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		ChatJID       string   `json:"chat_jid"`
		MessageID     string   `json:"message_id"`
		Message       string   `json:"message"`
		MediaPath     string   `json:"media_path,omitempty"`
		MentionedJIDs []string `json:"mentioned_jids,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}

	chatJID, err := parseJID(req.ChatJID)
	if err != nil {
		jsonError(w, 400, "invalid chat_jid")
		return
	}

	// Look up the original message to include in the quote
	origMsg, _ := s.store.GetMessage(req.MessageID, req.ChatJID)
	var quotedMsg *waE2E.Message
	participant := req.ChatJID
	if origMsg != nil {
		// Build the correct quoted message type based on original message type
		if origMsg.MediaType == "image" || origMsg.MessageType == "media" && strings.Contains(origMsg.Content, "[image:") {
			quotedMsg = &waE2E.Message{
				ImageMessage: &waE2E.ImageMessage{
					Caption: proto.String(origMsg.MediaCaption),
				},
			}
		} else {
			quotedMsg = &waE2E.Message{
				Conversation: proto.String(origMsg.Content),
			}
		}
		if origMsg.IsFromMe {
			participant = s.client.GetWhatsmeowClient().Store.ID.ToNonAD().String()
		} else if origMsg.Sender != "" {
			if strings.Contains(origMsg.Sender, "@") {
				participant = origMsg.Sender
			} else {
				participant = origMsg.Sender + "@s.whatsapp.net"
			}
		}
	}

	contextInfo := &waE2E.ContextInfo{
		StanzaID:      proto.String(req.MessageID),
		Participant:   proto.String(participant),
		QuotedMessage: quotedMsg,
		MentionedJID:  req.MentionedJIDs,
	}

	wa := s.client.GetWhatsmeowClient()
	var msg *waE2E.Message

	if req.MediaPath != "" {
		// Media reply — build media message with ContextInfo attached
		// Force webp as image (not sticker) for replies
		msg = &waE2E.Message{}
		if err := buildMediaMessageEx(wa, msg, req.MediaPath, req.Message, true, false); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		// Attach ContextInfo to whichever media type was built
		if msg.ImageMessage != nil {
			msg.ImageMessage.ContextInfo = contextInfo
		} else if msg.VideoMessage != nil {
			msg.VideoMessage.ContextInfo = contextInfo
		} else if msg.AudioMessage != nil {
			msg.AudioMessage.ContextInfo = contextInfo
		} else if msg.DocumentMessage != nil {
			msg.DocumentMessage.ContextInfo = contextInfo
		} else if msg.StickerMessage != nil {
			msg.StickerMessage.ContextInfo = contextInfo
		}
	} else {
		// Text reply
		msg = &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:        proto.String(req.Message),
				ContextInfo: contextInfo,
			},
		}
	}

	resp, err := wa.SendMessage(context.Background(), chatJID, msg)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("reply failed: %v", err))
		return
	}

	msgType := "text"
	if req.MediaPath != "" {
		msgType = "media"
	}
	s.storeOutgoingMessage(resp.ID, req.ChatJID, req.Message, msgType)

	jsonOK(w, map[string]interface{}{
		"success":    true,
		"message_id": resp.ID,
	})
}

func (s *Server) handleReact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		ChatJID   string `json:"chat_jid"`
		MessageID string `json:"message_id"`
		Emoji     string `json:"emoji"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}

	chatJID, err := parseJID(req.ChatJID)
	if err != nil {
		jsonError(w, 400, "invalid chat_jid")
		return
	}

	// Determine who sent the original message for BuildReaction
	wa := s.client.GetWhatsmeowClient()
	senderJID := wa.Store.ID.ToNonAD() // default: from me
	origMsg, _ := s.store.GetMessage(req.MessageID, req.ChatJID)
	if origMsg != nil && !origMsg.IsFromMe && origMsg.Sender != "" {
		parsed, perr := types.ParseJID(origMsg.Sender + "@s.whatsapp.net")
		if perr == nil {
			senderJID = parsed
		}
	}

	msg := wa.BuildReaction(chatJID, senderJID, types.MessageID(req.MessageID), req.Emoji)

	_, err = wa.SendMessage(context.Background(), chatJID, msg)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("react failed: %v", err))
		return
	}

	// Store reaction locally
	s.store.StoreReaction(&db.Reaction{
		MessageID:  req.MessageID,
		ChatJID:    req.ChatJID,
		Sender:     wa.Store.ID.User,
		SenderName: "",
		Emoji:      req.Emoji,
		Timestamp:  time.Now().Unix(),
	})

	jsonOK(w, map[string]interface{}{"success": true})
}

func (s *Server) handleMention(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		ChatJID       string   `json:"chat_jid"`
		Message       string   `json:"message"`
		MentionedJIDs []string `json:"mentioned_jids"`
		MentionAll    bool     `json:"mention_all"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}

	chatJID, err := parseJID(req.ChatJID)
	if err != nil {
		jsonError(w, 400, "invalid chat_jid")
		return
	}

	wa := s.client.GetWhatsmeowClient()
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(req.Message),
			ContextInfo: &waE2E.ContextInfo{
				MentionedJID: req.MentionedJIDs,
			},
		},
	}

	resp, err := wa.SendMessage(context.Background(), chatJID, msg)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("mention failed: %v", err))
		return
	}

	s.storeOutgoingMessage(resp.ID, req.ChatJID, req.Message, "text")

	jsonOK(w, map[string]interface{}{"success": true, "message_id": resp.ID})
}

func (s *Server) handleForward(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		FromChat  string `json:"from_chat"`
		MessageID string `json:"message_id"`
		ToChat    string `json:"to_chat"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}

	original, err := s.store.GetMessage(req.MessageID, req.FromChat)
	if err != nil || original == nil {
		jsonError(w, 404, "message not found")
		return
	}

	toJID, err := parseJID(req.ToChat)
	if err != nil {
		jsonError(w, 400, "invalid to_chat")
		return
	}

	wa := s.client.GetWhatsmeowClient()
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(original.Content),
			ContextInfo: &waE2E.ContextInfo{
				IsForwarded:     proto.Bool(true),
				ForwardingScore: proto.Uint32(1),
			},
		},
	}

	resp, err := wa.SendMessage(context.Background(), toJID, msg)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("forward failed: %v", err))
		return
	}

	s.storeOutgoingMessage(resp.ID, req.ToChat, original.Content, "text")

	jsonOK(w, map[string]interface{}{"success": true, "message_id": resp.ID})
}

func parseJID(jid string) (types.JID, error) {
	if strings.Contains(jid, "@") {
		return types.ParseJID(jid)
	}
	return types.JID{User: jid, Server: "s.whatsapp.net"}, nil
}

func buildMediaMessage(wa *whatsmeow.Client, msg *waE2E.Message, mediaPath, caption string) error {
	return buildMediaMessageEx(wa, msg, mediaPath, caption, false, false)
}

func buildMediaMessageEx(wa *whatsmeow.Client, msg *waE2E.Message, mediaPath, caption string, forceImage bool, ptt bool) error {
	data, err := os.ReadFile(mediaPath)
	if err != nil {
		return fmt.Errorf("read media file: %w", err)
	}

	ext := strings.ToLower(mediaPath[strings.LastIndex(mediaPath, ".")+1:])
	isSticker := ext == "webp" && !forceImage
	var mediaType whatsmeow.MediaType
	var mimeType string

	switch ext {
	case "jpg", "jpeg":
		mediaType = whatsmeow.MediaImage
		mimeType = "image/jpeg"
	case "png":
		mediaType = whatsmeow.MediaImage
		mimeType = "image/png"
	case "gif":
		mediaType = whatsmeow.MediaImage
		mimeType = "image/gif"
	case "webp":
		mediaType = whatsmeow.MediaImage
		mimeType = "image/webp"
	case "mp4":
		mediaType = whatsmeow.MediaVideo
		mimeType = "video/mp4"
	case "mov":
		mediaType = whatsmeow.MediaVideo
		mimeType = "video/quicktime"
	case "ogg":
		mediaType = whatsmeow.MediaAudio
		mimeType = "audio/ogg; codecs=opus"
	case "mp3":
		mediaType = whatsmeow.MediaAudio
		mimeType = "audio/mpeg"
	case "m4a", "aac":
		mediaType = whatsmeow.MediaAudio
		mimeType = "audio/mp4"
	case "pdf":
		mediaType = whatsmeow.MediaDocument
		mimeType = "application/pdf"
	default:
		mediaType = whatsmeow.MediaDocument
		mimeType = "application/octet-stream"
	}

	resp, err := wa.Upload(context.Background(), data, mediaType)
	if err != nil {
		return fmt.Errorf("upload media: %w", err)
	}

	if isSticker {
		msg.StickerMessage = &waE2E.StickerMessage{
			Mimetype:      proto.String(mimeType),
			URL:           &resp.URL,
			DirectPath:    &resp.DirectPath,
			MediaKey:      resp.MediaKey,
			FileEncSHA256: resp.FileEncSHA256,
			FileSHA256:    resp.FileSHA256,
			FileLength:    &resp.FileLength,
			PngThumbnail:  data, // use the webp data as thumbnail
		}
		return nil
	}

	// Detect image dimensions + generate thumbnail for proper WhatsApp layout
	var imgWidth, imgHeight uint32
	var jpegThumb []byte
	if mediaType == whatsmeow.MediaImage {
		if w, h, err := getImageDimensions(data); err == nil {
			imgWidth, imgHeight = uint32(w), uint32(h)
		}
		jpegThumb = makeJpegThumbnail(data)
	}

	switch mediaType {
	case whatsmeow.MediaImage:
		msg.ImageMessage = &waE2E.ImageMessage{
			Caption:       proto.String(caption),
			Mimetype:      proto.String(mimeType),
			URL:           &resp.URL,
			DirectPath:    &resp.DirectPath,
			MediaKey:      resp.MediaKey,
			FileEncSHA256: resp.FileEncSHA256,
			FileSHA256:    resp.FileSHA256,
			FileLength:    &resp.FileLength,
			Width:         &imgWidth,
			Height:        &imgHeight,
			JPEGThumbnail: jpegThumb,
		}
	case whatsmeow.MediaVideo:
		msg.VideoMessage = &waE2E.VideoMessage{
			Caption:       proto.String(caption),
			Mimetype:      proto.String(mimeType),
			URL:           &resp.URL,
			DirectPath:    &resp.DirectPath,
			MediaKey:      resp.MediaKey,
			FileEncSHA256: resp.FileEncSHA256,
			FileSHA256:    resp.FileSHA256,
			FileLength:    &resp.FileLength,
		}
	case whatsmeow.MediaAudio:
		audioPath := mediaPath
		audioMime := mimeType
		audioData := data

		// For voice notes (PTT): convert to Opus and generate waveform
		if ptt {
			if converted, isTemp := convertToOpus(mediaPath); converted != mediaPath {
				audioPath = converted
				audioMime = "audio/ogg; codecs=opus"
				if convData, err := os.ReadFile(audioPath); err == nil {
					audioData = convData
				}
				if isTemp {
					defer os.Remove(audioPath)
				}
				// Re-upload the converted file
				resp, err = wa.Upload(context.Background(), audioData, whatsmeow.MediaAudio)
				if err != nil {
					return fmt.Errorf("upload converted audio: %w", err)
				}
			}
		}

		dur := getAudioDuration(audioPath)
		audioMsg := &waE2E.AudioMessage{
			Mimetype:      proto.String(audioMime),
			URL:           &resp.URL,
			DirectPath:    &resp.DirectPath,
			MediaKey:      resp.MediaKey,
			FileEncSHA256: resp.FileEncSHA256,
			FileSHA256:    resp.FileSHA256,
			FileLength:    &resp.FileLength,
			Seconds:       &dur,
			PTT:           proto.Bool(ptt),
		}
		if ptt {
			if wf := generateWaveform(audioPath); wf != nil {
				audioMsg.Waveform = wf
			}
		}
		msg.AudioMessage = audioMsg
	case whatsmeow.MediaDocument:
		fileName := mediaPath[strings.LastIndex(mediaPath, "/")+1:]
		msg.DocumentMessage = &waE2E.DocumentMessage{
			Title:         proto.String(fileName),
			FileName:      proto.String(fileName),
			Caption:       proto.String(caption),
			Mimetype:      proto.String(mimeType),
			URL:           &resp.URL,
			DirectPath:    &resp.DirectPath,
			MediaKey:      resp.MediaKey,
			FileEncSHA256: resp.FileEncSHA256,
			FileSHA256:    resp.FileSHA256,
			FileLength:    &resp.FileLength,
		}
	}

	return nil
}

// buildAudioMessage uploads an audio file and returns a ready-to-send voice note message.
func buildAudioMessage(wa *whatsmeow.Client, audioPath string, ptt bool) *waE2E.Message {
	msg := &waE2E.Message{}
	if err := buildMediaMessageEx(wa, msg, audioPath, "", true, ptt); err != nil {
		return nil
	}
	return msg
}
