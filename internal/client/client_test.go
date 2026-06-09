package client_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/and-elf/omm/internal/api"
	"github.com/and-elf/omm/internal/client"
	"github.com/and-elf/omm/internal/enrollment"
	"github.com/and-elf/omm/internal/identity"
	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
)

func newServer(t *testing.T, autoAdopt bool) (*httptest.Server, storage.Store, *enrollment.Service) {
	t.Helper()
	db, err := storage.OpenDB(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)
	policy := enrollment.AdoptOff
	if autoAdopt {
		policy = enrollment.AdoptAlways
	}
	svc := enrollment.NewService(store, enrollment.Options{HomeID: "home-1", AdoptPolicy: policy})
	srv := httptest.NewServer(api.NewRouter(store, noopPM{}, api.WithEnrollment(svc)))
	t.Cleanup(srv.Close)
	return srv, store, svc
}

type noopPM struct{}

func (noopPM) ApplyProfile(context.Context, models.Profile) error { return nil }
func (noopPM) ApplyProfileForHome(context.Context, string) error  { return nil }

func TestClientEnrollAutoAdopt(t *testing.T) {
	srv, store, _ := newServer(t, true)
	id, _ := identity.Generate()

	c := client.New(id, srv.URL, client.Options{PollInterval: 10 * time.Millisecond})
	res, err := c.Enroll(context.Background(), "SN-client-1")
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if res.Status != models.EnrollmentActive {
		t.Fatalf("expected active, got %q", res.Status)
	}
	if c.State() != client.StateActive {
		t.Fatalf("expected state machine active, got %q", c.State())
	}
	if _, err := store.GetNode(context.Background(), id.NodeID()); err != nil {
		t.Fatalf("expected node enrolled: %v", err)
	}
}

func TestClientEnrollWaitsForManualAdoption(t *testing.T) {
	srv, _, svc := newServer(t, false)
	id, _ := identity.Generate()

	// Approve out-of-band shortly after the client starts polling.
	go func() {
		time.Sleep(40 * time.Millisecond)
		_, _ = svc.Adopt(context.Background(), id.NodeID())
	}()

	c := client.New(id, srv.URL, client.Options{PollInterval: 10 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := c.Enroll(ctx, "SN-client-2")
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if res.Status != models.EnrollmentActive {
		t.Fatalf("expected active after manual adoption, got %q", res.Status)
	}
}

func TestFetchProfile(t *testing.T) {
	srv, store, _ := newServer(t, false)
	ctx := context.Background()

	if err := store.CreateHome(ctx, models.Home{ID: "home-1", Name: "Home One"}); err != nil {
		t.Fatalf("create home: %v", err)
	}
	want := models.Profile{HomeID: "home-1", MeshSSID: "casa", MeshKey: "supersecret"}
	if err := store.CreateOrUpdateProfile(ctx, want); err != nil {
		t.Fatalf("create profile: %v", err)
	}

	got, err := client.FetchProfile(ctx, srv.URL, "home-1", nil)
	if err != nil {
		t.Fatalf("fetch profile: %v", err)
	}
	if got.MeshSSID != want.MeshSSID || got.MeshKey != want.MeshKey {
		t.Fatalf("fetched profile = %+v, want ssid/key %q/%q", got, want.MeshSSID, want.MeshKey)
	}
}

func TestFetchProfileUnknownHome(t *testing.T) {
	srv, _, _ := newServer(t, false)
	if _, err := client.FetchProfile(context.Background(), srv.URL, "no-such-home", nil); err == nil {
		t.Fatal("expected error fetching profile for unknown home")
	}
}

func TestClientEnrollFailsAgainstWrongURL(t *testing.T) {
	id, _ := identity.Generate()
	c := client.New(id, "http://127.0.0.1:1", client.Options{PollInterval: 10 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := c.Enroll(ctx, "SN-x"); err == nil {
		t.Fatal("expected enroll against an unreachable controller to fail")
	}
	if c.State() != client.StateFailed {
		t.Fatalf("expected failed state, got %q", c.State())
	}
}
