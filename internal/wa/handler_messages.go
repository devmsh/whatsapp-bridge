package wa

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatsapp-bridge-v2/internal/db"
)

func handleMessage(c *Client, evt *events.Message) {
	info := evt.Info
	msg := evt.Message
	if msg == nil {
		return
	}

	chatJID := info.Chat.String()
	senderJID := info.Sender.String()
	ts := info.Timestamp.Unix()

	// Resolve LID → phone JID so DM messages are stored under the phone number.
	// WhatsApp's LID migration means incoming DMs may arrive with LID-based JIDs
	// instead of phone-based JIDs, splitting conversations.
	if info.Chat.Server == types.DefaultUserServer || info.Chat.Server == "lid" {
		chatJID = resolveLIDToPhone(c, info.Chat, chatJID)
	}
	if info.Sender.Server == types.DefaultUserServer || info.Sender.Server == "lid" {
		senderJID = resolveLIDToPhone(c, info.Sender, senderJID)
	}

	// Handle protocol messages: edits, revokes, ephemeral settings
	if proto := msg.GetProtocolMessage(); proto != nil {
		handleProtocol(c, chatJID, senderJID, ts, info.ID, proto, evt)
		return
	}

	// Handle reactions — store separately
	if reaction := msg.GetReactionMessage(); reaction != nil {
		handleReaction(c, chatJID, senderJID, info.PushName, ts, reaction)
		return
	}

	// Build the message record
	rec := &db.Message{
		ID:          info.ID,
		ChatJID:     chatJID,
		Sender:      senderJID,
		SenderName:  "",
		PushName:    info.PushName,
		Timestamp:   ts,
		IsFromMe:    info.IsFromMe,
		IsGroup:     info.IsGroup,
		MessageType: info.Type,
		IsEphemeral: evt.IsEphemeral,
		IsViewOnce:  evt.IsViewOnce,
		IsEdit:      evt.IsEdit,
	}

	// Resolve sender name
	_, name := c.Store.ResolveSender(senderJID)
	rec.SenderName = name

	// Extract content from all known message types
	rec.Content = extractContent(msg)

	// Extract context info (replies, forwards, mentions)
	extractContextFields(msg, rec)

	// Handle media (image, video, audio, document, sticker)
	mediaInfo := DownloadMedia(c.WA, msg, info.ID, c.MediaDir, c.MediaPolicy(), c.Log)
	if mediaInfo != nil {
		rec.MediaType = mediaInfo.Type
		rec.MediaPath = mediaInfo.Path
		rec.MediaMime = mediaInfo.Mime
		rec.MediaSize = mediaInfo.Size
		rec.MediaCaption = mediaInfo.Caption
		rec.MediaFilename = mediaInfo.Filename
		rec.ThumbnailPath = mediaInfo.ThumbnailPath
		if rec.Content == "" {
			rec.Content = ContentFromMedia(mediaInfo)
		}
	}

	// Handle location messages
	extractLocation(msg, rec)

	// Handle contact/vcard messages
	extractVCard(msg, rec)

	// Handle poll creation
	extractPollCreation(c, msg, rec, chatJID, ts)

	// Skip empty messages
	if rec.Content == "" && rec.MediaType == "" && rec.Latitude == 0 && rec.VCardName == "" {
		return
	}

	if err := c.Store.StoreMessage(rec); err != nil {
		c.Log.Warnf("Failed to store message %s: %v", info.ID, err)
		return
	}

	// Broadcast to SSE listeners
	c.Broadcaster.Publish(rec)

	// Update chat
	chatName := ""
	if info.IsGroup {
		chatName = chatJID // Will be resolved by sync
	}
	c.Store.UpdateChatLastMessage(chatJID, chatName, ts)

	// Upsert contact from push name
	if info.PushName != "" && !info.IsFromMe {
		phone := info.Sender.User
		c.Store.StoreContact(&db.Contact{
			JID:      senderJID,
			Phone:    phone,
			PushName: info.PushName,
		})
	}
}

func extractContent(msg *waE2E.Message) string {
	if text := msg.GetConversation(); text != "" {
		return text
	}
	if ext := msg.GetExtendedTextMessage(); ext != nil {
		return ext.GetText()
	}
	if img := msg.GetImageMessage(); img != nil {
		return img.GetCaption()
	}
	if vid := msg.GetVideoMessage(); vid != nil {
		return vid.GetCaption()
	}
	if doc := msg.GetDocumentMessage(); doc != nil {
		if c := doc.GetCaption(); c != "" {
			return c
		}
		return fmt.Sprintf("[document:%s]", doc.GetFileName())
	}
	if msg.GetAudioMessage() != nil {
		return "[audio]"
	}
	if msg.GetStickerMessage() != nil {
		return "[sticker]"
	}
	if list := msg.GetListMessage(); list != nil {
		return list.GetDescription()
	}
	if tmpl := msg.GetTemplateMessage(); tmpl != nil {
		return "[template]"
	}
	if msg.GetOrderMessage() != nil {
		return "[order]"
	}
	if msg.GetProductMessage() != nil {
		return "[product]"
	}
	if gi := msg.GetGroupInviteMessage(); gi != nil {
		return fmt.Sprintf("[group_invite:%s]", gi.GetGroupName())
	}
	return ""
}

func extractContextFields(msg *waE2E.Message, rec *db.Message) {
	ci := getContextInfo(msg)
	if ci == nil {
		return
	}

	if ci.GetIsForwarded() {
		rec.IsForwarded = true
		rec.ForwardScore = int(ci.GetForwardingScore())
	}

	if ci.GetQuotedMessage() != nil {
		rec.ReplyToID = ci.GetStanzaID()
		rec.ReplyToSender = ci.GetParticipant()
		rec.ReplyToContent = extractQuotedContent(ci.GetQuotedMessage())
	}

	mentioned := ci.GetMentionedJID()
	for _, gm := range ci.GetGroupMentions() {
		if gm.GetGroupJID() != "" {
			mentioned = append(mentioned, "@all:"+gm.GetGroupJID())
		}
	}
	if len(mentioned) > 0 {
		data, _ := json.Marshal(mentioned)
		rec.Mentions = string(data)
	}
}

func getContextInfo(msg *waE2E.Message) *waE2E.ContextInfo {
	if msg == nil {
		return nil
	}
	if ext := msg.GetExtendedTextMessage(); ext != nil {
		return ext.GetContextInfo()
	}
	if img := msg.GetImageMessage(); img != nil {
		return img.GetContextInfo()
	}
	if vid := msg.GetVideoMessage(); vid != nil {
		return vid.GetContextInfo()
	}
	if doc := msg.GetDocumentMessage(); doc != nil {
		return doc.GetContextInfo()
	}
	if stk := msg.GetStickerMessage(); stk != nil {
		return stk.GetContextInfo()
	}
	if aud := msg.GetAudioMessage(); aud != nil {
		return aud.GetContextInfo()
	}
	return nil
}

func extractQuotedContent(qm *waE2E.Message) string {
	if qm == nil {
		return ""
	}
	if t := qm.GetConversation(); t != "" {
		return t
	}
	if ext := qm.GetExtendedTextMessage(); ext != nil {
		return ext.GetText()
	}
	if img := qm.GetImageMessage(); img != nil {
		if c := img.GetCaption(); c != "" {
			return c
		}
		return "[image]"
	}
	if vid := qm.GetVideoMessage(); vid != nil {
		if c := vid.GetCaption(); c != "" {
			return c
		}
		return "[video]"
	}
	if doc := qm.GetDocumentMessage(); doc != nil {
		return "[document:" + doc.GetFileName() + "]"
	}
	if qm.GetAudioMessage() != nil {
		return "[audio]"
	}
	if qm.GetStickerMessage() != nil {
		return "[sticker]"
	}
	return ""
}

func extractLocation(msg *waE2E.Message, rec *db.Message) {
	if loc := msg.GetLocationMessage(); loc != nil {
		rec.Latitude = loc.GetDegreesLatitude()
		rec.Longitude = loc.GetDegreesLongitude()
		rec.LocationName = loc.GetName()
		rec.LocationAddress = loc.GetAddress()
		if rec.Content == "" {
			rec.Content = fmt.Sprintf("[location:%.6f,%.6f]", rec.Latitude, rec.Longitude)
		}
	}
	if loc := msg.GetLiveLocationMessage(); loc != nil {
		rec.Latitude = loc.GetDegreesLatitude()
		rec.Longitude = loc.GetDegreesLongitude()
		if rec.Content == "" {
			rec.Content = fmt.Sprintf("[live_location:%.6f,%.6f]", rec.Latitude, rec.Longitude)
		}
	}
}

func extractVCard(msg *waE2E.Message, rec *db.Message) {
	if contact := msg.GetContactMessage(); contact != nil {
		rec.VCardName = contact.GetDisplayName()
		rec.VCardData = contact.GetVcard()
		if rec.Content == "" {
			rec.Content = fmt.Sprintf("[contact:%s]", rec.VCardName)
		}
	}
	if contacts := msg.GetContactsArrayMessage(); contacts != nil {
		var names []string
		for _, c := range contacts.GetContacts() {
			names = append(names, c.GetDisplayName())
		}
		rec.VCardName = strings.Join(names, ", ")
		if rec.Content == "" {
			rec.Content = fmt.Sprintf("[contacts:%s]", rec.VCardName)
		}
	}
}

func extractPollCreation(c *Client, msg *waE2E.Message, rec *db.Message, chatJID string, ts int64) {
	var poll *waE2E.PollCreationMessage
	if p := msg.GetPollCreationMessage(); p != nil {
		poll = p
	} else if p := msg.GetPollCreationMessageV2(); p != nil {
		poll = p
	} else if p := msg.GetPollCreationMessageV3(); p != nil {
		poll = p
	}
	if poll == nil {
		return
	}

	question := poll.GetName()
	var optionStrs []string
	for _, o := range poll.GetOptions() {
		optionStrs = append(optionStrs, o.GetOptionName())
	}
	optionsJSON, _ := json.Marshal(optionStrs)
	rec.Content = fmt.Sprintf("[poll:%s]", question)

	c.Store.StorePoll(&db.Poll{
		MessageID:     rec.ID,
		ChatJID:       chatJID,
		Question:      question,
		Options:       string(optionsJSON),
		MaxSelections: int(poll.GetSelectableOptionsCount()),
		CreatedAt:     ts,
	})
}

func handleProtocol(c *Client, chatJID, senderJID string, ts int64, msgID string, proto *waE2E.ProtocolMessage, evt *events.Message) {
	// Revoke (delete)
	if proto.GetType() == waE2E.ProtocolMessage_REVOKE {
		revokedID := proto.GetKey().GetID()
		if revokedID != "" {
			c.Store.MarkDeleted(revokedID, chatJID, senderJID, ts)
			c.Log.Infof("Message %s revoked by %s", revokedID, senderJID)
		}
		return
	}

	// Edit
	if edited := proto.GetEditedMessage(); edited != nil || evt.IsEdit {
		editedMsg := edited
		if editedMsg == nil {
			editedMsg = evt.Message
		}
		newContent := extractContent(editedMsg)
		if newContent != "" {
			c.Store.MarkEdited(msgID, chatJID, newContent, ts)
			c.Log.Infof("Message %s edited", msgID)
		}
		return
	}

	// Ephemeral setting changes — log as event
	if proto.GetEphemeralExpiration() > 0 {
		c.Store.StoreEventLog(&db.EventLog{
			EventType: "ephemeral_setting",
			JID:       chatJID,
			ActorJID:  senderJID,
			Data:      fmt.Sprintf(`{"timer":%d}`, proto.GetEphemeralExpiration()),
			Timestamp: ts,
		})
	}
}

// resolveLIDToPhone converts a LID-based JID to a phone-based JID using whatsmeow's
// LID map. Returns the original string if not a LID or if no mapping is found.
func resolveLIDToPhone(c *Client, jid types.JID, fallback string) string {
	if jid.Server != "lid" {
		return fallback
	}
	// Strip device suffix for lookup (e.g. "53901906723060:10@lid" → "53901906723060@lid")
	lookupJID := jid
	lookupJID.Device = 0
	lookupJID.RawAgent = 0

	pn, err := c.WA.Store.LIDs.GetPNForLID(context.Background(), lookupJID)
	if err != nil || pn.IsEmpty() {
		c.Log.Debugf("No PN mapping for LID %s: %v", jid.String(), err)
		return fallback
	}
	return pn.String()
}

func handleReaction(c *Client, chatJID, senderJID, pushName string, ts int64, reaction *waE2E.ReactionMessage) {
	targetID := reaction.GetKey().GetID()
	if targetID == "" {
		return
	}
	_, name := c.Store.ResolveSender(senderJID)
	if name == "" {
		name = pushName
	}
	c.Store.StoreReaction(&db.Reaction{
		MessageID:  targetID,
		ChatJID:    chatJID,
		Sender:     senderJID,
		SenderName: name,
		Emoji:      reaction.GetText(),
		Timestamp:  ts,
	})
}
