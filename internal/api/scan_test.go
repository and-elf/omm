package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/and-elf/omm/internal/discovery"
	"github.com/and-elf/omm/internal/storage"
)

func TestScanEndpoint(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })

	scanner := func(context.Context) ([]discovery.Announcement, error) {
		return []discovery.Announcement{
			{HomeID: "h1", Name: "Cottage", ControllerID: "gw01", API: "http://10.0.0.1:8080"},
			{HomeID: "h2", Name: "Parents", ControllerID: "gw02", API: "http://10.0.0.2:8080"},
		}, nil
	}
	router := NewRouter(storage.NewStore(db), noopProfileManager{}, WithScanner(scanner))

	rw := doGet(t, router, "/scan")
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rw.Code)
	}
	var resp scanResponse
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Controllers) != 2 || resp.Controllers[0].Name != "Cottage" || resp.Controllers[0].API == "" {
		t.Fatalf("unexpected controllers: %+v", resp.Controllers)
	}
}
