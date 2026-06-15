package batman

import (
	"context"
	"testing"
)

// recordUCI is a minimal in-memory UCI that records authored sections, scalar
// options, and list members so tests can assert the batman-adv layout without a
// real ubus/uci. It merges SetSection values and subsequent Set calls into one
// per-section option view, and tracks list add/del per (section, option).
type recordUCI struct {
	// section -> option -> value (covers both SetSection and Set)
	opts map[string]map[string]string
	// section -> option -> ordered list members
	lists   map[string]map[string][]string
	deleted []string
}

func newRecordUCI() *recordUCI {
	return &recordUCI{
		opts:  map[string]map[string]string{},
		lists: map[string]map[string][]string{},
	}
}

func (r *recordUCI) put(section, option, value string) {
	if r.opts[section] == nil {
		r.opts[section] = map[string]string{}
	}
	r.opts[section][option] = value
}

func (r *recordUCI) Set(ctx context.Context, pkg, section, option, value string) error {
	r.put(section, option, value)
	return nil
}

func (r *recordUCI) SetSection(ctx context.Context, pkg, section, sectionType string, values map[string]string) error {
	r.put(section, ".type", sectionType)
	for k, v := range values {
		r.put(section, k, v)
	}
	return nil
}

func (r *recordUCI) AddListItem(ctx context.Context, pkg, section, option, value string) error {
	if r.lists[section] == nil {
		r.lists[section] = map[string][]string{}
	}
	r.lists[section][option] = append(r.lists[section][option], value)
	return nil
}

func (r *recordUCI) DelListItem(ctx context.Context, pkg, section, option, value string) error {
	if r.lists[section] == nil {
		return nil
	}
	kept := r.lists[section][option][:0]
	for _, v := range r.lists[section][option] {
		if v != value {
			kept = append(kept, v)
		}
	}
	r.lists[section][option] = kept
	return nil
}

func (r *recordUCI) Delete(ctx context.Context, pkg, section string) error {
	r.deleted = append(r.deleted, section)
	delete(r.opts, section)
	delete(r.lists, section)
	return nil
}

func (r *recordUCI) opt(t *testing.T, section, option string) string {
	t.Helper()
	v, ok := r.opts[section][option]
	if !ok {
		t.Fatalf("section %q has no option %q (have %v)", section, option, r.opts[section])
	}
	return v
}

func TestApplyAuthorsSoftInterfaceWithBLA(t *testing.T) {
	r := newRecordUCI()
	m := NewManager(r, Config{Iface: "bat0", LanDevice: "@device[0]"})

	if err := m.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if got := r.opt(t, "bat0", ".type"); got != "interface" {
		t.Errorf("bat0 .type = %q, want interface", got)
	}
	if got := r.opt(t, "bat0", "proto"); got != "batadv" {
		t.Errorf("bat0 proto = %q, want batadv", got)
	}
	// Bridge-loop-avoidance is the whole point of using batman over a plain
	// bridge: it dedups the redundant wired+wireless L2 path. It must be on.
	if got := r.opt(t, "bat0", "bridge_loop_avoidance"); got != "1" {
		t.Errorf("bat0 bridge_loop_avoidance = %q, want 1", got)
	}
	// Default routing algorithm.
	if got := r.opt(t, "bat0", "routing_algo"); got != "BATMAN_IV" {
		t.Errorf("bat0 routing_algo = %q, want BATMAN_IV", got)
	}
}

func TestApplyAuthorsMeshHardif(t *testing.T) {
	r := newRecordUCI()
	m := NewManager(r, Config{Iface: "bat0"})

	if err := m.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	hardif := m.MeshHardif()
	if hardif == "" {
		t.Fatal("MeshHardif() returned empty")
	}
	if got := r.opt(t, hardif, "proto"); got != "batadv_hardif" {
		t.Errorf("%s proto = %q, want batadv_hardif", hardif, got)
	}
	if got := r.opt(t, hardif, "master"); got != "bat0" {
		t.Errorf("%s master = %q, want bat0", hardif, got)
	}
	// The wireless mesh vif supplies its own device dynamically (it attaches via
	// the wifi-iface `network`), so the mesh hardif must NOT pin a device.
	if dev, ok := r.opts[hardif]["device"]; ok {
		t.Errorf("mesh hardif %s pinned device=%q, want none", hardif, dev)
	}
}

func TestApplyEnslavesWiredPorts(t *testing.T) {
	r := newRecordUCI()
	m := NewManager(r, Config{Iface: "bat0", WiredPorts: []string{"eth0", "eth0.2"}})

	if err := m.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Each wired backhaul port becomes a batadv_hardif enslaved to bat0, with the
	// device pinned. Section names must be valid UCI (alnum/underscore only), so
	// "eth0.2" is sanitized.
	for _, port := range []string{"eth0", "eth0.2"} {
		section := m.WiredHardif(port)
		if got := r.opt(t, section, "proto"); got != "batadv_hardif" {
			t.Errorf("port %s: proto = %q, want batadv_hardif", port, got)
		}
		if got := r.opt(t, section, "master"); got != "bat0" {
			t.Errorf("port %s: master = %q, want bat0", port, got)
		}
		if got := r.opt(t, section, "device"); got != port {
			t.Errorf("port %s: device = %q, want %q", port, got, port)
		}
		for _, c := range section {
			if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
				t.Errorf("section %q contains invalid UCI char %q", section, c)
			}
		}
	}
}

func TestApplyBridgesBat0IntoLan(t *testing.T) {
	r := newRecordUCI()
	m := NewManager(r, Config{Iface: "bat0", LanDevice: "@device[0]"})

	if err := m.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	ports := r.lists["@device[0]"]["ports"]
	if len(ports) != 1 || ports[0] != "bat0" {
		t.Errorf("br-lan ports = %v, want [bat0]", ports)
	}
}

func TestApplyRemovesEnslavedWiredPortsFromLanBridge(t *testing.T) {
	r := newRecordUCI()
	// The LAN bridge starts with the uplink jack (wan) and a client jack (lan)
	// as members — the dumb-AP layout where both ethernet jacks are bridged.
	r.AddListItem(context.Background(), "network", "@device[0]", "ports", "lan")
	r.AddListItem(context.Background(), "network", "@device[0]", "ports", "wan")

	m := NewManager(r, Config{Iface: "bat0", LanDevice: "@device[0]", WiredPorts: []string{"wan"}})
	if err := m.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The enslaved uplink must LEAVE br-lan — a device that is both a bridge
	// member and a batadv hardif is the redundant L2 path that storms. The
	// client jack stays a normal bridge port, and bat0 is added.
	ports := r.lists["@device[0]"]["ports"]
	has := map[string]bool{}
	for _, p := range ports {
		has[p] = true
	}
	if has["wan"] {
		t.Errorf("enslaved uplink 'wan' still in br-lan ports %v; must leave the bridge", ports)
	}
	if !has["lan"] {
		t.Errorf("client jack 'lan' missing from br-lan ports %v; clients must stay bridged", ports)
	}
	if !has["bat0"] {
		t.Errorf("bat0 missing from br-lan ports %v", ports)
	}
}

func TestTeardownRestoresWiredPortsToLanBridge(t *testing.T) {
	r := newRecordUCI()
	r.AddListItem(context.Background(), "network", "@device[0]", "ports", "lan")
	r.AddListItem(context.Background(), "network", "@device[0]", "ports", "wan")

	m := NewManager(r, Config{Iface: "bat0", LanDevice: "@device[0]", WiredPorts: []string{"wan"}})
	if err := m.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := m.Teardown(context.Background()); err != nil {
		t.Fatalf("Teardown: %v", err)
	}

	// Degrading back to a direct lan bridge must hand the uplink back to br-lan
	// so the node keeps its wired connectivity, with no duplicate entry.
	ports := r.lists["@device[0]"]["ports"]
	count := 0
	for _, p := range ports {
		if p == "wan" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("after teardown, 'wan' appears %d times in br-lan ports %v, want exactly 1", count, ports)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	r := newRecordUCI()
	m := NewManager(r, Config{Iface: "bat0", LanDevice: "@device[0]", WiredPorts: []string{"eth0"}})

	for i := 0; i < 3; i++ {
		if err := m.Apply(context.Background()); err != nil {
			t.Fatalf("Apply #%d: %v", i, err)
		}
	}

	// Re-applying must not stack duplicate bat0 entries in the bridge ports — the
	// del-then-add pattern keeps exactly one.
	if ports := r.lists["@device[0]"]["ports"]; len(ports) != 1 || ports[0] != "bat0" {
		t.Errorf("after 3 applies, br-lan ports = %v, want [bat0]", ports)
	}
}

func TestApplyWithoutLanDeviceSkipsBridge(t *testing.T) {
	r := newRecordUCI()
	m := NewManager(r, Config{Iface: "bat0"})

	if err := m.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(r.lists) != 0 {
		t.Errorf("no LanDevice set, but list ops happened: %v", r.lists)
	}
}

func TestTeardownRemovesSections(t *testing.T) {
	r := newRecordUCI()
	m := NewManager(r, Config{Iface: "bat0", LanDevice: "@device[0]", WiredPorts: []string{"eth0"}})

	if err := m.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := m.Teardown(context.Background()); err != nil {
		t.Fatalf("Teardown: %v", err)
	}

	// bat0 must be unbridged from br-lan and its sections removed, so a degrade to
	// direct-bridge re-sets cleanly — while the enslaved uplink is handed back to
	// br-lan so the node keeps wired connectivity without batman.
	if ports := r.lists["@device[0]"]["ports"]; len(ports) != 1 || ports[0] != "eth0" {
		t.Errorf("after teardown, br-lan ports = %v, want [eth0] (bat0 gone, uplink restored)", ports)
	}
	for _, section := range []string{"bat0", m.MeshHardif(), m.WiredHardif("eth0")} {
		if _, ok := r.opts[section]; ok {
			t.Errorf("section %q survived teardown", section)
		}
	}
}

func TestCustomRoutingAlgo(t *testing.T) {
	r := newRecordUCI()
	m := NewManager(r, Config{Iface: "bat0", RoutingAlgo: "BATMAN_V"})

	if err := m.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := r.opt(t, "bat0", "routing_algo"); got != "BATMAN_V" {
		t.Errorf("routing_algo = %q, want BATMAN_V", got)
	}
}
