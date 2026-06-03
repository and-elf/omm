package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/and-elf/omm/internal/storage"
)

type setupResponse struct {
	SetupComplete bool   `json:"setup_complete"`
	NodeID        string `json:"node_id"`
	Serial        string `json:"serial"`
	HomeID        string `json:"home_id"`
	HomeName      string `json:"home_name"`
}

// getSetup reports onboarding state and this device's identity/home, so the PWA
// can decide whether to show the first-boot wizard.
func (h *apiHandler) getSetup(w http.ResponseWriter, r *http.Request) {
	complete, err := h.store.GetSetupComplete(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	resp := setupResponse{SetupComplete: complete, HomeID: h.selfHomeID, Serial: h.selfSerial}
	if h.self != nil {
		resp.NodeID = h.self.NodeID()
	}
	if h.selfHomeID != "" {
		if home, err := h.store.GetHome(r.Context(), h.selfHomeID); err == nil {
			resp.HomeName = home.Name
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// completeSetup marks onboarding as finished.
func (h *apiHandler) completeSetup(w http.ResponseWriter, r *http.Request) {
	if err := h.store.SetSetupComplete(r.Context(), true); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	// The device is now claimed: tear down the first-boot setup AP if one is
	// running. Best-effort — setup is already recorded complete, so a teardown
	// failure must not fail the request.
	if h.onSetupComplete != nil {
		if err := h.onSetupComplete(r.Context()); err != nil {
			log.Printf("setup-complete hook failed (non-fatal): %v", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"setup_complete": true})
}

// provisionUplinkHandler joins an unclaimed node to a home WiFi network as a
// station so a wireless-only node gains a route to its controller and can
// enroll. Refused once the device is claimed — the applied profile then owns the
// node's network config.
func (h *apiHandler) provisionUplinkHandler(w http.ResponseWriter, r *http.Request) {
	complete, err := h.store.GetSetupComplete(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	if complete {
		respondError(w, http.StatusConflict, errors.New("device already claimed; uplink is managed by its profile"))
		return
	}

	var req struct {
		SSID     string `json:"ssid"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if req.SSID == "" {
		respondError(w, http.StatusBadRequest, errors.New("ssid is required"))
		return
	}

	if err := h.provisionUplink(r.Context(), req.SSID, req.Password); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"provisioned": true})
}

// updateHome renames/updates an existing Home (used by "Create New Home" in the
// wizard to name the device's own Home).
func (h *apiHandler) updateHome(w http.ResponseWriter, r *http.Request) {
	homeID := chi.URLParam(r, "homeID")

	home, err := h.store.GetHome(r.Context(), homeID)
	if err != nil {
		if err == storage.ErrNotFound {
			respondError(w, http.StatusNotFound, err)
			return
		}
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	var update struct {
		Name       *string `json:"name"`
		Controller *string `json:"controller"`
	}
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if update.Name != nil {
		home.Name = *update.Name
	}
	if update.Controller != nil {
		home.Controller = *update.Controller
	}

	if err := h.store.UpdateHome(r.Context(), home); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, home)
}
