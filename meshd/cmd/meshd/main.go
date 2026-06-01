package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/and-elf/omm/internal/api"
	"github.com/and-elf/omm/internal/client"
	"github.com/and-elf/omm/internal/config"
	"github.com/and-elf/omm/internal/discovery"
	"github.com/and-elf/omm/internal/enrollment"
	"github.com/and-elf/omm/internal/identity"
	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/profiles"
	"github.com/and-elf/omm/internal/selection"
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

	// Ensure this daemon's own Home exists (with its mesh BSSID, so peers can
	// map our RSSI to this Home) without clobbering an onboarding rename.
	bssid := resolveBSSID(cfg)
	switch home, err := store.GetHome(ctx, cfg.HomeID); {
	case err == storage.ErrNotFound:
		if err := store.CreateHome(ctx, models.Home{
			ID: cfg.HomeID, Name: cfg.HomeName, Controller: cfg.ControllerID, BSSID: bssid, LastSeen: time.Now().Unix(),
		}); err != nil {
			log.Printf("failed to create home: %v", err)
		}
	case err == nil && bssid != "" && home.BSSID != bssid:
		home.BSSID = bssid
		if err := store.UpdateHome(ctx, home); err != nil {
			log.Printf("failed to update home bssid: %v", err)
		}
	}

	profileManager := profiles.NewManager(store, uciClient)
	enrollSvc := enrollment.NewService(store, enrollment.Options{HomeID: cfg.HomeID, AutoAdopt: cfg.AutoAdopt})

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
	)

	router := api.NewRouter(store, profileManager,
		api.WithEnrollment(enrollSvc),
		api.WithSelf(id, cfg.Serial),
		api.WithSelfHome(cfg.HomeID),
		api.WithTopology(collector),
		api.WithSignalSource(wifiClients),
	)

	// Announce this controller's presence for discovery.
	apiURL := cfg.APIAdvertise
	if apiURL == "" {
		apiURL = "http://" + cfg.HTTPAddr
	}
	go func() {
		ann := discovery.Announcement{HomeID: cfg.HomeID, Name: cfg.HomeName, ControllerID: cfg.ControllerID, API: apiURL}
		if err := discovery.Announce(ctx, cfg.UDPBroadcast, ann, 5*time.Second); err != nil {
			log.Printf("controller announce error: %v", err)
		}
	}()

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: router}
	go func() {
		log.Printf("meshd up node_id=%s home=%s addr=%s auto_adopt=%v", id.NodeID(), cfg.HomeID, cfg.HTTPAddr, cfg.AutoAdopt)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed: %v", err)
		}
	}()

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
		for _, controllerURL := range cfg.Join {
			go joinHome(ctx, id, cfg.Serial, controllerURL, store, profileManager, autoSelect)
		}
		// Push this node's local topology to its controllers for mesh-wide
		// aggregation.
		go reportTopologyLoop(ctx, collector, id.NodeID(), cfg.Join, 15*time.Second)
	}

	<-ctx.Done()
	log.Println("shutting down meshd")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown failed: %v", err)
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
func reportTopologyLoop(ctx context.Context, collector *topology.Collector, nodeID string, controllers []string, interval time.Duration) {
	httpClient := &http.Client{Timeout: 10 * time.Second}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		graph := collector.Collect(ctx)
		for _, controllerURL := range controllers {
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
func joinHome(ctx context.Context, id *identity.Identity, serial, controllerURL string, store storage.Store, pm profiles.ProfileManager, afterJoin func()) {
	const retry = 3 * time.Second
	for {
		result, err := client.JoinAndRecord(ctx, id, controllerURL, serial, store, client.Options{})
		if err == nil {
			log.Printf("joined controller %s status=%s", controllerURL, result.Status)
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
