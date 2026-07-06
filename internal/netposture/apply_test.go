package netposture

import (
	"context"
	"errors"
	"testing"
)

// fakeUCI records the operations Apply performs so tests can assert the authored
// posture without a live ubus. wanDevice is what Get(network.wan.device)
// returns ("" => not found, i.e. no wan to auto-detect).
type fakeUCI struct {
	sets      map[string]string // "pkg.section.option" -> value
	adds      []string          // "pkg.section.option=value"
	dels      []string
	ops       []string // ordered op kinds for sequencing checks
	wanDevice string
}

func newFakeUCI() *fakeUCI { return &fakeUCI{sets: map[string]string{}} }

func (f *fakeUCI) Get(ctx context.Context, pkg, section, option string) (string, error) {
	if pkg == "network" && section == "wan" && option == "device" && f.wanDevice != "" {
		return f.wanDevice, nil
	}
	return "", errors.New("not found")
}

func (f *fakeUCI) Set(ctx context.Context, pkg, section, option, value string) error {
	f.sets[pkg+"."+section+"."+option] = value
	f.ops = append(f.ops, "set")
	return nil
}

func (f *fakeUCI) AddListItem(ctx context.Context, pkg, section, option, value string) error {
	f.adds = append(f.adds, pkg+"."+section+"."+option+"="+value)
	f.ops = append(f.ops, "addlist")
	return nil
}

func (f *fakeUCI) DelListItem(ctx context.Context, pkg, section, option, value string) error {
	f.dels = append(f.dels, pkg+"."+section+"."+option+"="+value)
	f.ops = append(f.ops, "dellist")
	return nil
}

func (f *fakeUCI) Commit(ctx context.Context, pkg string) error {
	f.ops = append(f.ops, "commit:"+pkg)
	return nil
}

func (f *fakeUCI) Reload(ctx context.Context) error {
	f.ops = append(f.ops, "reload")
	return nil
}

func (f *fakeUCI) lastOp() string { return f.ops[len(f.ops)-1] }

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// Guest posture bridges the uplink into lan, makes lan a DHCP client, disables
// the wan, stands down authoritative DHCP, and reloads last.
func TestApplyGuest(t *testing.T) {
	f := newFakeUCI()
	m := NewManager(f, Config{UplinkPort: "wan", LanDevice: "@device[0]"})

	if err := m.Apply(context.Background(), RoleGuest); err != nil {
		t.Fatalf("apply guest: %v", err)
	}

	if !contains(f.adds, "network.@device[0].ports=wan") {
		t.Fatalf("uplink not bridged into lan; adds=%v", f.adds)
	}
	for k, want := range map[string]string{
		"network.lan.proto":     "dhcp",
		"network.wan.disabled":  "1",
		"network.wan6.disabled": "1",
		"dhcp.lan.ignore":       "1",
	} {
		if f.sets[k] != want {
			t.Fatalf("set %s = %q, want %q (sets=%v)", k, f.sets[k], want, f.sets)
		}
	}
	if f.lastOp() != "reload" {
		t.Fatalf("expected reload last, ops=%v", f.ops)
	}
}

// Without a known bridge layout, Guest skips the bridge edit but still stands
// the node down (no mis-bridging a board we don't understand).
func TestApplyGuestSkipsBridgeWhenUnconfigured(t *testing.T) {
	f := newFakeUCI()
	m := NewManager(f, Config{})

	if err := m.Apply(context.Background(), RoleGuest); err != nil {
		t.Fatalf("apply guest: %v", err)
	}
	if len(f.adds) != 0 {
		t.Fatalf("expected no bridge edit, adds=%v", f.adds)
	}
	if f.sets["dhcp.lan.ignore"] != "1" {
		t.Fatalf("expected authoritative DHCP stood down; sets=%v", f.sets)
	}
}

// With no explicit UplinkPort, Guest auto-detects the WAN port from
// network.wan.device and bridges that into br-lan — so any jack works without
// per-board config.
func TestApplyGuestAutoDetectsWanPort(t *testing.T) {
	f := newFakeUCI()
	f.wanDevice = "wan"
	m := NewManager(f, Config{LanDevice: "@device[0]"})

	if err := m.Apply(context.Background(), RoleGuest); err != nil {
		t.Fatalf("apply guest: %v", err)
	}
	if !contains(f.adds, "network.@device[0].ports=wan") {
		t.Fatalf("auto-detected wan not bridged into lan; adds=%v", f.adds)
	}
}

// A single-jack AP whose lone port is already the lan device has no wan to
// auto-detect: Guest skips the bridge edit but still stands the node down, and
// the lan-as-DHCP-client carries discovery.
func TestApplyGuestSingleJackNoWan(t *testing.T) {
	f := newFakeUCI() // wanDevice == "" -> Get returns not-found
	m := NewManager(f, Config{LanDevice: "@device[0]"})

	if err := m.Apply(context.Background(), RoleGuest); err != nil {
		t.Fatalf("apply guest: %v", err)
	}
	if len(f.adds) != 0 {
		t.Fatalf("no wan to bridge, expected no bridge edit; adds=%v", f.adds)
	}
	if f.sets["network.lan.proto"] != "dhcp" || f.sets["dhcp.lan.ignore"] != "1" {
		t.Fatalf("expected lan DHCP-client + authoritative DHCP off; sets=%v", f.sets)
	}
}

// Controller posture restores the gateway: un-bridge the uplink, re-enable wan,
// re-enable authoritative DHCP — and does not rewrite lan proto.
func TestApplyControllerRestoresGateway(t *testing.T) {
	f := newFakeUCI()
	m := NewManager(f, Config{UplinkPort: "wan", LanDevice: "@device[0]"})

	if err := m.Apply(context.Background(), RoleController); err != nil {
		t.Fatalf("apply controller: %v", err)
	}
	if !contains(f.dels, "network.@device[0].ports=wan") {
		t.Fatalf("uplink not removed from lan bridge; dels=%v", f.dels)
	}
	if f.sets["network.wan.disabled"] != "0" || f.sets["dhcp.lan.ignore"] != "0" {
		t.Fatalf("gateway not restored; sets=%v", f.sets)
	}
	if _, rewrote := f.sets["network.lan.proto"]; rewrote {
		t.Fatalf("controller posture must not rewrite lan proto; sets=%v", f.sets)
	}
}

// Without batman (plain-L2 fallback) a mesh node authors the full bridged
// single-gateway posture (the same shape as Guest): fold the uplink jack into
// br-lan, run lan as a DHCP client, disable the routed wan, and stand down
// authoritative DHCP. Otherwise a claimed node keeps a routed/NAT'd wan of its
// own and its bridged mesh is an island — mesh traffic can't reach the home WAN,
// which only egresses via the controller's single gateway.
func TestApplyMeshNodeBridgesIntoHome(t *testing.T) {
	f := newFakeUCI()
	m := NewManager(f, Config{UplinkPort: "wan", LanDevice: "@device[0]"}) // BatmanActive defaults false

	if err := m.Apply(context.Background(), RoleMeshNode); err != nil {
		t.Fatalf("apply mesh node: %v", err)
	}
	if !contains(f.adds, "network.@device[0].ports=wan") {
		t.Fatalf("uplink not folded into br-lan; adds=%v", f.adds)
	}
	for k, want := range map[string]string{
		"network.lan.proto":     "dhcp",
		"network.wan.disabled":  "1",
		"network.wan6.disabled": "1",
		"dhcp.lan.ignore":       "1",
	} {
		if f.sets[k] != want {
			t.Fatalf("set %s = %q, want %q (sets=%v)", k, f.sets[k], want, f.sets)
		}
	}
	if f.lastOp() != "reload" {
		t.Fatalf("expected reload last, ops=%v", f.ops)
	}
}

// With batman active, a mesh node must NOT fold the uplink into br-lan — bat0 is
// the bridged backhaul and the batman classifier owns the physical uplink port.
// Re-adding it to br-lan on every apply would undo the classifier's enslavement
// and recreate the storming br-lan+bat0 double path. The L3 posture (lan DHCP
// client, wan disabled, authoritative DHCP off) is still authored.
func TestApplyMeshNodeBatmanActiveLeavesUplinkToClassifier(t *testing.T) {
	f := newFakeUCI()
	m := NewManager(f, Config{UplinkPort: "wan", LanDevice: "@device[0]", BatmanActive: true})

	if err := m.Apply(context.Background(), RoleMeshNode); err != nil {
		t.Fatalf("apply mesh node: %v", err)
	}
	if len(f.adds) != 0 {
		t.Fatalf("batman-active mesh node must not fold the uplink into br-lan; adds=%v", f.adds)
	}
	for k, want := range map[string]string{
		"network.lan.proto":    "dhcp",
		"network.wan.disabled": "1",
		"dhcp.lan.ignore":      "1",
	} {
		if f.sets[k] != want {
			t.Fatalf("L3 posture still required; set %s = %q, want %q (sets=%v)", k, f.sets[k], want, f.sets)
		}
	}
}

// Guest folds the uplink even when batman is enabled: a still-discovering device
// has no active home/profile yet, so bat0 is not up and there is no classifier to
// defer to — L2 adjacency is what discovery needs.
func TestApplyGuestFoldsEvenWithBatmanActive(t *testing.T) {
	f := newFakeUCI()
	m := NewManager(f, Config{UplinkPort: "wan", LanDevice: "@device[0]", BatmanActive: true})

	if err := m.Apply(context.Background(), RoleGuest); err != nil {
		t.Fatalf("apply guest: %v", err)
	}
	if !contains(f.adds, "network.@device[0].ports=wan") {
		t.Fatalf("guest must fold the uplink regardless of batman; adds=%v", f.adds)
	}
}

// A mesh node on a board with no known bridge layout still stands down (no
// mis-bridging), exactly as Guest does — the two share the bridged posture.
func TestApplyMeshNodeSkipsBridgeWhenUnconfigured(t *testing.T) {
	f := newFakeUCI()
	m := NewManager(f, Config{})

	if err := m.Apply(context.Background(), RoleMeshNode); err != nil {
		t.Fatalf("apply mesh node: %v", err)
	}
	if len(f.adds) != 0 {
		t.Fatalf("expected no bridge edit, adds=%v", f.adds)
	}
	if f.sets["dhcp.lan.ignore"] != "1" || f.sets["network.lan.proto"] != "dhcp" {
		t.Fatalf("expected DHCP stood down + lan DHCP-client; sets=%v", f.sets)
	}
}

func TestApplyUnknownRoleErrors(t *testing.T) {
	if err := NewManager(newFakeUCI(), Config{}).Apply(context.Background(), Role("bogus")); err == nil {
		t.Fatal("expected error for unknown role")
	}
}
