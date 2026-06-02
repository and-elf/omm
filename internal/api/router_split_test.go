package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/and-elf/omm/internal/enrollment"
	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
)

func req(t *testing.T, h http.Handler, method, path string) int {
	t.Helper()
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest(method, path, nil))
	return rw.Code
}

// The mesh router exposes only the node-to-node control plane. The
// security-relevant property is that management endpoints are NOT reachable on
// the mesh (network) listener; the mesh router has no SPA catch-all, so missing
// routes return a real 404. GET /homes/{id} is intentionally on both (nodes
// fetch joined-Home metadata; the UI shows it).
func TestRouterMeshPlaneExcludesManagement(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)
	_ = store.CreateHome(context.Background(), models.Home{ID: "h1", Name: "Home"})

	mesh := NewMeshRouter(store, noopProfileManager{})

	// Mesh-plane / dual-plane routes are present.
	if code := req(t, mesh, http.MethodGet, "/health"); code != http.StatusOK {
		t.Errorf("mesh GET /health = %d, want 200", code)
	}
	if code := req(t, mesh, http.MethodGet, "/homes/h1"); code != http.StatusOK {
		t.Errorf("mesh GET /homes/h1 = %d, want 200", code)
	}
	// Management endpoints must NOT exist on the mesh plane.
	for _, mp := range []struct {
		method, path string
	}{
		{http.MethodPost, "/reset"},
		{http.MethodGet, "/nodes"},
		{http.MethodGet, "/active-home"},
		{http.MethodGet, "/status"},
	} {
		if code := req(t, mesh, mp.method, mp.path); code != http.StatusNotFound {
			t.Errorf("mesh %s %s = %d, want 404 (management-only)", mp.method, mp.path, code)
		}
	}
}

// The management router serves the admin/UI plane. (It also serves the SPA
// catch-all, so unknown paths return index.html rather than 404 — absence is
// therefore asserted on the mesh router above, not here.)
func TestRouterManagementPlaneServesAdmin(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)
	_ = store.CreateHome(context.Background(), models.Home{ID: "h1", Name: "Home"})

	mgmt := NewManagementRouter(store, noopProfileManager{})

	if code := req(t, mgmt, http.MethodGet, "/homes/h1"); code != http.StatusOK {
		t.Errorf("mgmt GET /homes/h1 = %d, want 200", code)
	}
	if code := req(t, mgmt, http.MethodGet, "/nodes"); code != http.StatusOK {
		t.Errorf("mgmt GET /nodes = %d, want 200", code)
	}
	// /reset wipes state, so assert it last.
	if code := req(t, mgmt, http.MethodPost, "/reset"); code != http.StatusNoContent {
		t.Errorf("mgmt POST /reset = %d, want 204", code)
	}
}

// Enrollment endpoints split by direction: inbound (other nodes call them) on
// mesh; the pending-approval list (admin) on management.
func TestRouterPlaneEnrollmentSplit(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)
	svc := enrollment.NewService(store, enrollment.Options{HomeID: "h1", AutoAdopt: true})

	mesh := NewMeshRouter(store, noopProfileManager{}, WithEnrollment(svc))
	mgmt := NewManagementRouter(store, noopProfileManager{}, WithEnrollment(svc))

	// Inbound enrollment is on mesh (empty body -> handler runs, route exists).
	if code := req(t, mesh, http.MethodPost, "/enroll/request"); code == http.StatusNotFound {
		t.Error("mesh POST /enroll/request should be registered")
	}
	// The admin pending list is on management, and absent from the mesh plane.
	if code := req(t, mgmt, http.MethodGet, "/enroll"); code != http.StatusOK {
		t.Errorf("mgmt GET /enroll = %d, want 200", code)
	}
	if code := req(t, mesh, http.MethodGet, "/enroll"); code != http.StatusNotFound {
		t.Errorf("mesh GET /enroll = %d, want 404 (management-only)", code)
	}
}
