package wa

import (
	"go.mau.fi/whatsmeow/types/events"
)

func handleConnected(c *Client, _ *events.Connected) {
	c.Log.Infof("Connected to WhatsApp")

	// Trigger immediate sync after connection
	go func() {
		c.Log.Infof("Running initial sync...")
		if err := SyncGroups(c); err != nil {
			c.Log.Warnf("Initial group sync failed: %v", err)
		}
		if err := SyncContacts(c); err != nil {
			c.Log.Warnf("Initial contact sync failed: %v", err)
		}
		c.Log.Infof("Initial sync complete")
	}()
}

func handleLoggedOut(c *Client, evt *events.LoggedOut) {
	c.Log.Errorf("Logged out: onConnect=%v reason=%s", evt.OnConnect, evt.Reason.String())
}

func handleDisconnected(c *Client, _ *events.Disconnected) {
	c.Log.Warnf("Disconnected from WhatsApp")
}

func handleStreamReplaced(c *Client, _ *events.StreamReplaced) {
	c.Log.Warnf("Stream replaced — another client connected with same session")
}

func handleTemporaryBan(c *Client, evt *events.TemporaryBan) {
	c.Log.Errorf("Temporary ban: %s", evt.String())
}

func handleClientOutdated(c *Client, _ *events.ClientOutdated) {
	c.Log.Errorf("Client outdated — please update whatsmeow")
}

func handleUndecryptable(c *Client, evt *events.UndecryptableMessage) {
	c.Log.Warnf("Undecryptable message from %s in %s (unavailable=%v)",
		evt.Info.Sender.String(), evt.Info.Chat.String(), evt.IsUnavailable)
}

func handleMediaRetry(c *Client, evt *events.MediaRetry) {
	c.Log.Debugf("Media retry for message %s in %s", evt.MessageID, evt.ChatID.String())
}
