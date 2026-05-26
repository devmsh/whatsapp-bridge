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

	// Join/Leave events — store the participant changes AND log a system
	// pill so the user can see "<actor> added X, Y" / "Z left" inline in
	// the chat. Each event flushes to one log row with a JSON list of
	// JIDs so client rendering can name them properly.
	if len(evt.Join) > 0 {
		jids := make([]string, 0, len(evt.Join))
		for _, j := range evt.Join {
			c.Store.StoreGroupParticipant(&db.GroupParticipant{
				GroupJID:  jid,
				JID:       j.String(),
				Phone:     j.User,
				UpdatedAt: now,
			})
			jids = append(jids, j.String())
		}
		c.Store.StoreEventLog(&db.EventLog{
			EventType: "group_join",
			JID:       jid,
			ActorJID:  actor,
			Data:      mustJSON(map[string][]string{"jids": jids}),
			Timestamp: ts,
		})
	}
	if len(evt.Leave) > 0 {
		jids := make([]string, 0, len(evt.Leave))
		for _, l := range evt.Leave {
			c.Store.RemoveGroupParticipant(jid, l.String())
			jids = append(jids, l.String())
		}
		c.Store.StoreEventLog(&db.EventLog{
			EventType: "group_leave",
			JID:       jid,
			ActorJID:  actor,
			Data:      mustJSON(map[string][]string{"jids": jids}),
			Timestamp: ts,
		})
	}

	// Promote/Demote — same shape; the client renders
	// "<actor> made X an admin" / "<actor> dismissed X as admin".
	if len(evt.Promote) > 0 {
		jids := make([]string, 0, len(evt.Promote))
		for _, p := range evt.Promote {
			c.Store.StoreGroupParticipant(&db.GroupParticipant{
				GroupJID:  jid,
				JID:       p.String(),
				Phone:     p.User,
				IsAdmin:   true,
				UpdatedAt: now,
			})
			jids = append(jids, p.String())
		}
		c.Store.StoreEventLog(&db.EventLog{
			EventType: "group_promote",
			JID:       jid,
			ActorJID:  actor,
			Data:      mustJSON(map[string][]string{"jids": jids}),
			Timestamp: ts,
		})
	}
	if len(evt.Demote) > 0 {
		jids := make([]string, 0, len(evt.Demote))
		for _, d := range evt.Demote {
			c.Store.StoreGroupParticipant(&db.GroupParticipant{
				GroupJID:  jid,
				JID:       d.String(),
				Phone:     d.User,
				IsAdmin:   false,
				UpdatedAt: now,
			})
			jids = append(jids, d.String())
		}
		c.Store.StoreEventLog(&db.EventLog{
			EventType: "group_demote",
			JID:       jid,
			ActorJID:  actor,
			Data:      mustJSON(map[string][]string{"jids": jids}),
			Timestamp: ts,
		})
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
