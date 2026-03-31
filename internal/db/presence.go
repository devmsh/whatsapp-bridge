package db

import "time"

// PresenceEntry maps to the presence_cache table.
type PresenceEntry struct {
	JID       string `json:"jid"`
	Status    string `json:"status"`
	LastSeen  int64  `json:"last_seen,omitempty"`
	UpdatedAt int64  `json:"updated_at"`
}

// StorePresence upserts a presence cache entry.
func (s *Store) StorePresence(p *PresenceEntry) error {
	if p.UpdatedAt == 0 {
		p.UpdatedAt = time.Now().Unix()
	}
	_, err := s.DB.Exec(`INSERT OR REPLACE INTO presence_cache (jid, status, last_seen, updated_at)
		VALUES (?,?,?,?)`, p.JID, p.Status, p.LastSeen, p.UpdatedAt)
	return err
}

// GetPresence returns a cached presence entry.
func (s *Store) GetPresence(jid string) (*PresenceEntry, error) {
	row := s.DB.QueryRow(`SELECT jid, status, last_seen, updated_at FROM presence_cache WHERE jid = ?`, jid)
	p := &PresenceEntry{}
	err := row.Scan(&p.JID, &p.Status, &p.LastSeen, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}
