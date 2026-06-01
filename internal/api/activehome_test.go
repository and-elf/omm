package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
)

func TestActiveHomeSetAndGet(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)
	if err := store.CreateHome(context.Background(), models.Home{ID: "home-1", Name: "Home"}); err != nil {
		t.Fatalf("create home: %v", err)
	}
	router := NewRouter(store, noopProfileManager{})

	// Initially unset.
	rw := doGet(t, router, "/active-home")
	var got activeHomeResponse
	_ = json.Unmarshal(rw.Body.Bytes(), &got)
	if rw.Code != http.StatusOK || got.HomeID != "" {
		t.Fatalf("expected empty active home, got %d %q", rw.Code, got.HomeID)
	}

	// Explicitly select home-1.
	rw = putJSON(t, router, "/active-home", `{"home_id":"home-1"}`)
	if rw.Code != http.StatusOK {
		t.Fatalf("set active home: expected 200, got %d (%s)", rw.Code, rw.Body)
	}

	rw = doGet(t, router, "/active-home")
	_ = json.Unmarshal(rw.Body.Bytes(), &got)
	if got.HomeID != "home-1" {
		t.Fatalf("expected active home home-1, got %q", got.HomeID)
	}
}

// spyProfileManager records ApplyProfileForHome calls so tests can assert that
// switching the active Home pushes that Home's profile to UCI.
type spyProfileManager struct {
	appliedHome string
	applyCalls  int
	applyErr    error
}

func (s *spyProfileManager) ApplyProfile(ctx context.Context, profile models.Profile) error {
	return nil
}

func (s *spyProfileManager) ApplyProfileForHome(ctx context.Context, homeID string) error {
	s.applyCalls++
	s.appliedHome = homeID
	return s.applyErr
}

func TestActiveHomeAppliesProfileOnSwitch(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)
	if err := store.CreateHome(context.Background(), models.Home{ID: "home-1", Name: "Home"}); err != nil {
		t.Fatalf("create home: %v", err)
	}
	spy := &spyProfileManager{}
	router := NewRouter(store, spy)

	rw := putJSON(t, router, "/active-home", `{"home_id":"home-1"}`)
	if rw.Code != http.StatusOK {
		t.Fatalf("set active home: expected 200, got %d (%s)", rw.Code, rw.Body)
	}
	if spy.applyCalls != 1 || spy.appliedHome != "home-1" {
		t.Fatalf("expected ApplyProfileForHome(home-1) once, got %d call(s) for %q", spy.applyCalls, spy.appliedHome)
	}
}

func TestActiveHomeApplyFailureReturns500(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)
	if err := store.CreateHome(context.Background(), models.Home{ID: "home-1", Name: "Home"}); err != nil {
		t.Fatalf("create home: %v", err)
	}
	spy := &spyProfileManager{applyErr: errors.New("uci boom")}
	router := NewRouter(store, spy)

	rw := putJSON(t, router, "/active-home", `{"home_id":"home-1"}`)
	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when apply fails, got %d (%s)", rw.Code, rw.Body)
	}
}

func TestActiveHomeRejectsUnknownHome(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	router := NewRouter(storage.NewStore(db), noopProfileManager{})

	rw := putJSON(t, router, "/active-home", `{"home_id":"nope"}`)
	if rw.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown home, got %d", rw.Code)
	}
}

func doGet(t *testing.T, router http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)
	return rw
}

func putJSON(t *testing.T, router http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)
	return rw
}
