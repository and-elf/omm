package topology

import (
	"context"
	"errors"
	"testing"
)

func TestLinkMetricsWiredReadsSpeed(t *testing.T) {
	m := SysfsIwLinkMetrics{
		// No phy80211 => wired.
		Exists: func(string) bool { return false },
		Read: func(path string) ([]byte, error) {
			if path == "/sys/class/net/eth1/speed" {
				return []byte("2500\n"), nil
			}
			return nil, errors.New("not found")
		},
	}
	got := m.LinkMetrics(context.Background(), "eth1", "bb:bb:cc:dd:ee:01")
	if got.Kind != LinkWired || got.SpeedMbps != 2500 || got.Signal != 0 {
		t.Fatalf("expected wired 2500 Mbps, got %+v", got)
	}
}

func TestLinkMetricsWirelessReadsRSSI(t *testing.T) {
	dump := `Station bb:bb:cc:dd:ee:01 (on mesh0)
	inactive time:	10 ms
	signal:  	-58 [-60, -62] dBm
	signal avg:	-57 dBm
Station ff:ff:ff:ff:ff:ff (on mesh0)
	signal:  	-80 dBm
`
	m := SysfsIwLinkMetrics{
		Exists: func(path string) bool { return path == "/sys/class/net/mesh0/phy80211" },
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "iw" {
				t.Fatalf("expected iw, got %q", name)
			}
			return []byte(dump), nil
		},
	}
	got := m.LinkMetrics(context.Background(), "mesh0", "bb:bb:cc:dd:ee:01")
	if got.Kind != LinkWireless || got.Signal != -58 || got.SpeedMbps != 0 {
		t.Fatalf("expected wireless -58 dBm, got %+v", got)
	}
}

func TestLinkMetricsDegradesGracefully(t *testing.T) {
	// No iface, or tools failing, yields a zero-value (unknown) classification.
	m := SysfsIwLinkMetrics{
		Exists: func(string) bool { return false },
		Read:   func(string) ([]byte, error) { return nil, errors.New("nope") },
	}
	if got := m.LinkMetrics(context.Background(), "", ""); got != (LinkMetrics{}) {
		t.Fatalf("expected zero metrics for empty iface, got %+v", got)
	}
	// Wired iface whose speed is unreadable still classifies as wired.
	if got := m.LinkMetrics(context.Background(), "eth1", "x"); got.Kind != LinkWired || got.SpeedMbps != 0 {
		t.Fatalf("expected wired with unknown speed, got %+v", got)
	}
}
