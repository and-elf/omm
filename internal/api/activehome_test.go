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
	appliedHome   string
	applyCalls    int
	applyErr      error
	appliedCtxErr error // ctx.Err() observed inside the apply
}

func (s *spyProfileManager) ApplyProfile(ctx context.Context, profile models.Profile) error {
	s.applyCalls++
	s.appliedCtxErr = ctx.Err()
	return s.applyErr
}

func (s *spyProfileManager) ApplyProfileForHome(ctx context.Context, homeID string) error {
	s.applyCalls++
	s.appliedHome = homeID
	s.appliedCtxErr = ctx.Err()
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

// Applying a profile mutates UCI over several seconds; it must not be cancelled
// when the client connection drops (the LuCI rpcd/nc transport closing the
// socket once it has the request body cancels r.Context() and was killing the
// in-flight `ubus` call with "context canceled"). The apply must run with a
// context detached from the request's cancellation.
func TestActiveHomeApplyDetachedFromRequestCancellation(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)
	if err := store.CreateHome(context.Background(), models.Home{ID: "home-1", Name: "Home"}); err != nil {
		t.Fatalf("create home: %v", err)
	}
	spy := &spyProfileManager{}
	router := NewRouter(store, spy)

	// A request whose context is already cancelled, as if the client hung up.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPut, "/active-home", strings.NewReader(`{"home_id":"home-1"}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), req)

	if spy.applyCalls != 1 {
		t.Fatalf("expected the apply to run once, got %d", spy.applyCalls)
	}
	if spy.appliedCtxErr != nil {
		t.Fatalf("apply ran with a cancelled context (%v); it must be detached from the request", spy.appliedCtxErr)
	}
}

// A freshly created Home has no profile yet, so applying it reports
// storage.ErrNotFound. Selecting such a Home (the last step of the setup
// wizard) must still succeed — meshd's own auto-select already treats a
// missing profile as non-fatal; the API must agree, or the wizard 500s.
func TestActiveHomeMissingProfileIsNonFatal(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)
	if err := store.CreateHome(context.Background(), models.Home{ID: "home-1", Name: "Home"}); err != nil {
		t.Fatalf("create home: %v", err)
	}
	spy := &spyProfileManager{applyErr: storage.ErrNotFound}
	router := NewRouter(store, spy)

	rw := putJSON(t, router, "/active-home", `{"home_id":"home-1"}`)
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200 when the home has no profile yet, got %d (%s)", rw.Code, rw.Body)
	}

	rw = doGet(t, router, "/active-home")
	var got activeHomeResponse
	_ = json.Unmarshal(rw.Body.Bytes(), &got)
	if got.HomeID != "home-1" {
		t.Fatalf("expected active home home-1, got %q", got.HomeID)
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
