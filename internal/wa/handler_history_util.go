package wa

import (
	"time"

	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
)

// parseWebMessageInfo converts a WebMessageInfo into a types.MessageInfo.
func parseWebMessageInfo(wmi *waWeb.WebMessageInfo) types.MessageInfo {
	info := types.MessageInfo{}

	key := wmi.GetKey()
	if key != nil {
		info.ID = key.GetID()
		info.IsFromMe = key.GetFromMe()

		remoteJID := key.GetRemoteJID()
		if remoteJID != "" {
			chatJID, _ := types.ParseJID(remoteJID)
			info.Chat = chatJID
		}

		participant := key.GetParticipant()
		if participant != "" {
			senderJID, _ := types.ParseJID(participant)
			info.Sender = senderJID
			info.IsGroup = true
		} else if info.IsFromMe {
			info.Sender = info.Chat
		} else {
			info.Sender = info.Chat
		}
	}

	info.PushName = wmi.GetPushName()

	ts := wmi.GetMessageTimestamp()
	if ts > 0 {
		info.Timestamp = time.Unix(int64(ts), 0)
	}

	return info
}
