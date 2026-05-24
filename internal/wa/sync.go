package wa

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/types"

	"whatsapp-bridge-v2/internal/db"
)

// SyncContacts populates contacts from all joined groups and device contact list.
func SyncContacts(c *Client) error {
	c.Log.Infof("Starting contacts sync...")

	groups, err := c.WA.GetJoinedGroups(context.Background())
	if err != nil {
		return err
	}

	now := time.Now().Unix()
	contactCount := 0

	for _, group := range groups {
		for _, p := range group.Participants {
			phone := p.JID.User
			if p.PhoneNumber.User != "" {
				phone = p.PhoneNumber.User
			}
			lid := p.LID.User

			name := ""
			contactJID := p.JID
			if p.PhoneNumber.User != "" {
				contactJID = p.PhoneNumber
			}
			contact, cerr := c.WA.Store.Contacts.GetContact(context.Background(), contactJID)
			if cerr == nil && contact.FullName != "" {
				name = contact.FullName
			}

			if phone != "" {
				jid := phone + "@s.whatsapp.net"
				c.Store.StoreContact(&db.Contact{
					JID:       jid,
					Phone:     phone,
					LID:       lid,
					Name:      name,
					PushName:  contact.PushName,
					UpdatedAt: now,
				})
				contactCount++
			}
		}
	}

	c.Log.Infof("Group contacts sync: %d contacts from %d groups", contactCount, len(groups))

	// Full contact list from device store
	allContacts, err := c.WA.Store.Contacts.GetAllContacts(context.Background())
	if err == nil {
		deviceCount := 0
		for jid, contact := range allContacts {
			if jid.User != "" && contact.FullName != "" {
				c.Store.StoreContact(&db.Contact{
					JID:          jid.String(),
					Phone:        jid.User,
					Name:         contact.FullName,
					PushName:     contact.PushName,
					BusinessName: contact.BusinessName,
					UpdatedAt:    now,
				})
				deviceCount++
			}
		}
		c.Log.Infof("Device contacts sync: %d contacts", deviceCount)
	}

	return nil
}

// SyncGroups fetches full metadata for all joined groups.
func SyncGroups(c *Client) error {
	c.Log.Infof("Starting groups sync...")

	groups, err := c.WA.GetJoinedGroups(context.Background())
	if err != nil {
		return err
	}

	now := time.Now().Unix()

	for _, gi := range groups {
		g := groupInfoToDB(gi)
		g.UpdatedAt = now

		if err := c.Store.StoreGroup(g); err != nil {
			c.Log.Warnf("Failed to store group %s: %v", gi.JID.String(), err)
			continue
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
		// No per-group network call here — participants come from the single
		// GetJoinedGroups response above — so there is nothing to rate-limit.
		// (A previous 2s sleep here made the initial sync take 30+ min and
		// blocked SyncContacts behind it.)
	}

	c.Log.Infof("Groups sync complete: %d groups", len(groups))
	return nil
}

// StartPeriodicSync runs contacts and groups sync every interval.
// The initial sync is triggered by handleConnected — this only handles the periodic repeat.
func StartPeriodicSync(c *Client, interval time.Duration) {
	go func() {
		for {
			time.Sleep(interval)
			if !c.IsConnected() {
				continue
			}
			c.Log.Infof("Periodic sync starting...")
			SyncGroups(c)
			SyncContacts(c)
			c.Log.Infof("Periodic sync complete")
		}
	}()
}

// groupInfoToDB converts a whatsmeow GroupInfo to our db.Group.
func groupInfoToDB(gi *types.GroupInfo) *db.Group {
	return &db.Group{
		JID:                          gi.JID.String(),
		OwnerJID:                     gi.OwnerJID.String(),
		Name:                         gi.Name,
		NameSetAt:                    gi.NameSetAt.Unix(),
		NameSetBy:                    gi.NameSetBy.String(),
		Topic:                        gi.Topic,
		TopicID:                      gi.TopicID,
		TopicSetAt:                   gi.TopicSetAt.Unix(),
		TopicSetBy:                   gi.TopicSetBy.String(),
		TopicDeleted:                 gi.TopicDeleted,
		IsLocked:                     gi.IsLocked,
		IsAnnounce:                   gi.IsAnnounce,
		AnnounceVersionID:            gi.AnnounceVersionID,
		IsEphemeral:                  gi.IsEphemeral,
		DisappearingTimer:            int(gi.DisappearingTimer),
		IsIncognito:                  gi.IsIncognito,
		IsParent:                     gi.IsParent,
		DefaultMembershipApprovalMode: gi.DefaultMembershipApprovalMode,
		LinkedParentJID:              gi.LinkedParentJID.String(),
		IsDefaultSub:                 gi.IsDefaultSubGroup,
		MemberAddMode:                string(gi.MemberAddMode),
		JoinApprovalRequired:         gi.IsJoinApprovalRequired,
		GroupCreated:                 gi.GroupCreated.Unix(),
		CreatorCountryCode:           gi.CreatorCountryCode,
		ParticipantCount:             gi.ParticipantCount,
		Suspended:                    gi.Suspended,
	}
}
