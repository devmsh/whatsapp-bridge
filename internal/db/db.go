package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// Store wraps the SQLite database connection.
type Store struct {
	DB *sql.DB
}

// NewStore opens (or creates) the SQLite database at path,
// enables WAL mode and foreign keys, and runs the full schema.
func NewStore(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000", path)
	sqlDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if _, err := sqlDB.Exec(Schema); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	// Idempotent column migrations for tables that predate a column.
	// Errors (e.g. "duplicate column name") are expected and ignored.
	for _, stmt := range []string{
		`ALTER TABLE circles ADD COLUMN keywords TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tasks ADD COLUMN review_status TEXT NOT NULL DEFAULT 'accepted'`,
		`ALTER TABLE tasks ADD COLUMN parent_id INTEGER DEFAULT NULL REFERENCES tasks(id) ON DELETE SET NULL`,
		// "refined" tracks whether a transcript row went through the LLM
		// refinement pass. 0 = raw whisper output, 1 = refined (or N/A for
		// image descriptions which don't need refinement).
		`ALTER TABLE media_understanding ADD COLUMN refined INTEGER NOT NULL DEFAULT 0`,
		// Indexes after the columns they depend on exist (so re-runs are safe).
		`CREATE INDEX IF NOT EXISTS idx_tasks_review ON tasks(review_status)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_parent ON tasks(parent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_mu_refined ON media_understanding(kind, refined, status)`,
	} {
		sqlDB.Exec(stmt)
	}

	return &Store{DB: sqlDB}, nil
}

// Close shuts down the database connection.
func (s *Store) Close() error {
	return s.DB.Close()
}
