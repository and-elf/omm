package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/and-elf/omm/internal/client"
	"github.com/and-elf/omm/internal/enrollment"
	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
)

// joinTimeout bounds the enrollment handshake when driven through the API. It
// must stay under rpcd/ubus's request timeout (~30s) so the LuCI call gets a
// real result rather than a transport timeout; auto-adopt completes in well
// under this.
const joinTimeout = 25 * time.Second

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
	// Tell the enrollment service whether this peer is on the controller's own
	// LAN, so the AdoptOnlink policy trusts the verifiable source address rather
	// than the node's self-declared backhaul.
	ctx := enrollment.WithPeerOnLink(r.Context(), h.peerOnLink(r))
	result, err := h.enrollment.Verify(ctx, in)
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

// enrollmentSummary is the list view of an enrollment (no key/challenge).
type enrollmentSummary struct {
	ID        string                  `json:"id"`
	NodeID    string                  `json:"node_id"`
	Serial    string                  `json:"serial"`
	Status    models.EnrollmentStatus `json:"status"`
	HomeID    string                  `json:"home_id"`
	CreatedAt int64                   `json:"created_at"`
}

type enrollmentsResponse struct {
	Enrollments []enrollmentSummary `json:"enrollments"`
}

// listEnrollments returns enrollments, defaulting to those awaiting approval.
// An optional ?status= filter (e.g. "" / "all" / a specific status) overrides.
func (h *apiHandler) listEnrollments(w http.ResponseWriter, r *http.Request) {
	status := models.EnrollmentPendingApproval
	switch s := r.URL.Query().Get("status"); s {
	case "":
		// default: pending approval
	case "all":
		status = ""
	default:
		status = models.EnrollmentStatus(s)
	}

	enrollments, err := h.enrollment.List(r.Context(), status)
	if err != nil {
		respondEnrollError(w, err)
		return
	}
	out := make([]enrollmentSummary, 0, len(enrollments))
	for _, e := range enrollments {
		out = append(out, enrollmentSummary{
			ID: e.ID, NodeID: e.NodeID, Serial: e.Serial,
			Status: e.Status, HomeID: e.HomeID, CreatedAt: e.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, enrollmentsResponse{Enrollments: out})
}

func (h *apiHandler) rejectNode(w http.ResponseWriter, r *http.Request) {
	result, err := h.enrollment.Reject(r.Context(), chi.URLParam(r, "nodeID"))
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

// joinInput is the body of POST /enroll/join.
type joinInput struct {
	ControllerURL string `json:"controller_url"`
	Serial        string `json:"serial"`
}

// enrollJoin makes this daemon enroll into another controller as a node, using
// its own device identity. The caller (e.g. a test or operator) decides the
// topology; a daemon can be a controller for its own Home and join others.
func (h *apiHandler) enrollJoin(w http.ResponseWriter, r *http.Request) {
	var in joinInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if in.ControllerURL == "" {
		respondError(w, http.StatusBadRequest, errMissingFields("controller_url"))
		return
	}
	serial := in.Serial
	if serial == "" {
		serial = h.selfSerial
	}

	// Detach from the request context. The LuCI rpcd plugin reaches meshd over
	// busybox nc, which half-closes the socket after writing the request — that
	// cancels r's context, which would abort the outbound enrollment handshake
	// mid-flight ("context canceled"). Run the join on a fresh, bounded context.
	joinCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), joinTimeout)
	defer cancel()

	result, err := client.JoinAndRecord(joinCtx, h.self, in.ControllerURL, serial, h.store, client.Options{})
	if err != nil {
		respondError(w, http.StatusBadGateway, err)
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
