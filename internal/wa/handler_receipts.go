package wa

import (
	"go.mau.fi/whatsmeow/types/events"

	"whatsapp-bridge-v2/internal/db"
)

func handleReceipt(c *Client, evt *events.Receipt) {
	chatJID := evt.Chat.String()
	senderJID := evt.Sender.String()
	ts := evt.Timestamp.Unix()
	receiptType := string(evt.Type)

	for _, msgID := range evt.MessageIDs {
		if err := c.Store.StoreReceipt(&db.Receipt{
			MessageID:   msgID,
			ChatJID:     chatJID,
			SenderJID:   senderJID,
			ReceiptType: receiptType,
			Timestamp:   ts,
		}); err != nil {
			c.Log.Warnf("Failed to store receipt for %s: %v", msgID, err)
		}
	}
}
