package wa

import (
	"encoding/json"
	"time"

	"go.mau.fi/whatsmeow/types/events"

	"whatsapp-bridge-v2/internal/db"
)

func handleGroupInfo(c *Client, evt *events.GroupInfo) {
	jid := evt.JID.String()
	ts := evt.Timestamp.Unix()
	now := time.Now().Unix()

	actor := ""
	if evt.Sender != nil {
		actor = evt.Sender.String()
	}

	// Name change
	if evt.Name != nil {
		c.Store.StoreEventLog(&db.EventLog{
			EventType: "group_name_change",
			JID:       jid,
			ActorJID:  actor,
			Data:      mustJSON(map[string]string{"name": evt.Name.Name}),
			Timestamp: ts,
		})
	}

	// Topic change
	if evt.Topic != nil {
		c.Store.StoreEventLog(&db.EventLog{
			EventType: "group_topic_change",
			JID:       jid,
			ActorJID:  actor,
			Data:      mustJSON(map[string]string{"topic": evt.Topic.Topic}),
			Timestamp: ts,
		})
	}

	// Locked change
	if evt.Locked != nil {
		c.Store.StoreEventLog(&db.EventLog{
			EventType: "group_locked_change",
			JID:       jid,
			ActorJID:  actor,
			Data:      mustJSON(map[string]bool{"is_locked": evt.Locked.IsLocked}),
			Timestamp: ts,
		})
	}

	// Announce change
	if evt.Announce != nil {
		c.Store.StoreEventLog(&db.EventLog{
			EventType: "group_announce_change",
			JID:       jid,
			ActorJID:  actor,
			Data:      mustJSON(map[string]bool{"is_announce": evt.Announce.IsAnnounce}),
			Timestamp: ts,
		})
	}

	// Ephemeral change
	if evt.Ephemeral != nil {
		c.Store.StoreEventLog(&db.EventLog{
			EventType: "group_ephemeral_change",
			JID:       jid,
			ActorJID:  actor,
			Data:      mustJSON(map[string]interface{}{"is_ephemeral": evt.Ephemeral.IsEphemeral, "timer": evt.Ephemeral.DisappearingTimer}),
			Timestamp: ts,
		})
	}

	// Join/Leave events
	if len(evt.Join) > 0 {
		for _, j := range evt.Join {
			c.Store.StoreGroupParticipant(&db.GroupParticipant{
				GroupJID:  jid,
				JID:       j.String(),
				Phone:     j.User,
				UpdatedAt: now,
			})
		}
	}
	if len(evt.Leave) > 0 {
		for _, l := range evt.Leave {
			c.Store.RemoveGroupParticipant(jid, l.String())
		}
	}

	// Promote/Demote
	if len(evt.Promote) > 0 {
		for _, p := range evt.Promote {
			c.Store.StoreGroupParticipant(&db.GroupParticipant{
				GroupJID:  jid,
				JID:       p.String(),
				Phone:     p.User,
				IsAdmin:   true,
				UpdatedAt: now,
			})
		}
	}
	if len(evt.Demote) > 0 {
		for _, d := range evt.Demote {
			c.Store.StoreGroupParticipant(&db.GroupParticipant{
				GroupJID:  jid,
				JID:       d.String(),
				Phone:     d.User,
				IsAdmin:   false,
				UpdatedAt: now,
			})
		}
	}
}

func handleJoinedGroup(c *Client, evt *events.JoinedGroup) {
	gi := evt.GroupInfo
	now := time.Now().Unix()

	g := groupInfoToDB(&gi)
	g.UpdatedAt = now

	if err := c.Store.StoreGroup(g); err != nil {
		c.Log.Warnf("Failed to store joined group %s: %v", gi.JID.String(), err)
	}

	for _, p := range gi.Participants {
		c.Store.StoreGroupParticipant(&db.GroupParticipant{
			GroupJID:     gi.JID.String(),
			JID:          p.JID.String(),
			Phone:        p.PhoneNumber.User,
			LID:          p.LID.User,
			IsAdmin:      p.IsAdmin,
			IsSuperAdmin: p.IsSuperAdmin,
			DisplayName:  p.DisplayName,
			ErrorCode:    p.Error,
			UpdatedAt:    now,
		})
	}

	c.Log.Infof("Joined group %s (%s)", gi.Name, gi.JID.String())
}

func mustJSON(v interface{}) string {
	data, _ := json.Marshal(v)
	return string(data)
}
