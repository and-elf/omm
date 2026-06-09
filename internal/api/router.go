package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/and-elf/omm/internal/discovery"
	"github.com/and-elf/omm/internal/enrollment"
	"github.com/and-elf/omm/internal/identity"
	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/profiles"
	"github.com/and-elf/omm/internal/storage"
	"github.com/and-elf/omm/internal/topology"
	"github.com/and-elf/omm/web"
)

type apiHandler struct {
	store           storage.Store
	profileManager  profiles.ProfileManager
	enrollment      *enrollment.Service
	self            *identity.Identity
	selfSerial      string
	selfHomeID      string
	topology        *topology.Collector
	topoAgg         *topology.Aggregator
	signals         SignalSource
	scan            Scanner
	meshClientAuth  bool
	devCORS         bool
	onSetupComplete func(ctx context.Context) error
	provisionUplink func(ctx context.Context, ssid, key string) error
	// onLink reports whether an IP is on one of this controller's own LAN
	// subnets; used to gate the AdoptOnlink enrollment policy. Defaults to a
	// net.Interfaces-based check; injectable for tests.
	onLink func(net.IP) bool
}

// peerOnLink reports whether the request's source address is on the controller's
// own LAN (on-link). Used so unattended adoption trusts a verifiable network
// position rather than the node's word.
func (h *apiHandler) peerOnLink(r *http.Request) bool {
	if h.onLink == nil {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return h.onLink(ip)
}

// defaultOnLink reports whether ip falls within any of this host's own
// interface subnets — a verifiable proxy for "physically on my LAN".
func defaultOnLink(ip net.IP) bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, ifi := range ifaces {
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.Contains(ip) {
				return true
			}
		}
	}
	return false
}

// Scanner discovers nearby controllers (announced Homes) so the UI can present
// a pick-list instead of a typed URL.
type Scanner func(ctx context.Context) ([]discovery.Announcement, error)

// SignalSource provides observed peer RSSI (dBm) keyed by lower-case MAC. The
// topology UbusClients satisfies it, so home selection reuses the same source.
type SignalSource interface {
	SignalByMAC(ctx context.Context) (map[string]int, error)
}

// Option customizes the router/handler.
type Option func(*apiHandler)

// WithEnrollment registers the controller-side enrollment endpoints, letting
// this daemon act as a controller for its Home.
func WithEnrollment(svc *enrollment.Service) Option {
	return func(h *apiHandler) { h.enrollment = svc }
}

// WithSelf supplies this daemon's device identity, enabling the /enroll/join
// endpoint so it can enroll into other controllers as a node.
func WithSelf(id *identity.Identity, serial string) Option {
	return func(h *apiHandler) {
		h.self = id
		h.selfSerial = serial
	}
}

// WithSelfHome identifies the Home this daemon controls, used by the setup
// endpoints to report and name the device's own Home.
func WithSelfHome(homeID string) Option {
	return func(h *apiHandler) { h.selfHomeID = homeID }
}

// WithTopology registers the topology endpoints backed by collector: GET
// /topology serves this node's local view merged with reports from member
// nodes, and POST /topology/report ingests those reports.
func WithTopology(collector *topology.Collector) Option {
	return func(h *apiHandler) {
		h.topology = collector
		h.topoAgg = topology.NewAggregator(90*time.Second, nil)
	}
}

// WithSignalSource enables the GET /home-selection endpoint, feeding observed
// RSSI into the home-selection policy.
func WithSignalSource(src SignalSource) Option {
	return func(h *apiHandler) { h.signals = src }
}

// WithScanner enables GET /scan, which discovers nearby controllers.
func WithScanner(scan Scanner) Option {
	return func(h *apiHandler) { h.scan = scan }
}

// WithSetupCompleteHook registers a callback run after onboarding is marked
// complete (POST /setup/complete). The daemon uses it to tear down the
// first-boot setup AP once the device is claimed. Hook errors are reported but
// do not fail the request — setup is already recorded as complete.
func WithSetupCompleteHook(hook func(ctx context.Context) error) Option {
	return func(h *apiHandler) { h.onSetupComplete = hook }
}

// WithUplinkProvisioner enables POST /setup/uplink, letting the companion app
// hand an unclaimed node home-WiFi credentials so a wireless-only node can join
// the home network as a station and reach its controller to enroll. The hook
// brings up the station interface (e.g. setupap.EnableUplink).
func WithUplinkProvisioner(hook func(ctx context.Context, ssid, key string) error) Option {
	return func(h *apiHandler) { h.provisionUplink = hook }
}

// WithDevCORS adds permissive CORS headers (any origin) and answers preflight
// OPTIONS on the management routes, so a companion app served from another
// origin can call the API directly. Development only — the management API is
// unauthenticated.
func WithDevCORS() Option {
	return func(h *apiHandler) { h.devCORS = true }
}

// devCORSMiddleware sets permissive CORS headers and short-circuits preflight.
func devCORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// WithMeshClientAuth requires a verified mesh client certificate on the mesh
// router's post-enrollment routes (everything except the bootstrap /enroll/*
// endpoints, which a node reaches before it has a cert). Set this only when the
// mesh listener actually serves mutual TLS.
func WithMeshClientAuth() Option {
	return func(h *apiHandler) { h.meshClientAuth = true }
}

// protected wraps a handler so it requires a verified mesh client certificate
// when mesh client-auth is enabled; otherwise it is a no-op.
func (h *apiHandler) protected(next http.HandlerFunc) http.HandlerFunc {
	if !h.meshClientAuth {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
			respondError(w, http.StatusUnauthorized, fmt.Errorf("mesh: client certificate required"))
			return
		}
		next(w, r)
	}
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

func newHandler(store storage.Store, profileManager profiles.ProfileManager, opts ...Option) *apiHandler {
	h := &apiHandler{store: store, profileManager: profileManager}
	for _, opt := range opts {
		opt(h)
	}
	if h.onLink == nil {
		h.onLink = defaultOnLink
	}
	return h
}

// WithOnLink overrides the on-link check (which gates the AdoptOnlink policy);
// used by tests to simulate a peer on/off the controller's LAN.
func WithOnLink(fn func(net.IP) bool) Option {
	return func(h *apiHandler) { h.onLink = fn }
}

// NewRouter serves both planes on one handler — the combined mode used by tests
// and by `MESHD_HTTP_ADDR` deployments. Split deployments use NewManagementRouter
// and NewMeshRouter instead.
func NewRouter(store storage.Store, profileManager profiles.ProfileManager, opts ...Option) http.Handler {
	r := chi.NewRouter()
	h := newHandler(store, profileManager, opts...)
	if h.devCORS {
		r.Use(devCORSMiddleware)
	}
	// Management routes register GET /homes/{homeID} and the PWA catch-all; add
	// only the mesh-plane routes that management does not already cover, to
	// avoid duplicate (method, path) registrations.
	h.registerManagementRoutes(r)
	r.Get("/health", healthHandler)
	if h.topology != nil {
		r.Post("/topology/report", h.reportTopology)
	}
	if h.enrollment != nil {
		r.Post("/enroll/request", h.enrollRequest)
		r.Post("/enroll/verify", h.enrollVerify)
		r.Get("/enroll/{enrollmentID}", h.enrollStatus)
		r.Post("/enroll/{enrollmentID}/ack", h.enrollAck)
	}
	return r
}

// NewManagementRouter serves only the admin/UI plane (intended to bind to
// localhost behind LuCI).
func NewManagementRouter(store storage.Store, profileManager profiles.ProfileManager, opts ...Option) http.Handler {
	r := chi.NewRouter()
	h := newHandler(store, profileManager, opts...)
	if h.devCORS {
		r.Use(devCORSMiddleware)
	}
	h.registerManagementRoutes(r)
	return r
}

// NewMeshRouter serves only the node-to-node control plane (stays network
// reachable on every controller).
func NewMeshRouter(store storage.Store, profileManager profiles.ProfileManager, opts ...Option) http.Handler {
	r := chi.NewRouter()
	newHandler(store, profileManager, opts...).registerMeshRoutes(r)
	return r
}

// registerMeshRoutes registers the endpoints remote nodes call: enrollment
// (inbound), topology reporting, joined-Home metadata, and health.
func (h *apiHandler) registerMeshRoutes(r chi.Router) {
	r.Get("/health", healthHandler) // liveness, unauthenticated
	// Post-enrollment endpoints require a verified client cert (when mesh
	// client-auth is on); the bootstrap /enroll/* endpoints do not, since a
	// node reaches them before it has one.
	r.Get("/homes/{homeID}", h.protected(h.getHome))            // nodes fetch joined-Home metadata
	r.Get("/homes/{homeID}/profile", h.protected(h.getProfile)) // nodes pull profile updates
	if h.topology != nil {
		r.Post("/topology/report", h.protected(h.reportTopology))
	}
	if h.enrollment != nil {
		r.Post("/enroll/request", h.enrollRequest)
		r.Post("/enroll/verify", h.enrollVerify)
		r.Get("/enroll/{enrollmentID}", h.enrollStatus)
		r.Post("/enroll/{enrollmentID}/ack", h.enrollAck)
	}
}

// registerManagementRoutes registers the admin/UI plane, including the PWA
// catch-all (last, so specific API routes win).
func (h *apiHandler) registerManagementRoutes(r chi.Router) {
	r.Get("/status", h.status)
	r.Get("/setup", h.getSetup)
	r.Post("/setup/complete", h.completeSetup)
	if h.provisionUplink != nil {
		r.Post("/setup/uplink", h.provisionUplinkHandler)
	}
	r.Get("/homes", h.listHomes)
	r.Post("/homes", h.createHome)
	r.Get("/homes/{homeID}", h.getHome)
	r.Put("/homes/{homeID}", h.updateHome)
	r.Delete("/homes/{homeID}", h.deleteHome)
	r.Get("/homes/{homeID}/profile", h.getProfile)
	r.Post("/homes/{homeID}/profile", h.createProfile)
	r.Get("/nodes", h.listNodes)
	r.Post("/nodes", h.createNode)
	r.Get("/nodes/{nodeID}", h.getNode)
	r.Delete("/nodes/{nodeID}", h.deleteNode)
	r.Get("/active-home", h.getActiveHome)
	r.Put("/active-home", h.setActiveHome)
	r.Post("/reset", h.reset)

	if h.topology != nil {
		r.Get("/topology", h.getTopology)
	}
	if h.signals != nil {
		r.Get("/home-selection", h.getHomeSelection)
	}
	if h.scan != nil {
		r.Get("/scan", h.scanControllers)
	}
	if h.enrollment != nil {
		r.Get("/enroll", h.listEnrollments)
		r.Post("/nodes/{nodeID}/adopt", h.adoptNode)
		r.Post("/nodes/{nodeID}/reject", h.rejectNode)
	}
	// A daemon with a device identity can enroll into other controllers as a
	// node, independently of hosting its own Home.
	if h.self != nil {
		r.Post("/enroll/join", h.enrollJoin)
	}

	// Serve the embedded Progressive Web App for any non-API route, last so the
	// specific API routes above always take precedence.
	r.Handle("/*", web.NewHandler(web.DistFS()))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

// managementStatus reports daemon readiness plus the applied wireless-backhaul
// outcome (802.11s mesh vs degraded multi-AP), so LuCI can show whether the
// mesh formed and, when degraded, why and how to fix it.
type managementStatusResponse struct {
	Status   string               `json:"status"`
	Backhaul models.BackhaulState `json:"backhaul"`
}

func (h *apiHandler) status(w http.ResponseWriter, r *http.Request) {
	state := models.BackhaulState{Mode: models.BackhaulModeUnknown}
	if h.store != nil {
		if s, err := h.store.GetBackhaulState(r.Context()); err == nil {
			state = s
		}
	}
	writeJSON(w, http.StatusOK, managementStatusResponse{Status: "ready", Backhaul: state})
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

	// 404 only when the Home itself is unknown.
	if _, err := h.store.GetHome(r.Context(), homeID); err != nil {
		if err == storage.ErrNotFound {
			respondError(w, http.StatusNotFound, fmt.Errorf("home %q not found", homeID))
			return
		}
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	profile, err := h.store.GetProfile(r.Context(), homeID)
	if err != nil {
		if err == storage.ErrNotFound {
			// A Home with no profile yet returns an empty, editable profile
			// (200) rather than 404, so the UI presents a blank form. It also
			// survives the LuCI ubus transport, which flattens any error body
			// to HTTP 400 and so cannot be distinguished from a real failure.
			writeJSON(w, http.StatusOK, profileResponse{Profile: models.Profile{HomeID: homeID}})
			return
		}
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, profileResponse{Profile: profile})
}

// applyContext detaches profile application from the request's lifetime. The
// profile is already persisted; pushing it to UCI is a multi-call, several-
// second sequence (uci add/set/commit + netifd/wireless reload) that must not
// be cancelled if the client connection drops. Notably the LuCI rpcd/nc
// transport closes the socket as soon as the plugin has the request body,
// cancelling r.Context() and killing the in-flight `ubus` exec ("context
// canceled") before the radio is ever configured. Keep request values for
// logging but drop cancellation, with a generous upper bound.
func applyContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(r.Context()), 90*time.Second)
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
		applyCtx, cancel := applyContext(r)
		defer cancel()
		if err := h.profileManager.ApplyProfile(applyCtx, profile); err != nil {
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
