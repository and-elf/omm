package topology

import (
	"testing"
	"time"
)

func TestMergeUnionsReports(t *testing.T) {
	clock := int64(1000)
	agg := NewAggregator(time.Minute, func() int64 { return clock })

	// Controller's own local view.
	local := Graph{
		Nodes:   []Node{{ID: "ctrl", Label: "Gateway", Role: "self"}, {ID: "n1", Role: "node"}},
		Links:   []Link{{Source: "ctrl", Target: "n1", TQ: 200}},
		Clients: []Client{{MAC: "c1", AP: "ctrl", Signal: -50}},
	}

	// Reports from two member nodes.
	agg.Ingest("n1", Graph{
		Nodes:   []Node{{ID: "n1", Role: "self"}, {ID: "n2", Role: "node"}},
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
	agg := NewAggregator(time.Minute, func() int64 { return clock })

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

func TestMergeKeepsStrongestLinkAndDropsStale(t *testing.T) {
	clock := int64(1000)
	agg := NewAggregator(30*time.Second, func() int64 { return clock })

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
