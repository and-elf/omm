package topology

import (
	"testing"
	"time"
)

func TestMergeUnionsReports(t *testing.T) {
	clock := int64(1000)
	agg := NewAggregator(time.Minute, 5*time.Minute, func() int64 { return clock })

	// Controller's own local view.
	local := Graph{
		Nodes:   []Node{{ID: "ctrl", Label: "Gateway", Role: "self"}, {ID: "n1", Role: "node"}},
		Links:   []Link{{Source: "ctrl", Target: "n1", TQ: 200}},
		Clients: []Client{{MAC: "c1", AP: "ctrl", Signal: -50}},
	}

	// Reports from two member nodes. n1 carries its own backhaul + mesh mode on
	// its "self" vertex, which must survive demotion to a regular node.
	agg.Ingest("n1", Graph{
		Nodes:   []Node{{ID: "n1", Role: "self", Backhaul: BackhaulWireless, MeshMode: "multi_ap"}, {ID: "n2", Role: "node"}},
		Links:   []Link{{Source: "n1", Target: "n2", TQ: 180}},
		Clients: []Client{{MAC: "c2", AP: "n1", Signal: -60}},
	})
	agg.Ingest("n2", Graph{
		Nodes:   []Node{{ID: "n2", Role: "self"}},
		Clients: []Client{{MAC: "c3", AP: "n2", Signal: -70}},
	})

	g := agg.Merge(local)

	if len(g.Nodes) != 3 { // ctrl, n1, n2
		t.Fatalf("expected 3 nodes, got %d: %+v", len(g.Nodes), g.Nodes)
	}
	// Local self preserved.
	if g.Nodes[0].ID != "ctrl" || g.Nodes[0].Role != "self" {
		t.Fatalf("expected ctrl self first, got %+v", g.Nodes[0])
	}
	// A reporting node is demoted to "node" but keeps its self-reported backhaul
	// and mesh mode, so the controller can show each node's wireless state.
	for _, n := range g.Nodes {
		if n.ID == "n1" {
			if n.Role != "node" || n.Backhaul != BackhaulWireless || n.MeshMode != "multi_ap" {
				t.Fatalf("n1 should be demoted but keep backhaul/mesh_mode, got %+v", n)
			}
		}
	}
	if len(g.Links) != 2 {
		t.Fatalf("expected 2 links, got %d: %+v", len(g.Links), g.Links)
	}
	if len(g.Clients) != 3 {
		t.Fatalf("expected 3 clients, got %d: %+v", len(g.Clients), g.Clients)
	}

	// Only the local node is "self"; reporters are demoted to "node".
	selfCount := 0
	for _, n := range g.Nodes {
		if n.Role == "self" {
			selfCount++
			if n.ID != "ctrl" {
				t.Fatalf("unexpected self node %q", n.ID)
			}
		}
	}
	if selfCount != 1 {
		t.Fatalf("expected exactly one self node, got %d", selfCount)
	}
}

func TestMergePreservesBackhaulOnDemotedNode(t *testing.T) {
	clock := int64(1000)
	agg := NewAggregator(time.Minute, 5*time.Minute, func() int64 { return clock })

	// A member node reports itself (self) carrying its backhaul type.
	agg.Ingest("n1", Graph{
		Nodes: []Node{{ID: "n1", Role: "self", Backhaul: BackhaulEthernet}},
	})

	g := agg.Merge(Graph{Nodes: []Node{{ID: "ctrl", Role: "self"}}})

	var n1 *Node
	for i := range g.Nodes {
		if g.Nodes[i].ID == "n1" {
			n1 = &g.Nodes[i]
		}
	}
	if n1 == nil {
		t.Fatalf("expected n1 in merged graph, got %+v", g.Nodes)
	}
	// The reporting node is demoted to "node" but keeps its backhaul type so the
	// controller can show how each node reaches the mesh.
	if n1.Role != "node" {
		t.Fatalf("expected n1 demoted to node, got role %q", n1.Role)
	}
	if n1.Backhaul != BackhaulEthernet {
		t.Fatalf("expected n1 to keep backhaul %q, got %q", BackhaulEthernet, n1.Backhaul)
	}
}

// TestMergeReconcilesMACLinksToNodeIDs is the core of issues #27/#28: batman-adv
// neighbours are keyed by originator MAC, but nodes self-report under a node ID.
// Without reconciliation, the controller's link points at a phantom MAC blob and
// the real nodes never connect. Each node reporting its own batman address lets
// the aggregator rewrite those MAC endpoints to the owning node's ID and drop
// the phantom MAC nodes.
func TestMergeReconcilesMACLinksToNodeIDs(t *testing.T) {
	clock := int64(1000)
	agg := NewAggregator(time.Minute, 5*time.Minute, func() int64 { return clock })

	// Controller's local view: it knows neighbour "n1" only by its batman MAC, so
	// it created a phantom node + a link to that MAC.
	local := Graph{
		Nodes: []Node{
			{ID: "ctrl", Label: "Gateway", Role: "self", Addrs: []string{"de:ad:be:ef:00:0c"}},
			{ID: "aa:bb:cc:dd:ee:01", Label: "aa:bb:cc:dd:ee:01", Role: "node"},
		},
		Links: []Link{{Source: "ctrl", Target: "aa:bb:cc:dd:ee:01", TQ: 220, Kind: LinkWireless, Signal: -55}},
	}

	// n1 reports itself, declaring the batman MAC the controller saw as its own.
	agg.Ingest("n1", Graph{
		Nodes: []Node{{ID: "n1", Label: "Kitchen", Role: "self", Addrs: []string{"aa:bb:cc:dd:ee:01"}}},
		Links: []Link{{Source: "n1", Target: "de:ad:be:ef:00:0c", TQ: 218, Kind: LinkWireless, Signal: -57}},
	})

	g := agg.Merge(local)

	// The phantom MAC node is gone; only ctrl and n1 remain.
	if len(g.Nodes) != 2 {
		t.Fatalf("expected phantom MAC node dropped, got %d: %+v", len(g.Nodes), g.Nodes)
	}
	for _, n := range g.Nodes {
		if n.ID == "aa:bb:cc:dd:ee:01" {
			t.Fatalf("phantom MAC node should be reconciled away: %+v", g.Nodes)
		}
	}
	// Both links now connect the real node IDs in both directions.
	var ctrlToN1, n1ToCtrl bool
	for _, l := range g.Links {
		if l.Source == "ctrl" && l.Target == "n1" {
			ctrlToN1 = true
		}
		if l.Source == "n1" && l.Target == "ctrl" {
			n1ToCtrl = true
		}
	}
	if !ctrlToN1 || !n1ToCtrl {
		t.Fatalf("expected links between real node IDs, got %+v", g.Links)
	}
}

func TestMergeKeepsStrongestLinkAndDropsStale(t *testing.T) {
	clock := int64(1000)
	agg := NewAggregator(30*time.Second, 2*time.Minute, func() int64 { return clock })

	agg.Ingest("n1", Graph{Links: []Link{{Source: "ctrl", Target: "n1", TQ: 120}}})

	// A fresh report with a stronger TQ for the same directed link.
	clock = 1005
	agg.Ingest("n1b", Graph{Links: []Link{{Source: "ctrl", Target: "n1", TQ: 240}}})

	// An old report that should be dropped (older than the 30s TTL).
	agg.reports["stale"] = timedGraph{graph: Graph{Nodes: []Node{{ID: "stale"}}}, at: 900}

	clock = 1010
	g := agg.Merge(Graph{Nodes: []Node{{ID: "ctrl", Role: "self"}}})

	for _, n := range g.Nodes {
		if n.ID == "stale" {
			t.Fatal("stale report should have been dropped")
		}
	}
	if len(g.Links) != 1 || g.Links[0].TQ != 240 {
		t.Fatalf("expected single link with strongest TQ 240, got %+v", g.Links)
	}
}

func nodeByID(g Graph, id string) *Node {
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return &g.Nodes[i]
		}
	}
	return nil
}

// A node reporting within the freshness window is alive and carries the
// timestamp of its own report as last_seen.
func TestMergeMarksAliveNodeWithLastSeen(t *testing.T) {
	clock := int64(1000)
	agg := NewAggregator(time.Minute, 5*time.Minute, func() int64 { return clock })

	agg.Ingest("n1", Graph{Nodes: []Node{{ID: "n1", Role: "self"}}})

	clock = 1010
	g := agg.Merge(Graph{Nodes: []Node{{ID: "ctrl", Role: "self"}}})

	if ctrl := nodeByID(g, "ctrl"); ctrl == nil || ctrl.Status != StatusAlive || ctrl.LastSeen != 1010 {
		t.Fatalf("ctrl (self) should be alive, last_seen=now: %+v", ctrl)
	}
	n1 := nodeByID(g, "n1")
	if n1 == nil || n1.Status != StatusAlive {
		t.Fatalf("n1 should be alive: %+v", n1)
	}
	if n1.LastSeen != 1000 {
		t.Fatalf("n1 last_seen should be its report time 1000, got %d", n1.LastSeen)
	}
}

// An onboarded node that has never reported (or last reported beyond the stale
// window) is surfaced as a down, isolated vertex from inventory alone — it does
// not silently vanish (#29).
func TestMergeSurfacesDownInventoryNode(t *testing.T) {
	clock := int64(10_000)
	agg := NewAggregator(time.Minute, 5*time.Minute, func() int64 { return clock })

	// n9 onboarded long ago and has never reported.
	g := agg.Merge(Graph{Nodes: []Node{{ID: "ctrl", Role: "self"}}},
		InventoryNode{ID: "n9", Label: "Garage", LastSeen: 100})

	n9 := nodeByID(g, "n9")
	if n9 == nil {
		t.Fatalf("onboarded-but-silent node must appear, got %+v", g.Nodes)
	}
	if n9.Status != StatusDown {
		t.Fatalf("n9 should be down, got %q", n9.Status)
	}
	if n9.Label != "Garage" || n9.Role != "node" {
		t.Fatalf("n9 should carry its inventory label and node role: %+v", n9)
	}
	if n9.LastSeen != 100 {
		t.Fatalf("n9 last_seen should fall back to inventory time 100, got %d", n9.LastSeen)
	}
	// A down node is isolated: no links reference it.
	for _, l := range g.Links {
		if l.Source == "n9" || l.Target == "n9" {
			t.Fatalf("down node must not have links: %+v", l)
		}
	}
}

// A node whose last report is past the freshness window but within the stale
// window shows as stale, and its (now untrustworthy) links are not merged.
func TestMergeSurfacesStaleInventoryNodeWithoutLinks(t *testing.T) {
	clock := int64(10_000)
	agg := NewAggregator(time.Minute, 5*time.Minute, func() int64 { return clock })

	// n3 last reported 2 minutes ago: past the 1-minute alive window, within the
	// 5-minute stale window. Its report carried a link.
	agg.reports["n3"] = timedGraph{
		graph: Graph{
			Nodes: []Node{{ID: "n3", Role: "self"}},
			Links: []Link{{Source: "n3", Target: "ctrl", TQ: 150}},
		},
		at: 10_000 - 120,
	}

	g := agg.Merge(Graph{Nodes: []Node{{ID: "ctrl", Role: "self"}}},
		InventoryNode{ID: "n3", Label: "Hallway", LastSeen: 100})

	n3 := nodeByID(g, "n3")
	if n3 == nil || n3.Status != StatusStale {
		t.Fatalf("n3 should be stale: %+v", n3)
	}
	// last_seen prefers the (newer) report time over the inventory time.
	if n3.LastSeen != 10_000-120 {
		t.Fatalf("n3 last_seen should be its last report time, got %d", n3.LastSeen)
	}
	for _, l := range g.Links {
		if l.Source == "n3" || l.Target == "n3" {
			t.Fatalf("stale node's links must not be merged: %+v", l)
		}
	}
}

// An inventory node that is also reporting fresh stays alive and appears once.
func TestMergeAliveInventoryNotDuplicated(t *testing.T) {
	clock := int64(1000)
	agg := NewAggregator(time.Minute, 5*time.Minute, func() int64 { return clock })

	agg.Ingest("n1", Graph{Nodes: []Node{{ID: "n1", Role: "self"}}})

	clock = 1005
	g := agg.Merge(Graph{Nodes: []Node{{ID: "ctrl", Role: "self"}}},
		InventoryNode{ID: "n1", Label: "Kitchen", LastSeen: 100})

	count := 0
	for _, n := range g.Nodes {
		if n.ID == "n1" {
			count++
			if n.Status != StatusAlive {
				t.Fatalf("reporting inventory node should be alive, got %q", n.Status)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected n1 exactly once, got %d", count)
	}
}
