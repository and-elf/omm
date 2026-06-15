package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/and-elf/omm/internal/api"
	"github.com/and-elf/omm/internal/backhaul"
	"github.com/and-elf/omm/internal/client"
	"github.com/and-elf/omm/internal/config"
	"github.com/and-elf/omm/internal/deviceled"
	"github.com/and-elf/omm/internal/discovery"
	"github.com/and-elf/omm/internal/enrollment"
	"github.com/and-elf/omm/internal/identity"
	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/netposture"
	"github.com/and-elf/omm/internal/onboard"
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

	// Derive a unique Home id from the device identity when none is configured,
	// so fresh devices don't all share "default-home" (which makes them mutually
	// invisible to discovery and unable to onboard). Done here, after identity
	// loads and before anything uses cfg.HomeID.
	if cfg.HomeID == "" {
		cfg.HomeID = config.DeriveHomeID(id.NodeID())
		log.Printf("home_id not configured; derived %q from device identity", cfg.HomeID)
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

	// ubus client: shared by the topology collector (batman/hostapd reads) and
	// the profile manager's mesh verification (network.wireless status).
	ubusClient, err := ubus.NewClient(ubus.Options{SocketPath: cfg.UbusSocket, BinaryPath: cfg.UbusBinary})
	if err != nil {
		log.Fatalf("failed to create ubus client: %v", err)
	}
	defer ubusClient.Close()

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

	// Seed a default wireless profile when the Home has none, so the controller's
	// own wireless comes up and a joining node receives wifi config — without the
	// setup wizard. An operator/wizard-set profile is never overwritten.
	if _, err := store.GetProfile(ctx, cfg.HomeID); errors.Is(err, storage.ErrNotFound) {
		key := cfg.MeshKey
		if key == "" {
			key = generateMeshKey()
		}
		prof := profiles.DefaultProfile(cfg.HomeID, cfg.MeshSSID, key)
		if err := store.CreateOrUpdateProfile(ctx, prof); err != nil {
			log.Printf("failed to seed default profile: %v", err)
		} else {
			log.Printf("seeded default wireless profile for %s (ssid=%s)", cfg.HomeID, prof.MeshSSID)
		}
	} else if err != nil {
		log.Printf("check home profile: %v", err)
	}

	profileManager := profiles.NewManager(store, uciClient, profiles.Config{
		Radio:     cfg.SetupAPRadio,
		MeshRadio: cfg.MeshRadio,
		Mesh:      profiles.UbusMeshInspector{Ubus: ubusClient},
		// batman-adv routing layer: author bat0 + a hard interface per backhaul
		// link and wire the 802.11s mesh onto it (loop-free multi-hop across
		// wired+wireless), degrading to the direct lan bridge when bat0 doesn't
		// come up. LanDevice is the LAN bridge bat0 is bridged into.
		BatmanEnable:      cfg.BatmanEnable,
		BatmanIface:       cfg.BatmanIface,
		BatmanRoutingAlgo: cfg.BatmanRoutingAlgo,
		BatmanPorts:       cfg.BatmanPorts,
		LanDevice:         cfg.LanDevice,
		Batman:            profiles.UbusBatmanInspector{Ubus: ubusClient},
	})

	// Network posture: keep the node's network/dhcp/firewall aligned with its
	// lifecycle (Guest dumb-AP while unclaimed so discovery works, gateway once
	// it controls its home). Opt-in; default off so a hand-wired device is never
	// reconfigured unexpectedly. applyPosture is a no-op when disabled.
	posture := netposture.NewManager(uciClient, netposture.Config{
		UplinkPort: cfg.UplinkPort,
		LanDevice:  cfg.LanDevice,
	})
	applyPosture := func(ctx context.Context) {
		if !cfg.ManageNetwork {
			return
		}
		active, _ := store.GetActiveHome(ctx)
		role := netposture.DecideRole(active, cfg.HomeID)
		if err := posture.Apply(ctx, role); err != nil {
			log.Printf("netposture: apply %s failed (non-fatal): %v", role, err)
			return
		}
		log.Printf("netposture: applied %s posture", role)
	}
	enrollSvc := enrollment.NewService(store, enrollment.Options{HomeID: cfg.HomeID, AdoptPolicy: enrollment.AdoptPolicy(cfg.AdoptPolicy), CA: homeCA})

	// First-boot setup AP: while the device is unclaimed (setup not complete) it
	// broadcasts a known, label-printable SSID serving its open management API,
	// so a companion app can reach it before it has joined any network. It is
	// torn down once setup completes (see the WithSetupCompleteHook below).
	setupAP := setupap.New(uciClient, setupap.Config{
		Radio: cfg.SetupAPRadio,
		Key:   cfg.SetupAPKey,
	})

	// Topology collector: batman-adv link quality + hostapd client RSSI, plus
	// this node's backhaul type and wireless-backhaul mode (802.11s vs multi-AP,
	// read from the applied backhaul state) so the controller can show both per
	// node across the mesh.
	wifiClients := topology.UbusClients{Ubus: ubusClient, Interfaces: cfg.APInterfaces}
	collector := topology.NewCollector(id.NodeID(), cfg.Serial,
		topology.BatctlMesh{Interface: cfg.BatmanIface},
		wifiClients,
		topology.SysfsBackhaul{Iface: cfg.BackhaulIface},
		storeMeshMode{store: store},
	)

	// Passively cache controller announcements so /scan answers instantly.
	discoCache := discovery.NewCache(90*time.Second, nil)
	go func() {
		if err := discoCache.Listen(ctx, cfg.UDPListen); err != nil {
			log.Printf("discovery listen error: %v", err)
		}
	}()

	// completeSetupLocal is the single teardown path run when the device becomes
	// claimed — by the wizard (the setup-complete hook below) or by wired
	// auto-onboard — so the first-boot setup AP (and any uplink station) comes
	// down exactly once, however setup finishes.
	completeSetupLocal := func(ctx context.Context) error {
		err := setupAP.Disable(ctx)
		// Claimed now: re-evaluate posture (Guest -> controller/mesh node).
		applyPosture(ctx)
		return err
	}

	apiOpts := []api.Option{
		api.WithEnrollment(enrollSvc),
		api.WithSelf(id, cfg.Serial),
		api.WithSelfHome(cfg.HomeID),
		api.WithTopology(collector),
		api.WithSignalSource(wifiClients),
		api.WithSetupCompleteHook(completeSetupLocal),
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

	log.Printf("meshd up node_id=%s home=%s combined=%v adopt_policy=%s", id.NodeID(), cfg.HomeID, cfg.Combined(), cfg.AdoptPolicy)
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

	// Align network posture with the current lifecycle state at boot (Guest
	// dumb-AP while unclaimed so discovery works). No-op unless MANAGE_NETWORK.
	applyPosture(ctx)

	// Pick and apply a Home when none is active yet. autoSelectHome is
	// idempotent (it respects an already-set active Home), so it is safe to run
	// again after each join lands. Re-evaluate posture afterwards, since gaining
	// an active Home transitions a Guest to controller/mesh-node.
	// retireSetupAP brings down the first-boot setup AP (and marks setup complete)
	// once a home is active: it is an unclaimed-only AP, and its radio vif would
	// otherwise block the home's 802.11s mesh from coming up on radios that can't
	// do AP+AP+mesh. Run before the profile is applied so the mesh has a free vif.
	retireSetupAP := func(ctx context.Context) {
		if complete, _ := store.GetSetupComplete(ctx); complete {
			return
		}
		if err := store.SetSetupComplete(ctx, true); err != nil {
			log.Printf("retire setup AP: mark complete failed: %v", err)
		}
		if err := completeSetupLocal(ctx); err != nil {
			log.Printf("retire setup AP failed (non-fatal): %v", err)
		}
	}
	// autoSelect activates a known Home and applies it WITHOUT retiring the setup
	// AP — used for re-selection after a join and for the manual path, where the
	// device keeps its first-boot setup AP until real setup completes.
	autoSelect := func() {
		autoSelectHome(ctx, store, profileManager, wifiClients, cfg.HomeID, nil)
		applyPosture(ctx)
	}
	// becomeOwnController is the zero-touch fallback: it retires the setup AP
	// (freeing the radio vif for the mesh and marking the device claimed) before
	// selecting+applying its own Home, so an unattended device that finds no
	// controller comes up as a working controller with its 802.11s mesh.
	becomeOwnController := func() {
		autoSelectHome(ctx, store, profileManager, wifiClients, cfg.HomeID, retireSetupAP)
		applyPosture(ctx)
	}

	if len(cfg.Join) == 0 {
		if cfg.AutoOnboardWired {
			// Wired auto-onboard: an unclaimed node on the wire enrolls into a
			// discovered controller unattended. Do NOT self-select here — that
			// would claim this node's own Home and block onboarding; the onboard
			// loop tries to enroll first and only falls back (becomeOwnController)
			// after a grace window with no controller.
			go autoOnboardWired(ctx, store, collector, discoCache, id, profileManager, uciClient, cfg, completeSetupLocal, autoSelect, becomeOwnController)
		} else {
			// No auto-onboard: select from the Homes already known (own, last-resort).
			go autoSelect()
		}
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
		// Pull profile updates from the joined controller(s) and re-apply on change.
		go syncProfileLoop(ctx, cfg.Join, meshClients, store, profileManager, 30*time.Second)
		// Keep ethernet prioritized: the 802.11s mesh is a standby that activates
		// only when the wired uplink loses carrier.
		startBackhaulFailover(ctx, uciClient, cfg.BackhaulIface, cfg.BatmanEnable)
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

// storeMeshMode adapts the store to topology.MeshModeSource, so this node
// reports its applied wireless-backhaul mode (802.11s vs multi-AP) on its own
// topology vertex. An unreadable state reports "unknown".
type storeMeshMode struct{ store storage.Store }

func (s storeMeshMode) MeshMode(ctx context.Context) string {
	state, err := s.store.GetBackhaulState(ctx)
	if err != nil {
		return models.BackhaulModeUnknown
	}
	return state.Mode
}

// generateMeshKey returns a random WPA/SAE passphrase (32 hex chars, within the
// 8–63 range) for a Home's default mesh key when none is configured. It is
// persisted in the seeded profile and pushed to nodes on join, so the mesh is
// closed by default rather than open.
func generateMeshKey() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is unexpected; a fixed fallback still beats an open
		// mesh and is overridable via MESHD_MESH_KEY / the wizard.
		return "ommmeshchangeme0"
	}
	return hex.EncodeToString(b)
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
// autoSelectHome activates a Home when none is set yet and applies its profile.
// beforeApply (may be nil) runs after the Home is activated but BEFORE its
// profile is applied — used to retire the first-boot setup AP so its radio vif
// doesn't block the home's mesh from coming up (AP+AP+mesh exceeds some drivers'
// interface combination).
func autoSelectHome(ctx context.Context, store storage.Store, pm profiles.ProfileManager, signals topology.UbusClients, selfHomeID string, beforeApply func(context.Context)) {
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

	if beforeApply != nil {
		beforeApply(ctx)
	}

	if err := pm.ApplyProfileForHome(ctx, best.HomeID); err != nil {
		log.Printf("auto-select: apply profile for %s failed (non-fatal): %v", best.HomeID, err)
	}
}

// startBackhaulFailover launches the ethernet/wireless backhaul switcher for a
// joined node: ethernet is always preferred, and the 802.11s mesh is a standby
// that activates only when the wired uplink loses carrier (and is torn down when
// the wire returns, which also avoids an ethernet+mesh bridging loop).
//
// backhaulIface is the wired uplink whose carrier is watched. It must be a
// discrete uplink port (e.g. "eth0"/"wan"); the LAN bridge "br-lan" (the
// default) is not — its carrier stays up from any bridge member, so it can't
// signal "the wire to the controller is gone". Failover therefore stays off for
// the default and engages only once an operator sets backhaul_iface to the real
// uplink port — leaving existing single-backhaul deployments unchanged. The
// actuator no-ops when no mesh iface is configured (a degraded multi-AP node has
// nothing to fail over to).
//
// When batman-adv is the routing layer the carrier toggle is disabled entirely:
// the mesh is a batadv hard interface, not a br-lan member, and batman's
// bridge-loop-avoidance dedups the redundant wired+wireless path — so the mesh
// must stay up as a standby batman link rather than be torn down by carrier, and
// path selection between wire and air is batman's job, not this loop's.
func startBackhaulFailover(ctx context.Context, uciClient uci.Client, backhaulIface string, batmanEnabled bool) {
	if batmanEnabled {
		log.Printf("backhaul: batman-adv active; carrier-toggle failover disabled (batman handles path selection + loop avoidance)")
		return
	}
	if backhaulIface == "" || backhaulIface == "br-lan" {
		return
	}
	setMesh := func(enabled bool) func(context.Context) error {
		return func(ctx context.Context) error {
			if mode, err := uciClient.Get(ctx, "wireless", profiles.MeshSection, "mode"); err != nil || mode == "" {
				return nil // no mesh configured: nothing to switch
			}
			disabled := "1"
			if enabled {
				disabled = "0"
			}
			if err := uciClient.Set(ctx, "wireless", profiles.MeshSection, "disabled", disabled); err != nil {
				return err
			}
			if err := uciClient.Commit(ctx, "wireless"); err != nil {
				return err
			}
			return uciClient.Reload(ctx)
		}
	}
	// observeMesh reports the actual backhaul from the mesh iface's enabled state
	// so the loop reconciles each tick (a profile re-apply on a joined node's boot
	// recreates the mesh enabled — without this the loop, having seen no carrier
	// transition, would leave a standing ethernet+mesh bridging loop). No mesh
	// section => "" (nothing to reconcile); enabled (disabled != "1") => wireless;
	// explicitly disabled => ethernet.
	observeMesh := func(ctx context.Context) string {
		mode, err := uciClient.Get(ctx, "wireless", profiles.MeshSection, "mode")
		if err != nil || mode == "" {
			return ""
		}
		if disabled, err := uciClient.Get(ctx, "wireless", profiles.MeshSection, "disabled"); err == nil && disabled == "1" {
			return topology.BackhaulEthernet
		}
		return topology.BackhaulWireless
	}
	go backhaul.Run(ctx, backhaul.Deps{
		Carrier:    topology.SysfsBackhaul{Iface: backhaulIface}.Backhaul,
		Current:    observeMesh,
		Activate:   setMesh(true),
		Deactivate: setMesh(false),
	})
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

// syncProfileLoop periodically pulls the active Home's profile from its
// controller and re-applies it when it changes, so an operator editing the
// Home's wireless (SSID/key) on the controller propagates to joined nodes
// without re-onboarding. Pull-based: the node already holds the authenticated
// mesh client; the controller need not track or reach nodes. A controller that
// doesn't serve the active Home (404) or a transient error is skipped silently.
func syncProfileLoop(ctx context.Context, controllers []string, clients *controllerClients, store storage.Store, pm profiles.ProfileManager, interval time.Duration) {
	plain := &http.Client{Timeout: 10 * time.Second}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		homeID, err := store.GetActiveHome(ctx)
		if err != nil || homeID == "" {
			continue
		}
		for _, controllerURL := range controllers {
			hc := clients.get(controllerURL)
			if hc == nil {
				hc = plain
			}
			remote, err := client.FetchProfile(ctx, controllerURL, homeID, hc)
			if err != nil {
				continue
			}
			local, _ := store.GetProfile(ctx, homeID)
			if reflect.DeepEqual(remote, local) {
				continue
			}
			log.Printf("profile for %s changed on %s; re-applying", homeID, controllerURL)
			if err := store.CreateOrUpdateProfile(ctx, remote); err != nil {
				log.Printf("sync profile: store failed: %v", err)
				continue
			}
			if err := pm.ApplyProfile(ctx, remote); err != nil {
				log.Printf("sync profile: apply failed: %v", err)
			}
		}
	}
}

// autoOnboardWired runs the wired auto-onboard loop: an unclaimed node on the
// wire enrolls into a discovered controller unattended. The decision policy
// lives in the onboard package (tested there); this wires it to the daemon's
// store, backhaul detection, discovery cache and join flow.
func autoOnboardWired(ctx context.Context, store storage.Store, collector *topology.Collector, disco *discovery.Cache, id *identity.Identity, pm profiles.ProfileManager, uciClient uci.Client, cfg config.Config, completeSetup func(context.Context) error, afterJoin, fallback func()) {
	backhaul := topology.SysfsBackhaul{Iface: cfg.BackhaulIface}
	clients := &controllerClients{}

	// join enrolls into the chosen controller and, on success, completes setup:
	// it mirrors joinHome but additionally marks the device claimed and tears the
	// setup AP down, since there is no wizard to do so.
	join := func(ctx context.Context, controllerURL string) error {
		result, err := client.JoinAndRecord(ctx, id, controllerURL, cfg.Serial, store, client.Options{})
		if err != nil {
			return err
		}
		// Record the authenticated mesh transport so topology reports go over
		// mutual TLS with the issued leaf.
		if len(result.Certificate) > 0 && len(result.CACertificate) > 0 {
			if ac, aerr := client.AuthenticatedClient(id, result.Certificate, result.CACertificate, 10*time.Second); aerr == nil {
				clients.set(controllerURL, ac)
			}
		}
		if result.Profile != nil {
			// Persist the received profile so a later auto-select / reboot can
			// re-apply it, then apply it now.
			if serr := store.CreateOrUpdateProfile(ctx, *result.Profile); serr != nil {
				log.Printf("auto-onboard: store profile from %s failed (non-fatal): %v", controllerURL, serr)
			}
			if perr := pm.ApplyProfile(ctx, *result.Profile); perr != nil {
				log.Printf("auto-onboard: apply profile from %s failed (non-fatal): %v", controllerURL, perr)
			}
		}
		// Mark claimed first (durable state), then best-effort tear down the
		// setup AP — the same ordering the wizard's completeSetup uses.
		if serr := store.SetSetupComplete(ctx, true); serr != nil {
			return serr
		}
		if terr := completeSetup(ctx); terr != nil {
			log.Printf("auto-onboard: setup AP teardown failed (non-fatal): %v", terr)
		}
		// Push local topology to the controller we just joined (the no-Join
		// branch starts no topology loop otherwise).
		go reportTopologyLoop(ctx, collector, id.NodeID(), []string{controllerURL}, clients, 15*time.Second)
		// Pull profile updates from the controller and re-apply on change.
		go syncProfileLoop(ctx, []string{controllerURL}, clients, store, pm, 30*time.Second)
		// Keep ethernet prioritized once joined; the mesh stands by for failover.
		startBackhaulFailover(ctx, uciClient, cfg.BackhaulIface, cfg.BatmanEnable)
		if afterJoin != nil {
			afterJoin()
		}
		return nil
	}

	onboard.Run(ctx, onboard.Deps{
		Interval:      5 * time.Second,
		Grace:         cfg.OnboardGrace,
		SelfHomeID:    cfg.HomeID,
		SetupComplete: store.GetSetupComplete,
		ActiveHome:    store.GetActiveHome,
		Backhaul:      backhaul.Backhaul,
		// After the grace window with no controller in reach, fall back to
		// selecting this node's own Home (become its own controller).
		Fallback: func(ctx context.Context) { fallback() },
		Discover: func() []discovery.Announcement {
			// Answer from the passively-maintained cache, dropping this device's
			// own Home (Decide also filters it, defensively).
			list := disco.List()
			out := list[:0]
			for _, a := range list {
				if a.HomeID != cfg.HomeID {
					out = append(out, a)
				}
			}
			return out
		},
		Join: join,
	})
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
				// Persist so a later auto-select / reboot re-applies it.
				if serr := store.CreateOrUpdateProfile(ctx, *result.Profile); serr != nil {
					log.Printf("store profile from %s failed: %v", controllerURL, serr)
				}
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
