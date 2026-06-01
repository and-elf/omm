package api

import (
	"encoding/json"
	"net/http"

	"github.com/and-elf/omm/internal/topology"
)

// getTopology returns the mesh topology: this node's local view (batman TQ +
// client RSSI) merged with fresh reports pushed by member nodes.
func (h *apiHandler) getTopology(w http.ResponseWriter, r *http.Request) {
	local := h.topology.Collect(r.Context())
	writeJSON(w, http.StatusOK, h.topoAgg.Merge(local))
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
