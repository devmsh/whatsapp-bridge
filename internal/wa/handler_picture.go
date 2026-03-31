package wa

import (
	"encoding/json"
	"strings"

	"go.mau.fi/whatsmeow/types/events"

	"whatsapp-bridge-v2/internal/db"
)

func handlePicture(c *Client, evt *events.Picture) {
	jid := evt.JID.String()
	ts := evt.Timestamp.Unix()

	data, _ := json.Marshal(map[string]interface{}{
		"picture_id": evt.PictureID,
		"remove":     evt.Remove,
		"author":     evt.Author.String(),
	})
	c.Store.StoreEventLog(&db.EventLog{
		EventType: "picture_change",
		JID:       jid,
		ActorJID:  evt.Author.String(),
		Data:      string(data),
		Timestamp: ts,
	})

	// Update the contact or group picture_id
	if !evt.Remove {
		if strings.HasSuffix(jid, "@g.us") {
			// Group picture — update group record would need a partial update
			// For now we log the event; sync will pick up the change
		} else {
			c.Store.UpdateContactPicture(jid, evt.PictureID)
		}
	}

	c.Log.Debugf("Picture changed for %s (remove=%v)", jid, evt.Remove)
}

func handleIdentityChange(c *Client, evt *events.IdentityChange) {
	c.Store.StoreEventLog(&db.EventLog{
		EventType: "identity_change",
		JID:       evt.JID.String(),
		Data:      mustJSON(map[string]bool{"implicit": evt.Implicit}),
		Timestamp: evt.Timestamp.Unix(),
	})
	c.Log.Infof("Identity changed for %s (implicit=%v)", evt.JID.String(), evt.Implicit)
}
