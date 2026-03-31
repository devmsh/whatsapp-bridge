package wa

import (
	"go.mau.fi/whatsmeow/types/events"
)

// RegisterHandlers adds a single event handler that dispatches to typed handlers.
func RegisterHandlers(c *Client) {
	c.WA.AddEventHandler(func(rawEvt interface{}) {
		switch evt := rawEvt.(type) {
		// Messages
		case *events.Message:
			handleMessage(c, evt)
		case *events.HistorySync:
			handleHistorySync(c, evt)

		// Receipts
		case *events.Receipt:
			handleReceipt(c, evt)

		// Groups
		case *events.GroupInfo:
			handleGroupInfo(c, evt)
		case *events.JoinedGroup:
			handleJoinedGroup(c, evt)

		// Presence
		case *events.ChatPresence:
			handleChatPresence(c, evt)
		case *events.Presence:
			handlePresence(c, evt)

		// Profile changes
		case *events.Picture:
			handlePicture(c, evt)
		case *events.UserAbout:
			handleUserAbout(c, evt)

		// Contact/push name changes (app state)
		case *events.PushName:
			handlePushName(c, evt)
		case *events.BusinessName:
			handleBusinessName(c, evt)
		case *events.Contact:
			handleContact(c, evt)

		// Identity
		case *events.IdentityChange:
			handleIdentityChange(c, evt)

		// Privacy & blocklist
		case *events.PrivacySettings:
			handlePrivacySettings(c, evt)
		case *events.Blocklist:
			handleBlocklist(c, evt)
		case *events.BlocklistChange:
			handleBlocklistChange(c, evt)

		// Calls
		case *events.CallOffer:
			handleCallOffer(c, evt)
		case *events.CallAccept:
			handleCallAccept(c, evt)
		case *events.CallTerminate:
			handleCallTerminate(c, evt)
		case *events.CallOfferNotice:
			handleCallOfferNotice(c, evt)

		// Newsletters
		case *events.NewsletterJoin:
			handleNewsletterJoin(c, evt)
		case *events.NewsletterLeave:
			handleNewsletterLeave(c, evt)
		case *events.NewsletterMuteChange:
			handleNewsletterMuteChange(c, evt)
		case *events.NewsletterLiveUpdate:
			handleNewsletterLiveUpdate(c, evt)

		// App state sync events (pin, mute, archive, etc.)
		case *events.Pin:
			handlePin(c, evt)
		case *events.Mute:
			handleMute(c, evt)
		case *events.Archive:
			handleArchive(c, evt)
		case *events.MarkChatAsRead:
			handleMarkChatAsRead(c, evt)
		case *events.AppStateSyncComplete:
			handleAppStateSyncComplete(c, evt)

		// Connection lifecycle
		case *events.Connected:
			handleConnected(c, evt)
		case *events.LoggedOut:
			handleLoggedOut(c, evt)
		case *events.Disconnected:
			handleDisconnected(c, evt)
		case *events.StreamReplaced:
			handleStreamReplaced(c, evt)
		case *events.TemporaryBan:
			handleTemporaryBan(c, evt)
		case *events.ClientOutdated:
			handleClientOutdated(c, evt)

		// Errors
		case *events.UndecryptableMessage:
			handleUndecryptable(c, evt)
		case *events.MediaRetry:
			handleMediaRetry(c, evt)

		default:
			// Unknown event — log at debug level
			c.Log.Debugf("Unhandled event type: %T", rawEvt)
		}
	})
}
