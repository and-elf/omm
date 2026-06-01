package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
)

type noopProfileManager struct{}

func (noopProfileManager) ApplyProfile(ctx context.Context, profile models.Profile) error {
	return nil
}

func (noopProfileManager) ApplyProfileForHome(ctx context.Context, homeID string) error {
	return nil
}

func setupRouter(t *testing.T) http.Handler {
	db, err := storage.OpenDB(":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	store := storage.NewStore(db)
	return NewRouter(store, noopProfileManager{})
}

func TestCreateAndGetHome(t *testing.T) {
	router := setupRouter(t)

	body := strings.NewReader(`{"id":"home-1","name":"Main Home","controller":"gw1"}`)
	req := httptest.NewRequest(http.MethodPost, "/homes", body)
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", rw.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/homes/home-1", nil)
	rw = httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}
}

func TestCreateAndGetNode(t *testing.T) {
	router := setupRouter(t)

	body := strings.NewReader(`{"id":"node-1","serial":"serial-1","current_home":"home-1","trusted_homes":["home-1"]}`)
	req := httptest.NewRequest(http.MethodPost, "/nodes", body)
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", rw.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/nodes/node-1", nil)
	rw = httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}
}

func TestListHomes(t *testing.T) {
	router := setupRouter(t)

	for i := 1; i <= 2; i++ {
		body := strings.NewReader(fmt.Sprintf(`{"id":"home-%d","name":"Home %d"}`, i, i))
		req := httptest.NewRequest(http.MethodPost, "/homes", body)
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()
		router.ServeHTTP(rw, req)
		if rw.Code != http.StatusCreated {
			t.Fatalf("expected status 201, got %d", rw.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/homes", nil)
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}
	if len(rw.Body.String()) == 0 {
		t.Fatal("expected non-empty homes response")
	}
}

func TestCreateAndGetProfile(t *testing.T) {
	router := setupRouter(t)

	body := strings.NewReader(`{"id":"home-1","name":"Main Home","controller":"gw1"}`)
	req := httptest.NewRequest(http.MethodPost, "/homes", body)
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)
	if rw.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", rw.Code)
	}

	profile := strings.NewReader(`{"node_name":"Garage","mesh_ssid":"OpenWrtMesh","mesh_key":"secret","vlans":["10","20"]}`)
	req = httptest.NewRequest(http.MethodPost, "/homes/home-1/profile", profile)
	req.Header.Set("Content-Type", "application/json")
	rw = httptest.NewRecorder()
	router.ServeHTTP(rw, req)
	if rw.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", rw.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/homes/home-1/profile", nil)
	rw = httptest.NewRecorder()
	router.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}
}
