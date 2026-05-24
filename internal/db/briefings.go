package db

import (
	"database/sql"
	"time"
)

// Briefing is a stored daily digest. `Data` is the full JSON the UI renders.
type Briefing struct {
	ID          int64  `json:"id"`
	ForDate     string `json:"for_date"`
	Data        string `json:"data"`
	GeneratedAt int64  `json:"generated_at"`
}

// SaveBriefing inserts a fresh briefing for the given date.
func (s *Store) SaveBriefing(forDate, data string) (*Briefing, error) {
	now := time.Now().Unix()
	res, err := s.DB.Exec(`INSERT INTO briefings (for_date, data, generated_at) VALUES (?,?,?)`,
		forDate, data, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Briefing{ID: id, ForDate: forDate, Data: data, GeneratedAt: now}, nil
}

// LatestBriefing returns the most recent briefing for a given date, or nil.
func (s *Store) LatestBriefing(forDate string) (*Briefing, error) {
	b := &Briefing{}
	err := s.DB.QueryRow(`SELECT id, for_date, data, generated_at FROM briefings
		WHERE for_date = ? ORDER BY generated_at DESC LIMIT 1`, forDate).
		Scan(&b.ID, &b.ForDate, &b.Data, &b.GeneratedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return b, err
}

// ListBriefings returns the N most recent briefings (any date).
func (s *Store) ListBriefings(limit int) ([]Briefing, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.DB.Query(`SELECT id, for_date, data, generated_at FROM briefings
		ORDER BY generated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Briefing{}
	for rows.Next() {
		var b Briefing
		if err := rows.Scan(&b.ID, &b.ForDate, &b.Data, &b.GeneratedAt); err != nil {
			return out, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
