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
	// Addrs are the batman-adv addresses (originator MACs) by which this node is
	// known to its peers. A node reports its own addresses on its "self" vertex
	// so the aggregator can rewrite the MAC-keyed links in other nodes' reports
	// back to this node's ID — without it, every node's neighbours appear as
	// anonymous MAC blobs and the real nodes never connect (issues #27/#28).
	Addrs []string `json:"addrs,omitempty"`
	// Status is the node's liveness, derived by the aggregator from how recently
	// the controller heard from it: StatusAlive (reporting within the freshness
	// window), StatusStale (was reporting, now overdue) or StatusDown (onboarded
	// but silent). Empty means alive — the UI treats it as such. This is what
	// surfaces an onboarded-but-not-alive node as a dimmed/crossed-out vertex
	// instead of letting it silently vanish from the graph (#29).
	Status string `json:"status,omitempty"`
	// LastSeen is the unix time (seconds) the controller last had a signal from
	// this node: its own most recent pushed report, or — for a node that has gone
	// quiet — its inventory record. 0/omitted when unknown.
	LastSeen int64 `json:"last_seen,omitempty"`
}

// Node liveness states (Node.Status), derived by the aggregator from report
// freshness against the alive/stale windows. Alive nodes participate fully in
// the graph; stale and down nodes are onboarded inventory the controller has
// stopped (or never started) hearing from, surfaced as isolated vertices.
const (
	StatusAlive = "alive"
	StatusStale = "stale"
	StatusDown  = "down"
)

// InventoryNode is an onboarded node the controller knows about (from its node
// inventory), independent of whether it is currently reporting. Merge unions
// these with live reports so a node that onboarded but went silent appears as a
// stale/down vertex rather than disappearing.
type InventoryNode struct {
	ID       string
	Label    string
	LastSeen int64
}

// Link kinds: the medium a mesh link runs over, driving solid (wired) vs dashed
// (wireless) rendering in the topology view.
const (
	LinkWired    = "wired"
	LinkWireless = "wireless"
)

// Link is a mesh link between two nodes with its batman-adv transmit quality
// (TQ, 0-255; higher is better).
type Link struct {
	Source string `json:"source"`
	Target string `json:"target"`
	TQ     int    `json:"tq"`
	// Kind is the medium this link runs over: "wired" or "wireless" (empty when
	// unclassified). batman-adv may carry a neighbour over either, so it is
	// derived from the originator's outgoing batman hard interface.
	Kind string `json:"kind,omitempty"`
	// Signal is the RSSI (dBm, negative) of a wireless link; 0/omitted when the
	// link is wired or the signal is unknown.
	Signal int `json:"signal,omitempty"`
	// SpeedMbps is the negotiated speed of a wired link in Mbps (e.g. 1000,
	// 2500, 5000, 10000); 0/omitted when the link is wireless or unknown.
	SpeedMbps int `json:"speed_mbps,omitempty"`
}

// Client is a station associated to a node's AP, with its signal (RSSI, dBm).
type Client struct {
	MAC    string `json:"mac"`
	AP     string `json:"ap"`
	Signal int    `json:"signal"`
	Band   string `json:"band,omitempty"`
	TxRate int    `json:"tx_rate,omitempty"`
	RxRate int    `json:"rx_rate,omitempty"`
	// IP and Hostname are the client's DHCP-assigned address and name, resolved
	// on the controller from its DHCP leases (a member node holds none). They let
	// the view show a recognizable name instead of a raw MAC (#35). Empty when the
	// client has no lease — a static, self-addressed or transient station.
	IP       string `json:"ip,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}

// Graph is the assembled topology.
type Graph struct {
	Nodes   []Node   `json:"nodes"`
	Links   []Link   `json:"links"`
	Clients []Client `json:"clients"`
}

// Neighbor is a batman-adv originator reachable from this node.
type Neighbor struct {
	ID string // originator MAC (the remote node's bat0 address)
	TQ int
	// Nexthop is the MAC of the next hop towards this originator and Iface the
	// local outgoing batman hard interface; both are used to classify the link
	// (wired vs wireless) and look up its speed or RSSI.
	Nexthop string
	Iface   string
}

// MeshSource yields this node's mesh neighbors (batman-adv).
type MeshSource interface {
	Neighbors(ctx context.Context) ([]Neighbor, error)
}

// LinkMetrics describes the physical medium of a mesh link to a neighbour: its
// kind (wired/wireless), and either the wired speed (Mbps) or the wireless RSSI
// (dBm). Fields are best-effort — zero values mean "unknown".
type LinkMetrics struct {
	Kind      string
	SpeedMbps int
	Signal    int
}

// LinkMetricsSource classifies a mesh link given the local outgoing batman hard
// interface and the next-hop MAC towards the originator. Implementations
// degrade gracefully: an unclassifiable link yields a zero-value LinkMetrics.
type LinkMetricsSource interface {
	LinkMetrics(ctx context.Context, iface, nexthop string) LinkMetrics
}

// SelfAddrsSource yields the batman-adv addresses by which this node is known to
// its peers (typically its bat0 interface MAC), so the aggregator can map
// MAC-keyed links back to this node.
type SelfAddrsSource interface {
	SelfAddrs(ctx context.Context) []string
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
	selfID      string
	selfLabel   string
	mesh        MeshSource
	clients     ClientSource
	backhaul    BackhaulSource
	meshMode    MeshModeSource
	linkMetrics LinkMetricsSource
	selfAddrs   SelfAddrsSource
}

// Option configures optional collector sources added after the core ones.
type Option func(*Collector)

// WithLinkMetrics classifies each mesh link's medium (wired/wireless) and reads
// its speed or RSSI.
func WithLinkMetrics(s LinkMetricsSource) Option { return func(c *Collector) { c.linkMetrics = s } }

// WithSelfAddrs reports this node's batman-adv addresses so the aggregator can
// reconcile MAC-keyed links to this node's ID.
func WithSelfAddrs(s SelfAddrsSource) Option { return func(c *Collector) { c.selfAddrs = s } }

// NewCollector builds a collector. mesh, clients, backhaul and/or meshMode may
// be nil; further optional sources are supplied via Options.
func NewCollector(selfID, selfLabel string, mesh MeshSource, clients ClientSource, backhaul BackhaulSource, meshMode MeshModeSource, opts ...Option) *Collector {
	if selfLabel == "" {
		selfLabel = selfID
	}
	c := &Collector{selfID: selfID, selfLabel: selfLabel, mesh: mesh, clients: clients, backhaul: backhaul, meshMode: meshMode}
	for _, opt := range opts {
		opt(c)
	}
	return c
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

	// This node's own batman addresses, so a peer's MAC-keyed link to us can be
	// rewritten to our node ID during aggregation.
	if c.selfAddrs != nil {
		g.Nodes[0].Addrs = c.selfAddrs.SelfAddrs(ctx)
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
				link := Link{Source: c.selfID, Target: n.ID, TQ: n.TQ}
				if c.linkMetrics != nil {
					m := c.linkMetrics.LinkMetrics(ctx, n.Iface, n.Nexthop)
					link.Kind = m.Kind
					link.Signal = m.Signal
					link.SpeedMbps = m.SpeedMbps
				}
				g.Links = append(g.Links, link)
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
