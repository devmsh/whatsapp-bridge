package api

import (
	"context"
	"net/http"

	"go.mau.fi/whatsmeow/types"
)

// handleLinkedDevices returns the user's currently-paired devices —
// equivalent of WA's "Settings → Linked devices" panel. Resolved via
// GetUserInfo against our own JID (whatsmeow ships device-list info inside
// the user-info usync response).
//
// Shape:
//
//	{
//	  "current": "<digits>:N@s.whatsapp.net",   // this session's device JID
//	  "devices": [
//	    { "jid": "...:0@s.whatsapp.net", "is_primary": true,  "is_current": false },
//	    { "jid": "...:7@s.whatsapp.net", "is_primary": false, "is_current": true  },
//	    ...
//	  ]
//	}
//
// Device :0 is always the primary (the phone that scanned the QR for every
// other device). Companions get IDs > 0. Whatsmeow doesn't expose
// per-device platform / last-active without an extra round-trip, so we
// stick with the JID + the two booleans the UI needs to render rows.
func (s *Server) handleLinkedDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	wa := s.client.GetWhatsmeowClient()
	dev := wa.Store
	if dev == nil || dev.ID == nil {
		jsonError(w, 503, "not connected")
		return
	}
	selfJID := dev.ID.ToNonAD()
	infos, err := wa.GetUserInfo(context.Background(), []types.JID{selfJID})
	if err != nil {
		jsonError(w, 500, "get self info: "+err.Error())
		return
	}
	info := infos[selfJID]

	type deviceRow struct {
		JID       string `json:"jid"`
		IsPrimary bool   `json:"is_primary"`
		IsCurrent bool   `json:"is_current"`
	}
	rows := make([]deviceRow, 0, len(info.Devices))
	for _, d := range info.Devices {
		rows = append(rows, deviceRow{
			JID:       d.String(),
			IsPrimary: d.Device == 0,
			IsCurrent: d.String() == dev.ID.String(),
		})
	}
	jsonOK(w, map[string]any{
		"current": dev.ID.String(),
		"devices": rows,
	})
}
