package db

import "time"

// Newsletter maps to the newsletters table, derived from types.NewsletterMetadata.
type Newsletter struct {
	JID               string `json:"jid"`
	Name              string `json:"name"`
	Description       string `json:"description,omitempty"`
	SubscriberCount   int    `json:"subscriber_count"`
	VerificationState string `json:"verification_state,omitempty"`
	PictureID         string `json:"picture_id,omitempty"`
	PictureURL        string `json:"picture_url,omitempty"`
	InviteCode        string `json:"invite_code,omitempty"`
	Role              string `json:"role,omitempty"`
	Muted             string `json:"muted,omitempty"`
	State             string `json:"state,omitempty"`
	CreationTime      int64  `json:"creation_time,omitempty"`
	UpdatedAt         int64  `json:"updated_at"`
}

// StoreNewsletter upserts a newsletter.
func (s *Store) StoreNewsletter(n *Newsletter) error {
	if n.UpdatedAt == 0 {
		n.UpdatedAt = time.Now().Unix()
	}
	_, err := s.DB.Exec(`INSERT OR REPLACE INTO newsletters (
		jid, name, description, subscriber_count, verification_state,
		picture_id, picture_url, invite_code, role, muted, state,
		creation_time, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		n.JID, n.Name, n.Description, n.SubscriberCount, n.VerificationState,
		n.PictureID, n.PictureURL, n.InviteCode, n.Role, n.Muted, n.State,
		n.CreationTime, n.UpdatedAt,
	)
	return err
}

// GetNewsletter returns a newsletter by JID.
func (s *Store) GetNewsletter(jid string) (*Newsletter, error) {
	row := s.DB.QueryRow(`SELECT jid, name, description, subscriber_count, verification_state,
		picture_id, picture_url, invite_code, role, muted, state, creation_time, updated_at
		FROM newsletters WHERE jid = ?`, jid)
	n := &Newsletter{}
	err := row.Scan(&n.JID, &n.Name, &n.Description, &n.SubscriberCount, &n.VerificationState,
		&n.PictureID, &n.PictureURL, &n.InviteCode, &n.Role, &n.Muted, &n.State,
		&n.CreationTime, &n.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return n, nil
}

// GetNewsletters returns all newsletters.
func (s *Store) GetNewsletters() ([]Newsletter, error) {
	rows, err := s.DB.Query(`SELECT jid, name, description, subscriber_count, verification_state,
		picture_id, picture_url, invite_code, role, muted, state, creation_time, updated_at
		FROM newsletters ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nls []Newsletter
	for rows.Next() {
		var n Newsletter
		if err := rows.Scan(&n.JID, &n.Name, &n.Description, &n.SubscriberCount, &n.VerificationState,
			&n.PictureID, &n.PictureURL, &n.InviteCode, &n.Role, &n.Muted, &n.State,
			&n.CreationTime, &n.UpdatedAt); err != nil {
			return nls, err
		}
		nls = append(nls, n)
	}
	return nls, rows.Err()
}

// DeleteNewsletter removes a newsletter.
func (s *Store) DeleteNewsletter(jid string) error {
	_, err := s.DB.Exec(`DELETE FROM newsletters WHERE jid = ?`, jid)
	return err
}
