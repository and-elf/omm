package backhaul

import "testing"

const sampleRoute = `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
br-lan	00000000	0102A8C0	0003	0	0	0	00000000	0	0	0
br-lan	0002A8C0	00000000	0001	0	0	0	00FFFFFF	0	0	0`

const sampleARP = `IP address       HW type     Flags       HW address            Mask     Device
192.168.2.104    0x1         0x2         04:00:6e:8d:fd:d0     *        br-lan
192.168.2.1      0x1         0x2         f8:5e:3c:a0:57:8a     *        br-lan`

func TestParseDefaultGateway(t *testing.T) {
	if got := parseDefaultGateway(sampleRoute); got != "192.168.2.1" {
		t.Fatalf("gateway = %q, want 192.168.2.1", got)
	}
	// No default route -> empty.
	if got := parseDefaultGateway("Iface\tDestination\tGateway\nbr-lan\t0002A8C0\t00000000"); got != "" {
		t.Fatalf("expected no default gateway, got %q", got)
	}
}

func TestParseARPMAC(t *testing.T) {
	if got := parseARPMAC(sampleARP, "192.168.2.1"); got != "f8:5e:3c:a0:57:8a" {
		t.Fatalf("mac = %q, want f8:5e:3c:a0:57:8a", got)
	}
	if got := parseARPMAC(sampleARP, "192.168.2.99"); got != "" {
		t.Fatalf("expected no mac for unknown ip, got %q", got)
	}
	incomplete := "IP address HW type Flags HW address Mask Device\n10.0.0.1 0x1 0x0 00:00:00:00:00:00 * br-lan"
	if got := parseARPMAC(incomplete, "10.0.0.1"); got != "" {
		t.Fatalf("expected no mac for incomplete arp, got %q", got)
	}
}

func TestParseFDBPort(t *testing.T) {
	fdb := `33:33:00:00:00:01 dev br-lan self permanent
f8:5e:3c:a0:57:8a dev lan1 master br-lan
9c:9d:7e:76:73:4a dev bat0 master br-lan`
	if got := parseFDBPort(fdb, "f8:5e:3c:a0:57:8a"); got != "lan1" {
		t.Fatalf("fdb port = %q, want lan1", got)
	}
	if got := parseFDBPort(fdb, "AA:BB:CC:DD:EE:FF"); got != "" {
		t.Fatalf("expected no port for unknown mac, got %q", got)
	}
}

func TestResolveUplinkPortExplicitWins(t *testing.T) {
	got := ResolveUplinkPort("wan", "bat0", UplinkReaders{})
	if got != "wan" {
		t.Fatalf("explicit uplink = %q, want wan", got)
	}
}

func TestResolveUplinkPortAutoDetectsGatewayPort(t *testing.T) {
	r := UplinkReaders{
		Route: func() (string, error) { return sampleRoute, nil },
		ARP:   func() (string, error) { return sampleARP, nil },
		FDB:   func() (string, error) { return "f8:5e:3c:a0:57:8a dev lan1 master br-lan", nil },
	}
	if got := ResolveUplinkPort("", "bat0", r); got != "lan1" {
		t.Fatalf("auto uplink = %q, want lan1", got)
	}
}

// A gateway reachable only over bat0 (or a wifi vif) means the wire is already
// down: no wired uplink to watch, so the mesh runs always-on, not as a standby.
func TestResolveUplinkPortRejectsMeshPath(t *testing.T) {
	r := UplinkReaders{
		Route: func() (string, error) { return sampleRoute, nil },
		ARP:   func() (string, error) { return sampleARP, nil },
		FDB:   func() (string, error) { return "f8:5e:3c:a0:57:8a dev bat0 master br-lan", nil },
	}
	if got := ResolveUplinkPort("", "bat0", r); got != "" {
		t.Fatalf("uplink over bat0 must resolve to empty (wireless-only), got %q", got)
	}
}

// No default gateway (e.g. still booting, no lease) -> no uplink resolved.
func TestResolveUplinkPortNoGateway(t *testing.T) {
	r := UplinkReaders{
		Route: func() (string, error) { return "Iface\tDestination\tGateway", nil },
		ARP:   func() (string, error) { return sampleARP, nil },
		FDB:   func() (string, error) { return "", nil },
	}
	if got := ResolveUplinkPort("", "bat0", r); got != "" {
		t.Fatalf("expected empty uplink with no gateway, got %q", got)
	}
}
