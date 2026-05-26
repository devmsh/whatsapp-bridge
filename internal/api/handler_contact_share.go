package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// handleSendContact sends a WA ContactMessage (vCard share). The user picks
// one of their contacts from the client-side picker; the bridge looks the
// contact up in its local DB to build a minimal RFC-2426 vCard (FN, TEL),
// then ships it via wa.SendMessage.
//
// Body:
//
//	{ "jid": "<chat>", "contact_jid": "<contact-to-share>" }
//
// We don't echo a placeholder bubble in the chat list preview the way
// handleSend does — ContactMessage is rare enough that the SSE round-trip
// landing the real bubble within a second is fine.
func (s *Server) handleSendContact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		JID        string `json:"jid"`
		ContactJID string `json:"contact_jid"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	if req.JID == "" || req.ContactJID == "" {
		jsonError(w, 400, "jid and contact_jid are required")
		return
	}
	if !s.guardChatAccess(w, r, req.JID) {
		return
	}

	recipientJID, err := parseJID(req.JID)
	if err != nil {
		jsonError(w, 400, fmt.Sprintf("invalid JID: %v", err))
		return
	}

	contact, err := s.store.GetContact(req.ContactJID)
	if err != nil || contact == nil {
		jsonError(w, 404, "contact not found")
		return
	}

	// Build the vCard body. WA accepts a minimal 4-line vCard with FN
	// (formatted name) + TEL (phone with WA-specific waid param). The waid
	// is what makes WA-on-the-other-side recognise the card as a WA
	// contact and let the recipient tap "Message" / "Call" from it.
	name := contact.Name
	if name == "" {
		name = contact.BusinessName
	}
	if name == "" {
		name = contact.PushName
	}
	if name == "" && contact.Phone != "" {
		name = "+" + contact.Phone
	}
	if name == "" {
		name = "Contact"
	}
	phone := contact.Phone
	if phone == "" {
		// Fallback: digits from the contact's bare JID.
		phone = strings.SplitN(contact.JID, "@", 2)[0]
		phone = strings.SplitN(phone, ":", 2)[0]
	}
	vcard := "BEGIN:VCARD\nVERSION:3.0\nFN:" + name + "\nTEL;type=CELL;type=VOICE;waid=" + phone + ":+" + phone + "\nEND:VCARD"

	msg := &waE2E.Message{
		ContactMessage: &waE2E.ContactMessage{
			DisplayName: proto.String(name),
			Vcard:       proto.String(vcard),
		},
	}

	wa := s.client.GetWhatsmeowClient()
	resp, err := wa.SendMessage(context.Background(), recipientJID, msg)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("send failed: %v", err))
		return
	}

	s.storeOutgoingMessage(resp.ID, recipientJID.String(), name, "contact")

	jsonOK(w, map[string]any{
		"success":      true,
		"message_id":   resp.ID,
		"timestamp":    resp.Timestamp.Unix(),
		"display_name": name,
	})
}
