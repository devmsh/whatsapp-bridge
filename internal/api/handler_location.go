package api

import (
	"context"
	"fmt"
	"net/http"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// handleSendLocation sends a static WA LocationMessage — the "📍 Location"
// attachment WA users send constantly to share where they are. WA also
// supports a LiveLocationMessage for moving / sharing-for-N-minutes
// updates; we deliberately ship static only this cycle.
//
// Body:
//
//	{ "jid": "...", "latitude": 24.71, "longitude": 46.67,
//	  "name": "Coffee shop", "address": "King Fahd Rd, Riyadh" }
//
// name and address are optional — render in the recipient's bubble as a
// title + subtitle. The actual coordinates are mandatory; whatsmeow will
// refuse a (0,0) location, which we don't bother gating client-side because
// the geolocation API can't return that anyway.
func (s *Server) handleSendLocation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		JID       string  `json:"jid"`
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Name      string  `json:"name,omitempty"`
		Address   string  `json:"address,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	if req.JID == "" {
		jsonError(w, 400, "jid is required")
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

	wa := s.client.GetWhatsmeowClient()
	loc := &waE2E.LocationMessage{
		DegreesLatitude:  proto.Float64(req.Latitude),
		DegreesLongitude: proto.Float64(req.Longitude),
	}
	if req.Name != "" {
		loc.Name = proto.String(req.Name)
	}
	if req.Address != "" {
		loc.Address = proto.String(req.Address)
	}
	msg := &waE2E.Message{LocationMessage: loc}

	resp, err := wa.SendMessage(context.Background(), recipientJID, msg)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("send failed: %v", err))
		return
	}

	// Mirror handleSend: stash a placeholder row so the chat list's
	// last_message preview lands instantly without waiting for the SSE
	// echo. We log it as type "location" for posterity.
	s.storeOutgoingMessage(resp.ID, recipientJID.String(), req.Name, "location")

	jsonOK(w, map[string]any{
		"success":    true,
		"message_id": resp.ID,
		"timestamp":  resp.Timestamp.Unix(),
	})
}
