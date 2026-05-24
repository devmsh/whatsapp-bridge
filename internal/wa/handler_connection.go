package wa

import (
	"go.mau.fi/whatsmeow/types/events"
)

func handlePairSuccess(c *Client, evt *events.PairSuccess) {
	c.Log.Infof("Paired with %s (platform=%s)", evt.ID.String(), evt.Platform)
	dev := &DeviceInfo{
		JID:          evt.ID.String(),
		Platform:     evt.Platform,
		BusinessName: evt.BusinessName,
	}
	if !evt.LID.IsEmpty() {
		dev.LID = evt.LID.String()
	}
	c.Auth.onPairSuccess(dev)
}

func handleConnected(c *Client, _ *events.Connected) {
	c.Log.Infof("Connected to WhatsApp")
	c.Auth.onConnected()

	// Trigger immediate sync after connection
	go func() {
		c.Log.Infof("Running initial sync...")
		if err := SyncGroups(c); err != nil {
			c.Log.Warnf("Initial group sync failed: %v", err)
		}
		if err := SyncContacts(c); err != nil {
			c.Log.Warnf("Initial contact sync failed: %v", err)
		}
		c.Sync.MarkInitialSyncDone()
		c.Log.Infof("Initial sync complete")
	}()
}

func handleOfflineSyncPreview(c *Client, evt *events.OfflineSyncPreview) {
	c.Log.Infof("Offline sync preview: %d messages queued", evt.Messages)
	c.Sync.RecordOfflinePreview(evt.Messages)
}

func handleOfflineSyncCompleted(c *Client, evt *events.OfflineSyncCompleted) {
	c.Log.Infof("Offline sync completed: %d items", evt.Count)
	c.Sync.RecordOfflineCompleted(evt.Count)
}

func handleLoggedOut(c *Client, evt *events.LoggedOut) {
	c.Log.Errorf("Logged out: onConnect=%v reason=%s", evt.OnConnect, evt.Reason.String())
	c.Auth.onLoggedOut()
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
