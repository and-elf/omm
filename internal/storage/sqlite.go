package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func OpenDB(path string) (*sql.DB, error) {
	dsn, inMemory := buildDSN(path)

	if !inMemory {
		if err := os.MkdirAll(dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	if inMemory {
		// A shared-cache in-memory database exists only while a connection is
		// open. Pin a single connection so the schema and data persist for the
		// lifetime of the *sql.DB and are visible across all callers.
		db.SetMaxOpenConns(1)
	}

	if err := initializeSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

// buildDSN constructs a modernc.org/sqlite DSN. File-backed databases use WAL
// journaling plus a busy timeout so concurrent writers (e.g. many nodes
// enrolling at once) wait for the lock instead of failing with SQLITE_BUSY.
func buildDSN(path string) (dsn string, inMemory bool) {
	if path == "" || path == ":memory:" {
		// Pinned to a single connection by the caller, so a plain in-memory
		// database stays private to this *sql.DB and consistent across calls.
		return ":memory:", true
	}
	return "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", false
}

func dir(path string) string {
	if path == "" {
		return "."
	}

	directory := filepath.Dir(path)
	if directory == "." || directory == string(filepath.Separator) {
		return directory
	}

	return directory
}

func initializeSchema(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS homes (
			id TEXT PRIMARY KEY,
			name TEXT,
			controller TEXT,
			certificate BLOB,
			last_connected INTEGER
		);`,
		`CREATE TABLE IF NOT EXISTS nodes (
			id TEXT PRIMARY KEY,
			serial TEXT,
			current_home TEXT,
			trusted_homes TEXT,
			last_seen INTEGER
		);`,
		`CREATE TABLE IF NOT EXISTS profiles (
			home_id TEXT PRIMARY KEY,
			node_name TEXT,
			mesh_ssid TEXT,
			mesh_key TEXT,
			vlans TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS enrollments (
			id TEXT PRIMARY KEY,
			node_id TEXT UNIQUE,
			serial TEXT,
			public_key BLOB,
			challenge BLOB,
			status TEXT,
			home_id TEXT,
			created_at INTEGER
		);`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return err
		}
	}

	return nil
}
