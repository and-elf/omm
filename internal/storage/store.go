package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/and-elf/omm/internal/models"
)

var ErrNotFound = errors.New("not found")

type Store interface {
	ListHomes(ctx context.Context) ([]models.Home, error)
	GetHome(ctx context.Context, id string) (models.Home, error)
	CreateHome(ctx context.Context, home models.Home) error
	ListNodes(ctx context.Context) ([]models.Node, error)
	GetNode(ctx context.Context, id string) (models.Node, error)
	CreateNode(ctx context.Context, node models.Node) error
	GetProfile(ctx context.Context, homeID string) (models.Profile, error)
	CreateOrUpdateProfile(ctx context.Context, profile models.Profile) error
	CreateEnrollment(ctx context.Context, enrollment models.Enrollment) error
	GetEnrollment(ctx context.Context, id string) (models.Enrollment, error)
	GetEnrollmentByNodeID(ctx context.Context, nodeID string) (models.Enrollment, error)
	UpdateEnrollment(ctx context.Context, enrollment models.Enrollment) error
	GetActiveHome(ctx context.Context) (string, error)
	SetActiveHome(ctx context.Context, homeID string) error
}

const settingActiveHome = "active_home"

type sqliteStore struct {
	db *sql.DB
}

func NewStore(db *sql.DB) Store {
	return &sqliteStore{db: db}
}

func (s *sqliteStore) ListHomes(ctx context.Context) ([]models.Home, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, controller, certificate, last_connected FROM homes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var homes []models.Home
	for rows.Next() {
		var home models.Home
		if err := rows.Scan(&home.ID, &home.Name, &home.Controller, &home.Certificate, &home.LastSeen); err != nil {
			return nil, err
		}
		homes = append(homes, home)
	}

	return homes, rows.Err()
}

func (s *sqliteStore) GetHome(ctx context.Context, id string) (models.Home, error) {
	var home models.Home
	row := s.db.QueryRowContext(ctx, `SELECT id, name, controller, certificate, last_connected FROM homes WHERE id = ?`, id)
	if err := row.Scan(&home.ID, &home.Name, &home.Controller, &home.Certificate, &home.LastSeen); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Home{}, ErrNotFound
		}
		return models.Home{}, err
	}
	return home, nil
}

func (s *sqliteStore) CreateHome(ctx context.Context, home models.Home) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO homes (id, name, controller, certificate, last_connected) VALUES (?, ?, ?, ?, ?)`,
		home.ID,
		home.Name,
		home.Controller,
		home.Certificate,
		home.LastSeen,
	)
	if err != nil {
		return fmt.Errorf("insert home: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListNodes(ctx context.Context) ([]models.Node, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, serial, current_home, trusted_homes, last_seen FROM nodes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []models.Node
	for rows.Next() {
		var rawTrusted string
		var node models.Node
		if err := rows.Scan(&node.ID, &node.Serial, &node.CurrentHome, &rawTrusted, &node.LastSeen); err != nil {
			return nil, err
		}
		node.TrustedHomes = decodeTrustedHomes(rawTrusted)
		nodes = append(nodes, node)
	}

	return nodes, rows.Err()
}

func (s *sqliteStore) GetNode(ctx context.Context, id string) (models.Node, error) {
	var node models.Node
	var rawTrusted string
	row := s.db.QueryRowContext(ctx, `SELECT id, serial, current_home, trusted_homes, last_seen FROM nodes WHERE id = ?`, id)
	if err := row.Scan(&node.ID, &node.Serial, &node.CurrentHome, &rawTrusted, &node.LastSeen); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Node{}, ErrNotFound
		}
		return models.Node{}, err
	}
	node.TrustedHomes = decodeTrustedHomes(rawTrusted)
	return node, nil
}

func (s *sqliteStore) CreateNode(ctx context.Context, node models.Node) error {
	trusted := encodeTrustedHomes(node.TrustedHomes)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO nodes (id, serial, current_home, trusted_homes, last_seen) VALUES (?, ?, ?, ?, ?)`,
		node.ID,
		node.Serial,
		node.CurrentHome,
		trusted,
		node.LastSeen,
	)
	if err != nil {
		return fmt.Errorf("insert node: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetProfile(ctx context.Context, homeID string) (models.Profile, error) {
	var profile models.Profile
	var vlans string
	row := s.db.QueryRowContext(ctx, `SELECT home_id, node_name, mesh_ssid, mesh_key, vlans FROM profiles WHERE home_id = ?`, homeID)
	if err := row.Scan(&profile.HomeID, &profile.NodeName, &profile.MeshSSID, &profile.MeshKey, &vlans); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Profile{}, ErrNotFound
		}
		return models.Profile{}, err
	}
	profile.VLANs = decodeVLANs(vlans)
	return profile, nil
}

func (s *sqliteStore) CreateOrUpdateProfile(ctx context.Context, profile models.Profile) error {
	vlans := encodeVLANs(profile.VLANs)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO profiles (home_id, node_name, mesh_ssid, mesh_key, vlans) VALUES (?, ?, ?, ?, ?) ON CONFLICT(home_id) DO UPDATE SET node_name = excluded.node_name, mesh_ssid = excluded.mesh_ssid, mesh_key = excluded.mesh_key, vlans = excluded.vlans`,
		profile.HomeID,
		profile.NodeName,
		profile.MeshSSID,
		profile.MeshKey,
		vlans,
	)
	if err != nil {
		return fmt.Errorf("insert/update profile: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetActiveHome(ctx context.Context) (string, error) {
	var value string
	row := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, settingActiveHome)
	if err := row.Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil // unset is a normal state
		}
		return "", err
	}
	return value, nil
}

func (s *sqliteStore) SetActiveHome(ctx context.Context, homeID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		settingActiveHome, homeID,
	)
	if err != nil {
		return fmt.Errorf("set active home: %w", err)
	}
	return nil
}

func (s *sqliteStore) CreateEnrollment(ctx context.Context, e models.Enrollment) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO enrollments (id, node_id, serial, public_key, challenge, status, home_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.NodeID, e.Serial, e.PublicKey, e.Challenge, string(e.Status), e.HomeID, e.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert enrollment: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetEnrollment(ctx context.Context, id string) (models.Enrollment, error) {
	return scanEnrollment(s.db.QueryRowContext(ctx,
		`SELECT id, node_id, serial, public_key, challenge, status, home_id, created_at FROM enrollments WHERE id = ?`, id))
}

func (s *sqliteStore) GetEnrollmentByNodeID(ctx context.Context, nodeID string) (models.Enrollment, error) {
	return scanEnrollment(s.db.QueryRowContext(ctx,
		`SELECT id, node_id, serial, public_key, challenge, status, home_id, created_at FROM enrollments WHERE node_id = ?`, nodeID))
}

func (s *sqliteStore) UpdateEnrollment(ctx context.Context, e models.Enrollment) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE enrollments SET serial = ?, public_key = ?, challenge = ?, status = ?, home_id = ? WHERE id = ?`,
		e.Serial, e.PublicKey, e.Challenge, string(e.Status), e.HomeID, e.ID,
	)
	if err != nil {
		return fmt.Errorf("update enrollment: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEnrollment(row rowScanner) (models.Enrollment, error) {
	var e models.Enrollment
	var status string
	if err := row.Scan(&e.ID, &e.NodeID, &e.Serial, &e.PublicKey, &e.Challenge, &status, &e.HomeID, &e.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Enrollment{}, ErrNotFound
		}
		return models.Enrollment{}, err
	}
	e.Status = models.EnrollmentStatus(status)
	return e, nil
}

func encodeVLANs(vlans []string) string {
	if len(vlans) == 0 {
		return "[]"
	}

	payload, err := json.Marshal(vlans)
	if err != nil {
		return "[]"
	}
	return string(payload)
}

func decodeVLANs(value string) []string {
	var vlans []string
	if len(value) == 0 {
		return nil
	}

	_ = json.Unmarshal([]byte(value), &vlans)
	return vlans
}

func encodeTrustedHomes(trusted []string) string {
	if len(trusted) == 0 {
		return "[]"
	}

	payload, err := json.Marshal(trusted)
	if err != nil {
		return "[]"
	}
	return string(payload)
}

func decodeTrustedHomes(value string) []string {
	var trusted []string
	if len(value) == 0 {
		return nil
	}

	_ = json.Unmarshal([]byte(value), &trusted)
	return trusted
}
