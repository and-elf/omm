package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/and-elf/omm/internal/storage"
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

	// Selecting a Home is only meaningful if its profile is pushed to UCI —
	// this is what lets a portable node move between Homes without a factory
	// reset (see doc/profiles.md "Profile Switching"). A freshly created Home
	// has no profile yet, so a missing profile is not an error: skip the push
	// (there is nothing to apply) and still report the selection as successful,
	// mirroring meshd's auto-select. Any other apply error is fatal.
	if h.profileManager != nil {
		if err := h.profileManager.ApplyProfileForHome(r.Context(), req.HomeID); err != nil {
			if !errors.Is(err, storage.ErrNotFound) {
				respondError(w, http.StatusInternalServerError, fmt.Errorf("apply profile: %w", err))
				return
			}
			log.Printf("set active home %s: no profile to apply yet (non-fatal)", req.HomeID)
		}
	}

	writeJSON(w, http.StatusOK, activeHomeResponse{HomeID: req.HomeID})
}
