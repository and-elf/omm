package netposture

import (
	"context"
	"fmt"
)

// UCI is the subset of uci.Client this package needs (interface segregation):
// a scalar get (to auto-detect the WAN port), scalar sets, list add/del for the
// bridge `ports`, commit and reload. The real *uci.client satisfies it.
type UCI interface {
	Get(ctx context.Context, pkg, section, option string) (string, error)
	Set(ctx context.Context, pkg, section, option, value string) error
	AddListItem(ctx context.Context, pkg, section, option, value string) error
	DelListItem(ctx context.Context, pkg, section, option, value string) error
	Commit(ctx context.Context, pkg string) error
	Reload(ctx context.Context) error
}

// Config tunes how a posture is authored to UCI.
type Config struct {
	// UplinkPort is the wired uplink device Guest posture bridges into br-lan so
	// the node is L2-adjacent to a controller and hears discovery broadcasts.
	// Empty means auto-detect: read network.wan.device and bridge that, so any
	// jack the operator uses (including the lone port of a single-jack AP wired
	// as `wan`) ends up on the home L2. Set explicitly only to override.
	UplinkPort string
	// LanDevice is the UCI section of the LAN bridge device whose list-valued
	// `ports` the uplink is added to / removed from (e.g. "@device[0]" or a named
	// "br_lan"). Empty skips the bridge edit, so a board with no LAN bridge is
	// left alone rather than mis-bridged.
	LanDevice string
}

// Manager authors a node's network posture to UCI.
type Manager struct {
	uci UCI
	cfg Config
}

func NewManager(uci UCI, cfg Config) *Manager {
	return &Manager{uci: uci, cfg: cfg}
}

// Apply authors the UCI for role and reloads. It is idempotent: re-applying the
// same posture re-asserts the same options. Callers decide *when* to apply (on a
// lifecycle transition) and whether posture management is enabled at all.
func (m *Manager) Apply(ctx context.Context, role Role) error {
	switch role {
	case RoleGuest:
		return m.applyGuest(ctx)
	case RoleController:
		return m.applyController(ctx)
	case RoleMeshNode:
		return m.applyMeshNode(ctx)
	default:
		return fmt.Errorf("netposture: unknown role %q", role)
	}
}

// applyGuest is the unclaimed dumb-AP posture: bridge the uplink into br-lan,
// make lan a DHCP client, disable the routed wan, and stand down the
// authoritative LAN DHCP server — so the node is L2-adjacent to a controller and
// discovery broadcasts reach the listener instead of being dropped at the wan
// firewall. The ommsetup (setup-AP) sections are never touched.
func (m *Manager) applyGuest(ctx context.Context) error {
	if uplink := m.uplinkPort(ctx); uplink != "" && m.cfg.LanDevice != "" {
		if err := m.uci.AddListItem(ctx, "network", m.cfg.LanDevice, "ports", uplink); err != nil {
			return fmt.Errorf("bridge uplink into lan: %w", err)
		}
	}
	sets := []set{
		{"network", "lan", "proto", "dhcp"},
		{"network", "wan", "disabled", "1"},
		{"network", "wan6", "disabled", "1"},
		{"dhcp", "lan", "ignore", "1"},
	}
	return m.applySets(ctx, sets, "network", "dhcp")
}

// applyController restores the gateway posture: un-bridge the uplink, re-enable
// the routed wan, and re-enable the authoritative LAN DHCP server. It does not
// rewrite lan's proto/address, to avoid clobbering an operator's gateway config.
func (m *Manager) applyController(ctx context.Context) error {
	if uplink := m.uplinkPort(ctx); uplink != "" && m.cfg.LanDevice != "" {
		if err := m.uci.DelListItem(ctx, "network", m.cfg.LanDevice, "ports", uplink); err != nil {
			return fmt.Errorf("unbridge uplink from lan: %w", err)
		}
	}
	sets := []set{
		{"network", "wan", "disabled", "0"},
		{"network", "wan6", "disabled", "0"},
		{"dhcp", "lan", "ignore", "0"},
	}
	return m.applySets(ctx, sets, "network", "dhcp")
}

// applyMeshNode is a claimed satellite: it must not run authoritative DHCP (the
// controller serves the home), but its uplink (often a wireless station) is left
// alone. Fuller mesh-node bridging is a follow-up; standing down DHCP is the
// part that prevents a competing server on the home segment.
func (m *Manager) applyMeshNode(ctx context.Context) error {
	return m.applySets(ctx, []set{{"dhcp", "lan", "ignore", "1"}}, "dhcp")
}

type set struct{ pkg, section, option, value string }

// applySets writes every option, commits each named package once, then reloads
// so the change takes effect (a bare commit only stages the files).
func (m *Manager) applySets(ctx context.Context, sets []set, pkgs ...string) error {
	for _, s := range sets {
		if err := m.uci.Set(ctx, s.pkg, s.section, s.option, s.value); err != nil {
			return fmt.Errorf("set %s.%s.%s: %w", s.pkg, s.section, s.option, err)
		}
	}
	for _, pkg := range pkgs {
		if err := m.uci.Commit(ctx, pkg); err != nil {
			return fmt.Errorf("commit %s: %w", pkg, err)
		}
	}
	return m.uci.Reload(ctx)
}

// uplinkPort resolves the wired uplink device to bridge into br-lan: the
// explicit UplinkPort if set, else auto-detected from network.wan.device.
// Returns "" when there is no wan to bridge (e.g. a single-jack AP whose only
// port is already the lan device) — the LAN ports are already in br-lan, so
// Guest still works without a bridge edit.
func (m *Manager) uplinkPort(ctx context.Context) string {
	if m.cfg.UplinkPort != "" {
		return m.cfg.UplinkPort
	}
	dev, err := m.uci.Get(ctx, "network", "wan", "device")
	if err != nil {
		return ""
	}
	return dev
}
