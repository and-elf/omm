package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
)

func doDelete(t *testing.T, router http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)
	return rw
}

func TestDeleteHome(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)
	if err := store.CreateHome(context.Background(), models.Home{ID: "home-1", Name: "Home"}); err != nil {
		t.Fatalf("create home: %v", err)
	}
	router := NewRouter(store, noopProfileManager{})

	if rw := doDelete(t, router, "/homes/nope"); rw.Code != http.StatusNotFound {
		t.Fatalf("delete unknown: expected 404, got %d", rw.Code)
	}
	if rw := doDelete(t, router, "/homes/home-1"); rw.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d (%s)", rw.Code, rw.Body)
	}
	if rw := doGet(t, router, "/homes/home-1"); rw.Code != http.StatusNotFound {
		t.Fatalf("home should be gone: got %d", rw.Code)
	}
}

func TestDeleteHomeRefusesActiveHome(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)
	ctx := context.Background()
	_ = store.CreateHome(ctx, models.Home{ID: "home-1", Name: "Home"})
	if err := store.SetActiveHome(ctx, "home-1"); err != nil {
		t.Fatalf("set active: %v", err)
	}
	router := NewRouter(store, noopProfileManager{})

	rw := doDelete(t, router, "/homes/home-1")
	if rw.Code != http.StatusConflict {
		t.Fatalf("deleting active home: expected 409, got %d (%s)", rw.Code, rw.Body)
	}
	// The Home must still exist after a refused delete.
	if rw := doGet(t, router, "/homes/home-1"); rw.Code != http.StatusOK {
		t.Fatalf("active home should survive refused delete: got %d", rw.Code)
	}
}

func TestDeleteNode(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)
	_ = store.CreateNode(context.Background(), models.Node{ID: "node-1", Serial: "s1"})
	router := NewRouter(store, noopProfileManager{})

	if rw := doDelete(t, router, "/nodes/nope"); rw.Code != http.StatusNotFound {
		t.Fatalf("delete unknown node: expected 404, got %d", rw.Code)
	}
	if rw := doDelete(t, router, "/nodes/node-1"); rw.Code != http.StatusNoContent {
		t.Fatalf("delete node: expected 204, got %d (%s)", rw.Code, rw.Body)
	}
	if rw := doGet(t, router, "/nodes/node-1"); rw.Code != http.StatusNotFound {
		t.Fatalf("node should be gone: got %d", rw.Code)
	}
}

func TestReset(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)
	ctx := context.Background()
	_ = store.CreateHome(ctx, models.Home{ID: "home-1", Name: "Home"})
	_ = store.CreateNode(ctx, models.Node{ID: "node-1", Serial: "s1"})
	_ = store.SetActiveHome(ctx, "home-1")
	_ = store.SetSetupComplete(ctx, true)
	router := NewRouter(store, noopProfileManager{})

	if rw := postJSON(t, router, "/reset", nil); rw.Code != http.StatusNoContent {
		t.Fatalf("reset: expected 204, got %d (%s)", rw.Code, rw.Body)
	}

	if homes, _ := store.ListHomes(ctx); len(homes) != 0 {
		t.Fatalf("homes not cleared: %v", homes)
	}
	if nodes, _ := store.ListNodes(ctx); len(nodes) != 0 {
		t.Fatalf("nodes not cleared: %v", nodes)
	}
	if complete, _ := store.GetSetupComplete(ctx); complete {
		t.Fatal("setup-complete not cleared after reset")
	}
}
