package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/and-elf/omm/internal/storage"
)

func TestDevCORSAddsHeadersWhenEnabled(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	router := NewRouter(storage.NewStore(db), noopProfileManager{}, WithDevCORS())

	rw := doGet(t, router, "/status")
	if got := rw.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
	}

	// Preflight is answered without hitting a handler.
	req := httptest.NewRequest(http.MethodOptions, "/nodes/abc/adopt", nil)
	pre := httptest.NewRecorder()
	router.ServeHTTP(pre, req)
	if pre.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS preflight status = %d, want 204", pre.Code)
	}
	if got := pre.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("expected Access-Control-Allow-Methods on preflight")
	}
}

func TestNoDevCORSByDefault(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	router := NewRouter(storage.NewStore(db), noopProfileManager{})

	rw := doGet(t, router, "/status")
	if got := rw.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no CORS header by default, got %q", got)
	}
}
