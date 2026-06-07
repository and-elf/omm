package setupap

import (
	"context"
	"errors"
	"testing"

	"github.com/and-elf/omm/internal/uci"
)

// recordingUCI captures the ordered UCI operations and the section values an
// operation carried, so tests can assert exactly what config the setup AP
// authors (and that it commits before it reloads).
type recordingUCI struct {
	ops       []string
	sections  map[string]map[string]string // "pkg.section" -> values
	sets      map[string]string            // "pkg.section.option" -> value
	reloadErr error
}

var _ uci.Client = (*recordingUCI)(nil)

func newRecordingUCI() *recordingUCI {
	return &recordingUCI{sections: map[string]map[string]string{}, sets: map[string]string{}}
}

func (r *recordingUCI) Get(ctx context.Context, pkg, section, option string) (string, error) {
	return "", nil
}

func (r *recordingUCI) Sections(ctx context.Context, pkg string) (map[string]map[string]string, error) {
	return nil, nil
}

func (r *recordingUCI) Set(ctx context.Context, pkg, section, option, value string) error {
	r.ops = append(r.ops, "set:"+pkg+"."+section+"."+option)
	r.sets[pkg+"."+section+"."+option] = value
	return nil
}

func (r *recordingUCI) SetSection(ctx context.Context, pkg, section, sectionType string, values map[string]string) error {
	r.ops = append(r.ops, "setsection:"+pkg+"."+section)
	r.sections[pkg+"."+section] = values
	return nil
}

func (r *recordingUCI) Delete(ctx context.Context, pkg, section string) error {
	r.ops = append(r.ops, "delete:"+pkg+"."+section)
	return nil
}

func (r *recordingUCI) Commit(ctx context.Context, pkg string) error {
	r.ops = append(r.ops, "commit:"+pkg)
	return nil
}

func (r *recordingUCI) Reload(ctx context.Context) error {
	r.ops = append(r.ops, "reload")
	return r.reloadErr
}

func (r *recordingUCI) Close() error { return nil }

func reloadIsLast(ops []string) bool {
	return len(ops) > 0 && ops[len(ops)-1] == "reload"
}

func commitsBeforeReload(ops []string) bool {
	for i, op := range ops {
		if op == "reload" {
			for _, later := range ops[i+1:] {
				if len(later) >= 7 && later[:7] == "commit:" {
					return false
				}
			}
		}
	}
	return true
}

func TestEnableDerivesSSIDFromNodeID(t *testing.T) {
	r := newRecordingUCI()
	m := New(r, Config{Radio: "radio0"})

	// Node IDs are 64-hex; the setup SSID's suffix is the last 4 so it is short,
	// stable, and printable on a device label/QR for out-of-band join (iOS has
	// no WiFi-scan API).
	nodeID := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abWXYZ"
	if err := m.Enable(context.Background(), nodeID); err != nil {
		t.Fatalf("enable: %v", err)
	}

	wifi, ok := r.sections["wireless.omm_setup"]
	if !ok {
		t.Fatalf("expected a wireless.omm_setup section, ops=%v", r.ops)
	}
	if want := "OMM-Setup-WXYZ"; wifi["ssid"] != want {
		t.Fatalf("ssid = %q, want %q", wifi["ssid"], want)
	}
	if wifi["mode"] != "ap" {
		t.Fatalf("mode = %q, want ap", wifi["mode"])
	}
	if wifi["network"] != "ommsetup" {
		t.Fatalf("network = %q, want ommsetup", wifi["network"])
	}
	if wifi["device"] != "radio0" {
		t.Fatalf("device = %q, want radio0", wifi["device"])
	}
}

func TestEnableAuthorsSetupNetworkAndDHCP(t *testing.T) {
	r := newRecordingUCI()
	m := New(r, Config{}) // defaults

	if err := m.Enable(context.Background(), "abcd"); err != nil {
		t.Fatalf("enable: %v", err)
	}

	net, ok := r.sections["network.ommsetup"]
	if !ok {
		t.Fatalf("expected network.ommsetup, ops=%v", r.ops)
	}
	if net["ipaddr"] != "192.168.254.1" {
		t.Fatalf("ipaddr = %q, want 192.168.254.1", net["ipaddr"])
	}
	if net["proto"] != "static" {
		t.Fatalf("proto = %q, want static", net["proto"])
	}

	dhcp, ok := r.sections["dhcp.ommsetup"]
	if !ok {
		t.Fatalf("expected dhcp.ommsetup, ops=%v", r.ops)
	}
	if dhcp["interface"] != "ommsetup" {
		t.Fatalf("dhcp interface = %q, want ommsetup", dhcp["interface"])
	}
}

func TestEnableWithKeyUsesWPA2(t *testing.T) {
	r := newRecordingUCI()
	m := New(r, Config{Key: "supersecret"})

	if err := m.Enable(context.Background(), "abcd"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	wifi := r.sections["wireless.omm_setup"]
	if wifi["encryption"] != "psk2" {
		t.Fatalf("encryption = %q, want psk2", wifi["encryption"])
	}
	if wifi["key"] != "supersecret" {
		t.Fatalf("key = %q, want supersecret", wifi["key"])
	}
}

func TestEnableWithoutKeyIsOpen(t *testing.T) {
	r := newRecordingUCI()
	m := New(r, Config{})

	if err := m.Enable(context.Background(), "abcd"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	wifi := r.sections["wireless.omm_setup"]
	if wifi["encryption"] != "none" {
		t.Fatalf("encryption = %q, want none", wifi["encryption"])
	}
	if _, set := wifi["key"]; set {
		t.Fatalf("open network must not set a key, got %q", wifi["key"])
	}
}

func TestEnableEnablesRadioAndReloadsLast(t *testing.T) {
	r := newRecordingUCI()
	m := New(r, Config{Radio: "radio0"})

	if err := m.Enable(context.Background(), "abcd"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// A fresh OpenWrt radio ships disabled; the AP cannot come up unless the
	// radio is enabled.
	if r.sets["wireless.radio0.disabled"] != "0" {
		t.Fatalf("expected radio0 disabled=0, sets=%v", r.sets)
	}
	if !reloadIsLast(r.ops) {
		t.Fatalf("reload must run last, ops=%v", r.ops)
	}
	if !commitsBeforeReload(r.ops) {
		t.Fatalf("all commits must precede reload, ops=%v", r.ops)
	}
}

func TestDisableRemovesSectionsAndReloadsLast(t *testing.T) {
	r := newRecordingUCI()
	m := New(r, Config{})

	if err := m.Disable(context.Background()); err != nil {
		t.Fatalf("disable: %v", err)
	}

	want := map[string]bool{
		"delete:wireless.omm_setup": false,
		"delete:network.ommsetup":   false,
		"delete:dhcp.ommsetup":      false,
	}
	for _, op := range r.ops {
		if _, ok := want[op]; ok {
			want[op] = true
		}
	}
	for op, seen := range want {
		if !seen {
			t.Fatalf("missing %q, ops=%v", op, r.ops)
		}
	}
	if !reloadIsLast(r.ops) {
		t.Fatalf("reload must run last, ops=%v", r.ops)
	}
	if !commitsBeforeReload(r.ops) {
		t.Fatalf("all commits must precede reload, ops=%v", r.ops)
	}
}

func TestEnableUplinkAuthorsStation(t *testing.T) {
	r := newRecordingUCI()
	m := New(r, Config{Radio: "radio0"})

	if err := m.EnableUplink(context.Background(), "HomeNet", "secret"); err != nil {
		t.Fatalf("enable uplink: %v", err)
	}

	wifi, ok := r.sections["wireless.omm_uplink"]
	if !ok {
		t.Fatalf("expected a wireless.omm_uplink section, ops=%v", r.ops)
	}
	if wifi["mode"] != "sta" {
		t.Fatalf("mode = %q, want sta", wifi["mode"])
	}
	if wifi["ssid"] != "HomeNet" {
		t.Fatalf("ssid = %q, want HomeNet", wifi["ssid"])
	}
	if wifi["device"] != "radio0" {
		t.Fatalf("device = %q, want radio0", wifi["device"])
	}
	if wifi["network"] != "ommuplink" {
		t.Fatalf("network = %q, want ommuplink", wifi["network"])
	}
	if wifi["encryption"] != "psk2" || wifi["key"] != "secret" {
		t.Fatalf("expected psk2 + key, got encryption=%q key=%q", wifi["encryption"], wifi["key"])
	}

	// The uplink interface takes its address from the home network over DHCP, so
	// the node gains a route to the controller.
	net, ok := r.sections["network.ommuplink"]
	if !ok {
		t.Fatalf("expected network.ommuplink, ops=%v", r.ops)
	}
	if net["proto"] != "dhcp" {
		t.Fatalf("proto = %q, want dhcp", net["proto"])
	}

	if !reloadIsLast(r.ops) {
		t.Fatalf("reload must run last, ops=%v", r.ops)
	}
	if !commitsBeforeReload(r.ops) {
		t.Fatalf("all commits must precede reload, ops=%v", r.ops)
	}
}

func TestEnableUplinkOpenNetwork(t *testing.T) {
	r := newRecordingUCI()
	m := New(r, Config{})

	if err := m.EnableUplink(context.Background(), "OpenHome", ""); err != nil {
		t.Fatalf("enable uplink: %v", err)
	}
	wifi := r.sections["wireless.omm_uplink"]
	if wifi["encryption"] != "none" {
		t.Fatalf("encryption = %q, want none", wifi["encryption"])
	}
	if _, set := wifi["key"]; set {
		t.Fatalf("open uplink must not set a key, got %q", wifi["key"])
	}
}

func TestDisableRemovesUplinkWhenActive(t *testing.T) {
	r := newRecordingUCI()
	m := New(r, Config{})

	if err := m.EnableUplink(context.Background(), "HomeNet", "secret"); err != nil {
		t.Fatalf("enable uplink: %v", err)
	}
	if err := m.Disable(context.Background()); err != nil {
		t.Fatalf("disable: %v", err)
	}

	deleted := map[string]bool{}
	for _, op := range r.ops {
		deleted[op] = true
	}
	for _, op := range []string{"delete:wireless.omm_uplink", "delete:network.ommuplink"} {
		if !deleted[op] {
			t.Fatalf("missing %q, ops=%v", op, r.ops)
		}
	}
}

func TestDisableWithoutUplinkLeavesUplinkSections(t *testing.T) {
	r := newRecordingUCI()
	m := New(r, Config{})

	if err := m.Disable(context.Background()); err != nil {
		t.Fatalf("disable: %v", err)
	}
	// Uplink was never provisioned: deleting its (absent) sections would error on
	// a real device, so Disable must not touch them.
	for _, op := range r.ops {
		if op == "delete:wireless.omm_uplink" || op == "delete:network.ommuplink" {
			t.Fatalf("must not delete un-provisioned uplink sections, ops=%v", r.ops)
		}
	}
}

func TestEnableReloadFailurePropagates(t *testing.T) {
	r := newRecordingUCI()
	r.reloadErr = errors.New("netifd down")
	m := New(r, Config{})

	if err := m.Enable(context.Background(), "abcd"); err == nil {
		t.Fatal("expected reload failure to propagate, got nil")
	}
}
