package batman

import (
	"context"
	"os"
	"strings"
)

// BackhaulConfig is the input to ResolveBackhaul: how to find the node's wired
// uplink, whether it is cabled, and whether a batman peer speaks on that wire.
type BackhaulConfig struct {
	// Sources resolve candidate uplink device names in priority order; the first
	// non-empty result is the uplink (e.g. an explicit uplink_port, the
	// network.wan.device convention, then a discrete backhaul_iface). A source
	// returning an error aborts resolution.
	Sources []func(ctx context.Context) (string, error)
	// HasCarrier reports whether a device has a live cable. A resolved uplink with
	// no carrier means the node is on wireless backhaul right now. Nil => assume
	// cabled.
	HasCarrier func(dev string) bool
	// PeerOnWire reports whether a batman-adv peer is observed on the device (e.g.
	// by passively sniffing batadv OGM frames). It gates enslavement: a wired
	// uplink is only made a batman hardif when the far end batman-speaks on that
	// wire (a dedicated inter-node link). Nil disables the gate — enslave a cabled
	// uplink unconditionally (operator/dedicated-link path). A probe error is
	// treated as "no peer" (the safe direction: keep the wire plain-bridged).
	PeerOnWire func(ctx context.Context, dev string) (bool, error)
}

// BackhaulPlan is the resolved wiring decision for a node's backhaul. It maps to
// three cases:
//
//	case 1 (wireless-only): no cabled uplink — WiredPorts empty, no failover,
//	  mesh always-on (MeshStandby false).
//	case 2 (batman wired): a cabled uplink with a batman peer on the wire —
//	  WiredPorts=[uplink] (enslaved to bat0, leaves br-lan), no failover, mesh
//	  always-on; batman + BLA own path selection across wire and air.
//	case 3 (plain wired + standby mesh): a cabled uplink with NO batman peer (a
//	  node on the controller's shared LAN) — WiredPorts empty (the wire stays a
//	  plain bridge port for L2 reach to the controller), FailoverIface=uplink and
//	  MeshStandby true so the carrier-toggle failover keeps the mesh a standby and
//	  wired + mesh never bridge-loop.
type BackhaulPlan struct {
	WiredPorts    []string
	FailoverIface string
	MeshStandby   bool
}

// ResolveBackhaul decides how a node should wire its backhaul. It is pure given
// its injected closures, so the case logic is unit-testable off-OpenWrt.
func ResolveBackhaul(ctx context.Context, cfg BackhaulConfig) (BackhaulPlan, error) {
	dev := ""
	for _, source := range cfg.Sources {
		d, err := source(ctx)
		if err != nil {
			return BackhaulPlan{}, err
		}
		if d = strings.TrimSpace(d); d != "" {
			dev = d
			break
		}
	}
	// case 1: no uplink resolved, or it has no live cable — wireless-only.
	if dev == "" {
		return BackhaulPlan{}, nil
	}
	if cfg.HasCarrier != nil && !cfg.HasCarrier(dev) {
		return BackhaulPlan{}, nil
	}

	// A nil prober means "don't gate": treat the wire as a known-good batman link.
	peer := true
	if cfg.PeerOnWire != nil {
		// A probe error keeps peer=false — the safe direction (plain-bridge + failover).
		if ok, err := cfg.PeerOnWire(ctx, dev); err == nil {
			peer = ok
		} else {
			peer = false
		}
	}

	if peer {
		// case 2: enslave the wire to batman; mesh always-on; no failover.
		return BackhaulPlan{WiredPorts: []string{dev}}, nil
	}
	// case 3: keep the wire plain-bridged; mesh is an admin standby toggled by the
	// uplink's carrier.
	return BackhaulPlan{FailoverIface: dev, MeshStandby: true}, nil
}

// SysfsCarrier reports a device's link state from /sys/class/net. carrier is the
// most direct signal ("1" cable up, "0" down); the kernel returns an error for
// carrier on an administratively-down device, so it falls back to operstate.
// read is injected for tests; nil uses os.ReadFile.
func SysfsCarrier(read func(path string) ([]byte, error)) func(dev string) bool {
	if read == nil {
		read = os.ReadFile
	}
	return func(dev string) bool {
		base := "/sys/class/net/" + dev
		if b, err := read(base + "/carrier"); err == nil {
			return strings.TrimSpace(string(b)) == "1"
		}
		if b, err := read(base + "/operstate"); err == nil {
			return strings.TrimSpace(string(b)) == "up"
		}
		return false
	}
}
