package api

import (
	"net/http"

	"github.com/and-elf/omm/internal/selection"
)

type homeSelectionCandidate struct {
	HomeID         string `json:"home_id"`
	Signal         int    `json:"signal"`
	LastActive     int64  `json:"last_active"`
	SelfControlled bool   `json:"self_controlled"`
}

type homeSelectionResponse struct {
	RecommendedHomeID string                   `json:"recommended_home_id"`
	ActiveHomeID      string                   `json:"active_home_id"`
	Candidates        []homeSelectionCandidate `json:"candidates"`
}

// getHomeSelection reports which Home the device should activate, combining
// known Homes, the explicitly-set active Home, and live RSSI from the signal
// source (reusing the topology UbusClients).
func (h *apiHandler) getHomeSelection(w http.ResponseWriter, r *http.Request) {
	homes, err := h.store.ListHomes(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	activeHome, _ := h.store.GetActiveHome(r.Context())

	signals := selection.Signals{}
	if observed, err := h.signals.SignalByMAC(r.Context()); err == nil {
		for mac, rssi := range observed {
			signals[mac] = rssi
		}
	}

	candidates := selection.Candidates(homes, h.selfHomeID, signals)

	resp := homeSelectionResponse{ActiveHomeID: activeHome}
	for _, c := range candidates {
		resp.Candidates = append(resp.Candidates, homeSelectionCandidate{
			HomeID: c.HomeID, Signal: c.Signal, LastActive: c.LastActive, SelfControlled: c.SelfControlled,
		})
	}
	if best, ok := selection.Recommend(homes, h.selfHomeID, activeHome, signals); ok {
		resp.RecommendedHomeID = best.HomeID
	}

	writeJSON(w, http.StatusOK, resp)
}
