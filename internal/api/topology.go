package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/and-elf/omm/internal/topology"
)

// getTopology returns the mesh topology: this node's local view (batman TQ +
// client RSSI) merged with fresh reports pushed by member nodes, plus onboarded
// nodes that are not currently reporting (surfaced as stale/down vertices from
// the node inventory so they don't silently vanish — #29).
func (h *apiHandler) getTopology(w http.ResponseWriter, r *http.Request) {
	local := h.topology.Collect(r.Context())
	writeJSON(w, http.StatusOK, h.topoAgg.Merge(local, h.nodeInventory(r.Context())...))
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
		inv = append(inv, topology.InventoryNode{ID: n.ID, Label: n.ID, LastSeen: n.LastSeen})
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
