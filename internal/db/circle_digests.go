package db

import (
	"database/sql"
	"time"
)

// CircleDigest is the cached, incrementally-refreshed digest for one circle.
// One row per circle (upserted on regenerate) — see circle_digests in schema.go.
type CircleDigest struct {
	CircleID    int64  `json:"circle_id"`
	Summary     string `json:"summary"`
	Data        string `json:"data"`
	LastMsgTS   int64  `json:"last_msg_ts"`
	GeneratedAt int64  `json:"generated_at"`
}

// GetCircleDigest returns the cached digest row for a circle, or nil (not an
// error) if none has been generated yet.
func (s *Store) GetCircleDigest(circleID int64) (*CircleDigest, error) {
	d := &CircleDigest{}
	err := s.DB.QueryRow(`SELECT circle_id, summary, data, last_msg_ts, generated_at
		FROM circle_digests WHERE circle_id = ?`, circleID).
		Scan(&d.CircleID, &d.Summary, &d.Data, &d.LastMsgTS, &d.GeneratedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

// SaveCircleDigest upserts the single cached digest row for a circle.
func (s *Store) SaveCircleDigest(circleID int64, summary, data string, lastMsgTS int64) error {
	_, err := s.DB.Exec(`INSERT INTO circle_digests (circle_id, summary, data, last_msg_ts, generated_at)
		VALUES (?,?,?,?,?)
		ON CONFLICT(circle_id) DO UPDATE SET
			summary=excluded.summary, data=excluded.data,
			last_msg_ts=excluded.last_msg_ts, generated_at=excluded.generated_at`,
		circleID, summary, data, lastMsgTS, time.Now().Unix())
	return err
}
