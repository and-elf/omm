package main

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/and-elf/omm/internal/api"
	"github.com/and-elf/omm/internal/client"
	"github.com/and-elf/omm/internal/config"
	"github.com/and-elf/omm/internal/deviceled"
	"github.com/and-elf/omm/internal/discovery"
	"github.com/and-elf/omm/internal/enrollment"
	"github.com/and-elf/omm/internal/identity"
	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/profiles"
	"github.com/and-elf/omm/internal/selection"
	"github.com/and-elf/omm/internal/setupap"
	"github.com/and-elf/omm/internal/storage"
	"github.com/and-elf/omm/internal/topology"
	"github.com/and-elf/omm/internal/ubus"
	"github.com/and-elf/omm/internal/uci"
)

func main() {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Every daemon has a device identity, so it can enroll into other
	// controllers as a node.
	id, err := identity.LoadOrCreate(cfg.IdentityDir)
	if err != nil {
		log.Fatalf("failed to load device identity: %v", err)
	}

	db, err := storage.OpenDB(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()
	store := storage.NewStore(db)

	uciClient, err := uci.NewClient(uci.Options{SocketPath: cfg.UbusSocket, BinaryPath: cfg.UbusBinary})
	if err != nil {
		log.Fatalf("failed to create uci client: %v", err)
	}
	defer uciClient.Close()

	// This daemon's Home CA signs a leaf certificate for each node it adopts;
	// its certificate is the trust anchor nodes pin (TOFU) for mesh TLS.
	homeCA, err := identity.LoadOrCreateCA(cfg.IdentityDir, "home-"+cfg.HomeID)
	if err != nil {
		log.Fatalf("failed to load/create Home CA: %v", err)
	}
	caCertPEM := homeCA.CertificatePEM()

	// Ensure this daemon's own Home exists (with its mesh BSSID, so peers can
	// map our RSSI to this Home, and the CA cert nodes fetch when joining)
	// without clobbering an onboarding rename.
	bssid := resolveBSSID(cfg)
	switch home, err := store.GetHome(ctx, cfg.HomeID); {
	case err == storage.ErrNotFound:
		if err := store.CreateHome(ctx, models.Home{
			ID: cfg.HomeID, Name: cfg.HomeName, Controller: cfg.ControllerID,
			BSSID: bssid, Certificate: caCertPEM, LastSeen: time.Now().Unix(),
		}); err != nil {
			log.Printf("failed to create home: %v", err)
		}
	case err == nil:
		// Persist BSSID/CA changes via a full put so the (possibly renamed)
		// Name and other fields are preserved.
		changed := false
		if bssid != "" && home.BSSID != bssid {
			home.BSSID = bssid
			changed = true
		}
		if !bytes.Equal(home.Certificate, caCertPEM) {
			home.Certificate = caCertPEM
			changed = true
		}
		if changed {
			if err := store.CreateHome(ctx, home); err != nil {
				log.Printf("failed to update home: %v", err)
			}
		}
	}

	profileManager := profiles.NewManager(store, uciClient, profiles.Config{Radio: cfg.SetupAPRadio})
	enrollSvc := enrollment.NewService(store, enrollment.Options{HomeID: cfg.HomeID, AutoAdopt: cfg.AutoAdopt, CA: homeCA})

	// First-boot setup AP: while the device is unclaimed (setup not complete) it
	// broadcasts a known, label-printable SSID serving its open management API,
	// so a companion app can reach it before it has joined any network. It is
	// torn down once setup completes (see the WithSetupCompleteHook below).
	setupAP := setupap.New(uciClient, setupap.Config{
		Radio: cfg.SetupAPRadio,
		Key:   cfg.SetupAPKey,
	})

	// Topology collector: batman-adv link quality + hostapd client RSSI.
	ubusClient, err := ubus.NewClient(ubus.Options{SocketPath: cfg.UbusSocket, BinaryPath: cfg.UbusBinary})
	if err != nil {
		log.Fatalf("failed to create ubus client: %v", err)
	}
	defer ubusClient.Close()
	wifiClients := topology.UbusClients{Ubus: ubusClient, Interfaces: cfg.APInterfaces}
	collector := topology.NewCollector(id.NodeID(), cfg.Serial,
		topology.BatctlMesh{Interface: cfg.BatmanIface},
		wifiClients,
		topology.SysfsBackhaul{Iface: cfg.BackhaulIface},
	)

	// Passively cache controller announcements so /scan answers instantly.
	discoCache := discovery.NewCache(90*time.Second, nil)
	go func() {
		if err := discoCache.Listen(ctx, cfg.UDPListen); err != nil {
			log.Printf("discovery listen error: %v", err)
		}
	}()

	apiOpts := []api.Option{
		api.WithEnrollment(enrollSvc),
		api.WithSelf(id, cfg.Serial),
		api.WithSelfHome(cfg.HomeID),
		api.WithTopology(collector),
		api.WithSignalSource(wifiClients),
		api.WithSetupCompleteHook(func(ctx context.Context) error {
			// Device is now claimed: take down the first-boot setup AP (and the
			// uplink station, if one was provisioned during onboarding).
			return setupAP.Disable(ctx)
		}),
		api.WithUplinkProvisioner(func(ctx context.Context, ssid, key string) error {
			// Wireless-only onboarding: join the home network as a station so the
			// node gains a route to its controller and can enroll.
			return setupAP.EnableUplink(ctx, ssid, key)
		}),
		api.WithScanner(func(context.Context) ([]discovery.Announcement, error) {
			// Answer from the passively-maintained cache, dropping this
			// device's own Home.
			list := discoCache.List()
			out := list[:0]
			for _, a := range list {
				if a.HomeID != cfg.HomeID {
					out = append(out, a)
				}
			}
			return out, nil
		}),
	}

	if cfg.DevCORS {
		log.Printf("WARNING: MESHD_DEV_CORS enabled — management API allows any origin (development only)")
		apiOpts = append(apiOpts, api.WithDevCORS())
	}

	// Combined mode (MESHD_HTTP_ADDR set) serves both planes on one address over
	// plain HTTP. Split mode binds the management plane (admin/UI) and the mesh
	// control plane (node-to-node) separately, and the mesh plane runs mutual
	// TLS rooted in the Home CA.
	var servers []*http.Server
	scheme := "http"
	if cfg.Combined() {
		servers = []*http.Server{{Addr: cfg.HTTPAddr, Handler: api.NewRouter(store, profileManager, apiOpts...)}}
	} else {
		// Issue this controller a server leaf from its Home CA for the mesh
		// listener, and require verified client certs on post-enrollment routes.
		serverLeaf, err := homeCA.IssueCert(id.PublicKeyDER())
		if err != nil {
			log.Fatalf("issue mesh server certificate: %v", err)
		}
		meshTLS, err := identity.ServerTLSConfig(serverLeaf, id.PrivateKeyPEM(), caCertPEM)
		if err != nil {
			log.Fatalf("mesh tls config: %v", err)
		}
		meshOpts := append(append([]api.Option{}, apiOpts...), api.WithMeshClientAuth())
		servers = []*http.Server{
			{Addr: cfg.MgmtAddr, Handler: api.NewManagementRouter(store, profileManager, apiOpts...)},
			{Addr: cfg.MeshAddr, Handler: api.NewMeshRouter(store, profileManager, meshOpts...), TLSConfig: meshTLS},
		}
		scheme = "https"
	}

	// Announce this controller's presence for discovery. Nodes reach the mesh
	// control plane at the announced address, so it must be the mesh-facing one.
	apiURL := cfg.APIAdvertise
	if apiURL == "" {
		apiURL = scheme + "://" + cfg.AnnounceAddr()
	}
	go func() {
		ann := discovery.Announcement{HomeID: cfg.HomeID, Name: cfg.HomeName, ControllerID: cfg.ControllerID, API: apiURL}
		if err := discovery.Announce(ctx, cfg.UDPBroadcast, ann, 5*time.Second); err != nil {
			log.Printf("controller announce error: %v", err)
		}
	}()

	log.Printf("meshd up node_id=%s home=%s combined=%v auto_adopt=%v", id.NodeID(), cfg.HomeID, cfg.Combined(), cfg.AutoAdopt)
	for _, srv := range servers {
		srv := srv
		go func() {
			log.Printf("serving on %s (tls=%v)", srv.Addr, srv.TLSConfig != nil)
			var err error
			if srv.TLSConfig != nil {
				err = srv.ListenAndServeTLS("", "")
			} else {
				err = srv.ListenAndServe()
			}
			if err != nil && err != http.ErrServerClosed {
				log.Fatalf("server %s failed: %v", srv.Addr, err)
			}
		}()
	}

	// Pick and apply a Home when none is active yet. autoSelectHome is
	// idempotent (it respects an already-set active Home), so it is safe to run
	// again after each join lands.
	autoSelect := func() { autoSelectHome(ctx, store, profileManager, wifiClients, cfg.HomeID) }

	if len(cfg.Join) == 0 {
		// No joins configured: select from the Homes already known.
		go autoSelect()
	} else {
		// Joining devices: wait to record the joined Home(s) before selecting,
		// so an external Home can win over the device's own (last-resort) Home.
		// joinHome records each controller's authenticated mesh client here for
		// the topology loop to reuse.
		meshClients := &controllerClients{}
		for _, controllerURL := range cfg.Join {
			go joinHome(ctx, id, cfg.Serial, controllerURL, store, profileManager, meshClients, autoSelect)
		}
		// Push this node's local topology to its controllers for mesh-wide
		// aggregation.
		go reportTopologyLoop(ctx, collector, id.NodeID(), cfg.Join, meshClients, 15*time.Second)
	}

	// Bring up the first-boot setup AP while the device is unclaimed, so a
	// companion app can reach the open management API before the device has
	// joined any network. Best-effort: a device without radios (e.g. a wired
	// controller) simply logs and carries on.
	go func() {
		if !cfg.SetupAPEnabled {
			return
		}
		complete, err := store.GetSetupComplete(ctx)
		if err != nil {
			log.Printf("setup-ap: read setup state failed: %v", err)
			return
		}
		if complete {
			return
		}
		if err := setupAP.Enable(ctx, id.NodeID()); err != nil {
			log.Printf("setup-ap: enable failed (non-fatal): %v", err)
			return
		}
		log.Printf("setup AP up: ssid=%s", setupAP.SSID(id.NodeID()))
	}()

	// Drive the status LED from the onboarding state so an installer can read it
	// off the device: blink while unclaimed, heartbeat while joining, solid once
	// active. No-op on hardware lacking the configured LED.
	if cfg.LEDEnabled {
		go ledReactorLoop(ctx, store, deviceled.New(""), cfg.LEDName, 2*time.Second)
	}

	<-ctx.Done()
	log.Println("shutting down meshd")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, srv := range servers {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown %s failed: %v", srv.Addr, err)
		}
	}
}

// ledReactorLoop polls the onboarding state and drives the status LED to match,
// re-asserting on a ticker so the LED self-heals if something else touches it.
// Reads are cheap bolt lookups; the reactor only writes sysfs when the derived
// state changes.
func ledReactorLoop(ctx context.Context, store storage.Store, led deviceled.LEDController, name string, interval time.Duration) {
	r := deviceled.NewReactor(led, name)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		complete, err := store.GetSetupComplete(ctx)
		if err != nil {
			log.Printf("led: read setup state failed: %v", err)
		}
		active, err := store.GetActiveHome(ctx)
		if err != nil {
			log.Printf("led: read active home failed: %v", err)
		}
		if _, err := r.Update(deviceled.StateFor(complete, active)); err != nil {
			log.Printf("led: update failed (non-fatal): %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// resolveBSSID returns this controller's mesh BSSID: the explicit MESHD_BSSID,
// or the MAC read from MESHD_MESH_IFACE (/sys/class/net/<iface>/address).
func resolveBSSID(cfg config.Config) string {
	if cfg.BSSID != "" {
		return strings.ToLower(cfg.BSSID)
	}
	if cfg.MeshIface != "" {
		if b, err := os.ReadFile("/sys/class/net/" + cfg.MeshIface + "/address"); err == nil {
			return strings.ToLower(strings.TrimSpace(string(b)))
		}
	}
	return ""
}

// autoSelectHome activates a Home on boot when none is explicitly set, using
// the home-selection policy fed by live RSSI. An already-set active Home is
// respected (the operator's explicit choice wins). Best-effort: failures are
// logged, not fatal.
func autoSelectHome(ctx context.Context, store storage.Store, pm profiles.ProfileManager, signals topology.UbusClients, selfHomeID string) {
	if active, err := store.GetActiveHome(ctx); err != nil || active != "" {
		return
	}

	homes, err := store.ListHomes(ctx)
	if err != nil || len(homes) == 0 {
		return
	}

	sig := selection.Signals{}
	if observed, err := signals.SignalByMAC(ctx); err == nil {
		sig = observed
	}

	best, ok := selection.Recommend(homes, selfHomeID, "", sig)
	if !ok {
		return
	}

	if err := store.SetActiveHome(ctx, best.HomeID); err != nil {
		log.Printf("auto-select: failed to set active home: %v", err)
		return
	}
	log.Printf("auto-selected active home %s (signal=%d self=%v)", best.HomeID, best.Signal, best.SelfControlled)

	if err := pm.ApplyProfileForHome(ctx, best.HomeID); err != nil {
		log.Printf("auto-select: apply profile for %s failed (non-fatal): %v", best.HomeID, err)
	}
}

// reportTopologyLoop periodically pushes this node's local topology to its
// controllers so they can build a mesh-wide view.
// controllerClients holds the authenticated mesh HTTP client for each joined
// controller. joinHome populates it after a successful join; the topology loop
// reads it so its reports go over the same mutual-TLS transport.
type controllerClients struct {
	mu sync.Mutex
	m  map[string]*http.Client
}

func (c *controllerClients) set(url string, hc *http.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]*http.Client{}
	}
	c.m[url] = hc
}

func (c *controllerClients) get(url string) *http.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[url]
}

func reportTopologyLoop(ctx context.Context, collector *topology.Collector, nodeID string, controllers []string, clients *controllerClients, interval time.Duration) {
	plain := &http.Client{Timeout: 10 * time.Second}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		graph := collector.Collect(ctx)
		for _, controllerURL := range controllers {
			// Prefer the authenticated client recorded at join; fall back to a
			// plain client (combined-mode/http controllers) until one is set.
			httpClient := clients.get(controllerURL)
			if httpClient == nil {
				httpClient = plain
			}
			if err := client.ReportTopology(ctx, controllerURL, nodeID, graph, httpClient); err != nil {
				log.Printf("topology report to %s failed: %v", controllerURL, err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// joinHome enrolls this device into another controller, retrying until it
// succeeds or the daemon stops.
func joinHome(ctx context.Context, id *identity.Identity, serial, controllerURL string, store storage.Store, pm profiles.ProfileManager, clients *controllerClients, afterJoin func()) {
	const retry = 3 * time.Second
	for {
		result, err := client.JoinAndRecord(ctx, id, controllerURL, serial, store, client.Options{})
		if err == nil {
			log.Printf("joined controller %s status=%s", controllerURL, result.Status)
			// Record the authenticated mesh transport so topology reports to
			// this controller go over mutual TLS with the issued leaf.
			if clients != nil && len(result.Certificate) > 0 && len(result.CACertificate) > 0 {
				if ac, aerr := client.AuthenticatedClient(id, result.Certificate, result.CACertificate, 10*time.Second); aerr == nil {
					clients.set(controllerURL, ac)
				}
			}
			if result.Profile != nil {
				if err := pm.ApplyProfile(ctx, *result.Profile); err != nil {
					log.Printf("apply profile from %s failed: %v", controllerURL, err)
				}
			}
			if afterJoin != nil {
				afterJoin()
			}
			return
		}
		log.Printf("join %s failed, retrying in %s: %v", controllerURL, retry, err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(retry):
		}
	}
}
