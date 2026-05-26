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

// ActiveComposers returns every DM contact whose last presence beacon was
// 'composing' within freshSec. Used by the chat-list "typing…" preview so a
// single request covers every visible DM row at once.
func (s *Store) ActiveComposers(freshSec int64) ([]string, error) {
	cutoff := time.Now().Unix() - freshSec
	rows, err := s.DB.Query(`SELECT jid FROM presence_cache WHERE status = 'composing' AND updated_at >= ?`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var j string
		if err := rows.Scan(&j); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
