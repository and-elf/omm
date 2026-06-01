package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

// Bucket names. Every record is stored as JSON keyed by its primary identifier;
// enrollmentByNodeBucket is a secondary index mapping node_id -> enrollment id
// so GetEnrollmentByNodeID is a direct lookup instead of a full scan.
var (
	homesBucket            = []byte("homes")
	nodesBucket            = []byte("nodes")
	profilesBucket         = []byte("profiles")
	settingsBucket         = []byte("settings")
	enrollmentsBucket      = []byte("enrollments")
	enrollmentByNodeBucket = []byte("enrollments_by_node")
)

var buckets = [][]byte{
	homesBucket,
	nodesBucket,
	profilesBucket,
	settingsBucket,
	enrollmentsBucket,
	enrollmentByNodeBucket,
}

// DB owns a bbolt database handle. It is returned by OpenDB and consumed by
// NewStore; callers only ever Close it. For an in-memory database (":memory:")
// it is backed by a temporary file that Close removes.
type DB struct {
	bolt    *bolt.DB
	tmpPath string
}

// Close releases the bbolt handle and removes the backing temp file when the
// database was opened in-memory.
func (d *DB) Close() error {
	err := d.bolt.Close()
	if d.tmpPath != "" {
		if rmErr := os.Remove(d.tmpPath); rmErr != nil && err == nil {
			err = rmErr
		}
	}
	return err
}

// OpenDB opens (creating if needed) a bbolt database at path and ensures every
// bucket exists. An empty path or ":memory:" yields an ephemeral database
// backed by a temp file that Close removes, matching the previous in-memory
// SQLite behaviour used by tests. bbolt serializes writes internally, so
// concurrent writers wait for the single write lock instead of failing.
func OpenDB(path string) (*DB, error) {
	var tmpPath string
	if path == "" || path == ":memory:" {
		f, err := os.CreateTemp("", "meshd-mem-*.bolt")
		if err != nil {
			return nil, fmt.Errorf("create temp database: %w", err)
		}
		tmpPath = f.Name()
		f.Close()
		path = tmpPath
	} else if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	// Timeout makes a contended open fail fast rather than block forever, in
	// the spirit of the old SQLite busy_timeout.
	b, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		if tmpPath != "" {
			os.Remove(tmpPath)
		}
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := b.Update(func(tx *bolt.Tx) error {
		for _, name := range buckets {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		b.Close()
		if tmpPath != "" {
			os.Remove(tmpPath)
		}
		return nil, fmt.Errorf("initialize buckets: %w", err)
	}

	return &DB{bolt: b, tmpPath: tmpPath}, nil
}
