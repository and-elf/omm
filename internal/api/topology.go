package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/and-elf/omm/internal/topology"
)

// getTopology returns the mesh topology: this node's local view (batman TQ +
// client RSSI) merged with fresh reports pushed by member nodes, plus onboarded
// nodes that are not currently reporting (surfaced as stale/down vertices from
// the node inventory so they don't silently vanish — #29).
func (h *apiHandler) getTopology(w http.ResponseWriter, r *http.Request) {
	// Collect under a context detached from the request's cancellation. The LuCI
	// ubus transport (rpcd → busybox nc) half-closes the socket once the request
	// is written; Go's server reads that as the client going away and cancels
	// r.Context(). Passing it to the collector would kill its batctl/iw
	// subprocesses mid-run and return an empty graph, even though nc is still
	// reading the response. We run to completion under our own timeout instead
	// (internal/ubus.Call detaches downstream work for the same reason).
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Second)
	defer cancel()
	local := h.topology.Collect(ctx)
	graph := h.topoAgg.Merge(local, h.nodeInventory(ctx)...)
	// Enrich clients with their DHCP-assigned IP/hostname (this controller runs
	// the home's authoritative DHCP), so the view labels them by name not MAC (#35).
	if h.leases != nil {
		graph = topology.LabelClients(graph, h.leases.Leases(ctx))
	}
	writeJSON(w, http.StatusOK, graph)
}

// nodeInventory lists the onboarded nodes belonging to this controller's home so
// Merge can surface ones that are not currently reporting. It is scoped to the
// controlled home (CurrentHome == selfHomeID) so a controller never shows nodes
// from another home as down. Errors degrade to an empty inventory — the live
// graph is still served.
func (h *apiHandler) nodeInventory(ctx context.Context) []topology.InventoryNode {
	if h.store == nil {
		return nil
	}
	nodes, err := h.store.ListNodes(ctx)
	if err != nil {
		return nil
	}
	var selfID string
	if h.self != nil {
		selfID = h.self.NodeID()
	}
	inv := make([]topology.InventoryNode, 0, len(nodes))
	for _, n := range nodes {
		// Only this home's nodes, and never the controller itself (the live "self"
		// vertex already represents it).
		if n.ID == selfID || n.CurrentHome != h.selfHomeID {
			continue
		}
		// Label by the friendly serial so a silent node renders as a recognizable
		// name rather than its raw 64-char node ID; fall back to the ID when a
		// record predates serial capture.
		label := n.Serial
		if label == "" {
			label = n.ID
		}
		inv = append(inv, topology.InventoryNode{ID: n.ID, Label: label, LastSeen: n.LastSeen})
	}
	return inv
}

type topologyReport struct {
	NodeID string         `json:"node_id"`
	Graph  topology.Graph `json:"graph"`
}

// reportTopology ingests a member node's local topology for aggregation.
func (h *apiHandler) reportTopology(w http.ResponseWriter, r *http.Request) {
	var report topologyReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if report.NodeID == "" {
		respondError(w, http.StatusBadRequest, errMissingFields("node_id"))
		return
	}
	h.topoAgg.Ingest(report.NodeID, report.Graph)
	w.WriteHeader(http.StatusAccepted)
}
