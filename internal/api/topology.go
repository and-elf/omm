package api

import "net/http"

// getTopology returns the current mesh topology (nodes, links with batman TQ,
// and clients with RSSI) from this node's point of view.
func (h *apiHandler) getTopology(w http.ResponseWriter, r *http.Request) {
	graph := h.topology.Collect(r.Context())
	writeJSON(w, http.StatusOK, graph)
}
