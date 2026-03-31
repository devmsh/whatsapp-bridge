package api

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	_ "golang.org/x/image/webp"

	"whatsapp-bridge-v2/internal/db"
)

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
		if err := buildMediaMessageEx(wa, msg, req.MediaPath, req.Message, !req.Sticker); err != nil {
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
		ChatJID   string `json:"chat_jid"`
		MessageID string `json:"message_id"`
		Message   string `json:"message"`
		MediaPath string `json:"media_path,omitempty"`
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
	}

	wa := s.client.GetWhatsmeowClient()
	var msg *waE2E.Message

	if req.MediaPath != "" {
		// Media reply — build media message with ContextInfo attached
		// Force webp as image (not sticker) for replies
		msg = &waE2E.Message{}
		if err := buildMediaMessageEx(wa, msg, req.MediaPath, req.Message, true); err != nil {
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
	return buildMediaMessageEx(wa, msg, mediaPath, caption, false)
}

func buildMediaMessageEx(wa *whatsmeow.Client, msg *waE2E.Message, mediaPath, caption string, forceImage bool) error {
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
		msg.AudioMessage = &waE2E.AudioMessage{
			Mimetype:      proto.String(mimeType),
			URL:           &resp.URL,
			DirectPath:    &resp.DirectPath,
			MediaKey:      resp.MediaKey,
			FileEncSHA256: resp.FileEncSHA256,
			FileSHA256:    resp.FileSHA256,
			FileLength:    &resp.FileLength,
			PTT:           proto.Bool(true),
		}
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
