package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/and-elf/omm/internal/enrollment"
	"github.com/and-elf/omm/internal/identity"
	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
)

// TestJoinEndpointEnrollsIntoAnotherController starts a controller daemon and a
// second daemon that, driven only through its /enroll/join API, enrolls into
// the controller as a node — demonstrating the role is a runtime action, not a
// startup mode.
func TestJoinEndpointEnrollsIntoAnotherController(t *testing.T) {
	// Controller daemon (its own Home, auto-adopt).
	cdb, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { cdb.Close() })
	cstore := storage.NewStore(cdb)
	if err := cstore.CreateHome(context.Background(), models.Home{ID: "home-controller", Name: "Controller Home", Controller: "ctrl-mac"}); err != nil {
		t.Fatalf("seed controller home: %v", err)
	}
	csvc := enrollment.NewService(cstore, enrollment.Options{HomeID: "home-controller", AutoAdopt: true})
	controllerSrv := httptest.NewServer(NewRouter(cstore, noopProfileManager{}, WithEnrollment(csvc)))
	t.Cleanup(controllerSrv.Close)

	// Second daemon: has its own identity and the join endpoint enabled.
	id, _ := identity.Generate()
	ddb, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { ddb.Close() })
	dstore := storage.NewStore(ddb)
	dsvc := enrollment.NewService(dstore, enrollment.Options{HomeID: "home-device", AutoAdopt: true})
	deviceRouter := NewRouter(dstore, noopProfileManager{}, WithEnrollment(dsvc), WithSelf(id, "device-serial"))

	// Drive the join through the device's API.
	rw := postJSON(t, deviceRouter, "/enroll/join", joinInput{ControllerURL: controllerSrv.URL})
	if rw.Code != http.StatusOK {
		t.Fatalf("join: expected 200, got %d (%s)", rw.Code, rw.Body)
	}
	var res enrollment.Result
	_ = json.Unmarshal(rw.Body.Bytes(), &res)
	if res.Status != models.EnrollmentActive {
		t.Fatalf("expected active after join, got %q", res.Status)
	}

	// The device now appears as a node in the controller's Home...
	if _, err := cstore.GetNode(context.Background(), id.NodeID()); err != nil {
		t.Fatalf("device should be a node in the controller home: %v", err)
	}

	// ...and has recorded the joined Home locally as a membership, so boot
	// selection can later consider it.
	joined, err := dstore.GetHome(context.Background(), "home-controller")
	if err != nil {
		t.Fatalf("joined home should be recorded locally: %v", err)
	}
	if joined.ID != "home-controller" {
		t.Fatalf("unexpected recorded home: %+v", joined)
	}
}

func TestJoinEndpointAbsentWithoutIdentity(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)
	svc := enrollment.NewService(store, enrollment.Options{HomeID: "h", AutoAdopt: true})
	router := NewRouter(store, noopProfileManager{}, WithEnrollment(svc)) // no WithSelf

	rw := postJSON(t, router, "/enroll/join", joinInput{ControllerURL: "http://example"})
	if ct := rw.Header().Get("Content-Type"); ct == "application/json" {
		t.Fatalf("join endpoint should be absent without an identity (got json response)")
	}
}
