package wa

import (
	"encoding/json"
	"time"

	"go.mau.fi/whatsmeow/types/events"

	"whatsapp-bridge-v2/internal/db"
)

func handlePrivacySettings(c *Client, evt *events.PrivacySettings) {
	data, _ := json.Marshal(evt.NewSettings)
	c.Store.StoreEventLog(&db.EventLog{
		EventType: "privacy_settings_change",
		JID:       "self",
		Data:      string(data),
		Timestamp: time.Now().Unix(),
	})
	c.Log.Infof("Privacy settings updated")
}

func handleBlocklist(c *Client, evt *events.Blocklist) {
	data, _ := json.Marshal(map[string]interface{}{
		"action":     evt.Action,
		"dhash":      evt.DHash,
		"prev_dhash": evt.PrevDHash,
	})
	c.Store.StoreEventLog(&db.EventLog{
		EventType: "blocklist",
		JID:       "self",
		Data:      string(data),
		Timestamp: time.Now().Unix(),
	})
}

func handleBlocklistChange(c *Client, evt *events.BlocklistChange) {
	// Individual block/unblock events within a blocklist update
	data, _ := json.Marshal(map[string]string{
		"jid":    evt.JID.String(),
		"action": string(evt.Action),
	})
	c.Store.StoreEventLog(&db.EventLog{
		EventType: "blocklist_change",
		JID:       evt.JID.String(),
		Data:      string(data),
		Timestamp: time.Now().Unix(),
	})
}
