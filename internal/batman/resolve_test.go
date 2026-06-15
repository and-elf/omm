package batman

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// src is a constant device source for tests.
func src(dev string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return dev, nil }
}

func srcs(devs ...string) []func(context.Context) (string, error) {
	out := make([]func(context.Context) (string, error), 0, len(devs))
	for _, d := range devs {
		out = append(out, src(d))
	}
	return out
}

// Case 2 — a wired uplink with a batman peer on the wire: enslave it to batman,
// mesh stays always-on, no carrier-toggle failover (batman + BLA own both links).
func TestResolveBackhaulPeerOnWireEnslaves(t *testing.T) {
	plan, err := ResolveBackhaul(context.Background(), BackhaulConfig{
		Sources:    srcs("", "wan"),
		HasCarrier: func(dev string) bool { return true },
		PeerOnWire: func(ctx context.Context, dev string) (bool, error) { return true, nil },
	})
	if err != nil {
		t.Fatalf("ResolveBackhaul: %v", err)
	}
	if !reflect.DeepEqual(plan.WiredPorts, []string{"wan"}) {
		t.Errorf("WiredPorts = %v, want [wan]", plan.WiredPorts)
	}
	if plan.FailoverIface != "" {
		t.Errorf("FailoverIface = %q, want none (batman owns path selection)", plan.FailoverIface)
	}
	if plan.MeshStandby {
		t.Error("MeshStandby = true, want false (mesh is an always-on batman hardif)")
	}
}

// Case 3 — a cabled wired uplink with NO batman peer (a node on the controller's
// shared LAN, like .162): do NOT enslave it (it must stay a plain bridge port for
// L2 reach to the controller), and run the carrier-toggle failover with the mesh
// as an admin standby so wired + mesh never bridge-loop.
func TestResolveBackhaulNoPeerKeepsPlainBridgeWithFailover(t *testing.T) {
	plan, err := ResolveBackhaul(context.Background(), BackhaulConfig{
		Sources:    srcs("wan"),
		HasCarrier: func(dev string) bool { return true },
		PeerOnWire: func(ctx context.Context, dev string) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatalf("ResolveBackhaul: %v", err)
	}
	if len(plan.WiredPorts) != 0 {
		t.Errorf("WiredPorts = %v, want none (no peer on wire, stay plain-bridged)", plan.WiredPorts)
	}
	if plan.FailoverIface != "wan" {
		t.Errorf("FailoverIface = %q, want wan (case 3 needs carrier-toggle failover)", plan.FailoverIface)
	}
	if !plan.MeshStandby {
		t.Error("MeshStandby = false, want true (mesh is an admin standby while wired)")
	}
}

// A probe error is treated as "no peer" — the safe direction: a wrong enslave
// strands a controller-LAN node on wireless-only, so default to plain-bridge +
// failover, which always works.
func TestResolveBackhaulProbeErrorIsNoPeer(t *testing.T) {
	plan, err := ResolveBackhaul(context.Background(), BackhaulConfig{
		Sources:    srcs("wan"),
		HasCarrier: func(dev string) bool { return true },
		PeerOnWire: func(ctx context.Context, dev string) (bool, error) { return false, errors.New("sniff failed") },
	})
	if err != nil {
		t.Fatalf("ResolveBackhaul: %v", err)
	}
	if len(plan.WiredPorts) != 0 || plan.FailoverIface != "wan" || !plan.MeshStandby {
		t.Errorf("probe error: plan = %+v, want plain-bridge + failover on wan", plan)
	}
}

// Case 1 — no wired uplink resolves: wireless-only backhaul. Mesh always-on, no
// failover (nothing to fail over from), nothing enslaved.
func TestResolveBackhaulNoUplinkIsWirelessOnly(t *testing.T) {
	probed := false
	plan, err := ResolveBackhaul(context.Background(), BackhaulConfig{
		Sources:    srcs("", "  "),
		HasCarrier: func(dev string) bool { return true },
		PeerOnWire: func(ctx context.Context, dev string) (bool, error) { probed = true; return true, nil },
	})
	if err != nil {
		t.Fatalf("ResolveBackhaul: %v", err)
	}
	if len(plan.WiredPorts) != 0 || plan.FailoverIface != "" || plan.MeshStandby {
		t.Errorf("no uplink: plan = %+v, want wireless-only (nothing enslaved, no failover)", plan)
	}
	if probed {
		t.Error("probed the wire with no uplink resolved")
	}
}

// A resolved uplink with no carrier is also wireless-only: the node is not
// actually wired right now, so nothing is enslaved and the wire isn't probed.
func TestResolveBackhaulNoCarrierIsWirelessOnly(t *testing.T) {
	probed := false
	plan, err := ResolveBackhaul(context.Background(), BackhaulConfig{
		Sources:    srcs("wan"),
		HasCarrier: func(dev string) bool { return false },
		PeerOnWire: func(ctx context.Context, dev string) (bool, error) { probed = true; return true, nil },
	})
	if err != nil {
		t.Fatalf("ResolveBackhaul: %v", err)
	}
	if len(plan.WiredPorts) != 0 || plan.FailoverIface != "" || plan.MeshStandby {
		t.Errorf("no carrier: plan = %+v, want wireless-only", plan)
	}
	if probed {
		t.Error("probed the wire with no carrier")
	}
}

// A nil prober means "don't gate" — enslave a cabled uplink unconditionally. This
// is the dedicated-link / explicit-operator path where the wire is known good.
func TestResolveBackhaulNilProberEnslaves(t *testing.T) {
	plan, err := ResolveBackhaul(context.Background(), BackhaulConfig{
		Sources:    srcs("eth0.2"),
		HasCarrier: func(dev string) bool { return true },
	})
	if err != nil {
		t.Fatalf("ResolveBackhaul: %v", err)
	}
	if !reflect.DeepEqual(plan.WiredPorts, []string{"eth0.2"}) {
		t.Errorf("WiredPorts = %v, want [eth0.2] (nil prober => enslave)", plan.WiredPorts)
	}
	if plan.FailoverIface != "" || plan.MeshStandby {
		t.Errorf("nil prober: plan = %+v, want enslave with no failover", plan)
	}
}

func TestResolveBackhaulPropagatesSourceError(t *testing.T) {
	want := errors.New("uci read failed")
	_, err := ResolveBackhaul(context.Background(), BackhaulConfig{
		Sources: []func(context.Context) (string, error){
			func(context.Context) (string, error) { return "", want },
		},
	})
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

func TestSysfsCarrier(t *testing.T) {
	files := map[string]string{
		"/sys/class/net/wan/carrier":    "1\n",
		"/sys/class/net/lan/carrier":    "0\n",
		"/sys/class/net/up/operstate":   "up\n", // carrier unreadable -> operstate fallback
		"/sys/class/net/down/operstate": "down\n",
	}
	carrier := SysfsCarrier(func(path string) ([]byte, error) {
		if v, ok := files[path]; ok {
			return []byte(v), nil
		}
		return nil, errors.New("ENOENT")
	})

	cases := []struct {
		dev  string
		want bool
	}{
		{"wan", true},
		{"lan", false},
		{"up", true},
		{"down", false},
		{"gone", false},
	}
	for _, c := range cases {
		if got := carrier(c.dev); got != c.want {
			t.Errorf("carrier(%q) = %v, want %v", c.dev, got, c.want)
		}
	}
}
