package wa

import (
	"go.mau.fi/whatsmeow/types/events"

	"whatsapp-bridge-v2/internal/db"
)

func handlePushName(c *Client, evt *events.PushName) {
	jid := evt.JID.String()
	c.Store.StoreContact(&db.Contact{
		JID:      jid,
		Phone:    evt.JID.User,
		PushName: evt.NewPushName,
	})
	c.Log.Debugf("Push name for %s: %s -> %s", jid, evt.OldPushName, evt.NewPushName)
}

func handleBusinessName(c *Client, evt *events.BusinessName) {
	jid := evt.JID.String()
	c.Store.StoreContact(&db.Contact{
		JID:          jid,
		Phone:        evt.JID.User,
		BusinessName: evt.NewBusinessName,
		IsBusiness:   true,
	})
	c.Log.Debugf("Business name for %s: %s -> %s", jid, evt.OldBusinessName, evt.NewBusinessName)
}

func handleContact(c *Client, evt *events.Contact) {
	jid := evt.JID.String()
	action := evt.Action
	if action == nil {
		return
	}
	name := action.GetFullName()
	firstName := action.GetFirstName()
	if name == "" {
		name = firstName
	}
	c.Store.StoreContact(&db.Contact{
		JID:       jid,
		Phone:     evt.JID.User,
		Name:      name,
		UpdatedAt: evt.Timestamp.Unix(),
	})
}

func handleUserAbout(c *Client, evt *events.UserAbout) {
	jid := evt.JID.String()
	c.Store.UpdateContactStatus(jid, evt.Status, evt.Timestamp.Unix())
	c.Log.Debugf("Status update for %s: %s", jid, evt.Status)
}
