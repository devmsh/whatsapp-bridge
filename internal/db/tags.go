package db

import (
	"database/sql"
	"strings"
	"time"
)

// Tag is a user-defined label (company, position, etc.) for contacts.
type Tag struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	CreatedAt int64  `json:"created_at"`
}

// ListTags returns all defined tags, by name.
func (s *Store) ListTags() ([]Tag, error) {
	rows, err := s.DB.Query(`SELECT id, name, color, created_at FROM tags ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tag{}
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.CreatedAt); err != nil {
			return out, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetOrCreateTag returns the tag with the given name, creating it if needed.
func (s *Store) GetOrCreateTag(name, color string) (*Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, sql.ErrNoRows
	}
	var t Tag
	err := s.DB.QueryRow(`SELECT id, name, color, created_at FROM tags WHERE name = ? COLLATE NOCASE`, name).
		Scan(&t.ID, &t.Name, &t.Color, &t.CreatedAt)
	if err == nil {
		return &t, nil
	}
	now := time.Now().Unix()
	res, err := s.DB.Exec(`INSERT INTO tags (name, color, created_at) VALUES (?,?,?)`, name, color, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Tag{ID: id, Name: name, Color: color, CreatedAt: now}, nil
}

// DeleteTag removes a tag and all its assignments (via cascade).
func (s *Store) DeleteTag(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM tags WHERE id = ?`, id)
	return err
}

// AssignTag links a tag to a contact.
func (s *Store) AssignTag(contactJID string, tagID int64) error {
	_, err := s.DB.Exec(`INSERT OR IGNORE INTO contact_tags (contact_jid, tag_id, added_at) VALUES (?,?,?)`,
		contactJID, tagID, time.Now().Unix())
	return err
}

// UnassignTag removes a tag from a contact.
func (s *Store) UnassignTag(contactJID string, tagID int64) error {
	_, err := s.DB.Exec(`DELETE FROM contact_tags WHERE contact_jid = ? AND tag_id = ?`, contactJID, tagID)
	return err
}

// TagsForContact returns the tags on one contact.
func (s *Store) TagsForContact(jid string) ([]Tag, error) {
	rows, err := s.DB.Query(`SELECT t.id, t.name, t.color, t.created_at
		FROM tags t JOIN contact_tags ct ON ct.tag_id = t.id
		WHERE ct.contact_jid = ? ORDER BY t.name COLLATE NOCASE`, jid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tag{}
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.CreatedAt); err != nil {
			return out, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// AllContactTags returns a map of contact JID -> its tags, for bulk display.
func (s *Store) AllContactTags() (map[string][]Tag, error) {
	rows, err := s.DB.Query(`SELECT ct.contact_jid, t.id, t.name, t.color, t.created_at
		FROM contact_tags ct JOIN tags t ON t.id = ct.tag_id
		ORDER BY t.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]Tag{}
	for rows.Next() {
		var jid string
		var t Tag
		if err := rows.Scan(&jid, &t.ID, &t.Name, &t.Color, &t.CreatedAt); err != nil {
			return out, err
		}
		out[jid] = append(out[jid], t)
	}
	return out, rows.Err()
}
