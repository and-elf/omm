package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func OpenDB(path string) (*sql.DB, error) {
	dir := dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	if err := initializeSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
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
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return err
		}
	}

	return nil
}
