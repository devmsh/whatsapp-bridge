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
	// Chat presence (typing indicators). Two outputs:
	//
	//   1) Per-(chat, sender) typing set in c.Typing — drives the group
	//      header's "X is typing…" via /api/v2/chats/{jid}/typing.
	//   2) Per-sender entry in presence_cache — drives the DM header's
	//      "typing…" line via /api/v2/presence/{jid}. The DM-typing flow
	//      from loop #12 already lives here; group typing needs the chat
	//      context too, hence the separate cache.
	chat := evt.Chat.String()
	sender := evt.Sender.String()
	status := string(evt.State) // "composing" or "paused"

	if status == "composing" {
		c.Typing.Set(chat, sender)
	} else {
		// Any non-composing state ('paused', empty) clears the entry.
		c.Typing.Clear(chat, sender)
	}

	c.Store.StorePresence(&db.PresenceEntry{
		JID:       sender,
		Status:    status,
		UpdatedAt: time.Now().Unix(),
	})
}
