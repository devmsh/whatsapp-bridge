package db

import (
	"database/sql"
	"strings"
	"time"
)

// Contact maps to the contacts table.
type Contact struct {
	JID          string `json:"jid"`
	LID          string `json:"lid,omitempty"`
	Phone        string `json:"phone,omitempty"`
	Name         string `json:"name"`
	PushName     string `json:"push_name,omitempty"`
	BusinessName string `json:"business_name,omitempty"`
	VerifiedName string `json:"verified_name,omitempty"`
	IsBusiness   bool   `json:"is_business"`
	StatusText   string `json:"status_text,omitempty"`
	StatusSetAt  int64  `json:"status_set_at,omitempty"`
	PictureID    string `json:"picture_id,omitempty"`
	PictureURL   string `json:"picture_url,omitempty"`
	FirstSeen    int64  `json:"first_seen,omitempty"`
	LastSeen     int64  `json:"last_seen,omitempty"`
	// Yours, not WhatsApp's. See schema.go for why sync cannot overwrite them.
	Kunya    string `json:"kunya,omitempty"`
	HowWeMet string `json:"how_we_met,omitempty"`
	// WhatsApp @username. Empty means we have not been sent one; a later
	// upsert never overwrites a stored username with empty.
	Username  string `json:"username,omitempty"`
	UpdatedAt int64  `json:"updated_at"`
}

const contactColumns = `jid, lid, phone, name, push_name, business_name, verified_name,
		is_business, status_text, status_set_at, picture_id, picture_url,
		first_seen, last_seen, kunya, how_we_met, username, updated_at`

func scanContact(s scanner, c *Contact) error {
	return s.Scan(&c.JID, &c.LID, &c.Phone, &c.Name, &c.PushName, &c.BusinessName, &c.VerifiedName,
		&c.IsBusiness, &c.StatusText, &c.StatusSetAt, &c.PictureID, &c.PictureURL,
		&c.FirstSeen, &c.LastSeen, &c.Kunya, &c.HowWeMet, &c.Username, &c.UpdatedAt)
}

// NormalizeUsername trims whitespace and a leading @. Lookup is case-insensitive
// elsewhere; this does not change case so stored values stay as WhatsApp sent them.
func NormalizeUsername(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "@")
	return strings.TrimSpace(s)
}

// StoreContact upserts a contact record.
func (s *Store) StoreContact(c *Contact) error {
	now := time.Now().Unix()
	if c.UpdatedAt == 0 {
		c.UpdatedAt = now
	}
	if c.FirstSeen == 0 {
		c.FirstSeen = now
	}
	c.Username = NormalizeUsername(c.Username)
	_, err := s.DB.Exec(`INSERT INTO contacts (
		jid, lid, phone, name, push_name, business_name, verified_name,
		is_business, status_text, status_set_at, picture_id, picture_url,
		first_seen, last_seen, username, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(jid) DO UPDATE SET
		lid = CASE WHEN excluded.lid != '' THEN excluded.lid ELSE contacts.lid END,
		phone = CASE WHEN excluded.phone != '' THEN excluded.phone ELSE contacts.phone END,
		name = CASE WHEN excluded.name != '' THEN excluded.name ELSE contacts.name END,
		push_name = CASE WHEN excluded.push_name != '' THEN excluded.push_name ELSE contacts.push_name END,
		business_name = CASE WHEN excluded.business_name != '' THEN excluded.business_name ELSE contacts.business_name END,
		verified_name = CASE WHEN excluded.verified_name != '' THEN excluded.verified_name ELSE contacts.verified_name END,
		is_business = CASE WHEN excluded.is_business THEN excluded.is_business ELSE contacts.is_business END,
		status_text = CASE WHEN excluded.status_text != '' THEN excluded.status_text ELSE contacts.status_text END,
		status_set_at = CASE WHEN excluded.status_set_at > 0 THEN excluded.status_set_at ELSE contacts.status_set_at END,
		picture_id = CASE WHEN excluded.picture_id != '' THEN excluded.picture_id ELSE contacts.picture_id END,
		picture_url = CASE WHEN excluded.picture_url != '' THEN excluded.picture_url ELSE contacts.picture_url END,
		last_seen = CASE WHEN excluded.last_seen > contacts.last_seen THEN excluded.last_seen ELSE contacts.last_seen END,
		username = CASE WHEN excluded.username != '' THEN excluded.username ELSE contacts.username END,
		updated_at = excluded.updated_at`,
		c.JID, c.LID, c.Phone, c.Name, c.PushName, c.BusinessName, c.VerifiedName,
		c.IsBusiness, c.StatusText, c.StatusSetAt, c.PictureID, c.PictureURL,
		c.FirstSeen, c.LastSeen, c.Username, c.UpdatedAt,
	)
	if err != nil {
		return err
	}
	s.propagateUsername(c)
	return nil
}

// propagateUsername copies a newly learned username onto matching group
// participant rows that do not already have one.
func (s *Store) propagateUsername(c *Contact) {
	if c == nil || c.Username == "" {
		return
	}
	_, _ = s.DB.Exec(`UPDATE group_participants SET username = ?
		WHERE username = '' AND (
			jid = ?
			OR (? != '' AND lid = ?)
			OR (? != '' AND phone = ?)
		)`, c.Username, c.JID, c.LID, c.LID, c.Phone, c.Phone)
}

// GetContact returns a contact by JID.
func (s *Store) GetContact(jid string) (*Contact, error) {
	row := s.DB.QueryRow(`SELECT `+contactColumns+` FROM contacts WHERE jid = ?`, jid)
	c := &Contact{}
	err := scanContact(row, c)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

// GetContactByUsername returns one stored contact whose username matches
// (case-insensitive, leading @ stripped). Prefers a phone-form JID when
// both a phone row and a LID row exist. Returns nil if none is stored —
// this never talks to WhatsApp.
func (s *Store) GetContactByUsername(username string) (*Contact, error) {
	username = NormalizeUsername(username)
	if username == "" {
		return nil, nil
	}
	row := s.DB.QueryRow(`SELECT `+contactColumns+` FROM contacts
		WHERE username != '' AND LOWER(username) = LOWER(?)
		ORDER BY CASE
			WHEN jid LIKE '%@s.whatsapp.net' THEN 0
			WHEN jid LIKE '%@lid' THEN 1
			ELSE 2
		END
		LIMIT 1`, username)
	c := &Contact{}
	err := scanContact(row, c)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

// GetContacts returns all contacts, optionally filtered by a search query
// (name / push_name / phone / business_name / username LIKE) and/or an
// exact case-insensitive username (leading @ stripped).
func (s *Store) GetContacts(query, username string) ([]Contact, error) {
	username = NormalizeUsername(username)
	sqlStr := `SELECT ` + contactColumns + ` FROM contacts`
	var where []string
	var args []any
	if query != "" {
		q := "%" + query + "%"
		where = append(where, `(name LIKE ? OR push_name LIKE ? OR phone LIKE ? OR business_name LIKE ? OR username LIKE ?)`)
		args = append(args, q, q, q, q, q)
	}
	if username != "" {
		where = append(where, `username != '' AND LOWER(username) = LOWER(?)`)
		args = append(args, username)
	}
	if len(where) > 0 {
		sqlStr += " WHERE " + strings.Join(where, " AND ")
	}
	sqlStr += " ORDER BY updated_at DESC"
	rows, err := s.DB.Query(sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var contacts []Contact
	for rows.Next() {
		var c Contact
		if err := scanContact(rows, &c); err != nil {
			return contacts, err
		}
		contacts = append(contacts, c)
	}
	return contacts, rows.Err()
}

// ResolveSender takes a raw sender JID string and returns the canonical phone
// JID and display name by checking the contacts table.
func (s *Store) ResolveSender(raw string) (phone, name string) {
	cleaned := raw
	cleaned = strings.TrimSuffix(cleaned, "@s.whatsapp.net")
	cleaned = strings.TrimSuffix(cleaned, "@lid")

	// Try direct JID match
	c, err := s.GetContact(raw)
	if err == nil && c != nil {
		best := c.Name
		if best == "" {
			best = c.PushName
		}
		if best == "" {
			best = c.BusinessName
		}
		return c.Phone, best
	}

	// Try by phone number
	var n string
	err = s.DB.QueryRow("SELECT phone, name FROM contacts WHERE phone = ?", cleaned).Scan(&phone, &n)
	if err == nil {
		return phone, n
	}

	// Try by LID
	err = s.DB.QueryRow("SELECT phone, name FROM contacts WHERE lid = ?", cleaned).Scan(&phone, &n)
	if err == nil {
		return phone, n
	}

	return cleaned, ""
}

// ResolveChatJID converts a LID-based chat JID to a phone-based JID.
func (s *Store) ResolveChatJID(chatJID string) string {
	if !strings.HasSuffix(chatJID, "@lid") {
		return chatJID
	}
	lid := strings.TrimSuffix(chatJID, "@lid")
	var phone string
	err := s.DB.QueryRow("SELECT phone FROM contacts WHERE lid = ?", lid).Scan(&phone)
	if err == nil && phone != "" {
		return phone + "@s.whatsapp.net"
	}
	return chatJID
}

// UpdateContactStatus updates a contact's status text.
func (s *Store) UpdateContactStatus(jid, status string, ts int64) error {
	_, err := s.DB.Exec(`INSERT INTO contacts (jid, status_text, status_set_at, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(jid) DO UPDATE SET status_text = excluded.status_text,
		status_set_at = excluded.status_set_at, updated_at = excluded.updated_at`,
		jid, status, ts, ts)
	return err
}

// UpdateContactPicture updates a contact's picture ID.
func (s *Store) UpdateContactPicture(jid, pictureID string) error {
	now := time.Now().Unix()
	_, err := s.DB.Exec(`INSERT INTO contacts (jid, picture_id, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(jid) DO UPDATE SET picture_id = excluded.picture_id, updated_at = excluded.updated_at`,
		jid, pictureID, now)
	return err
}

// SetContactNotes stores the two things only you know about a person: the kunya
// they are really addressed by, and how you came to know them.
//
// It writes ONLY those columns, so it can never disturb what sync owns. A
// contact row may not exist yet — someone can be worth a note before WhatsApp
// has told us anything about them — so it inserts when missing.
func (s *Store) SetContactNotes(jid, kunya, howWeMet string) error {
	now := time.Now().Unix()
	_, err := s.DB.Exec(`INSERT INTO contacts (jid, kunya, how_we_met, first_seen, updated_at)
		VALUES (?,?,?,?,?)
		ON CONFLICT(jid) DO UPDATE SET
			kunya = excluded.kunya,
			how_we_met = excluded.how_we_met,
			updated_at = excluded.updated_at`,
		jid, kunya, howWeMet, now, now)
	return err
}
