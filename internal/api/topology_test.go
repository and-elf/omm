package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/and-elf/omm/internal/models"
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

// stubLeases is a fixed LeaseSource for tests.
type stubLeases map[string]topology.Lease

func (s stubLeases) Leases(context.Context) map[string]topology.Lease { return s }

// GET /topology enriches associated clients with the DHCP-assigned IP/hostname
// so the view can label them by name instead of MAC (#35).
func TestTopologyEnrichesClientsWithLeases(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	collector := topology.NewCollector("ctrl", "Gateway", nil, nil, nil, nil)
	leases := stubLeases{
		"aa:bb:cc:dd:ee:01": {IP: "192.168.1.50", Hostname: "laptop"},
		"aa:bb:cc:dd:ee:02": {IP: "192.168.1.51"}, // IP only, no hostname
	}
	router := NewRouter(storage.NewStore(db), noopProfileManager{},
		WithTopology(collector), WithClientLeases(leases))

	report := topologyReport{NodeID: "n1", Graph: topology.Graph{
		Nodes: []topology.Node{{ID: "n1", Role: "self"}},
		Clients: []topology.Client{
			{MAC: "aa:bb:cc:dd:ee:01", AP: "n1", Signal: -55},
			{MAC: "aa:bb:cc:dd:ee:02", AP: "n1", Signal: -60},
			{MAC: "aa:bb:cc:dd:ee:99", AP: "n1", Signal: -70}, // no lease
		},
	}}
	if rw := postJSON(t, router, "/topology/report", report); rw.Code != http.StatusAccepted {
		t.Fatalf("report: expected 202, got %d", rw.Code)
	}

	rw := doGet(t, router, "/topology")
	var g topology.Graph
	if err := json.Unmarshal(rw.Body.Bytes(), &g); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byMAC := map[string]topology.Client{}
	for _, c := range g.Clients {
		byMAC[c.MAC] = c
	}
	if c := byMAC["aa:bb:cc:dd:ee:01"]; c.IP != "192.168.1.50" || c.Hostname != "laptop" {
		t.Errorf("leased client not enriched: %+v", c)
	}
	if c := byMAC["aa:bb:cc:dd:ee:02"]; c.IP != "192.168.1.51" || c.Hostname != "" {
		t.Errorf("IP-only client wrong: %+v", c)
	}
	if c := byMAC["aa:bb:cc:dd:ee:99"]; c.IP != "" || c.Hostname != "" {
		t.Errorf("unleased client should stay bare: %+v", c)
	}
}

// An onboarded node that has never reported its topology must still appear in
// the merged graph as a down vertex, so an operator can see it failed to come up
// instead of it silently missing from the map (#29). The inventory comes from
// the controller's node store, scoped to the home it controls.
func TestTopologySurfacesOnboardedSilentNode(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)

	// A node onboarded into this controller's home but never reporting.
	if err := store.CreateNode(context.Background(), models.Node{
		ID: "n-silent", Serial: "SN9", CurrentHome: "home-1", LastSeen: 1,
	}); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	// A node belonging to a different home must NOT be surfaced here.
	if err := store.CreateNode(context.Background(), models.Node{
		ID: "n-elsewhere", CurrentHome: "home-2", LastSeen: 1,
	}); err != nil {
		t.Fatalf("seed other node: %v", err)
	}

	collector := topology.NewCollector("ctrl", "Gateway", nil, nil, nil, nil)
	router := NewRouter(store, noopProfileManager{},
		WithSelfHome("home-1"), WithTopology(collector))

	rw := doGet(t, router, "/topology")
	var g topology.Graph
	if err := json.Unmarshal(rw.Body.Bytes(), &g); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var silent *topology.Node
	for i := range g.Nodes {
		if g.Nodes[i].ID == "n-elsewhere" {
			t.Fatalf("a node from another home must not be surfaced: %+v", g.Nodes)
		}
		if g.Nodes[i].ID == "n-silent" {
			silent = &g.Nodes[i]
		}
	}
	if silent == nil {
		t.Fatalf("onboarded-but-silent node must appear, got %+v", g.Nodes)
	}
	if silent.Status != topology.StatusDown {
		t.Fatalf("silent node should be down, got %q", silent.Status)
	}
	// The vertex must be labelled with the node's friendly serial, not its raw
	// 64-char node ID — otherwise silent nodes render as anonymous hex blobs.
	if silent.Label != "SN9" {
		t.Fatalf("silent node should be labelled by serial, got %q", silent.Label)
	}
}

// ctxSensitiveMesh mimics the real batctl source: its work fails if the context
// is already cancelled (CommandContext kills the subprocess). It is how we assert
// getTopology does not pass the request's cancellation to the collector.
type ctxSensitiveMesh struct{}

func (ctxSensitiveMesh) Neighbors(ctx context.Context) ([]topology.Neighbor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []topology.Neighbor{{ID: "n9", TQ: 200}}, nil
}

// The LuCI ubus transport cancels the request context mid-handler (rpcd's nc
// half-closes the socket). getTopology must still collect a full graph — it runs
// the collector under a context detached from that cancellation.
func TestTopologyCollectsDespiteCancelledRequestContext(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	collector := topology.NewCollector("ctrl", "Gateway", ctxSensitiveMesh{}, nil, nil, nil)
	router := NewRouter(storage.NewStore(db), noopProfileManager{}, WithTopology(collector))

	req := httptest.NewRequest(http.MethodGet, "/topology", nil)
	ctx, cancel := context.WithCancel(req.Context())
	cancel() // client "gone" before the handler runs the collector
	req = req.WithContext(ctx)
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	var g topology.Graph
	if err := json.Unmarshal(rw.Body.Bytes(), &g); err != nil {
		t.Fatalf("decode graph: %v", err)
	}
	// The neighbour (and its link) survive only if the collector ran with a
	// live context despite the cancelled request.
	if len(g.Links) != 1 || g.Links[0].Target != "n9" {
		t.Fatalf("expected the collected neighbour link, got nodes=%+v links=%+v", g.Nodes, g.Links)
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
