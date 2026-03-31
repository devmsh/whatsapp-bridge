package wa

import (
	"go.mau.fi/whatsmeow/types/events"

	"whatsapp-bridge-v2/internal/db"
)

func handlePin(c *Client, evt *events.Pin) {
	jid := evt.JID.String()
	pinned := evt.Action.GetPinned()
	c.Store.SetChatPinned(jid, pinned)
	c.Log.Debugf("Chat %s pinned=%v", jid, pinned)
}

func handleMute(c *Client, evt *events.Mute) {
	jid := evt.JID.String()
	muted := evt.Action.GetMuted()
	muteEnd := evt.Action.GetMuteEndTimestamp()
	c.Store.SetChatMuted(jid, muted, muteEnd)
	c.Log.Debugf("Chat %s muted=%v until=%d", jid, muted, muteEnd)
}

func handleArchive(c *Client, evt *events.Archive) {
	jid := evt.JID.String()
	archived := evt.Action.GetArchived()
	c.Store.SetChatArchived(jid, archived)
	c.Log.Debugf("Chat %s archived=%v", jid, archived)
}

func handleMarkChatAsRead(c *Client, evt *events.MarkChatAsRead) {
	jid := evt.JID.String()
	read := evt.Action.GetRead()
	if read {
		c.Store.StoreChat(&db.Chat{JID: jid, UnreadCount: 0})
	}
	c.Log.Debugf("Chat %s marked_as_read=%v", jid, read)
}

func handleAppStateSyncComplete(c *Client, evt *events.AppStateSyncComplete) {
	c.Log.Infof("App state sync complete: %s (version %d)", evt.Name, evt.Version)
}
