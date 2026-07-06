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
	// BatmanActive reports whether batman-adv is the forwarding layer on this
	// node. When true, `bat0` is the device bridged into br-lan and the physical
	// uplink is a batman hardif owned by the batman port classifier — so the
	// Mesh-node posture must NOT fold the uplink into br-lan (doing so on every
	// apply would fight the classifier: a port in both br-lan and bat0 is the
	// redundant L2 path that storms). The Guest posture folds regardless, since a
	// still-discovering device has no home/profile yet and thus no live bat0.
	BatmanActive bool
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

// applyBridgedDumbAP authors the bridged dumb-AP posture shared by the Guest and
// Mesh-node roles: make lan a DHCP client, disable the routed wan, and stand down
// the authoritative LAN DHCP server. The node then sits on the home L2 with no
// routed/NAT'd wan of its own, so its clients — and its 802.11s mesh, which the
// batman layer bridges into the same br-lan — reach the internet through the
// controller's single gateway instead of a local NAT that would strand mesh
// traffic from the wan. When foldUplink is set, the wired uplink jack is also
// added to br-lan; callers clear it when the batman classifier owns the physical
// port (see Config.BatmanActive). The ommsetup (setup-AP) sections are never
// touched.
func (m *Manager) applyBridgedDumbAP(ctx context.Context, foldUplink bool) error {
	if foldUplink {
		if uplink := m.uplinkPort(ctx); uplink != "" && m.cfg.LanDevice != "" {
			if err := m.uci.AddListItem(ctx, "network", m.cfg.LanDevice, "ports", uplink); err != nil {
				return fmt.Errorf("bridge uplink into lan: %w", err)
			}
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

// applyGuest is the unclaimed dumb-AP posture: it authors the bridged posture so
// the node is L2-adjacent to a controller and discovery broadcasts reach the
// listener instead of being dropped at a routed, firewalled wan. It always folds
// the uplink into br-lan: a still-discovering device has no active home and thus
// no profile/bat0, so batman is not yet forwarding and there is no classifier to
// defer to — L2 adjacency is what discovery needs. The Guest fold is also what
// first makes the uplink a br-lan-member candidate for the batman classifier once
// the device is later claimed.
func (m *Manager) applyGuest(ctx context.Context) error {
	return m.applyBridgedDumbAP(ctx, true)
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

// applyMeshNode is a claimed satellite: it authors the bridged posture so the
// node is a pure L2 bridge into the home. It must not run authoritative DHCP (the
// controller serves the home), and it must not keep a routed/NAT'd wan of its own
// — otherwise its bridged 802.11s mesh is an island and mesh traffic can't reach
// the home WAN, which egresses only via the controller's gateway.
//
// It folds the uplink into br-lan ONLY when batman is not the forwarding layer.
// With batman active, bat0 is the bridged backhaul and the physical uplink is a
// batman hardif owned by the port classifier (which enslaves peer-facing br-lan
// ports out of the bridge); re-adding the uplink to br-lan on every posture apply
// would undo that enslavement and recreate the storming br-lan+bat0 double path.
// The uplink is already a br-lan-member candidate from the Guest phase, so the
// classifier still sees it — netposture just stops fighting for it.
func (m *Manager) applyMeshNode(ctx context.Context) error {
	return m.applyBridgedDumbAP(ctx, !m.cfg.BatmanActive)
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
