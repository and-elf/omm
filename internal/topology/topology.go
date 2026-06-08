// Package topology collects the live mesh topology from the system: batman-adv
// link quality between nodes and hostapd client signal (RSSI). Sources are
// injected so the collector is testable and degrades to an empty graph on
// hosts where the underlying tools are unavailable.
package topology

import "context"

// Node is a vertex in the topology graph.
type Node struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Role  string `json:"role"` // "self" | "node"
	// Backhaul is how this node reaches the rest of the mesh: "ethernet",
	// "wireless" or "unknown". It is set only on the node reporting its own
	// graph (its "self" node) and rides along when the aggregator demotes that
	// node, so the controller can show each node's backhaul type.
	Backhaul string `json:"backhaul,omitempty"`
	// MeshMode is the wireless-backhaul technology actually in effect on this
	// node: "802.11s" when the mesh formed, or "multi_ap" when it degraded.
	// Like Backhaul it is set on the reporting node's "self" vertex and rides
	// along through aggregation so the controller can show each node's mode.
	MeshMode string `json:"mesh_mode,omitempty"`
}

// Link is a mesh link between two nodes with its batman-adv transmit quality
// (TQ, 0-255; higher is better).
type Link struct {
	Source string `json:"source"`
	Target string `json:"target"`
	TQ     int    `json:"tq"`
}

// Client is a station associated to a node's AP, with its signal (RSSI, dBm).
type Client struct {
	MAC    string `json:"mac"`
	AP     string `json:"ap"`
	Signal int    `json:"signal"`
	Band   string `json:"band,omitempty"`
	TxRate int    `json:"tx_rate,omitempty"`
	RxRate int    `json:"rx_rate,omitempty"`
}

// Graph is the assembled topology.
type Graph struct {
	Nodes   []Node   `json:"nodes"`
	Links   []Link   `json:"links"`
	Clients []Client `json:"clients"`
}

// Neighbor is a batman-adv originator reachable from this node.
type Neighbor struct {
	ID string
	TQ int
}

// MeshSource yields this node's mesh neighbors (batman-adv).
type MeshSource interface {
	Neighbors(ctx context.Context) ([]Neighbor, error)
}

// ClientSource yields the wireless clients associated to this node.
type ClientSource interface {
	Clients(ctx context.Context) ([]Client, error)
}

// MeshModeSource yields this node's wireless-backhaul mode ("802.11s",
// "multi_ap" or "unknown") so the topology can show whether each node formed a
// mesh or degraded to a wired AP.
type MeshModeSource interface {
	MeshMode(ctx context.Context) string
}

// Collector assembles a Graph from this node's point of view.
type Collector struct {
	selfID    string
	selfLabel string
	mesh      MeshSource
	clients   ClientSource
	backhaul  BackhaulSource
	meshMode  MeshModeSource
}

// NewCollector builds a collector. mesh, clients, backhaul and/or meshMode may
// be nil.
func NewCollector(selfID, selfLabel string, mesh MeshSource, clients ClientSource, backhaul BackhaulSource, meshMode MeshModeSource) *Collector {
	if selfLabel == "" {
		selfLabel = selfID
	}
	return &Collector{selfID: selfID, selfLabel: selfLabel, mesh: mesh, clients: clients, backhaul: backhaul, meshMode: meshMode}
}

// Collect gathers the current topology. Source errors are tolerated so a
// partially-available system still returns a useful graph.
func (c *Collector) Collect(ctx context.Context) Graph {
	g := Graph{Nodes: []Node{{ID: c.selfID, Label: c.selfLabel, Role: "self"}}}
	seen := map[string]bool{c.selfID: true}

	if c.backhaul != nil {
		g.Nodes[0].Backhaul = c.backhaul.Backhaul(ctx)
	}

	if c.meshMode != nil {
		g.Nodes[0].MeshMode = c.meshMode.MeshMode(ctx)
	}

	if c.mesh != nil {
		if neighbors, err := c.mesh.Neighbors(ctx); err == nil {
			for _, n := range neighbors {
				if n.ID == "" {
					continue
				}
				if !seen[n.ID] {
					g.Nodes = append(g.Nodes, Node{ID: n.ID, Label: n.ID, Role: "node"})
					seen[n.ID] = true
				}
				g.Links = append(g.Links, Link{Source: c.selfID, Target: n.ID, TQ: n.TQ})
			}
		}
	}

	if c.clients != nil {
		if clients, err := c.clients.Clients(ctx); err == nil {
			for _, cl := range clients {
				if cl.AP == "" {
					cl.AP = c.selfID
				}
				g.Clients = append(g.Clients, cl)
			}
		}
	}

	return g
}
