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
	} {
		sqlDB.Exec(stmt)
	}

	return &Store{DB: sqlDB}, nil
}

// Close shuts down the database connection.
func (s *Store) Close() error {
	return s.DB.Close()
}
