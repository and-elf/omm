package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/and-elf/omm/internal/models"
	bolt "go.etcd.io/bbolt"
)

var ErrNotFound = errors.New("not found")

type Store interface {
	ListHomes(ctx context.Context) ([]models.Home, error)
	GetHome(ctx context.Context, id string) (models.Home, error)
	CreateHome(ctx context.Context, home models.Home) error
	UpdateHome(ctx context.Context, home models.Home) error
	UpsertHome(ctx context.Context, home models.Home) error
	DeleteHome(ctx context.Context, id string) error
	ListNodes(ctx context.Context) ([]models.Node, error)
	GetNode(ctx context.Context, id string) (models.Node, error)
	CreateNode(ctx context.Context, node models.Node) error
	DeleteNode(ctx context.Context, id string) error
	GetProfile(ctx context.Context, homeID string) (models.Profile, error)
	CreateOrUpdateProfile(ctx context.Context, profile models.Profile) error
	CreateEnrollment(ctx context.Context, enrollment models.Enrollment) error
	GetEnrollment(ctx context.Context, id string) (models.Enrollment, error)
	GetEnrollmentByNodeID(ctx context.Context, nodeID string) (models.Enrollment, error)
	ListEnrollments(ctx context.Context, status models.EnrollmentStatus) ([]models.Enrollment, error)
	UpdateEnrollment(ctx context.Context, enrollment models.Enrollment) error
	GetActiveHome(ctx context.Context) (string, error)
	SetActiveHome(ctx context.Context, homeID string) error
	GetSetupComplete(ctx context.Context) (bool, error)
	SetSetupComplete(ctx context.Context, complete bool) error
	// GetBackhaulState returns the applied wireless-backhaul outcome (802.11s
	// mesh vs degraded multi-AP). An unset state reports mode "unknown".
	GetBackhaulState(ctx context.Context) (models.BackhaulState, error)
	SetBackhaulState(ctx context.Context, state models.BackhaulState) error
	// Reset clears all stored state, returning the device to its
	// just-installed condition.
	Reset(ctx context.Context) error
}

const (
	settingActiveHome    = "active_home"
	settingSetupComplete = "setup_complete"
	settingBackhaulState = "backhaul_state"
)

type boltStore struct {
	db *bolt.DB
}

func NewStore(db *DB) Store {
	return &boltStore{db: db.bolt}
}

// --- Homes ---

func (s *boltStore) ListHomes(ctx context.Context) ([]models.Home, error) {
	var homes []models.Home
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(homesBucket).ForEach(func(_, v []byte) error {
			var home models.Home
			if err := json.Unmarshal(v, &home); err != nil {
				return err
			}
			homes = append(homes, home)
			return nil
		})
	})
	return homes, err
}

func (s *boltStore) GetHome(ctx context.Context, id string) (models.Home, error) {
	var home models.Home
	err := s.db.View(func(tx *bolt.Tx) error {
		return get(tx.Bucket(homesBucket), id, &home)
	})
	return home, err
}

func (s *boltStore) CreateHome(ctx context.Context, home models.Home) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return put(tx.Bucket(homesBucket), home.ID, home)
	})
}

// UpsertHome inserts the home or, when it already exists, updates only the
// fields carried by peer discovery while preserving the stored certificate
// (discovery never carries one).
func (s *boltStore) UpsertHome(ctx context.Context, home models.Home) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(homesBucket)
		var existing models.Home
		if err := get(b, home.ID, &existing); err == nil {
			existing.Name = home.Name
			existing.Controller = home.Controller
			existing.BSSID = home.BSSID
			existing.LastSeen = home.LastSeen
			return put(b, home.ID, existing)
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		return put(b, home.ID, home)
	})
}

// UpdateHome updates only name/controller/bssid, leaving the certificate and
// last-seen untouched, and reports ErrNotFound for an unknown home.
func (s *boltStore) UpdateHome(ctx context.Context, home models.Home) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(homesBucket)
		var existing models.Home
		if err := get(b, home.ID, &existing); err != nil {
			return err
		}
		existing.Name = home.Name
		existing.Controller = home.Controller
		existing.BSSID = home.BSSID
		return put(b, home.ID, existing)
	})
}

// DeleteHome removes a Home and everything scoped to it: its profile, its
// enrollment records (and their node index entries), and any reference to it
// from nodes (current-home pointers and trusted-homes lists). Nodes themselves
// survive — only their membership of this Home is dropped. Reports ErrNotFound
// for an unknown Home.
func (s *boltStore) DeleteHome(ctx context.Context, id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		homes := tx.Bucket(homesBucket)
		if homes.Get([]byte(id)) == nil {
			return ErrNotFound
		}
		if err := homes.Delete([]byte(id)); err != nil {
			return err
		}
		if err := tx.Bucket(profilesBucket).Delete([]byte(id)); err != nil {
			return err
		}

		// Collect enrollments scoped to this Home first; mutating a bucket
		// while iterating it with ForEach is unsafe in bbolt.
		enr := tx.Bucket(enrollmentsBucket)
		var delEnrollments, delNodeIndex []string
		if err := enr.ForEach(func(k, v []byte) error {
			var e models.Enrollment
			if err := json.Unmarshal(v, &e); err != nil {
				return err
			}
			if e.HomeID == id {
				delEnrollments = append(delEnrollments, string(k))
				if e.NodeID != "" {
					delNodeIndex = append(delNodeIndex, e.NodeID)
				}
			}
			return nil
		}); err != nil {
			return err
		}
		for _, k := range delEnrollments {
			if err := enr.Delete([]byte(k)); err != nil {
				return err
			}
		}
		idx := tx.Bucket(enrollmentByNodeBucket)
		for _, k := range delNodeIndex {
			if err := idx.Delete([]byte(k)); err != nil {
				return err
			}
		}

		// Strip the Home from every node's membership (collect then write).
		nodes := tx.Bucket(nodesBucket)
		var updated []models.Node
		if err := nodes.ForEach(func(_, v []byte) error {
			var n models.Node
			if err := json.Unmarshal(v, &n); err != nil {
				return err
			}
			changed := false
			if n.CurrentHome == id {
				n.CurrentHome = ""
				changed = true
			}
			kept := make([]string, 0, len(n.TrustedHomes))
			for _, h := range n.TrustedHomes {
				if h == id {
					changed = true
					continue
				}
				kept = append(kept, h)
			}
			if changed {
				n.TrustedHomes = kept
				updated = append(updated, n)
			}
			return nil
		}); err != nil {
			return err
		}
		for _, n := range updated {
			if err := put(nodes, n.ID, n); err != nil {
				return err
			}
		}
		return nil
	})
}

// --- Nodes ---

func (s *boltStore) ListNodes(ctx context.Context) ([]models.Node, error) {
	var nodes []models.Node
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(nodesBucket).ForEach(func(_, v []byte) error {
			var node models.Node
			if err := json.Unmarshal(v, &node); err != nil {
				return err
			}
			nodes = append(nodes, node)
			return nil
		})
	})
	return nodes, err
}

func (s *boltStore) GetNode(ctx context.Context, id string) (models.Node, error) {
	var node models.Node
	err := s.db.View(func(tx *bolt.Tx) error {
		return get(tx.Bucket(nodesBucket), id, &node)
	})
	return node, err
}

func (s *boltStore) CreateNode(ctx context.Context, node models.Node) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return put(tx.Bucket(nodesBucket), node.ID, node)
	})
}

// DeleteNode removes a node and its enrollment record (resolved through the
// node index). Reports ErrNotFound for an unknown node.
func (s *boltStore) DeleteNode(ctx context.Context, id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		nodes := tx.Bucket(nodesBucket)
		if nodes.Get([]byte(id)) == nil {
			return ErrNotFound
		}
		if err := nodes.Delete([]byte(id)); err != nil {
			return err
		}
		idx := tx.Bucket(enrollmentByNodeBucket)
		if eid := idx.Get([]byte(id)); eid != nil {
			if err := tx.Bucket(enrollmentsBucket).Delete(eid); err != nil {
				return err
			}
			if err := idx.Delete([]byte(id)); err != nil {
				return err
			}
		}
		return nil
	})
}

// --- Profiles ---

func (s *boltStore) GetProfile(ctx context.Context, homeID string) (models.Profile, error) {
	var profile models.Profile
	err := s.db.View(func(tx *bolt.Tx) error {
		return get(tx.Bucket(profilesBucket), homeID, &profile)
	})
	return profile, err
}

func (s *boltStore) CreateOrUpdateProfile(ctx context.Context, profile models.Profile) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return put(tx.Bucket(profilesBucket), profile.HomeID, profile)
	})
}

// --- Settings ---

func (s *boltStore) GetActiveHome(ctx context.Context) (string, error) {
	return s.getSetting(settingActiveHome)
}

func (s *boltStore) SetActiveHome(ctx context.Context, homeID string) error {
	return s.setSetting(settingActiveHome, homeID)
}

func (s *boltStore) GetSetupComplete(ctx context.Context) (bool, error) {
	value, err := s.getSetting(settingSetupComplete)
	if err != nil {
		return false, err
	}
	return value == "1", nil
}

func (s *boltStore) SetSetupComplete(ctx context.Context, complete bool) error {
	value := "0"
	if complete {
		value = "1"
	}
	return s.setSetting(settingSetupComplete, value)
}

// GetBackhaulState returns the applied backhaul outcome, defaulting to mode
// "unknown" when no profile has been applied yet (unset key).
func (s *boltStore) GetBackhaulState(ctx context.Context) (models.BackhaulState, error) {
	raw, err := s.getSetting(settingBackhaulState)
	if err != nil {
		return models.BackhaulState{}, err
	}
	if raw == "" {
		return models.BackhaulState{Mode: models.BackhaulModeUnknown}, nil
	}
	var state models.BackhaulState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return models.BackhaulState{}, fmt.Errorf("decode backhaul state: %w", err)
	}
	return state, nil
}

func (s *boltStore) SetBackhaulState(ctx context.Context, state models.BackhaulState) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode backhaul state: %w", err)
	}
	return s.setSetting(settingBackhaulState, string(payload))
}

// getSetting returns the empty string for an unset key; unset is a normal state.
func (s *boltStore) getSetting(key string) (string, error) {
	var value string
	err := s.db.View(func(tx *bolt.Tx) error {
		if v := tx.Bucket(settingsBucket).Get([]byte(key)); v != nil {
			value = string(v)
		}
		return nil
	})
	return value, err
}

func (s *boltStore) setSetting(key, value string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(settingsBucket).Put([]byte(key), []byte(value))
	})
}

// Reset clears every bucket, returning the store to its just-installed state
// (no Homes, nodes, profiles, enrollments, active Home or setup flag). Used to
// factory-reset a device and to reset state between e2e runs that reuse a
// container. The buckets are recreated so the store stays usable afterwards.
func (s *boltStore) Reset(ctx context.Context) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, name := range buckets {
			if err := tx.DeleteBucket(name); err != nil && err != bolt.ErrBucketNotFound {
				return err
			}
			if _, err := tx.CreateBucket(name); err != nil {
				return err
			}
		}
		return nil
	})
}

// --- Enrollments ---

func (s *boltStore) CreateEnrollment(ctx context.Context, e models.Enrollment) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := put(tx.Bucket(enrollmentsBucket), e.ID, e); err != nil {
			return err
		}
		if e.NodeID != "" {
			return tx.Bucket(enrollmentByNodeBucket).Put([]byte(e.NodeID), []byte(e.ID))
		}
		return nil
	})
}

func (s *boltStore) GetEnrollment(ctx context.Context, id string) (models.Enrollment, error) {
	var e models.Enrollment
	err := s.db.View(func(tx *bolt.Tx) error {
		return get(tx.Bucket(enrollmentsBucket), id, &e)
	})
	return e, err
}

func (s *boltStore) GetEnrollmentByNodeID(ctx context.Context, nodeID string) (models.Enrollment, error) {
	var e models.Enrollment
	err := s.db.View(func(tx *bolt.Tx) error {
		id := tx.Bucket(enrollmentByNodeBucket).Get([]byte(nodeID))
		if id == nil {
			return ErrNotFound
		}
		return get(tx.Bucket(enrollmentsBucket), string(id), &e)
	})
	return e, err
}

// ListEnrollments returns enrollments ordered by created_at (ascending),
// optionally filtered by status.
func (s *boltStore) ListEnrollments(ctx context.Context, status models.EnrollmentStatus) ([]models.Enrollment, error) {
	var enrollments []models.Enrollment
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(enrollmentsBucket).ForEach(func(_, v []byte) error {
			var e models.Enrollment
			if err := json.Unmarshal(v, &e); err != nil {
				return err
			}
			if status == "" || e.Status == status {
				enrollments = append(enrollments, e)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(enrollments, func(i, j int) bool {
		return enrollments[i].CreatedAt < enrollments[j].CreatedAt
	})
	return enrollments, nil
}

func (s *boltStore) UpdateEnrollment(ctx context.Context, e models.Enrollment) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(enrollmentsBucket)
		var existing models.Enrollment
		if err := get(b, e.ID, &existing); err != nil {
			return err
		}
		if err := put(b, e.ID, e); err != nil {
			return err
		}
		// Keep the node-id index consistent if the mapped node changed.
		idx := tx.Bucket(enrollmentByNodeBucket)
		if existing.NodeID != "" && existing.NodeID != e.NodeID {
			if err := idx.Delete([]byte(existing.NodeID)); err != nil {
				return err
			}
		}
		if e.NodeID != "" {
			return idx.Put([]byte(e.NodeID), []byte(e.ID))
		}
		return nil
	})
}

// --- helpers ---

// get unmarshals the JSON record at key into dest, returning ErrNotFound when
// the key is absent.
func get(b *bolt.Bucket, key string, dest any) error {
	v := b.Get([]byte(key))
	if v == nil {
		return ErrNotFound
	}
	if err := json.Unmarshal(v, dest); err != nil {
		return fmt.Errorf("decode record %q: %w", key, err)
	}
	return nil
}

// put marshals value as JSON and stores it at key.
func put(b *bolt.Bucket, key string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode record %q: %w", key, err)
	}
	return b.Put([]byte(key), payload)
}
