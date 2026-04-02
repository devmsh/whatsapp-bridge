package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
)

// ── Tool definitions ────────────────────────────────────────────────

func toolSend() mcp.Tool {
	return mcp.NewTool("wa_send",
		mcp.WithDescription("Send a WhatsApp message (text and/or media). Requires the bridge daemon to be running."),
		mcp.WithString("jid", mcp.Required(), mcp.Description("Recipient JID (e.g. 966535435254@s.whatsapp.net or 120363406393924600@g.us)")),
		mcp.WithString("message", mcp.Description("Text message to send")),
		mcp.WithString("media_path", mcp.Description("Absolute path to media file to send")),
		mcp.WithBoolean("ptt", mcp.Description("Send audio as voice note (push-to-talk). Only applies to audio files.")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			ReadOnlyHint:    mcp.ToBoolPtr(false),
			DestructiveHint: mcp.ToBoolPtr(false),
		}),
	)
}

func toolReply() mcp.Tool {
	return mcp.NewTool("wa_reply",
		mcp.WithDescription("Reply to a specific WhatsApp message (quoted reply). Requires the bridge daemon to be running."),
		mcp.WithString("chat_jid", mcp.Required(), mcp.Description("Chat JID where the original message is")),
		mcp.WithString("message_id", mcp.Required(), mcp.Description("ID of the message to reply to")),
		mcp.WithString("message", mcp.Required(), mcp.Description("Reply text")),
		mcp.WithString("media_path", mcp.Description("Optional media file to include in reply")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			ReadOnlyHint:    mcp.ToBoolPtr(false),
			DestructiveHint: mcp.ToBoolPtr(false),
		}),
	)
}

func toolReact() mcp.Tool {
	return mcp.NewTool("wa_react",
		mcp.WithDescription("React to a WhatsApp message with an emoji. Requires the bridge daemon to be running."),
		mcp.WithString("chat_jid", mcp.Required(), mcp.Description("Chat JID where the message is")),
		mcp.WithString("message_id", mcp.Required(), mcp.Description("ID of the message to react to")),
		mcp.WithString("emoji", mcp.Required(), mcp.Description("Emoji to react with (e.g. 👍, ❤️, 😂)")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			ReadOnlyHint:    mcp.ToBoolPtr(false),
			DestructiveHint: mcp.ToBoolPtr(false),
		}),
	)
}

func toolTTSSend() mcp.Tool {
	return mcp.NewTool("wa_voice",
		mcp.WithDescription("Generate speech from text using ElevenLabs TTS and send it as a WhatsApp voice note. Requires ELEVENLABS_API_KEY and ELEVENLABS_VOICE_ID env vars (or pass voice_id)."),
		mcp.WithString("jid", mcp.Required(), mcp.Description("Recipient JID (e.g. 966535435254@s.whatsapp.net or 120363406393924600@g.us)")),
		mcp.WithString("text", mcp.Required(), mcp.Description("Text to convert to speech and send as voice note")),
		mcp.WithString("voice_id", mcp.Description("ElevenLabs voice ID (defaults to ELEVENLABS_VOICE_ID env var)")),
		mcp.WithString("model_id", mcp.Description("ElevenLabs model ID (defaults to eleven_multilingual_v2)")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			ReadOnlyHint:    mcp.ToBoolPtr(false),
			DestructiveHint: mcp.ToBoolPtr(false),
		}),
	)
}

func toolMarkRead() mcp.Tool {
	return mcp.NewTool("wa_mark_read",
		mcp.WithDescription("Mark WhatsApp messages as read. Requires the bridge daemon to be running."),
		mcp.WithString("chat_jid", mcp.Required(), mcp.Description("Chat JID")),
		mcp.WithString("message_ids", mcp.Required(), mcp.Description("Comma-separated message IDs to mark as read")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			ReadOnlyHint:    mcp.ToBoolPtr(false),
			DestructiveHint: mcp.ToBoolPtr(false),
		}),
	)
}

// ── Handlers ────────────────────────────────────────────────────────

func (s *Server) handleSend(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	jid, _ := args["jid"].(string)
	if jid == "" {
		return mcp.NewToolResultError("jid is required"), nil
	}

	message, _ := args["message"].(string)
	mediaPath, _ := args["media_path"].(string)
	if message == "" && mediaPath == "" {
		return mcp.NewToolResultError("message or media_path is required"), nil
	}

	payload := map[string]interface{}{
		"jid":     jid,
		"message": message,
	}
	if mediaPath != "" {
		payload["media_path"] = mediaPath
	}
	if ptt, ok := args["ptt"].(bool); ok && ptt {
		payload["ptt"] = true
	}

	return s.postAPIAny("/send", payload)
}

func (s *Server) handleTTSSend(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	jid, _ := args["jid"].(string)
	if jid == "" {
		return mcp.NewToolResultError("jid is required"), nil
	}
	text, _ := args["text"].(string)
	if text == "" {
		return mcp.NewToolResultError("text is required"), nil
	}

	payload := map[string]interface{}{
		"jid":  jid,
		"text": text,
	}
	if voiceID, _ := args["voice_id"].(string); voiceID != "" {
		payload["voice_id"] = voiceID
	}
	if modelID, _ := args["model_id"].(string); modelID != "" {
		payload["model_id"] = modelID
	}

	return s.postAPIAny("/tts-send", payload)
}

func (s *Server) handleReply(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	chatJID, _ := args["chat_jid"].(string)
	messageID, _ := args["message_id"].(string)
	message, _ := args["message"].(string)
	mediaPath, _ := args["media_path"].(string)

	if chatJID == "" || messageID == "" || message == "" {
		return mcp.NewToolResultError("chat_jid, message_id, and message are required"), nil
	}

	payload := map[string]string{
		"chat_jid":   chatJID,
		"message_id": messageID,
		"message":    message,
	}
	if mediaPath != "" {
		payload["media_path"] = mediaPath
	}

	return s.postAPI("/reply", payload)
}

func (s *Server) handleReact(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	chatJID, _ := args["chat_jid"].(string)
	messageID, _ := args["message_id"].(string)
	emoji, _ := args["emoji"].(string)

	if chatJID == "" || messageID == "" || emoji == "" {
		return mcp.NewToolResultError("chat_jid, message_id, and emoji are required"), nil
	}

	return s.postAPI("/react", map[string]string{
		"chat_jid":   chatJID,
		"message_id": messageID,
		"emoji":      emoji,
	})
}

func (s *Server) handleMarkRead(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	chatJID, _ := args["chat_jid"].(string)
	messageIDsStr, _ := args["message_ids"].(string)

	if chatJID == "" || messageIDsStr == "" {
		return mcp.NewToolResultError("chat_jid and message_ids are required"), nil
	}

	var ids []string
	for _, id := range splitAndTrim(messageIDsStr) {
		if id != "" {
			ids = append(ids, id)
		}
	}

	payload := map[string]any{
		"chat_jid":    chatJID,
		"message_ids": ids,
	}

	return s.postAPIAny("/messages/mark-read", payload)
}

// ── REST API helpers ────────────────────────────────────────────────

func (s *Server) postAPI(path string, payload map[string]string) (*mcp.CallToolResult, error) {
	return s.postAPIAny(path, payload)
}

func (s *Server) postAPIAny(path string, payload any) (*mcp.CallToolResult, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal error: %v", err)), nil
	}

	url := s.apiURL + path
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("bridge unreachable at %s: %v", s.apiURL, err)), nil
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return mcp.NewToolResultError(fmt.Sprintf("bridge returned %d: %s", resp.StatusCode, string(respBody))), nil
	}

	return mcp.NewToolResultText(string(respBody)), nil
}

func splitAndTrim(s string) []string {
	parts := make([]string, 0)
	for _, p := range bytes.Split([]byte(s), []byte(",")) {
		trimmed := bytes.TrimSpace(p)
		if len(trimmed) > 0 {
			parts = append(parts, string(trimmed))
		}
	}
	return parts
}
