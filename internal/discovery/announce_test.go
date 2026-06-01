package discovery

import (
	"context"
	"net"
	"testing"
	"time"
)

func freeUDPPort(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := conn.LocalAddr().String()
	_ = conn.Close()
	return addr
}

func TestAnnounceDiscoverRoundTrip(t *testing.T) {
	addr := freeUDPPort(t)
	ann := Announcement{HomeID: "home-1", Name: "Home", ControllerID: "gw01", API: "http://10.0.0.1:8080"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Announce repeatedly so the discoverer reliably catches a packet even if
	// it binds slightly later.
	go func() { _ = Announce(ctx, addr, ann, 20*time.Millisecond) }()

	got, err := DiscoverController(ctx, addr)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if got.API != ann.API || got.HomeID != ann.HomeID {
		t.Fatalf("unexpected announcement: %+v", got)
	}
}

func TestDiscoverControllerCancels(t *testing.T) {
	addr := freeUDPPort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if _, err := DiscoverController(ctx, addr); err == nil {
		t.Fatal("expected error when no announcement arrives before cancel")
	}
}
