// Package batman authors a batman-adv mesh routing layer to UCI/netifd. It is
// the forwarding tier OMM lays under the 802.11s mesh: a `bat0` soft interface
// with bridge-loop-avoidance, one batadv hard interface per backhaul link (the
// wireless mesh vif and each wired backhaul port), and `bat0` bridged into the
// LAN. batman-adv then does loop-free, multi-hop layer-2.5 forwarding across any
// mix of wired and wireless links, so chained nodes and simultaneous
// wired+wireless backhaul on one node work without per-node path arbitration.
// See doc/network-model.md#batman-adv-routing-layer.
//
// The decision logic is kept here, separate from the daemon wiring and the
// profile manager, and authored through a segregated UCI interface so it is
// unit-testable off-OpenWrt. Authoring does not commit or reload — the caller
// batches that with its other network/wireless writes.
package batman

import (
	"context"
	"fmt"
	"strings"
)

// UCI is the subset of uci.Client this package needs (interface segregation):
// scalar set, whole-section author, list add/del for the LAN bridge `ports`, and
// section delete for teardown. The real *uci.client satisfies it.
type UCI interface {
	Set(ctx context.Context, pkg, section, option, value string) error
	SetSection(ctx context.Context, pkg, section, sectionType string, values map[string]string) error
	AddListItem(ctx context.Context, pkg, section, option, value string) error
	DelListItem(ctx context.Context, pkg, section, option, value string) error
	Delete(ctx context.Context, pkg, section string) error
}

// Config tunes how the batman-adv layer is authored.
type Config struct {
	// Iface is the batman soft interface / netdev name (default "bat0"). It is
	// both the UCI interface section name and the device bridged into the LAN.
	Iface string
	// RoutingAlgo selects batman-adv's metric (default "BATMAN_IV"; "BATMAN_V" is
	// the throughput-aware alternative).
	RoutingAlgo string
	// WiredPorts are ethernet backhaul devices to enslave to bat0 as hard
	// interfaces, so a wired hop is routed by batman-adv just like a wireless one.
	// Empty means the mesh is the only batman link (pure wireless backhaul).
	WiredPorts []string
	// LanDevice is the UCI section of the LAN bridge device whose list-valued
	// `ports` bat0 is added to (e.g. "@device[0]" or a named "br_lan"). Empty
	// skips bridging — bat0 is authored but not joined to the LAN.
	LanDevice string
}

func (c Config) withDefaults() Config {
	if c.Iface == "" {
		c.Iface = "bat0"
	}
	if c.RoutingAlgo == "" {
		c.RoutingAlgo = "BATMAN_IV"
	}
	return c
}

// Manager authors the batman-adv layer to UCI.
type Manager struct {
	uci UCI
	cfg Config
}

func NewManager(uci UCI, cfg Config) *Manager {
	return &Manager{uci: uci, cfg: cfg.withDefaults()}
}

// MeshHardif is the batadv_hardif interface name the 802.11s mesh vif attaches
// to via its wifi-iface `network` option. The vif supplies its own device
// dynamically, so this hard interface pins no device.
func (m *Manager) MeshHardif() string {
	return m.cfg.Iface + "_mesh"
}

// WiredHardif is the batadv_hardif interface name for a wired backhaul port,
// sanitized to a valid UCI section name (alnum/underscore only).
func (m *Manager) WiredHardif(port string) string {
	return m.cfg.Iface + "_" + sanitize(port)
}

// Apply authors bat0, a hard interface for the wireless mesh, a hard interface
// per wired port, and bridges bat0 into the LAN. It is idempotent: re-applying
// re-asserts the same sections and keeps exactly one bat0 entry in the bridge
// ports (del-then-add). It authors only `network`; the caller commits/reloads.
func (m *Manager) Apply(ctx context.Context) error {
	// Soft interface: the bat0 mesh device. bridge_loop_avoidance is what lets a
	// node carry both a wired link and the wireless mesh without a broadcast storm
	// — batman detects the redundant L2 path and dedups it where plain bridge STP
	// could not on the home kit's switches.
	soft := map[string]string{
		"proto":                 "batadv",
		"routing_algo":          m.cfg.RoutingAlgo,
		"bridge_loop_avoidance": "1",
	}
	if err := m.uci.SetSection(ctx, "network", m.cfg.Iface, "interface", soft); err != nil {
		return fmt.Errorf("author %s soft interface: %w", m.cfg.Iface, err)
	}

	// Wireless mesh hard interface: no device — the 802.11s vif attaches to it via
	// the wifi-iface `network`, supplying the device dynamically.
	if err := m.uci.SetSection(ctx, "network", m.MeshHardif(), "interface", map[string]string{
		"proto":  "batadv_hardif",
		"master": m.cfg.Iface,
	}); err != nil {
		return fmt.Errorf("author mesh hardif: %w", err)
	}

	// Wired backhaul hard interfaces: pin each ethernet device to bat0, and take
	// it OUT of the LAN bridge. A device that is both a br-lan member and a batadv
	// hardif is the redundant wired+wireless L2 path that storms — the uplink must
	// belong to batman exclusively, while client jacks stay normal bridge ports.
	for _, port := range m.cfg.WiredPorts {
		if err := m.uci.SetSection(ctx, "network", m.WiredHardif(port), "interface", map[string]string{
			"proto":  "batadv_hardif",
			"master": m.cfg.Iface,
			"device": port,
		}); err != nil {
			return fmt.Errorf("author wired hardif %s: %w", port, err)
		}
		if m.cfg.LanDevice != "" {
			if err := m.uci.DelListItem(ctx, "network", m.cfg.LanDevice, "ports", port); err != nil {
				return fmt.Errorf("remove enslaved %s from lan bridge: %w", port, err)
			}
		}
	}

	// Bridge bat0 into the LAN so DHCP and clients ride on top of the mesh.
	// del-then-add keeps the apply idempotent (uci add_list appends, no dedup).
	if m.cfg.LanDevice != "" {
		if err := m.uci.DelListItem(ctx, "network", m.cfg.LanDevice, "ports", m.cfg.Iface); err != nil {
			return fmt.Errorf("clear stale %s bridge port: %w", m.cfg.Iface, err)
		}
		if err := m.uci.AddListItem(ctx, "network", m.cfg.LanDevice, "ports", m.cfg.Iface); err != nil {
			return fmt.Errorf("bridge %s into lan: %w", m.cfg.Iface, err)
		}
	}
	return nil
}

// Teardown removes the batman sections and unbridges bat0 from the LAN, so a
// degrade to a direct mesh-on-lan bridge re-sets cleanly. It is best-effort per
// section is not required — callers degrade only when batman failed to come up.
func (m *Manager) Teardown(ctx context.Context) error {
	if m.cfg.LanDevice != "" {
		if err := m.uci.DelListItem(ctx, "network", m.cfg.LanDevice, "ports", m.cfg.Iface); err != nil {
			return fmt.Errorf("unbridge %s from lan: %w", m.cfg.Iface, err)
		}
		// Hand each enslaved uplink back to br-lan so the node keeps its wired
		// connectivity once batman is gone. del-then-add keeps it to one entry.
		for _, port := range m.cfg.WiredPorts {
			if err := m.uci.DelListItem(ctx, "network", m.cfg.LanDevice, "ports", port); err != nil {
				return fmt.Errorf("clear stale %s lan port: %w", port, err)
			}
			if err := m.uci.AddListItem(ctx, "network", m.cfg.LanDevice, "ports", port); err != nil {
				return fmt.Errorf("restore %s to lan bridge: %w", port, err)
			}
		}
	}
	sections := []string{m.MeshHardif()}
	for _, port := range m.cfg.WiredPorts {
		sections = append(sections, m.WiredHardif(port))
	}
	sections = append(sections, m.cfg.Iface)
	for _, s := range sections {
		if err := m.uci.Delete(ctx, "network", s); err != nil {
			return fmt.Errorf("delete %s: %w", s, err)
		}
	}
	return nil
}

// sanitize maps any character that is not allowed in a UCI section name to an
// underscore, so an interface like "eth0.2" yields a valid section "eth0_2".
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, s)
}
