package wa

import (
	"go.mau.fi/whatsmeow/types/events"

	"whatsapp-bridge-v2/internal/db"
)

// MigrateLIDMessages re-maps existing LID-stored messages to their phone JIDs.
// Returns the number of messages migrated.
func MigrateLIDMessages(c *Client) (int, error) {
	// Find all distinct LID chat_jids in messages table
	rows, err := c.Store.DB.Query(`SELECT DISTINCT chat_jid FROM messages WHERE chat_jid LIKE '%@lid'`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	total := 0
	for rows.Next() {
		var lidJID string
		if err := rows.Scan(&lidJID); err != nil {
			continue
		}
		phoneJID := c.ResolvePhoneForLID(lidJID)
		if phoneJID == "" {
			continue
		}
		res, err := c.Store.DB.Exec(
			`UPDATE messages SET chat_jid = ? WHERE chat_jid = ?`,
			phoneJID, lidJID,
		)
		if err != nil {
			c.Log.Warnf("Failed to migrate messages from %s to %s: %v", lidJID, phoneJID, err)
			continue
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			c.Log.Infof("Migrated %d messages: %s → %s", n, lidJID, phoneJID)
			total += int(n)
		}
	}

	// Also migrate sender LIDs
	senderRows, err := c.Store.DB.Query(`SELECT DISTINCT sender FROM messages WHERE sender LIKE '%@lid'`)
	if err == nil {
		defer senderRows.Close()
		for senderRows.Next() {
			var lidJID string
			if err := senderRows.Scan(&lidJID); err != nil {
				continue
			}
			phoneJID := c.ResolvePhoneForLID(lidJID)
			if phoneJID == "" {
				continue
			}
			c.Store.DB.Exec(`UPDATE messages SET sender = ? WHERE sender = ?`, phoneJID, lidJID)
		}
	}

	return total, nil
}

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
