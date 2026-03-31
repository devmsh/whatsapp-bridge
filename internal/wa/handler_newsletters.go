package wa

import (
	"time"

	"go.mau.fi/whatsmeow/types/events"

	"whatsapp-bridge-v2/internal/db"
)

func handleNewsletterJoin(c *Client, evt *events.NewsletterJoin) {
	meta := evt.NewsletterMetadata
	now := time.Now().Unix()

	pictureID := ""
	pictureURL := ""
	if meta.ThreadMeta.Picture != nil {
		pictureID = meta.ThreadMeta.Picture.ID
		pictureURL = meta.ThreadMeta.Picture.URL
	}

	role := ""
	muted := ""
	if meta.ViewerMeta != nil {
		role = string(meta.ViewerMeta.Role)
		muted = string(meta.ViewerMeta.Mute)
	}

	c.Store.StoreNewsletter(&db.Newsletter{
		JID:               meta.ID.String(),
		Name:              meta.ThreadMeta.Name.Text,
		Description:       meta.ThreadMeta.Description.Text,
		SubscriberCount:   meta.ThreadMeta.SubscriberCount,
		VerificationState: string(meta.ThreadMeta.VerificationState),
		PictureID:         pictureID,
		PictureURL:        pictureURL,
		InviteCode:        meta.ThreadMeta.InviteCode,
		Role:              role,
		Muted:             muted,
		State:             string(meta.State.Type),
		CreationTime:      meta.ThreadMeta.CreationTime.Unix(),
		UpdatedAt:         now,
	})

	c.Log.Infof("Joined newsletter %s (%s)", meta.ThreadMeta.Name.Text, meta.ID.String())
}

func handleNewsletterLeave(c *Client, evt *events.NewsletterLeave) {
	c.Store.DeleteNewsletter(evt.ID.String())
	c.Log.Infof("Left newsletter %s", evt.ID.String())
}

func handleNewsletterMuteChange(c *Client, evt *events.NewsletterMuteChange) {
	jid := evt.ID.String()
	nl, err := c.Store.GetNewsletter(jid)
	if err != nil || nl == nil {
		return
	}
	nl.Muted = string(evt.Mute)
	nl.UpdatedAt = time.Now().Unix()
	c.Store.StoreNewsletter(nl)
}

func handleNewsletterLiveUpdate(c *Client, evt *events.NewsletterLiveUpdate) {
	chatJID := evt.JID.String()
	ts := evt.Time.Unix()

	for _, msg := range evt.Messages {
		if msg.Message == nil {
			continue
		}
		rec := &db.Message{
			ID:          msg.MessageID,
			ChatJID:     chatJID,
			Sender:      chatJID,
			Timestamp:   msg.Timestamp.Unix(),
			MessageType: msg.Type,
			Content:     extractContent(msg.Message),
		}
		mediaInfo := DownloadMedia(c.WA, msg.Message, msg.MessageID, c.MediaDir, c.Log)
		if mediaInfo != nil {
			rec.MediaType = mediaInfo.Type
			rec.MediaPath = mediaInfo.Path
			rec.MediaMime = mediaInfo.Mime
			rec.MediaSize = mediaInfo.Size
			rec.MediaCaption = mediaInfo.Caption
			rec.MediaFilename = mediaInfo.Filename
			if rec.Content == "" {
				rec.Content = ContentFromMedia(mediaInfo)
			}
		}
		if rec.Content != "" {
			c.Store.StoreMessage(rec)
		}
	}

	c.Store.UpdateChatLastMessage(chatJID, "", ts)
}
