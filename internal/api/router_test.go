package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
)

func newTestStore(t *testing.T) storage.Store {
	t.Helper()
	db, err := storage.OpenDB(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return storage.NewStore(db)
}

func TestHealthHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rw := httptest.NewRecorder()

	healthHandler(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}
}

// /status reports readiness and the applied backhaul. With no profile applied
// the backhaul mode defaults to unknown.
func TestStatusHandlerDefaultsToUnknownBackhaul(t *testing.T) {
	h := newHandler(newTestStore(t), nil)
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rw := httptest.NewRecorder()

	h.status(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}
	var resp managementStatusResponse
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ready" || resp.Backhaul.Mode != models.BackhaulModeUnknown {
		t.Fatalf("unexpected status response: %+v", resp)
	}
}

// A degraded backhaul is surfaced with its reason and remediation.
func TestStatusHandlerSurfacesDegradedBackhaul(t *testing.T) {
	store := newTestStore(t)
	want := models.BackhaulState{
		Mode:        models.BackhaulModeMultiAP,
		Reason:      "802.11s unavailable",
		Remediation: "install wpad-mesh-wolfssl",
	}
	if err := store.SetBackhaulState(t.Context(), want); err != nil {
		t.Fatalf("seed backhaul: %v", err)
	}
	h := newHandler(store, nil)
	rw := httptest.NewRecorder()

	h.status(rw, httptest.NewRequest(http.MethodGet, "/status", nil))

	var resp managementStatusResponse
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Backhaul != want {
		t.Fatalf("backhaul: got %+v want %+v", resp.Backhaul, want)
	}
}
