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

func TestDirectedBroadcasts(t *testing.T) {
	addrs := []net.Addr{
		&net.IPNet{IP: net.ParseIP("192.168.2.50"), Mask: net.CIDRMask(24, 32)},
		&net.IPNet{IP: net.ParseIP("10.0.0.5"), Mask: net.CIDRMask(16, 32)},
		&net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)}, // IPv6 skipped
	}
	got := directedBroadcasts(addrs)
	want := []string{"192.168.2.255", "10.0.255.255"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Fatalf("broadcast[%d] = %s, want %s", i, got[i], w)
		}
	}
}

// broadcastTargets always includes an explicit target (e.g. 255.255.255.255)
// even when no interface yields one, and deduplicates.
func TestBroadcastTargetsIncludesExplicit(t *testing.T) {
	limited := net.IPv4bcast // 255.255.255.255
	found := false
	for _, ip := range broadcastTargets(limited) {
		if ip.Equal(limited) {
			found = true
		}
	}
	if !found {
		t.Fatal("explicit broadcast target not included")
	}
}

func TestFixAPIHostRewritesUnspecified(t *testing.T) {
	src := net.ParseIP("10.0.0.5")
	cases := []struct{ in, want string }{
		{"https://0.0.0.0:8081", "https://10.0.0.5:8081"},
		{"http://0.0.0.0:8080/x", "http://10.0.0.5:8080/x"},
		{"http://10.0.0.1:8080", "http://10.0.0.1:8080"}, // concrete host kept
		{"", ""}, // empty kept
	}
	for _, c := range cases {
		if got := fixAPIHost(c.in, src); got != c.want {
			t.Errorf("fixAPIHost(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The cache fills an unspecified announced host (0.0.0.0) from the UDP packet's
// source address, so a controller can announce its bind port without knowing
// its own routable IP.
func TestCacheListenDerivesHostFromSource(t *testing.T) {
	addr := freeUDPPort(t)
	ann := Announcement{HomeID: "home-1", Name: "Casa", ControllerID: "gw01", API: "http://0.0.0.0:9999"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache := NewCache(time.Minute, nil)
	go func() { _ = cache.Listen(ctx, addr) }()
	time.Sleep(50 * time.Millisecond)
	go func() { _ = Announce(ctx, addr, ann, 30*time.Millisecond) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if list := cache.List(); len(list) == 1 {
			if list[0].API == "http://127.0.0.1:9999" {
				return // success: host rewritten from the loopback source
			}
			t.Fatalf("API host not rewritten: %q", list[0].API)
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("announcement not cached: %+v", cache.List())
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
