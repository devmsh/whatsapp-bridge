package db

// Receipt maps to the receipts table, derived from events.Receipt.
type Receipt struct {
	MessageID   string `json:"message_id"`
	ChatJID     string `json:"chat_jid"`
	SenderJID   string `json:"sender_jid"`
	ReceiptType string `json:"receipt_type"`
	Timestamp   int64  `json:"timestamp"`
}

// StoreReceipt upserts a receipt.
func (s *Store) StoreReceipt(r *Receipt) error {
	_, err := s.DB.Exec(`INSERT OR REPLACE INTO receipts (
		message_id, chat_jid, sender_jid, receipt_type, timestamp
	) VALUES (?,?,?,?,?)`,
		r.MessageID, r.ChatJID, r.SenderJID, r.ReceiptType, r.Timestamp,
	)
	return err
}

// GetReceipts returns all receipts for a message.
func (s *Store) GetReceipts(messageID, chatJID string) ([]Receipt, error) {
	rows, err := s.DB.Query(
		`SELECT message_id, chat_jid, sender_jid, receipt_type, timestamp
		 FROM receipts WHERE message_id = ? AND chat_jid = ?`,
		messageID, chatJID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var receipts []Receipt
	for rows.Next() {
		var r Receipt
		if err := rows.Scan(&r.MessageID, &r.ChatJID, &r.SenderJID, &r.ReceiptType, &r.Timestamp); err != nil {
			return receipts, err
		}
		receipts = append(receipts, r)
	}
	return receipts, rows.Err()
}
