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
	collector := topology.NewCollector("self-1", "Gateway", nil, nil, nil, nil)
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

func TestTopologyAggregatesReports(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	collector := topology.NewCollector("ctrl", "Gateway", nil, nil, nil, nil)
	router := NewRouter(storage.NewStore(db), noopProfileManager{}, WithTopology(collector))

	// A member node reports its local view.
	report := topologyReport{
		NodeID: "n1",
		Graph: topology.Graph{
			Nodes:   []topology.Node{{ID: "n1", Role: "self"}, {ID: "n2", Role: "node"}},
			Links:   []topology.Link{{Source: "n1", Target: "n2", TQ: 190}},
			Clients: []topology.Client{{MAC: "c2", AP: "n1", Signal: -65}},
		},
	}
	rw := postJSON(t, router, "/topology/report", report)
	if rw.Code != http.StatusAccepted {
		t.Fatalf("report: expected 202, got %d", rw.Code)
	}

	rw = doGet(t, router, "/topology")
	var g topology.Graph
	if err := json.Unmarshal(rw.Body.Bytes(), &g); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Aggregated: controller self + n1 + n2, the reported link and client.
	if len(g.Nodes) != 3 {
		t.Fatalf("expected 3 aggregated nodes, got %d: %+v", len(g.Nodes), g.Nodes)
	}
	if g.Nodes[0].ID != "ctrl" || g.Nodes[0].Role != "self" {
		t.Fatalf("expected controller self first, got %+v", g.Nodes[0])
	}
	if len(g.Links) != 1 || len(g.Clients) != 1 {
		t.Fatalf("expected reported link+client, got links=%+v clients=%+v", g.Links, g.Clients)
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
