package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/and-elf/omm/internal/api"
	"github.com/and-elf/omm/internal/config"
	"github.com/and-elf/omm/internal/discovery"
	"github.com/and-elf/omm/internal/profiles"
	"github.com/and-elf/omm/internal/storage"
	"github.com/and-elf/omm/internal/uci"
)

func main() {
	cfg := config.Load()

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

	profileManager := profiles.NewManager(store, uciClient)
	router := api.NewRouter(store, profileManager)

	discoveryCtx, discoveryCancel := context.WithCancel(context.Background())
	go func() {
		if err := discovery.ListenUDP(discoveryCtx, cfg.UDPListen, func(data []byte, addr *net.UDPAddr) {
			log.Printf("udp announce from %s: %s", addr, string(data))
		}); err != nil {
			log.Printf("udp discovery error: %v", err)
		}
	}()

	go func() {
		if err := discovery.DiscoverMeshControllers(discoveryCtx, "_mesh._tcp", "local.", func(entry *discovery.ServiceEntry) {
			log.Printf("mDNS controller found: %s %v", entry.Instance, entry.AddrIPv4)
		}); err != nil {
			log.Printf("mDNS discovery error: %v", err)
		}
	}()

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: router,
	}

	go func() {
		log.Printf("starting meshd on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	discoveryCancel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Println("shutting down meshd")
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown failed: %v", err)
	}
}
