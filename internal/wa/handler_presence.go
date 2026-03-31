package wa

import (
	"time"

	"go.mau.fi/whatsmeow/types/events"

	"whatsapp-bridge-v2/internal/db"
)

func handlePresence(c *Client, evt *events.Presence) {
	jid := evt.From.String()
	status := "available"
	if evt.Unavailable {
		status = "unavailable"
	}
	lastSeen := int64(0)
	if !evt.LastSeen.IsZero() {
		lastSeen = evt.LastSeen.Unix()
	}

	c.Store.StorePresence(&db.PresenceEntry{
		JID:       jid,
		Status:    status,
		LastSeen:  lastSeen,
		UpdatedAt: time.Now().Unix(),
	})
}

func handleChatPresence(c *Client, evt *events.ChatPresence) {
	// Chat presence (typing indicators) — store in presence cache
	jid := evt.Sender.String()
	status := string(evt.State) // "composing" or "paused"

	c.Store.StorePresence(&db.PresenceEntry{
		JID:       jid,
		Status:    status,
		UpdatedAt: time.Now().Unix(),
	})
}
