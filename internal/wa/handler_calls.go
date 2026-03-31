package wa

import (
	"go.mau.fi/whatsmeow/types/events"

	"whatsapp-bridge-v2/internal/db"
)

func handleCallOffer(c *Client, evt *events.CallOffer) {
	c.Store.StoreCall(&db.CallEvent{
		CallID:         evt.CallID,
		FromJID:        evt.From.String(),
		Timestamp:      evt.Timestamp.Unix(),
		CallCreator:    evt.CallCreator.String(),
		GroupJID:        evt.GroupJID.String(),
		EventType:      "offer",
		RemotePlatform: evt.RemotePlatform,
		RemoteVersion:  evt.RemoteVersion,
	})
	c.Log.Infof("Call offer from %s (ID: %s)", evt.From.String(), evt.CallID)
}

func handleCallAccept(c *Client, evt *events.CallAccept) {
	c.Store.StoreCall(&db.CallEvent{
		CallID:         evt.CallID,
		FromJID:        evt.From.String(),
		Timestamp:      evt.Timestamp.Unix(),
		CallCreator:    evt.CallCreator.String(),
		GroupJID:        evt.GroupJID.String(),
		EventType:      "accept",
		RemotePlatform: evt.RemotePlatform,
		RemoteVersion:  evt.RemoteVersion,
	})
}

func handleCallTerminate(c *Client, evt *events.CallTerminate) {
	c.Store.StoreCall(&db.CallEvent{
		CallID:    evt.CallID,
		FromJID:   evt.From.String(),
		Timestamp: evt.Timestamp.Unix(),
		GroupJID:   evt.GroupJID.String(),
		EventType: "terminate",
	})
}

func handleCallOfferNotice(c *Client, evt *events.CallOfferNotice) {
	c.Store.StoreCall(&db.CallEvent{
		CallID:    evt.CallID,
		FromJID:   evt.From.String(),
		Timestamp: evt.Timestamp.Unix(),
		GroupJID:   evt.GroupJID.String(),
		EventType: "offer_notice",
	})
}
