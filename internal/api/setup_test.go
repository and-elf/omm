package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/and-elf/omm/internal/identity"
	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
)

func TestSetupFlow(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)
	if err := store.CreateHome(context.Background(), models.Home{ID: "default-home", Name: "Home"}); err != nil {
		t.Fatalf("seed home: %v", err)
	}
	id, _ := identity.Generate()
	router := NewRouter(store, noopProfileManager{}, WithSelf(id, "sn-1"), WithSelfHome("default-home"))

	// Initially not set up; reports identity and self home.
	rw := doGet(t, router, "/setup")
	var s setupResponse
	_ = json.Unmarshal(rw.Body.Bytes(), &s)
	if s.SetupComplete {
		t.Fatal("expected setup_complete false initially")
	}
	if s.NodeID != id.NodeID() || s.HomeID != "default-home" || s.HomeName != "Home" {
		t.Fatalf("unexpected setup payload: %+v", s)
	}

	// Rename the device's own home (the "Create New Home" action).
	rw = putJSON(t, router, "/homes/default-home", `{"name":"Cottage"}`)
	if rw.Code != http.StatusOK {
		t.Fatalf("update home: expected 200, got %d (%s)", rw.Code, rw.Body)
	}

	// Complete setup.
	rw = postJSON(t, router, "/setup/complete", nil)
	if rw.Code != http.StatusOK {
		t.Fatalf("complete setup: expected 200, got %d", rw.Code)
	}

	rw = doGet(t, router, "/setup")
	_ = json.Unmarshal(rw.Body.Bytes(), &s)
	if !s.SetupComplete || s.HomeName != "Cottage" {
		t.Fatalf("expected completed setup with renamed home, got %+v", s)
	}
}

func TestUpdateHomeUnknown(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	router := NewRouter(storage.NewStore(db), noopProfileManager{})
	rw := putJSON(t, router, "/homes/nope", `{"name":"x"}`)
	if rw.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown home, got %d", rw.Code)
	}
}
