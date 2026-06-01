package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/and-elf/omm/internal/enrollment"
	"github.com/and-elf/omm/internal/storage"
)

func (h *apiHandler) enrollRequest(w http.ResponseWriter, r *http.Request) {
	var in enrollment.RequestInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	result, err := h.enrollment.Request(r.Context(), in)
	if err != nil {
		respondEnrollError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *apiHandler) enrollVerify(w http.ResponseWriter, r *http.Request) {
	var in enrollment.VerifyInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	result, err := h.enrollment.Verify(r.Context(), in)
	if err != nil {
		respondEnrollError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *apiHandler) enrollStatus(w http.ResponseWriter, r *http.Request) {
	result, err := h.enrollment.Get(r.Context(), chi.URLParam(r, "enrollmentID"))
	if err != nil {
		respondEnrollError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *apiHandler) enrollAck(w http.ResponseWriter, r *http.Request) {
	result, err := h.enrollment.Ack(r.Context(), chi.URLParam(r, "enrollmentID"))
	if err != nil {
		respondEnrollError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *apiHandler) adoptNode(w http.ResponseWriter, r *http.Request) {
	result, err := h.enrollment.Adopt(r.Context(), chi.URLParam(r, "nodeID"))
	if err != nil {
		respondEnrollError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// respondEnrollError maps enrollment/storage errors to HTTP status codes.
func respondEnrollError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		respondError(w, http.StatusNotFound, err)
	case errors.Is(err, enrollment.ErrIdentityMismatch):
		respondError(w, http.StatusBadRequest, err)
	case errors.Is(err, enrollment.ErrInvalidSignature):
		respondError(w, http.StatusUnauthorized, err)
	case errors.Is(err, enrollment.ErrNotAdoptable):
		respondError(w, http.StatusConflict, err)
	default:
		respondError(w, http.StatusInternalServerError, err)
	}
}
