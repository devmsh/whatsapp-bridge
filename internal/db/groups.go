package db

import "time"

// Group maps to the groups table, derived from types.GroupInfo.
type Group struct {
	JID                          string `json:"jid"`
	OwnerJID                     string `json:"owner_jid,omitempty"`
	Name                         string `json:"name"`
	NameSetAt                    int64  `json:"name_set_at,omitempty"`
	NameSetBy                    string `json:"name_set_by,omitempty"`
	Topic                        string `json:"topic,omitempty"`
	TopicID                      string `json:"topic_id,omitempty"`
	TopicSetAt                   int64  `json:"topic_set_at,omitempty"`
	TopicSetBy                   string `json:"topic_set_by,omitempty"`
	TopicDeleted                 bool   `json:"topic_deleted"`
	IsLocked                     bool   `json:"is_locked"`
	IsAnnounce                   bool   `json:"is_announce"`
	AnnounceVersionID            string `json:"announce_version_id,omitempty"`
	IsEphemeral                  bool   `json:"is_ephemeral"`
	DisappearingTimer            int    `json:"disappearing_timer,omitempty"`
	IsIncognito                  bool   `json:"is_incognito"`
	IsParent                     bool   `json:"is_parent"`
	DefaultMembershipApprovalMode string `json:"default_membership_approval_mode,omitempty"`
	LinkedParentJID              string `json:"linked_parent_jid,omitempty"`
	IsDefaultSub                 bool   `json:"is_default_sub"`
	MemberAddMode                string `json:"member_add_mode,omitempty"`
	JoinApprovalRequired         bool   `json:"join_approval_required"`
	GroupCreated                 int64  `json:"group_created,omitempty"`
	CreatorCountryCode           string `json:"creator_country_code,omitempty"`
	ParticipantCount             int    `json:"participant_count"`
	Suspended                    bool   `json:"suspended"`
	UpdatedAt                    int64  `json:"updated_at"`
}

// GroupParticipant maps to the group_participants table.
type GroupParticipant struct {
	GroupJID     string `json:"group_jid"`
	JID          string `json:"jid"`
	Phone        string `json:"phone,omitempty"`
	LID          string `json:"lid,omitempty"`
	IsAdmin      bool   `json:"is_admin"`
	IsSuperAdmin bool   `json:"is_super_admin"`
	DisplayName  string `json:"display_name,omitempty"`
	ErrorCode    int    `json:"error_code,omitempty"`
	UpdatedAt    int64  `json:"updated_at"`
}

// StoreGroup upserts a group record.
func (s *Store) StoreGroup(g *Group) error {
	if g.UpdatedAt == 0 {
		g.UpdatedAt = time.Now().Unix()
	}
	_, err := s.DB.Exec(`INSERT OR REPLACE INTO groups (
		jid, owner_jid, name, name_set_at, name_set_by,
		topic, topic_id, topic_set_at, topic_set_by, topic_deleted,
		is_locked, is_announce, announce_version_id,
		is_ephemeral, disappearing_timer, is_incognito,
		is_parent, default_membership_approval_mode, linked_parent_jid, is_default_sub,
		member_add_mode, join_approval_required,
		group_created, creator_country_code, participant_count, suspended, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		g.JID, g.OwnerJID, g.Name, g.NameSetAt, g.NameSetBy,
		g.Topic, g.TopicID, g.TopicSetAt, g.TopicSetBy, g.TopicDeleted,
		g.IsLocked, g.IsAnnounce, g.AnnounceVersionID,
		g.IsEphemeral, g.DisappearingTimer, g.IsIncognito,
		g.IsParent, g.DefaultMembershipApprovalMode, g.LinkedParentJID, g.IsDefaultSub,
		g.MemberAddMode, g.JoinApprovalRequired,
		g.GroupCreated, g.CreatorCountryCode, g.ParticipantCount, g.Suspended, g.UpdatedAt,
	)
	return err
}

// GetGroup returns a group by JID.
func (s *Store) GetGroup(jid string) (*Group, error) {
	row := s.DB.QueryRow(`SELECT jid, owner_jid, name, name_set_at, name_set_by,
		topic, topic_id, topic_set_at, topic_set_by, topic_deleted,
		is_locked, is_announce, announce_version_id,
		is_ephemeral, disappearing_timer, is_incognito,
		is_parent, default_membership_approval_mode, linked_parent_jid, is_default_sub,
		member_add_mode, join_approval_required,
		group_created, creator_country_code, participant_count, suspended, updated_at
		FROM groups WHERE jid = ?`, jid)
	g := &Group{}
	err := row.Scan(&g.JID, &g.OwnerJID, &g.Name, &g.NameSetAt, &g.NameSetBy,
		&g.Topic, &g.TopicID, &g.TopicSetAt, &g.TopicSetBy, &g.TopicDeleted,
		&g.IsLocked, &g.IsAnnounce, &g.AnnounceVersionID,
		&g.IsEphemeral, &g.DisappearingTimer, &g.IsIncognito,
		&g.IsParent, &g.DefaultMembershipApprovalMode, &g.LinkedParentJID, &g.IsDefaultSub,
		&g.MemberAddMode, &g.JoinApprovalRequired,
		&g.GroupCreated, &g.CreatorCountryCode, &g.ParticipantCount, &g.Suspended, &g.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return g, nil
}

// GetGroups returns all groups.
func (s *Store) GetGroups() ([]Group, error) {
	rows, err := s.DB.Query(`SELECT jid, owner_jid, name, name_set_at, name_set_by,
		topic, topic_id, topic_set_at, topic_set_by, topic_deleted,
		is_locked, is_announce, announce_version_id,
		is_ephemeral, disappearing_timer, is_incognito,
		is_parent, default_membership_approval_mode, linked_parent_jid, is_default_sub,
		member_add_mode, join_approval_required,
		group_created, creator_country_code, participant_count, suspended, updated_at
		FROM groups ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.JID, &g.OwnerJID, &g.Name, &g.NameSetAt, &g.NameSetBy,
			&g.Topic, &g.TopicID, &g.TopicSetAt, &g.TopicSetBy, &g.TopicDeleted,
			&g.IsLocked, &g.IsAnnounce, &g.AnnounceVersionID,
			&g.IsEphemeral, &g.DisappearingTimer, &g.IsIncognito,
			&g.IsParent, &g.DefaultMembershipApprovalMode, &g.LinkedParentJID, &g.IsDefaultSub,
			&g.MemberAddMode, &g.JoinApprovalRequired,
			&g.GroupCreated, &g.CreatorCountryCode, &g.ParticipantCount, &g.Suspended, &g.UpdatedAt); err != nil {
			return groups, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// GroupDiscovery holds a group with activity stats for new group detection.
type GroupDiscovery struct {
	Group
	MessageCount   int    `json:"message_count"`
	LastMessageAt  int64  `json:"last_message_at,omitempty"`
	ParticipantNum int    `json:"participant_num"`
	Tracked        bool   `json:"tracked"`
}

// GetGroupsDiscovery returns all groups with activity stats, marking which are tracked.
func (s *Store) GetGroupsDiscovery(trackedJIDs map[string]bool) ([]GroupDiscovery, error) {
	rows, err := s.DB.Query(`
		SELECT g.jid, COALESCE(g.name,''), g.owner_jid, g.group_created, g.is_announce, g.is_parent, g.suspended,
			COALESCE(msg.cnt, 0) as message_count,
			COALESCE(msg.last_ts, 0) as last_message_at,
			COALESCE(gp.pcnt, 0) as participant_num
		FROM groups g
		LEFT JOIN (
			SELECT chat_jid, COUNT(*) as cnt, MAX(timestamp) as last_ts
			FROM messages WHERE chat_jid LIKE '%@g.us'
			GROUP BY chat_jid
		) msg ON g.jid = msg.chat_jid
		LEFT JOIN (
			SELECT group_jid, COUNT(*) as pcnt
			FROM group_participants
			GROUP BY group_jid
		) gp ON g.jid = gp.group_jid
		ORDER BY g.group_created DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []GroupDiscovery
	for rows.Next() {
		var d GroupDiscovery
		if err := rows.Scan(&d.JID, &d.Name, &d.OwnerJID, &d.GroupCreated,
			&d.IsAnnounce, &d.IsParent, &d.Suspended,
			&d.MessageCount, &d.LastMessageAt, &d.ParticipantNum); err != nil {
			continue
		}
		d.Tracked = trackedJIDs[d.JID]
		results = append(results, d)
	}
	return results, rows.Err()
}

// StoreGroupParticipant upserts a group participant.
func (s *Store) StoreGroupParticipant(p *GroupParticipant) error {
	if p.UpdatedAt == 0 {
		p.UpdatedAt = time.Now().Unix()
	}
	_, err := s.DB.Exec(`INSERT OR REPLACE INTO group_participants (
		group_jid, jid, phone, lid, is_admin, is_super_admin, display_name, error_code, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?)`,
		p.GroupJID, p.JID, p.Phone, p.LID, p.IsAdmin, p.IsSuperAdmin, p.DisplayName, p.ErrorCode, p.UpdatedAt,
	)
	return err
}

// GetGroupParticipants returns all participants of a group.
func (s *Store) GetGroupParticipants(groupJID string) ([]GroupParticipant, error) {
	rows, err := s.DB.Query(`SELECT group_jid, jid, phone, lid, is_admin, is_super_admin,
		display_name, error_code, updated_at
		FROM group_participants WHERE group_jid = ?`, groupJID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var parts []GroupParticipant
	for rows.Next() {
		var p GroupParticipant
		if err := rows.Scan(&p.GroupJID, &p.JID, &p.Phone, &p.LID, &p.IsAdmin, &p.IsSuperAdmin,
			&p.DisplayName, &p.ErrorCode, &p.UpdatedAt); err != nil {
			return parts, err
		}
		parts = append(parts, p)
	}
	return parts, rows.Err()
}

// RemoveGroupParticipant deletes a participant from a group.
func (s *Store) RemoveGroupParticipant(groupJID, jid string) error {
	_, err := s.DB.Exec(`DELETE FROM group_participants WHERE group_jid = ? AND jid = ?`, groupJID, jid)
	return err
}
