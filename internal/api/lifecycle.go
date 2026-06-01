package api

import (
	"fmt"
	"net/http"

	"github.com/and-elf/omm/internal/storage"
	"github.com/go-chi/chi/v5"
)

// deleteHome forgets a Home and everything scoped to it. It refuses to remove
// the Home the device is currently active in — the operator must switch to
// another Home (or factory-reset) first, so we never leave a dangling
// active-home pointer or strip the running configuration out from under the
// device.
func (h *apiHandler) deleteHome(w http.ResponseWriter, r *http.Request) {
	homeID := chi.URLParam(r, "homeID")

	active, err := h.store.GetActiveHome(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	if active == homeID {
		respondError(w, http.StatusConflict, fmt.Errorf("cannot delete the active Home; switch to another Home first"))
		return
	}

	if err := h.store.DeleteHome(r.Context(), homeID); err != nil {
		if err == storage.ErrNotFound {
			respondError(w, http.StatusNotFound, err)
			return
		}
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteNode decommissions a node, removing it and its enrollment record.
func (h *apiHandler) deleteNode(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "nodeID")
	if err := h.store.DeleteNode(r.Context(), nodeID); err != nil {
		if err == storage.ErrNotFound {
			respondError(w, http.StatusNotFound, err)
			return
		}
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reset factory-resets the daemon: it clears all stored state and returns the
// device to its just-installed, unconfigured condition. Also used to reset
// state between integration/e2e runs that reuse the same container.
func (h *apiHandler) reset(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Reset(r.Context()); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
