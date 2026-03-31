package wa

import (
	"go.mau.fi/whatsmeow/types/events"

	"whatsapp-bridge-v2/internal/db"
)

func handleHistorySync(c *Client, evt *events.HistorySync) {
	data := evt.Data
	if data == nil {
		return
	}

	c.Log.Infof("History sync: type=%s, conversations=%d",
		data.GetSyncType().String(), len(data.GetConversations()))

	for _, conv := range data.GetConversations() {
		chatJID := conv.GetID()
		chatName := conv.GetDisplayName()

		// Store chat with unread info
		chat := &db.Chat{
			JID:              chatJID,
			Name:             chatName,
			UnreadCount:      int(conv.GetUnreadCount()),
			DisappearingTimer: int64(conv.GetDisappearingMode().GetInitiator()),
		}

		// Determine chat type
		if conv.GetNotSpam() {
			// Not spam, regular chat
		}

		if err := c.Store.StoreChat(chat); err != nil {
			c.Log.Warnf("Failed to store chat %s: %v", chatJID, err)
		}

		for _, hm := range conv.GetMessages() {
			wmi := hm.GetMessage()
			if wmi == nil {
				continue
			}

			msgEvt := &events.Message{
				Info: parseWebMessageInfo(wmi),
			}
			msgEvt.RawMessage = wmi.GetMessage()
			if msgEvt.RawMessage == nil {
				continue
			}
			msgEvt.UnwrapRaw()

			// Re-use the main message handler
			handleMessage(c, msgEvt)
		}
	}

	c.Log.Infof("History sync processing complete")
}
