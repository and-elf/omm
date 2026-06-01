package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"os/signal"
	"syscall"

	"github.com/and-elf/omm/internal/api"
	"github.com/and-elf/omm/internal/client"
	"github.com/and-elf/omm/internal/config"
	"github.com/and-elf/omm/internal/discovery"
	"github.com/and-elf/omm/internal/enrollment"
	"github.com/and-elf/omm/internal/identity"
	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/profiles"
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

	// Ensure this daemon's own Home exists so it can act as its controller.
	if _, err := store.GetHome(ctx, cfg.HomeID); err == storage.ErrNotFound {
		if err := store.CreateHome(ctx, models.Home{
			ID: cfg.HomeID, Name: cfg.HomeName, Controller: cfg.ControllerID, LastSeen: time.Now().Unix(),
		}); err != nil {
			log.Printf("failed to create home: %v", err)
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
	collector := topology.NewCollector(id.NodeID(), cfg.Serial,
		topology.BatctlMesh{Interface: cfg.BatmanIface},
		topology.UbusClients{Ubus: ubusClient, Interfaces: cfg.APInterfaces},
	)

	router := api.NewRouter(store, profileManager,
		api.WithEnrollment(enrollSvc),
		api.WithSelf(id, cfg.Serial),
		api.WithSelfHome(cfg.HomeID),
		api.WithTopology(collector),
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

	// Optionally enroll into other homes at startup (membership; a device is
	// only ever active in one home at a time).
	for _, controllerURL := range cfg.Join {
		go joinHome(ctx, id, cfg.Serial, controllerURL, profileManager)
	}

	<-ctx.Done()
	log.Println("shutting down meshd")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown failed: %v", err)
	}
}

// joinHome enrolls this device into another controller, retrying until it
// succeeds or the daemon stops.
func joinHome(ctx context.Context, id *identity.Identity, serial, controllerURL string, pm profiles.ProfileManager) {
	const retry = 3 * time.Second
	for {
		result, err := client.Join(ctx, id, controllerURL, serial, client.Options{})
		if err == nil {
			log.Printf("joined controller %s status=%s", controllerURL, result.Status)
			if result.Profile != nil {
				if err := pm.ApplyProfile(ctx, *result.Profile); err != nil {
					log.Printf("apply profile from %s failed: %v", controllerURL, err)
				}
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
