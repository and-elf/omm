package storage

import (
	"context"
	"testing"

	"github.com/and-elf/omm/internal/models"
)

// newStore opens a fresh in-memory store for a test.
func newStore(t *testing.T) (Store, context.Context) {
	t.Helper()
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewStore(db), context.Background()
}

// UpsertHome must update the mutable fields but preserve a previously stored
// certificate, since the upsert path (peer discovery) never carries one.
func TestUpsertHomePreservesCertificate(t *testing.T) {
	store, ctx := newStore(t)

	cert := []byte("pinned-cert")
	if err := store.CreateHome(ctx, models.Home{ID: "h1", Name: "Home", Certificate: cert, LastSeen: 1}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Upsert from discovery: no certificate carried, new metadata.
	if err := store.UpsertHome(ctx, models.Home{ID: "h1", Name: "Renamed", Controller: "ctrl", BSSID: "aa:bb", LastSeen: 2}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	home, err := store.GetHome(ctx, "h1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(home.Certificate) != string(cert) {
		t.Fatalf("certificate not preserved: got %q", home.Certificate)
	}
	if home.Name != "Renamed" || home.Controller != "ctrl" || home.BSSID != "aa:bb" || home.LastSeen != 2 {
		t.Fatalf("upsert did not update metadata: %+v", home)
	}
}

// UpdateHome touches only name/controller/bssid and reports a missing row.
func TestUpdateHome(t *testing.T) {
	store, ctx := newStore(t)

	if err := store.UpdateHome(ctx, models.Home{ID: "missing", Name: "x"}); err != ErrNotFound {
		t.Fatalf("update missing: want ErrNotFound, got %v", err)
	}

	cert := []byte("cert")
	_ = store.CreateHome(ctx, models.Home{ID: "h1", Name: "Old", Certificate: cert, LastSeen: 9})
	if err := store.UpdateHome(ctx, models.Home{ID: "h1", Name: "New", Controller: "c", BSSID: "b"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	home, _ := store.GetHome(ctx, "h1")
	if home.Name != "New" || home.Controller != "c" || home.BSSID != "b" {
		t.Fatalf("fields not updated: %+v", home)
	}
	if string(home.Certificate) != string(cert) || home.LastSeen != 9 {
		t.Fatalf("update clobbered preserved fields: %+v", home)
	}
}

func TestGetHomeNotFound(t *testing.T) {
	store, ctx := newStore(t)
	if _, err := store.GetHome(ctx, "nope"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// Node round-trip must preserve the trusted-homes list and last-seen.
func TestNodeRoundTrip(t *testing.T) {
	store, ctx := newStore(t)

	node := models.Node{ID: "n1", Serial: "SN1", CurrentHome: "h1", TrustedHomes: []string{"h1", "h2"}, LastSeen: 5}
	if err := store.CreateNode(ctx, node); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := store.GetNode(ctx, "n1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Serial != "SN1" || got.CurrentHome != "h1" || got.LastSeen != 5 {
		t.Fatalf("node fields lost: %+v", got)
	}
	if len(got.TrustedHomes) != 2 || got.TrustedHomes[0] != "h1" || got.TrustedHomes[1] != "h2" {
		t.Fatalf("trusted homes lost: %+v", got.TrustedHomes)
	}
	if _, err := store.GetNode(ctx, "missing"); err != ErrNotFound {
		t.Fatalf("missing node: want ErrNotFound, got %v", err)
	}
}

func TestProfileRoundTrip(t *testing.T) {
	store, ctx := newStore(t)

	if _, err := store.GetProfile(ctx, "h1"); err != ErrNotFound {
		t.Fatalf("missing profile: want ErrNotFound, got %v", err)
	}
	p := models.Profile{HomeID: "h1", NodeName: "node", MeshSSID: "mesh", MeshKey: "key", VLANs: []string{"10", "20"}}
	if err := store.CreateOrUpdateProfile(ctx, p); err != nil {
		t.Fatalf("create profile: %v", err)
	}
	// Overwrite replaces all fields.
	if err := store.CreateOrUpdateProfile(ctx, models.Profile{HomeID: "h1", NodeName: "node2", MeshSSID: "m2", MeshKey: "k2", VLANs: []string{"30"}}); err != nil {
		t.Fatalf("update profile: %v", err)
	}
	got, err := store.GetProfile(ctx, "h1")
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	if got.NodeName != "node2" || got.MeshSSID != "m2" || got.MeshKey != "k2" || len(got.VLANs) != 1 || got.VLANs[0] != "30" {
		t.Fatalf("profile not replaced: %+v", got)
	}
}

// Settings: active home and setup-complete default to empty/false when unset.
func TestSettingsDefaultsAndRoundTrip(t *testing.T) {
	store, ctx := newStore(t)

	if v, err := store.GetActiveHome(ctx); err != nil || v != "" {
		t.Fatalf("active home default: got %q err %v", v, err)
	}
	if v, err := store.GetSetupComplete(ctx); err != nil || v {
		t.Fatalf("setup complete default: got %v err %v", v, err)
	}

	if err := store.SetActiveHome(ctx, "h1"); err != nil {
		t.Fatalf("set active: %v", err)
	}
	if err := store.SetSetupComplete(ctx, true); err != nil {
		t.Fatalf("set complete: %v", err)
	}
	if v, _ := store.GetActiveHome(ctx); v != "h1" {
		t.Fatalf("active home: got %q", v)
	}
	if v, _ := store.GetSetupComplete(ctx); !v {
		t.Fatalf("setup complete: got %v", v)
	}
	// Overwrite is idempotent / last-writer-wins.
	_ = store.SetActiveHome(ctx, "h2")
	if v, _ := store.GetActiveHome(ctx); v != "h2" {
		t.Fatalf("active home overwrite: got %q", v)
	}
}

// Enrollments must be retrievable by id and by node id, listable with an
// optional status filter, and ordered by created_at.
func TestEnrollmentByNodeAndListOrder(t *testing.T) {
	store, ctx := newStore(t)

	if _, err := store.GetEnrollment(ctx, "x"); err != ErrNotFound {
		t.Fatalf("missing enrollment: want ErrNotFound, got %v", err)
	}
	if _, err := store.GetEnrollmentByNodeID(ctx, "x"); err != ErrNotFound {
		t.Fatalf("missing by node: want ErrNotFound, got %v", err)
	}

	// Insert out of created_at order to prove ListEnrollments sorts.
	e1 := models.Enrollment{ID: "e1", NodeID: "n1", Serial: "S1", PublicKey: []byte("pk1"), Challenge: []byte("c1"), Status: models.EnrollmentPendingApproval, HomeID: "h1", CreatedAt: 30}
	e2 := models.Enrollment{ID: "e2", NodeID: "n2", Serial: "S2", PublicKey: []byte("pk2"), Challenge: []byte("c2"), Status: models.EnrollmentApproved, HomeID: "h1", CreatedAt: 10}
	e3 := models.Enrollment{ID: "e3", NodeID: "n3", Serial: "S3", Status: models.EnrollmentPendingApproval, HomeID: "h1", CreatedAt: 20}
	for _, e := range []models.Enrollment{e1, e2, e3} {
		if err := store.CreateEnrollment(ctx, e); err != nil {
			t.Fatalf("create %s: %v", e.ID, err)
		}
	}

	byNode, err := store.GetEnrollmentByNodeID(ctx, "n2")
	if err != nil {
		t.Fatalf("by node: %v", err)
	}
	if byNode.ID != "e2" || string(byNode.PublicKey) != "pk2" || byNode.Status != models.EnrollmentApproved {
		t.Fatalf("by node mismatch: %+v", byNode)
	}

	all, err := store.ListEnrollments(ctx, "")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 || all[0].ID != "e2" || all[1].ID != "e3" || all[2].ID != "e1" {
		t.Fatalf("not ordered by created_at: %v", ids(all))
	}

	pending, err := store.ListEnrollments(ctx, models.EnrollmentPendingApproval)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 2 || pending[0].ID != "e3" || pending[1].ID != "e1" {
		t.Fatalf("status filter wrong: %v", ids(pending))
	}
}

// UpdateEnrollment must persist new fields, keep the by-node index correct, and
// report a missing record.
func TestUpdateEnrollment(t *testing.T) {
	store, ctx := newStore(t)

	if err := store.UpdateEnrollment(ctx, models.Enrollment{ID: "missing"}); err != ErrNotFound {
		t.Fatalf("update missing: want ErrNotFound, got %v", err)
	}

	_ = store.CreateEnrollment(ctx, models.Enrollment{ID: "e1", NodeID: "n1", Status: models.EnrollmentPendingVerification, HomeID: "h1", CreatedAt: 1})
	if err := store.UpdateEnrollment(ctx, models.Enrollment{ID: "e1", NodeID: "n1", Serial: "S1", PublicKey: []byte("pk"), Status: models.EnrollmentActive, HomeID: "h1", CreatedAt: 1}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := store.GetEnrollmentByNodeID(ctx, "n1")
	if err != nil {
		t.Fatalf("by node after update: %v", err)
	}
	if got.Status != models.EnrollmentActive || got.Serial != "S1" || string(got.PublicKey) != "pk" {
		t.Fatalf("update not persisted: %+v", got)
	}
}

// DeleteHome removes the Home and everything scoped to it (profile,
// enrollments, node memberships) while leaving other Homes intact.
func TestDeleteHomeCascades(t *testing.T) {
	store, ctx := newStore(t)

	_ = store.CreateHome(ctx, models.Home{ID: "h1", Name: "Home"})
	_ = store.CreateHome(ctx, models.Home{ID: "h2", Name: "Cottage"})
	_ = store.CreateOrUpdateProfile(ctx, models.Profile{HomeID: "h1", NodeName: "garage"})
	_ = store.CreateNode(ctx, models.Node{ID: "n1", Serial: "s1", CurrentHome: "h1", TrustedHomes: []string{"h1", "h2"}})
	_ = store.CreateEnrollment(ctx, models.Enrollment{ID: "e1", NodeID: "n1", HomeID: "h1", Status: models.EnrollmentActive})

	if err := store.DeleteHome(ctx, "missing"); err != ErrNotFound {
		t.Fatalf("delete missing: want ErrNotFound, got %v", err)
	}
	if err := store.DeleteHome(ctx, "h1"); err != nil {
		t.Fatalf("delete h1: %v", err)
	}

	if _, err := store.GetHome(ctx, "h1"); err != ErrNotFound {
		t.Fatalf("home not deleted: %v", err)
	}
	if _, err := store.GetProfile(ctx, "h1"); err != ErrNotFound {
		t.Fatalf("profile not deleted: %v", err)
	}
	if _, err := store.GetEnrollment(ctx, "e1"); err != ErrNotFound {
		t.Fatalf("enrollment not deleted: %v", err)
	}
	if _, err := store.GetEnrollmentByNodeID(ctx, "n1"); err != ErrNotFound {
		t.Fatalf("enrollment node-index not cleared: %v", err)
	}
	node, err := store.GetNode(ctx, "n1")
	if err != nil {
		t.Fatalf("node should survive: %v", err)
	}
	if node.CurrentHome != "" {
		t.Fatalf("current home not cleared: %q", node.CurrentHome)
	}
	if len(node.TrustedHomes) != 1 || node.TrustedHomes[0] != "h2" {
		t.Fatalf("trusted homes not pruned: %v", node.TrustedHomes)
	}
	if _, err := store.GetHome(ctx, "h2"); err != nil {
		t.Fatalf("unrelated home h2 was affected: %v", err)
	}
}

// DeleteNode removes the node and its enrollment (direct and via the node
// index).
func TestDeleteNodeRemovesEnrollment(t *testing.T) {
	store, ctx := newStore(t)

	_ = store.CreateNode(ctx, models.Node{ID: "n1", Serial: "s1"})
	_ = store.CreateEnrollment(ctx, models.Enrollment{ID: "e1", NodeID: "n1", HomeID: "h1", Status: models.EnrollmentActive})

	if err := store.DeleteNode(ctx, "missing"); err != ErrNotFound {
		t.Fatalf("delete missing: want ErrNotFound, got %v", err)
	}
	if err := store.DeleteNode(ctx, "n1"); err != nil {
		t.Fatalf("delete n1: %v", err)
	}
	if _, err := store.GetNode(ctx, "n1"); err != ErrNotFound {
		t.Fatalf("node not deleted: %v", err)
	}
	if _, err := store.GetEnrollment(ctx, "e1"); err != ErrNotFound {
		t.Fatalf("enrollment not deleted: %v", err)
	}
	if _, err := store.GetEnrollmentByNodeID(ctx, "n1"); err != ErrNotFound {
		t.Fatalf("enrollment node-index not cleared: %v", err)
	}
}

// Reset returns the store to a just-installed state across every bucket.
func TestReset(t *testing.T) {
	store, ctx := newStore(t)

	_ = store.CreateHome(ctx, models.Home{ID: "h1", Name: "Home"})
	_ = store.CreateNode(ctx, models.Node{ID: "n1", Serial: "s1"})
	_ = store.CreateOrUpdateProfile(ctx, models.Profile{HomeID: "h1"})
	_ = store.CreateEnrollment(ctx, models.Enrollment{ID: "e1", NodeID: "n1", HomeID: "h1"})
	_ = store.SetActiveHome(ctx, "h1")
	_ = store.SetSetupComplete(ctx, true)

	if err := store.Reset(ctx); err != nil {
		t.Fatalf("reset: %v", err)
	}

	if homes, _ := store.ListHomes(ctx); len(homes) != 0 {
		t.Fatalf("homes not cleared: %v", homes)
	}
	if nodes, _ := store.ListNodes(ctx); len(nodes) != 0 {
		t.Fatalf("nodes not cleared: %v", nodes)
	}
	if es, _ := store.ListEnrollments(ctx, ""); len(es) != 0 {
		t.Fatalf("enrollments not cleared: %v", es)
	}
	if active, _ := store.GetActiveHome(ctx); active != "" {
		t.Fatalf("active home not cleared: %q", active)
	}
	if complete, _ := store.GetSetupComplete(ctx); complete {
		t.Fatal("setup-complete not cleared")
	}
	// Buckets must still be usable after a reset.
	if err := store.CreateHome(ctx, models.Home{ID: "h2", Name: "New"}); err != nil {
		t.Fatalf("store unusable after reset: %v", err)
	}
}

func ids(es []models.Enrollment) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.ID
	}
	return out
}
