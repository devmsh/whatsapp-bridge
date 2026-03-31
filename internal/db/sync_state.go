package db

import (
	"database/sql"
	"time"
)

// GetSyncState retrieves a sync state value by key.
func (s *Store) GetSyncState(key string) (string, int64, error) {
	var value string
	var updatedAt int64
	err := s.DB.QueryRow(`SELECT value, updated_at FROM sync_state WHERE key = ?`, key).Scan(&value, &updatedAt)
	if err == sql.ErrNoRows {
		return "", 0, nil
	}
	return value, updatedAt, err
}

// PutSyncState upserts a sync state value.
func (s *Store) PutSyncState(key, value string) error {
	_, err := s.DB.Exec(`INSERT OR REPLACE INTO sync_state (key, value, updated_at)
		VALUES (?,?,?)`, key, value, time.Now().Unix())
	return err
}
