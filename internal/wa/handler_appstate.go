package wa

import (
	"time"

	"go.mau.fi/whatsmeow/types/events"

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
		c.Store.SetChatUnread(jid, 0)
	}
	c.Log.Debugf("Chat %s marked_as_read=%v", jid, read)
}

// handleDeleteForMe mirrors a "Delete for me" the user did on another device.
// Unlike a revoke (sent to everyone), this is purely a personal-view deletion
// — we mark the message as deleted in our local store so it shows the same
// "🚫 This message was deleted" placeholder the UI already renders for revokes.
//
// Storage shape: messages are stored under their phone-based JID, but app-
// state events arrive with LID-based JIDs (`<n>@lid`). We resolve the LID
// before updating, and fall back to the raw LID in case our storage is mixed.
func handleDeleteForMe(c *Client, evt *events.DeleteForMe) {
	if evt.MessageID == "" {
		return
	}
	rawChat := evt.ChatJID.String()
	resolvedChat := resolveLIDToPhone(c, evt.ChatJID, rawChat)
	deletedBy := evt.SenderJID.String()
	if deletedBy == "" && c.WA.Store.ID != nil {
		deletedBy = c.WA.Store.ID.String()
	}
	ts := evt.Timestamp.Unix()
	if ts == 0 {
		ts = time.Now().Unix()
	}
	// Try the resolved JID first, fall back to the raw LID. Whichever the
	// message was stored under will match — the other UPDATE is a no-op.
	jids := []string{resolvedChat}
	if rawChat != resolvedChat {
		jids = append(jids, rawChat)
	}
	for _, j := range jids {
		if err := c.Store.MarkDeleted(evt.MessageID, j, deletedBy, ts); err != nil {
			c.Log.Errorf("MarkDeleted (DeleteForMe %s) failed: %v", j, err)
		}
	}
	c.Log.Infof("Message %s deleted-for-me in %s", evt.MessageID, resolvedChat)
}

// handleClearChat mirrors a "Clear chat" the user did on another device: the
// conversation stays in the list, its messages go. We bulk-mark the messages as
// deleted up to the moment of the clear — anything sent afterwards is untouched,
// which is what WhatsApp does.
func handleClearChat(c *Client, evt *events.ClearChat) {
	rawChat := evt.JID.String()
	resolvedChat := resolveLIDToPhone(c, evt.JID, rawChat)
	ts := evt.Timestamp.Unix()
	if ts == 0 {
		ts = time.Now().Unix()
	}
	by := ""
	if c.WA.Store.ID != nil {
		by = c.WA.Store.ID.String()
	}
	jids := []string{resolvedChat}
	if rawChat != resolvedChat {
		jids = append(jids, rawChat)
	}
	total := int64(0)
	for _, j := range jids {
		n, err := c.Store.MarkChatMessagesDeleted(j, by, ts)
		if err != nil {
			c.Log.Errorf("MarkChatMessagesDeleted (%s) failed: %v", j, err)
			continue
		}
		total += n
	}
	c.Log.Infof("Chat %s cleared — %d messages marked deleted", resolvedChat, total)
}

// handleDeleteChat mirrors a "Delete chat" the user did on another device —
// deleting a conversation, or exiting a group and then deleting it.
//
// This is NOT the same as clearing. WhatsApp removes the chat from the list
// completely; it never shows a chat full of deleted-message placeholders. We
// used to treat the two the same, so a deleted group stayed in the list with
// every message struck through. Now the chat itself is marked deleted and
// disappears, while its rows stay on disk so history and media survive.
func handleDeleteChat(c *Client, evt *events.DeleteChat) {
	rawChat := evt.JID.String()
	resolvedChat := resolveLIDToPhone(c, evt.JID, rawChat)
	ts := evt.Timestamp.Unix()
	if ts == 0 {
		ts = time.Now().Unix()
	}
	by := ""
	if c.WA.Store.ID != nil {
		by = c.WA.Store.ID.String()
	}
	jids := []string{resolvedChat}
	if rawChat != resolvedChat {
		jids = append(jids, rawChat)
	}
	total := int64(0)
	for _, j := range jids {
		if err := c.Store.MarkChatDeleted(j, ts); err != nil {
			c.Log.Errorf("MarkChatDeleted (%s) failed: %v", j, err)
		}
		n, err := c.Store.MarkChatMessagesDeleted(j, by, ts)
		if err != nil {
			c.Log.Errorf("MarkChatMessagesDeleted (%s) failed: %v", j, err)
			continue
		}
		total += n
	}
	c.Log.Infof("Chat %s deleted — removed from the list, %d messages marked deleted", resolvedChat, total)
}

func handleAppStateSyncComplete(c *Client, evt *events.AppStateSyncComplete) {
	c.Log.Infof("App state sync complete: %s (version %d)", evt.Name, evt.Version)
}
