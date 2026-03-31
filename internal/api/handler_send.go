package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waCommon "go.mau.fi/whatsmeow/proto/waCommon"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"whatsapp-bridge-v2/internal/db"
)

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		JID       string `json:"jid"`
		Message   string `json:"message"`
		MediaPath string `json:"media_path,omitempty"`
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
		if err := buildMediaMessage(wa, msg, req.MediaPath, req.Message); err != nil {
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

	chatJID := recipientJID.String()
	now := time.Now().Unix()
	s.store.StoreMessage(&db.Message{
		ID:       resp.ID,
		ChatJID:  chatJID,
		Sender:   wa.Store.ID.User,
		Content:  req.Message,
		Timestamp: now,
		IsFromMe: true,
	})
	s.store.UpdateChatLastMessage(chatJID, "", now)

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
	participant := req.ChatJID // default: the chat itself (for 1:1)
	if origMsg != nil {
		quotedMsg = &waE2E.Message{
			Conversation: proto.String(origMsg.Content),
		}
		if origMsg.Sender != "" {
			participant = origMsg.Sender + "@s.whatsapp.net"
		}
		if origMsg.IsFromMe {
			participant = s.client.GetWhatsmeowClient().Store.ID.ToNonAD().String()
		}
	}

	wa := s.client.GetWhatsmeowClient()
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(req.Message),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:      proto.String(req.MessageID),
				Participant:   proto.String(participant),
				QuotedMessage: quotedMsg,
			},
		},
	}

	resp, err := wa.SendMessage(context.Background(), chatJID, msg)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("reply failed: %v", err))
		return
	}

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

	wa := s.client.GetWhatsmeowClient()
	msg := &waE2E.Message{
		ReactionMessage: &waE2E.ReactionMessage{
			Key: &waCommon.MessageKey{
				RemoteJID: proto.String(req.ChatJID),
				ID:        proto.String(req.MessageID),
				FromMe:    proto.Bool(false),
			},
			Text: proto.String(req.Emoji),
		},
	}

	resp, err := wa.SendMessage(context.Background(), chatJID, msg)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("react failed: %v", err))
		return
	}

	jsonOK(w, map[string]interface{}{"success": true, "message_id": resp.ID})
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
				IsForwarded:    proto.Bool(true),
				ForwardingScore: proto.Uint32(1),
			},
		},
	}

	resp, err := wa.SendMessage(context.Background(), toJID, msg)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("forward failed: %v", err))
		return
	}

	jsonOK(w, map[string]interface{}{"success": true, "message_id": resp.ID})
}

func parseJID(jid string) (types.JID, error) {
	if strings.Contains(jid, "@") {
		return types.ParseJID(jid)
	}
	return types.JID{User: jid, Server: "s.whatsapp.net"}, nil
}

func buildMediaMessage(wa *whatsmeow.Client, msg *waE2E.Message, mediaPath, caption string) error {
	data, err := os.ReadFile(mediaPath)
	if err != nil {
		return fmt.Errorf("read media file: %w", err)
	}

	ext := strings.ToLower(mediaPath[strings.LastIndex(mediaPath, ".")+1:])
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
