package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
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
	"github.com/and-elf/omm/internal/storage"
	"github.com/and-elf/omm/internal/uci"
)

func main() {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch cfg.Role {
	case config.RoleClient:
		runClient(ctx, cfg)
	default:
		runController(ctx, cfg)
	}
}

func runController(ctx context.Context, cfg config.Config) {
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

	// Ensure this controller's Home exists so profiles and enrollments can
	// reference it.
	if _, err := store.GetHome(ctx, cfg.HomeID); err == storage.ErrNotFound {
		if err := store.CreateHome(ctx, models.Home{
			ID: cfg.HomeID, Name: cfg.HomeName, Controller: cfg.ControllerID, LastSeen: time.Now().Unix(),
		}); err != nil {
			log.Printf("failed to create home: %v", err)
		}
	}

	profileManager := profiles.NewManager(store, uciClient)
	enrollSvc := enrollment.NewService(store, enrollment.Options{HomeID: cfg.HomeID, AutoAdopt: cfg.AutoAdopt})
	router := api.NewRouter(store, profileManager, api.WithEnrollment(enrollSvc))

	// Announce controller presence for client discovery.
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
		log.Printf("meshd controller listening on %s (home=%s auto_adopt=%v)", cfg.HTTPAddr, cfg.HomeID, cfg.AutoAdopt)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down meshd controller")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown failed: %v", err)
	}
}

func runClient(ctx context.Context, cfg config.Config) {
	id, err := identity.LoadOrCreate(cfg.IdentityDir)
	if err != nil {
		log.Fatalf("failed to load device identity: %v", err)
	}
	log.Printf("meshd client node_id=%s serial=%s", id.NodeID(), cfg.Serial)

	controllerURL := cfg.ControllerURL
	if controllerURL == "" {
		log.Printf("discovering controller on %s", cfg.UDPListen)
		ann, err := discovery.DiscoverController(ctx, cfg.UDPListen)
		if err != nil {
			log.Fatalf("controller discovery failed: %v", err)
		}
		controllerURL = ann.API
		log.Printf("discovered controller %s at %s", ann.HomeID, controllerURL)
	}

	c := client.New(id, controllerURL, client.Options{})

	// Retry until enrolled or the process is asked to stop; the controller may
	// not be reachable the instant the client starts.
	result, err := enrollWithRetry(ctx, c, cfg.Serial)
	if err != nil {
		log.Fatalf("enrollment failed: %v", err)
	}
	log.Printf("node active node_id=%s home_status=%s", id.NodeID(), result.Status)

	if result.Profile != nil {
		uciClient, err := uci.NewClient(uci.Options{SocketPath: cfg.UbusSocket, BinaryPath: cfg.UbusBinary})
		if err != nil {
			log.Printf("uci client unavailable, skipping profile apply: %v", err)
		} else {
			defer uciClient.Close()
			pm := profiles.NewManager(nil, uciClient)
			if err := pm.ApplyProfile(ctx, *result.Profile); err != nil {
				log.Printf("apply profile failed: %v", err)
			}
		}
	}

	<-ctx.Done()
	log.Println("shutting down meshd client")
}

func enrollWithRetry(ctx context.Context, c *client.Client, serial string) (enrollment.Result, error) {
	const retry = 3 * time.Second
	for {
		result, err := c.Enroll(ctx, serial)
		if err == nil {
			return result, nil
		}
		log.Printf("enroll attempt failed, retrying in %s: %v", retry, err)
		select {
		case <-ctx.Done():
			return enrollment.Result{}, ctx.Err()
		case <-time.After(retry):
		}
	}
}
