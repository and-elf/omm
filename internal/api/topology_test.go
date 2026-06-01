package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/and-elf/omm/internal/storage"
	"github.com/and-elf/omm/internal/topology"
)

func TestTopologyEndpoint(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	collector := topology.NewCollector("self-1", "Gateway", nil, nil)
	router := NewRouter(storage.NewStore(db), noopProfileManager{}, WithTopology(collector))

	rw := doGet(t, router, "/topology")
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rw.Code)
	}
	var g topology.Graph
	if err := json.Unmarshal(rw.Body.Bytes(), &g); err != nil {
		t.Fatalf("decode graph: %v", err)
	}
	if len(g.Nodes) != 1 || g.Nodes[0].ID != "self-1" || g.Nodes[0].Role != "self" {
		t.Fatalf("expected self node, got %+v", g.Nodes)
	}
}

func TestTopologyAbsentWithoutCollector(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	router := NewRouter(storage.NewStore(db), noopProfileManager{})

	rw := doGet(t, router, "/topology")
	if ct := rw.Header().Get("Content-Type"); ct == "application/json" {
		t.Fatalf("topology endpoint should be absent without a collector")
	}
}
