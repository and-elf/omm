package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/and-elf/omm/internal/api"
	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
)

// Split deployments serve the management plane and the mesh control plane on
// separate listeners. This exercises the real HTTP boundary: a management-only
// endpoint must be reachable on the management server and absent (404) on the
// mesh server, while a dual-plane endpoint works on both.
func TestSplitListenerIsolation(t *testing.T) {
	db, err := storage.OpenDB(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)
	if err := store.CreateHome(context.Background(), models.Home{ID: "h1", Name: "Home"}); err != nil {
		t.Fatalf("create home: %v", err)
	}

	mgmt := httptest.NewServer(api.NewManagementRouter(store, noopProfileManager{}))
	defer mgmt.Close()
	mesh := httptest.NewServer(api.NewMeshRouter(store, noopProfileManager{}))
	defer mesh.Close()

	get := func(base, path string) int {
		resp, err := http.Get(base + path)
		if err != nil {
			t.Fatalf("GET %s%s: %v", base, path, err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	// Management-only endpoint: present on mgmt, absent on the mesh listener.
	if code := get(mgmt.URL, "/nodes"); code != http.StatusOK {
		t.Errorf("mgmt /nodes = %d, want 200", code)
	}
	if code := get(mesh.URL, "/nodes"); code != http.StatusNotFound {
		t.Errorf("mesh /nodes = %d, want 404 (management must not be on the mesh listener)", code)
	}

	// Dual-plane endpoint (nodes fetch joined-Home metadata; UI shows it).
	if code := get(mgmt.URL, "/homes/h1"); code != http.StatusOK {
		t.Errorf("mgmt /homes/h1 = %d, want 200", code)
	}
	if code := get(mesh.URL, "/homes/h1"); code != http.StatusOK {
		t.Errorf("mesh /homes/h1 = %d, want 200", code)
	}
}
