package db

import (
	"fmt"
	"strings"
)

// MessageStatus is the highest receipt state observed for a message we sent.
// It mirrors the WhatsApp tick UX: sent → delivered → read → played.
type MessageStatus string

const (
	StatusSent      MessageStatus = "sent"      // accepted by server, no receipt yet
	StatusDelivered MessageStatus = "delivered" // delivered to at least one device
	StatusRead      MessageStatus = "read"      // recipient (or any group participant) read it
	StatusPlayed    MessageStatus = "played"    // voice / video receipt observed
)

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

// GetMessageStatuses aggregates the receipts table into one MessageStatus per
// message ID, taking the highest-ranked receipt observed: played > read >
// delivered. Messages with no receipt at all are absent from the result —
// callers should treat them as StatusSent.
//
// chatJIDs may include both the phone JID and the LID JID of a DM, so the
// caller's GetMessagesMerged set lines up with the same receipt table view.
func (s *Store) GetMessageStatuses(chatJIDs []string, msgIDs []string) (map[string]MessageStatus, error) {
	out := map[string]MessageStatus{}
	if len(chatJIDs) == 0 || len(msgIDs) == 0 {
		return out, nil
	}

	chatPH := make([]string, len(chatJIDs))
	msgPH := make([]string, len(msgIDs))
	args := make([]interface{}, 0, len(chatJIDs)+len(msgIDs))
	for i, j := range chatJIDs {
		chatPH[i] = "?"
		args = append(args, j)
	}
	for i, id := range msgIDs {
		msgPH[i] = "?"
		args = append(args, id)
	}

	q := fmt.Sprintf(
		`SELECT message_id, receipt_type FROM receipts
		 WHERE chat_jid IN (%s) AND message_id IN (%s)`,
		strings.Join(chatPH, ","), strings.Join(msgPH, ","),
	)
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	ranks := make(map[string]int, len(msgIDs))
	for rows.Next() {
		var id, t string
		if err := rows.Scan(&id, &t); err != nil {
			return out, err
		}
		r := rankReceiptType(t)
		if r == 0 {
			continue
		}
		if ranks[id] < r {
			ranks[id] = r
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	for id, r := range ranks {
		switch r {
		case 3:
			out[id] = StatusPlayed
		case 2:
			out[id] = StatusRead
		case 1:
			out[id] = StatusDelivered
		}
	}
	return out, nil
}

// rankReceiptType maps a whatsmeow receipt_type string to a comparable rank.
// 0 = ignored (retry, sender, server-error, inactive, peer_msg, hist_sync).
func rankReceiptType(t string) int {
	switch t {
	case "played", "played-self":
		return 3
	case "read", "read-self":
		return 2
	case "": // ReceiptTypeDelivered is the empty string in whatsmeow
		return 1
	}
	return 0
}
