package api

import (
	"encoding/json"
	"net/http"
)

type activeHomeResponse struct {
	HomeID string `json:"home_id"`
}

type activeHomeRequest struct {
	HomeID string `json:"home_id"`
}

func (h *apiHandler) getActiveHome(w http.ResponseWriter, r *http.Request) {
	homeID, err := h.store.GetActiveHome(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, activeHomeResponse{HomeID: homeID})
}

// setActiveHome explicitly selects which Home the device is active in,
// overriding automatic selection. The Home must be known to this device.
func (h *apiHandler) setActiveHome(w http.ResponseWriter, r *http.Request) {
	var req activeHomeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if req.HomeID == "" {
		respondError(w, http.StatusBadRequest, errMissingFields("home_id"))
		return
	}

	if _, err := h.store.GetHome(r.Context(), req.HomeID); err != nil {
		respondEnrollError(w, err) // maps ErrNotFound -> 404
		return
	}

	if err := h.store.SetActiveHome(r.Context(), req.HomeID); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, activeHomeResponse{HomeID: req.HomeID})
}
