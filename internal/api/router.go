package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/profiles"
	"github.com/and-elf/omm/internal/storage"
	"github.com/and-elf/omm/web"
)

type apiHandler struct {
	store          storage.Store
	profileManager profiles.ProfileManager
}

type statusResponse struct {
	Status string `json:"status"`
}

type nodesResponse struct {
	Nodes []models.Node `json:"nodes"`
}

type homesResponse struct {
	Homes []models.Home `json:"homes"`
}

type profileResponse struct {
	Profile models.Profile `json:"profile"`
}

func NewRouter(store storage.Store, profileManager profiles.ProfileManager) http.Handler {
	r := chi.NewRouter()
	h := &apiHandler{store: store, profileManager: profileManager}

	r.Get("/health", healthHandler)
	r.Get("/status", statusHandler)
	r.Get("/homes", h.listHomes)
	r.Post("/homes", h.createHome)
	r.Get("/homes/{homeID}", h.getHome)
	r.Get("/homes/{homeID}/profile", h.getProfile)
	r.Post("/homes/{homeID}/profile", h.createProfile)
	r.Get("/nodes", h.listNodes)
	r.Post("/nodes", h.createNode)
	r.Get("/nodes/{nodeID}", h.getNode)

	// Serve the embedded Progressive Web App for any non-API route. This is
	// registered last so the specific API routes above always take precedence.
	r.Handle("/*", web.NewHandler(web.DistFS()))

	return r
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, statusResponse{Status: "ready"})
}

func (h *apiHandler) listHomes(w http.ResponseWriter, r *http.Request) {
	homes, err := h.store.ListHomes(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, homesResponse{Homes: homes})
}

func (h *apiHandler) createHome(w http.ResponseWriter, r *http.Request) {
	var home models.Home
	if err := json.NewDecoder(r.Body).Decode(&home); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}

	if home.ID == "" || home.Name == "" {
		respondError(w, http.StatusBadRequest, errMissingFields("id", "name"))
		return
	}

	home.LastSeen = time.Now().Unix()
	if err := h.store.CreateHome(r.Context(), home); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusCreated, home)
}

func (h *apiHandler) getHome(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, home)
}

func (h *apiHandler) getProfile(w http.ResponseWriter, r *http.Request) {
	homeID := chi.URLParam(r, "homeID")
	profile, err := h.store.GetProfile(r.Context(), homeID)
	if err != nil {
		if err == storage.ErrNotFound {
			respondError(w, http.StatusNotFound, err)
			return
		}
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, profileResponse{Profile: profile})
}

func (h *apiHandler) createProfile(w http.ResponseWriter, r *http.Request) {
	homeID := chi.URLParam(r, "homeID")
	var profile models.Profile
	if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if profile.HomeID == "" {
		profile.HomeID = homeID
	}
	if profile.HomeID != homeID {
		respondError(w, http.StatusBadRequest, fmt.Errorf("path homeID does not match profile home_id"))
		return
	}

	if profile.HomeID == "" {
		respondError(w, http.StatusBadRequest, errMissingFields("home_id"))
		return
	}

	if err := h.store.CreateOrUpdateProfile(r.Context(), profile); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	if h.profileManager != nil {
		if err := h.profileManager.ApplyProfile(r.Context(), profile); err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Errorf("apply profile: %w", err))
			return
		}
	}

	writeJSON(w, http.StatusCreated, profileResponse{Profile: profile})
}

func (h *apiHandler) listNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.store.ListNodes(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, nodesResponse{Nodes: nodes})
}

func (h *apiHandler) createNode(w http.ResponseWriter, r *http.Request) {
	var node models.Node
	if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}

	if node.ID == "" || node.Serial == "" {
		respondError(w, http.StatusBadRequest, errMissingFields("id", "serial"))
		return
	}

	node.LastSeen = time.Now().Unix()
	if err := h.store.CreateNode(r.Context(), node); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusCreated, node)
}

func (h *apiHandler) getNode(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "nodeID")
	node, err := h.store.GetNode(r.Context(), nodeID)
	if err != nil {
		if err == storage.ErrNotFound {
			respondError(w, http.StatusNotFound, err)
			return
		}
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, node)
}

func errMissingFields(fields ...string) error {
	return fmt.Errorf("missing required fields: %v", fields)
}

func respondError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
