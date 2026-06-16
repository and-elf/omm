package topology

import (
	"sort"
	"sync"
	"time"
)

// Aggregator merges per-node topology reports into a single mesh-wide graph.
// Each node pushes its local graph (Ingest); a controller serves the merged
// view (Merge) combining its own local graph with fresh reports.
type Aggregator struct {
	mu      sync.Mutex
	reports map[string]timedGraph
	ttl     int64 // seconds; <=0 means reports never expire
	now     func() int64
}

type timedGraph struct {
	graph Graph
	at    int64
}

// NewAggregator creates an aggregator. Reports older than ttl are ignored by
// Merge. now defaults to time.Now (unix seconds).
func NewAggregator(ttl time.Duration, now func() int64) *Aggregator {
	if now == nil {
		now = func() int64 { return time.Now().Unix() }
	}
	return &Aggregator{reports: map[string]timedGraph{}, ttl: int64(ttl.Seconds()), now: now}
}

// Ingest records the latest topology report from a node.
func (a *Aggregator) Ingest(nodeID string, g Graph) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reports[nodeID] = timedGraph{graph: g, at: a.now()}
}

// Merge combines the local graph with all fresh reports into one graph.
func (a *Aggregator) Merge(local Graph) Graph {
	a.mu.Lock()
	defer a.mu.Unlock()

	cutoff := a.now() - a.ttl
	type entry struct {
		id string
		tg timedGraph
	}
	var fresh []entry
	for id, tg := range a.reports {
		if a.ttl <= 0 || tg.at >= cutoff {
			fresh = append(fresh, entry{id, tg})
		}
	}
	// Newest first; tie-break by node id for deterministic output.
	sort.Slice(fresh, func(i, j int) bool {
		if fresh[i].tg.at != fresh[j].tg.at {
			return fresh[i].tg.at > fresh[j].tg.at
		}
		return fresh[i].id < fresh[j].id
	})

	graphs := make([]Graph, 0, len(fresh)+1)
	graphs = append(graphs, local)
	for _, e := range fresh {
		graphs = append(graphs, e.tg.graph)
	}
	return mergeGraphs(graphs)
}

// mergeGraphs unions nodes (first occurrence wins, so the local "self" is
// preserved), keeps the strongest link per directed pair, and unions clients by
// MAC (first occurrence wins — newest report, since graphs are newest-first).
func mergeGraphs(graphs []Graph) Graph {
	out := Graph{Nodes: []Node{}, Links: []Link{}, Clients: []Client{}}
	nodeIdx := map[string]int{}
	clientSeen := map[string]bool{}
	links := map[string]Link{}

	for i, g := range graphs {
		for _, n := range g.Nodes {
			if pos, ok := nodeIdx[n.ID]; ok {
				// A node already in the graph (e.g. the controller's bare
				// neighbor entry) gets enriched with the self-reported backhaul
				// and mesh mode from the node's own report, which is
				// authoritative about its own wireless state regardless of which
				// graph mentioned it first.
				if out.Nodes[pos].Backhaul == "" && n.Backhaul != "" {
					out.Nodes[pos].Backhaul = n.Backhaul
				}
				if out.Nodes[pos].MeshMode == "" && n.MeshMode != "" {
					out.Nodes[pos].MeshMode = n.MeshMode
				}
				continue
			}
			// Only the local graph's node is "self"; a reporting node's own
			// "self" becomes a regular node in the aggregate.
			if i > 0 && n.Role == "self" {
				n.Role = "node"
			}
			nodeIdx[n.ID] = len(out.Nodes)
			out.Nodes = append(out.Nodes, n)
		}
		for _, l := range g.Links {
			key := l.Source + "\x00" + l.Target
			if ex, ok := links[key]; !ok || l.TQ > ex.TQ {
				links[key] = l
			}
		}
		for _, c := range g.Clients {
			if !clientSeen[c.MAC] {
				clientSeen[c.MAC] = true
				out.Clients = append(out.Clients, c)
			}
		}
	}

	keys := make([]string, 0, len(links))
	for k := range links {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out.Links = append(out.Links, links[k])
	}
	return reconcile(out)
}

// reconcile rewrites MAC-keyed endpoints to node IDs and drops the phantom MAC
// nodes those endpoints referred to. batman-adv neighbours are keyed by
// originator MAC, but each node self-reports under a node ID and declares its
// batman addresses; this maps every MAC back to its owning node so the real
// nodes connect to each other instead of to anonymous MAC blobs (issues
// #27/#28). A MAC with no declaring node is left untouched (an unknown node).
func reconcile(g Graph) Graph {
	addrToID := map[string]string{}
	for _, n := range g.Nodes {
		for _, a := range n.Addrs {
			addrToID[a] = n.ID
		}
	}
	if len(addrToID) == 0 {
		return g
	}
	canon := func(id string) string {
		if real, ok := addrToID[id]; ok {
			return real
		}
		return id
	}

	// Keep nodes that are not aliases of another node (a phantom MAC node has an
	// ID that canonicalises to a different, real node).
	nodes := g.Nodes[:0]
	for _, n := range g.Nodes {
		if canon(n.ID) == n.ID {
			nodes = append(nodes, n)
		}
	}

	// Rewrite link endpoints to canonical IDs, re-deduping by directed pair and
	// keeping the strongest TQ (a rewrite can collapse two MAC/ID links into one).
	links := map[string]Link{}
	order := []string{}
	for _, l := range g.Links {
		l.Source, l.Target = canon(l.Source), canon(l.Target)
		if l.Source == l.Target {
			continue // a self-loop from a node listing its own other address
		}
		key := l.Source + "\x00" + l.Target
		if ex, ok := links[key]; !ok || l.TQ > ex.TQ {
			if !ok {
				order = append(order, key)
			}
			links[key] = l
		}
	}
	sort.Strings(order)
	outLinks := make([]Link, 0, len(order))
	for _, k := range order {
		outLinks = append(outLinks, links[k])
	}

	// Rewrite client AP references too, in case a client was reported against a
	// node's MAC rather than its ID.
	for i := range g.Clients {
		g.Clients[i].AP = canon(g.Clients[i].AP)
	}

	g.Nodes, g.Links = nodes, outLinks
	return g
}
