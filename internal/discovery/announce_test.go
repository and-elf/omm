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

func TestCacheListenCollectsAnnouncements(t *testing.T) {
	addr := freeUDPPort(t)
	ann := Announcement{HomeID: "home-1", Name: "Cottage", ControllerID: "gw01", API: "http://10.0.0.1:8080"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache := NewCache(time.Minute, nil)
	go func() { _ = cache.Listen(ctx, addr) }()
	time.Sleep(50 * time.Millisecond) // let the listener bind
	go func() { _ = Announce(ctx, addr, ann, 30*time.Millisecond) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if list := cache.List(); len(list) == 1 && list[0].API == ann.API {
			return // success
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("expected the announcement to be cached, got %+v", cache.List())
}

func TestCacheExpiresEntries(t *testing.T) {
	clock := int64(1000)
	cache := NewCache(30*time.Second, func() int64 { return clock })
	cache.Add(Announcement{HomeID: "h1", API: "http://x"})

	if len(cache.List()) != 1 {
		t.Fatal("expected fresh entry")
	}
	clock = 1031 // past TTL
	if len(cache.List()) != 0 {
		t.Fatal("expected expired entry to be dropped")
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
