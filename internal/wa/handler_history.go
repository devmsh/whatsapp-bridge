package wa

import (
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types"
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

	syncType := data.GetSyncType().String()
	c.Log.Infof("History sync: type=%s, progress=%d%%, conversations=%d",
		syncType, data.GetProgress(), len(data.GetConversations()))
	c.Sync.RecordHistory(syncType)

	for _, ic := range data.GetInlineContacts() {
		storeInlineContact(c, ic)
	}

	for _, conv := range data.GetConversations() {
		chatJID := conv.GetID()
		chatName := conv.GetDisplayName()

		// Store chat with unread info
		chat := &db.Chat{
			JID:               chatJID,
			Name:              chatName,
			UnreadCount:       int(conv.GetUnreadCount()),
			DisappearingTimer: int64(conv.GetDisappearingMode().GetInitiator()),
		}

		// Determine chat type
		if conv.GetNotSpam() {
			// Not spam, regular chat
		}

		if err := c.Store.StoreChatFromHistorySync(chat); err != nil {
			c.Log.Warnf("Failed to store chat %s: %v", chatJID, err)
		}

		if un := conv.GetUsername(); un != "" {
			if parsed, err := types.ParseJID(chatJID); err == nil &&
				(parsed.Server == types.DefaultUserServer || parsed.Server == types.HiddenUserServer) {
				c.Store.StoreContact(&db.Contact{
					JID:      chatJID,
					Phone:    parsed.User,
					Name:     chatName,
					Username: un,
				})
			}
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

func storeInlineContact(c *Client, ic *waHistorySync.InlineContact) {
	if ic == nil {
		return
	}
	username := ic.GetUsername()
	name := ic.GetFullName()
	if name == "" {
		name = ic.GetFirstName()
	}
	if username == "" && name == "" {
		return
	}
	pn := ic.GetPnJID()
	lid := ic.GetLidJID()
	jid := pn
	if jid == "" {
		jid = lid
	}
	if jid == "" {
		return
	}
	phone := ""
	if parsed, err := types.ParseJID(pn); err == nil && !parsed.IsEmpty() {
		phone = parsed.User
	}
	lidUser := ""
	if parsed, err := types.ParseJID(lid); err == nil && !parsed.IsEmpty() {
		lidUser = parsed.User
	}
	c.Store.StoreContact(&db.Contact{
		JID:      jid,
		LID:      lidUser,
		Phone:    phone,
		Name:     name,
		Username: username,
	})
}
