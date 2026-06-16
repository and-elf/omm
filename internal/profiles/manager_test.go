package profiles

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
	"github.com/and-elf/omm/internal/uci"
)

// fakeUCI records the ordered sequence of UCI operations so tests can assert
// that a profile is committed and then applied (reloaded), plus the sections it
// authored (keyed by section name) and plain Set calls so tests can check the
// wireless layout.
type fakeUCI struct {
	ops       []string
	sections  map[string]map[string]string
	sets      []string                     // "pkg.section.option=value" for plain Set calls
	deleted   []string                     // section names passed to Delete
	wireless  map[string]map[string]string // canned Sections("wireless") response
	reloadErr error
}

var _ uci.Client = (*fakeUCI)(nil)

func (f *fakeUCI) Get(ctx context.Context, pkg, section, option string) (string, error) {
	return "", nil
}

func (f *fakeUCI) Sections(ctx context.Context, pkg string) (map[string]map[string]string, error) {
	if pkg == "wireless" {
		return f.wireless, nil
	}
	return nil, nil
}

func (f *fakeUCI) Set(ctx context.Context, pkg, section, option, value string) error {
	f.ops = append(f.ops, "set:"+pkg)
	f.sets = append(f.sets, pkg+"."+section+"."+option+"="+value)
	// Reflect a scalar set into an already-authored section so tests can observe
	// post-author edits (e.g. re-pointing omm_mesh.network on a batman fallback).
	if sec := f.sections[section]; sec != nil {
		sec[option] = value
	}
	return nil
}

func (f *fakeUCI) SetSection(ctx context.Context, pkg, section, sectionType string, values map[string]string) error {
	f.ops = append(f.ops, "setsection:"+pkg)
	if f.sections == nil {
		f.sections = map[string]map[string]string{}
	}
	f.sections[section] = values
	return nil
}

func (f *fakeUCI) Delete(ctx context.Context, pkg, section string) error {
	f.ops = append(f.ops, "delete:"+pkg)
	f.deleted = append(f.deleted, section)
	return nil
}

func (f *fakeUCI) AddListItem(ctx context.Context, pkg, section, option, value string) error {
	f.ops = append(f.ops, "addlist:"+pkg)
	return nil
}

func (f *fakeUCI) DelListItem(ctx context.Context, pkg, section, option, value string) error {
	f.ops = append(f.ops, "dellist:"+pkg)
	return nil
}

// fakeMesh is an injectable MeshInspector: it reports a fixed up/down result
// (or an error) for the mesh verification step.
type fakeMesh struct {
	up  bool
	err error
}

func (f fakeMesh) MeshUp(ctx context.Context, section string) (bool, error) {
	return f.up, f.err
}

func (f *fakeUCI) Commit(ctx context.Context, pkg string) error {
	f.ops = append(f.ops, "commit:"+pkg)
	return nil
}

func (f *fakeUCI) Reload(ctx context.Context) error {
	f.ops = append(f.ops, "reload")
	return f.reloadErr
}

func (f *fakeUCI) Close() error { return nil }

func TestApplyProfileReloadsAfterCommit(t *testing.T) {
	fake := &fakeUCI{}
	m := NewManager(nil, fake, Config{})

	profile := models.Profile{HomeID: "h1", NodeName: "garage", MeshSSID: "omm", MeshKey: "secret123"}
	if err := m.ApplyProfile(context.Background(), profile); err != nil {
		t.Fatalf("apply profile: %v", err)
	}

	// Committing UCI only writes the staged config; the change is not live
	// until netifd is reloaded. The reload must therefore run, and run after
	// every commit.
	last := fake.ops[len(fake.ops)-1]
	if last != "reload" {
		t.Fatalf("expected reload to run last, got ops %v", fake.ops)
	}
	for i, op := range fake.ops {
		if op == "reload" {
			for _, later := range fake.ops[i+1:] {
				if later == "commit:wireless" || later == "commit:system" {
					t.Fatalf("commit ran after reload, ops %v", fake.ops)
				}
			}
		}
	}
}

func TestApplyProfileReloadFailurePropagates(t *testing.T) {
	fake := &fakeUCI{reloadErr: errors.New("netifd down")}
	m := NewManager(nil, fake, Config{})

	err := m.ApplyProfile(context.Background(), models.Profile{HomeID: "h1", MeshSSID: "omm"})
	if err == nil {
		t.Fatal("expected reload failure to propagate, got nil")
	}
}

// A mesh-only profile must still bring up wireless: the 802.11s backhaul and a
// client AP that reuses the mesh SSID/key, with the radio enabled. Without this
// a claimed home has no active wifi at all (the original bug: ApplyProfile only
// `uci set` an absent `mesh` section, so nothing was created).
func TestApplyProfileAuthorsMeshAndAPAndEnablesRadio(t *testing.T) {
	fake := &fakeUCI{}
	m := NewManager(nil, fake, Config{Radio: "radio1"})

	profile := models.Profile{HomeID: "h1", MeshSSID: "omm-mesh", MeshKey: "secret123"}
	if err := m.ApplyProfile(context.Background(), profile); err != nil {
		t.Fatalf("apply profile: %v", err)
	}

	mesh := fake.sections[meshSection]
	if mesh == nil {
		t.Fatalf("mesh section %q not authored; sections=%v", meshSection, fake.sections)
	}
	if mesh["mode"] != "mesh" || mesh["mesh_id"] != "omm-mesh" || mesh["encryption"] != "sae" ||
		mesh["key"] != "secret123" || mesh["device"] != "radio1" || mesh["network"] != "lan" {
		t.Fatalf("mesh section wrong: %v", mesh)
	}

	// AP falls back to the mesh SSID/key (psk2) when no explicit AP is given.
	ap := fake.sections[apSection]
	if ap == nil {
		t.Fatalf("ap section %q not authored; sections=%v", apSection, fake.sections)
	}
	if ap["mode"] != "ap" || ap["ssid"] != "omm-mesh" || ap["encryption"] != "psk2" ||
		ap["key"] != "secret123" || ap["device"] != "radio1" || ap["network"] != "lan" {
		t.Fatalf("ap section wrong: %v", ap)
	}

	if !contains(fake.sets, "wireless.radio1.disabled=0") {
		t.Fatalf("radio not enabled; sets=%v", fake.sets)
	}
}

// A radio pinned on the profile overrides the daemon default for both ifaces
// and the enable, so an operator can put a home on the 2.4 GHz device for range.
func TestApplyProfilePinsRadioFromProfile(t *testing.T) {
	fake := &fakeUCI{}
	m := NewManager(nil, fake, Config{Radio: "radio0"})

	profile := models.Profile{HomeID: "h1", MeshSSID: "omm", MeshKey: "secret123", Radio: "radio1"}
	if err := m.ApplyProfile(context.Background(), profile); err != nil {
		t.Fatalf("apply profile: %v", err)
	}

	if fake.sections[meshSection]["device"] != "radio1" || fake.sections[apSection]["device"] != "radio1" {
		t.Fatalf("expected both ifaces on radio1; got mesh=%v ap=%v",
			fake.sections[meshSection], fake.sections[apSection])
	}
	if !contains(fake.sets, "wireless.radio1.disabled=0") || contains(fake.sets, "wireless.radio0.disabled=0") {
		t.Fatalf("expected radio1 enabled (not radio0); sets=%v", fake.sets)
	}
}

// On a board whose client-AP radio can't run 802.11s (e.g. the Lyra's radio0),
// the mesh must go on a dedicated backhaul radio (radio2) while the client AP
// stays on the AP radio, and both nodes share one mesh channel/width so they
// peer. cfg.MeshRadio selects the mesh radio; profile.MeshChannel/MeshHTMode
// pin the shared channel and width on it.
func TestApplyProfileMeshOnDedicatedBackhaulRadio(t *testing.T) {
	fake := &fakeUCI{wireless: lyraRadios()}
	m := NewManager(nil, fake, Config{Radio: "radio0", MeshRadio: "radio2"})

	profile := models.Profile{
		HomeID: "h1", MeshSSID: "omm", MeshKey: "secret123",
		MeshChannel: "149", MeshHTMode: "HT20",
	}
	if err := m.ApplyProfile(context.Background(), profile); err != nil {
		t.Fatalf("apply profile: %v", err)
	}

	// Mesh on the dedicated backhaul radio; client AP on the AP radio.
	if got := fake.sections[meshSection]["device"]; got != "radio2" {
		t.Fatalf("mesh device = %q, want radio2", got)
	}
	if got := fake.sections[apSection]["device"]; got != "radio0" {
		t.Fatalf("ap device = %q, want radio0", got)
	}
	// Channel + width pinned on the mesh radio so peers line up.
	if !contains(fake.sets, "wireless.radio2.channel=149") {
		t.Fatalf("mesh channel not set on radio2; sets=%v", fake.sets)
	}
	if !contains(fake.sets, "wireless.radio2.htmode=HT20") {
		t.Fatalf("mesh htmode not set on radio2; sets=%v", fake.sets)
	}
	// Both the mesh radio and the AP radio must be enabled.
	if !contains(fake.sets, "wireless.radio2.disabled=0") || !contains(fake.sets, "wireless.radio0.disabled=0") {
		t.Fatalf("both radios should be enabled; sets=%v", fake.sets)
	}
}

// lyraRadios mirrors an Asus Lyra's layout: radio0/radio2 are 5 GHz, radio1 is
// 2.4 GHz — i.e. radio0 is NOT 2.4 GHz, so band resolution can't assume names.
func lyraRadios() map[string]map[string]string {
	return map[string]map[string]string{
		"radio0":         {".type": "wifi-device", ".name": "radio0", "band": "5g"},
		"radio1":         {".type": "wifi-device", ".name": "radio1", "band": "2g"},
		"radio2":         {".type": "wifi-device", ".name": "radio2", "band": "5g"},
		"default_radio0": {".type": "wifi-iface", "device": "radio0"},
	}
}

// With no explicit MeshRadio, the mesh must auto-select the 2.4 GHz radio (the
// only band 802.11s can legally/interoperably use on this gear), NOT the AP
// radio — even when the AP radio (radio0) is 5 GHz. This is what lets a node
// "just peer" without per-board mesh_radio config.
func TestApplyProfileAutoSelectsMeshRadioByBand(t *testing.T) {
	fake := &fakeUCI{wireless: lyraRadios()}
	m := NewManager(nil, fake, Config{Radio: "radio0"}) // AP on 5 GHz radio0, MeshRadio unset

	if err := m.ApplyProfile(context.Background(), models.Profile{HomeID: "h1", MeshSSID: "omm"}); err != nil {
		t.Fatalf("apply profile: %v", err)
	}
	if got := fake.sections[meshSection]["device"]; got != "radio1" {
		t.Errorf("mesh auto-selected radio %q, want radio1 (the 2.4 GHz radio)", got)
	}
	// The client AP still lands on the configured AP radio.
	if got := fake.sections[apSection]["device"]; got != "radio0" {
		t.Errorf("AP radio = %q, want radio0", got)
	}
}

// An explicit MeshRadio always wins over auto-selection.
func TestApplyProfileExplicitMeshRadioWins(t *testing.T) {
	fake := &fakeUCI{wireless: lyraRadios()}
	m := NewManager(nil, fake, Config{Radio: "radio0", MeshRadio: "radio2"})

	if err := m.ApplyProfile(context.Background(), models.Profile{HomeID: "h1", MeshSSID: "omm"}); err != nil {
		t.Fatalf("apply profile: %v", err)
	}
	if got := fake.sections[meshSection]["device"]; got != "radio2" {
		t.Errorf("mesh radio = %q, want radio2 (explicit override)", got)
	}
}

// With no 2.4 GHz radio present, mesh auto-selection falls back to the AP radio
// rather than failing.
func TestApplyProfileMeshRadioFallsBackWhenNo2g(t *testing.T) {
	only5g := map[string]map[string]string{
		"radio0": {".type": "wifi-device", ".name": "radio0", "band": "5g"},
	}
	fake := &fakeUCI{wireless: only5g}
	m := NewManager(nil, fake, Config{Radio: "radio0"})

	if err := m.ApplyProfile(context.Background(), models.Profile{HomeID: "h1", MeshSSID: "omm"}); err != nil {
		t.Fatalf("apply profile: %v", err)
	}
	if got := fake.sections[meshSection]["device"]; got != "radio0" {
		t.Errorf("mesh radio = %q, want radio0 (fallback to AP radio)", got)
	}
}

func TestApplyProfileResolvesBandToRadio(t *testing.T) {
	fake := &fakeUCI{wireless: lyraRadios()}
	m := NewManager(nil, fake, Config{Radio: "radio0"})

	if err := m.ApplyProfile(context.Background(), models.Profile{HomeID: "h1", MeshSSID: "omm", Band: "2g"}); err != nil {
		t.Fatalf("apply profile: %v", err)
	}
	if got := fake.sections[apSection]["device"]; got != "radio1" {
		t.Fatalf("band 2g resolved to %q, want radio1", got)
	}
}

func TestApplyProfileBandPicksLowestNumberedMatch(t *testing.T) {
	fake := &fakeUCI{wireless: lyraRadios()}
	m := NewManager(nil, fake, Config{})

	if err := m.ApplyProfile(context.Background(), models.Profile{HomeID: "h1", MeshSSID: "omm", Band: "5g"}); err != nil {
		t.Fatalf("apply profile: %v", err)
	}
	// Band resolves the AP radio (the mesh auto-selects its own 2.4 GHz radio).
	if got := fake.sections[apSection]["device"]; got != "radio0" {
		t.Fatalf("band 5g resolved to %q, want radio0 (lowest of radio0/radio2)", got)
	}
}

func TestApplyProfileUnavailableBandErrors(t *testing.T) {
	fake := &fakeUCI{wireless: lyraRadios()}
	m := NewManager(nil, fake, Config{})

	err := m.ApplyProfile(context.Background(), models.Profile{HomeID: "h1", MeshSSID: "omm", Band: "6g"})
	if err == nil {
		t.Fatal("expected an error for a band with no matching radio")
	}
}

func TestApplyProfileRadioOverridesBand(t *testing.T) {
	fake := &fakeUCI{wireless: lyraRadios()}
	m := NewManager(nil, fake, Config{})

	// Band would resolve to radio1, but an explicit Radio wins.
	prof := models.Profile{HomeID: "h1", MeshSSID: "omm", Band: "2g", Radio: "radio2"}
	if err := m.ApplyProfile(context.Background(), prof); err != nil {
		t.Fatalf("apply profile: %v", err)
	}
	if got := fake.sections[apSection]["device"]; got != "radio2" {
		t.Fatalf("explicit radio override = %q, want radio2", got)
	}
}

// Explicit AP SSID/key override the mesh fallback.
func TestApplyProfileExplicitAPOverridesMeshFallback(t *testing.T) {
	fake := &fakeUCI{}
	m := NewManager(nil, fake, Config{})

	profile := models.Profile{
		HomeID: "h1", MeshSSID: "backhaul", MeshKey: "meshkey12",
		APSSID: "HomeWiFi", APKey: "guestpass",
	}
	if err := m.ApplyProfile(context.Background(), profile); err != nil {
		t.Fatalf("apply profile: %v", err)
	}

	ap := fake.sections[apSection]
	if ap["ssid"] != "HomeWiFi" || ap["key"] != "guestpass" {
		t.Fatalf("explicit AP not honored: %v", ap)
	}
	if fake.sections[meshSection]["mesh_id"] != "backhaul" {
		t.Fatalf("mesh id wrong: %v", fake.sections[meshSection])
	}
}

// newStore builds a real in-memory store so backhaul-state persistence is
// exercised end-to-end (there is no fake Store in the codebase).
func newStore(t *testing.T) storage.Store {
	t.Helper()
	db, err := storage.OpenDB(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return storage.NewStore(db)
}

// When the mesh comes up, the backhaul is recorded as 802.11s and the mesh
// section is left in place.
func TestApplyProfileMeshUpRecords80211s(t *testing.T) {
	fake := &fakeUCI{}
	store := newStore(t)
	m := NewManager(store, fake, Config{Mesh: fakeMesh{up: true}})

	prof := models.Profile{HomeID: "h1", MeshSSID: "omm", MeshKey: "secret123"}
	if err := m.ApplyProfile(context.Background(), prof); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if len(fake.deleted) != 0 {
		t.Fatalf("mesh section should not be deleted when mesh is up; deleted=%v", fake.deleted)
	}
	state, _ := store.GetBackhaulState(context.Background())
	if state.Mode != models.BackhaulMode80211s || state.Reason != "" {
		t.Fatalf("want 802.11s with no reason, got %+v", state)
	}
}

// When the mesh does not come up, meshd degrades to multi-AP: it deletes the
// mesh section, reloads again, and records the reason + remediation.
func TestApplyProfileMeshDownDegradesToMultiAP(t *testing.T) {
	fake := &fakeUCI{}
	store := newStore(t)
	m := NewManager(store, fake, Config{Mesh: fakeMesh{up: false}, MeshVerifyInterval: time.Nanosecond})

	prof := models.Profile{HomeID: "h1", MeshSSID: "omm", MeshKey: "secret123"}
	if err := m.ApplyProfile(context.Background(), prof); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if !contains(fake.deleted, meshSection) {
		t.Fatalf("expected mesh section %q to be deleted; deleted=%v", meshSection, fake.deleted)
	}
	// Two reloads: the initial apply, then the re-set after removing the mesh.
	reloads := 0
	for _, op := range fake.ops {
		if op == "reload" {
			reloads++
		}
	}
	if reloads != 2 {
		t.Fatalf("expected 2 reloads (apply + degrade), got %d in %v", reloads, fake.ops)
	}
	state, _ := store.GetBackhaulState(context.Background())
	if state.Mode != models.BackhaulModeMultiAP || state.Reason == "" || state.Remediation == "" {
		t.Fatalf("want degraded multi_ap with reason+remediation, got %+v", state)
	}
}

// With no inspector wired, verification is skipped and the configured mesh is
// assumed in effect (no spurious teardown).
func TestApplyProfileNoInspectorAssumes80211s(t *testing.T) {
	fake := &fakeUCI{}
	store := newStore(t)
	m := NewManager(store, fake, Config{})

	if err := m.ApplyProfile(context.Background(), models.Profile{HomeID: "h1", MeshSSID: "omm"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(fake.deleted) != 0 {
		t.Fatalf("no inspector should not delete mesh; deleted=%v", fake.deleted)
	}
	state, _ := store.GetBackhaulState(context.Background())
	if state.Mode != models.BackhaulMode80211s {
		t.Fatalf("want 802.11s, got %+v", state)
	}
}

// A profile with no mesh SSID is a wired multi-AP by choice — recorded as
// multi_ap with no degrade reason, and the mesh is never verified or deleted.
func TestApplyProfileNoMeshRecordsMultiAPWithoutReason(t *testing.T) {
	fake := &fakeUCI{}
	store := newStore(t)
	m := NewManager(store, fake, Config{Mesh: fakeMesh{up: false}, MeshVerifyInterval: time.Nanosecond})

	if err := m.ApplyProfile(context.Background(), models.Profile{HomeID: "h1", APSSID: "HomeWiFi", APKey: "guestpass"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(fake.deleted) != 0 {
		t.Fatalf("no mesh configured should not delete anything; deleted=%v", fake.deleted)
	}
	state, _ := store.GetBackhaulState(context.Background())
	if state.Mode != models.BackhaulModeMultiAP || state.Reason != "" {
		t.Fatalf("want multi_ap with no reason, got %+v", state)
	}
}

func TestDefaultProfile(t *testing.T) {
	// Derives a unique SSID from the home id and carries the supplied key; the
	// AP is left to ApplyProfile to derive from the mesh SSID/key.
	p := DefaultProfile("home-edb61002a448", "", "secretkey123")
	if p.HomeID != "home-edb61002a448" || p.MeshSSID != "OMM-edb610" || p.MeshKey != "secretkey123" {
		t.Fatalf("unexpected default profile: %+v", p)
	}
	if p.NodeName != "" {
		t.Fatalf("default profile must not set NodeName (would rename device): %q", p.NodeName)
	}
	// An explicit SSID wins.
	if got := DefaultProfile("home-x", "MyHome", "k").MeshSSID; got != "MyHome" {
		t.Fatalf("explicit SSID = %q, want MyHome", got)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// fakeBatman is an injectable BatmanInspector reporting a fixed bat0 up/down.
type fakeBatman struct {
	up  bool
	err error
}

func (f fakeBatman) BatmanUp(ctx context.Context, iface string) (bool, error) {
	return f.up, f.err
}

// With batman enabled, the 802.11s mesh must attach to a batadv hard interface
// (so batman-adv forwards multi-hop) rather than bridge straight onto lan, and
// the bat0 soft interface must be authored.
func TestApplyProfileBatmanWiresMeshToHardif(t *testing.T) {
	fake := &fakeUCI{}
	m := NewManager(nil, fake, Config{
		Radio:        "radio0",
		BatmanEnable: true,
		LanDevice:    "@device[0]",
		Mesh:         fakeMesh{up: true},
		Batman:       fakeBatman{up: true},
	})
	prof := models.Profile{HomeID: "h1", MeshSSID: "omm", MeshKey: "secret123"}
	if err := m.ApplyProfile(context.Background(), prof); err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}

	mesh := fake.sections["omm_mesh"]
	if mesh["network"] == "lan" || mesh["network"] == "" {
		t.Fatalf("omm_mesh.network = %q, want a batadv hardif", mesh["network"])
	}
	bat0 := fake.sections["bat0"]
	if bat0["proto"] != "batadv" {
		t.Errorf("bat0 proto = %q, want batadv", bat0["proto"])
	}
	hardif := fake.sections[mesh["network"]]
	if hardif["proto"] != "batadv_hardif" || hardif["master"] != "bat0" {
		t.Errorf("mesh hardif %q = %v, want batadv_hardif master bat0", mesh["network"], hardif)
	}
}

// In batman mode the 802.11s mesh is an always-enabled batman hardif (BLA owns
// loop avoidance, there is no carrier-toggle failover to re-enable it), so
// ApplyProfile must explicitly clear any stale disabled='1' left by a prior
// failover — otherwise the wireless backup never comes up under batman.
func TestApplyProfileBatmanEnablesMesh(t *testing.T) {
	fake := &fakeUCI{}
	m := NewManager(nil, fake, Config{
		Radio:        "radio0",
		BatmanEnable: true,
		LanDevice:    "@device[0]",
		Mesh:         fakeMesh{up: true},
		Batman:       fakeBatman{up: true},
	})
	prof := models.Profile{HomeID: "h1", MeshSSID: "omm", MeshKey: "secret123"}
	if err := m.ApplyProfile(context.Background(), prof); err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}
	if got := fake.sections["omm_mesh"]["disabled"]; got != "0" {
		t.Errorf("omm_mesh.disabled = %q, want 0 (batman mesh is an always-on hardif)", got)
	}
}

// With batman disabled, ApplyProfile must NOT force the mesh enabled — the
// carrier-toggle failover owns disabled there, and re-enabling the mesh on a
// wired node would recreate the ethernet+mesh bridge loop.
func TestApplyProfileNonBatmanLeavesMeshDisabledUnset(t *testing.T) {
	fake := &fakeUCI{}
	m := NewManager(nil, fake, Config{Radio: "radio0"})
	prof := models.Profile{HomeID: "h1", MeshSSID: "omm"}
	if err := m.ApplyProfile(context.Background(), prof); err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}
	if _, ok := fake.sections["omm_mesh"]["disabled"]; ok {
		t.Errorf("omm_mesh.disabled was set without batman; failover must own it")
	}
}

// ApplyProfile enslaves the configured batman ports (resolved by the daemon's
// startup backhaul resolution) to bat0 as batadv hardifs.
func TestApplyProfileBatmanEnslavesConfiguredPorts(t *testing.T) {
	fake := &fakeUCI{}
	m := NewManager(nil, fake, Config{
		Radio:        "radio0",
		BatmanEnable: true,
		LanDevice:    "@device[0]",
		Mesh:         fakeMesh{up: true},
		Batman:       fakeBatman{up: true},
		BatmanPorts:  []string{"wan"},
	})
	prof := models.Profile{HomeID: "h1", MeshSSID: "omm"}
	if err := m.ApplyProfile(context.Background(), prof); err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}
	wired := fake.sections["bat0_wan"]
	if wired["proto"] != "batadv_hardif" || wired["master"] != "bat0" || wired["device"] != "wan" {
		t.Errorf("uplink hardif = %v, want batadv_hardif master bat0 device wan", wired)
	}
}

// BatmanPortsFn supplies the enslaved ports dynamically (the reconcile loop's
// live set), overriding the static BatmanPorts.
func TestApplyProfileBatmanPortsFnOverrides(t *testing.T) {
	fake := &fakeUCI{}
	m := NewManager(nil, fake, Config{
		Radio:         "radio0",
		BatmanEnable:  true,
		LanDevice:     "@device[0]",
		Mesh:          fakeMesh{up: true},
		Batman:        fakeBatman{up: true},
		BatmanPorts:   []string{"stale"},
		BatmanPortsFn: func() []string { return []string{"wan"} },
	})
	if err := m.ApplyProfile(context.Background(), models.Profile{HomeID: "h1", MeshSSID: "omm"}); err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}
	if wired := fake.sections["bat0_wan"]; wired["device"] != "wan" {
		t.Errorf("dynamic port wan not enslaved; bat0_wan = %v", wired)
	}
	if _, ok := fake.sections["bat0_stale"]; ok {
		t.Error("stale static BatmanPorts used despite BatmanPortsFn")
	}
}

// In MeshStandby (case 3: a node whose wired uplink stays plain-bridged), the
// mesh is authored DISABLED even under batman — the carrier-toggle failover
// enables it only on wire loss, so wired + mesh never bridge-loop.
func TestApplyProfileBatmanMeshStandbyDisablesMesh(t *testing.T) {
	fake := &fakeUCI{}
	m := NewManager(nil, fake, Config{
		Radio:        "radio0",
		BatmanEnable: true,
		LanDevice:    "@device[0]",
		Mesh:         fakeMesh{up: true},
		Batman:       fakeBatman{up: true},
		MeshStandby:  true,
	})
	prof := models.Profile{HomeID: "h1", MeshSSID: "omm"}
	if err := m.ApplyProfile(context.Background(), prof); err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}
	if got := fake.sections["omm_mesh"]["disabled"]; got != "1" {
		t.Errorf("omm_mesh.disabled = %q, want 1 (mesh is an admin standby in case 3)", got)
	}
}

// A standby mesh (case 3) is authored disabled on purpose, so verifyBackhaul
// must NOT treat "mesh down" as a failed start: it must leave the omm_mesh
// section intact (the failover enables it on wire loss) and report 802.11s, not
// degrade to multi-AP.
func TestApplyProfileMeshStandbyDoesNotDegrade(t *testing.T) {
	fake := &fakeUCI{}
	store := newStore(t)
	m := NewManager(store, fake, Config{
		Radio:              "radio0",
		BatmanEnable:       true,
		LanDevice:          "@device[0]",
		Mesh:               fakeMesh{up: false}, // standby mesh reads as "not up"
		Batman:             fakeBatman{up: true},
		MeshStandby:        true,
		MeshVerifyInterval: time.Nanosecond,
	})
	prof := models.Profile{HomeID: "h1", MeshSSID: "omm"}
	if err := m.ApplyProfile(context.Background(), prof); err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}
	for _, s := range fake.deleted {
		if s == meshSection {
			t.Fatalf("omm_mesh deleted on a standby mesh; failover would have no backup")
		}
	}
	state, err := store.GetBackhaulState(context.Background())
	if err != nil {
		t.Fatalf("GetBackhaulState: %v", err)
	}
	if state.Mode != models.BackhaulMode80211s {
		t.Errorf("backhaul mode = %q, want %q (standby mesh is configured, not degraded)", state.Mode, models.BackhaulMode80211s)
	}
}

// With batman disabled (the zero-value Config default), behaviour is unchanged:
// the mesh bridges directly onto lan and no bat0 is authored.
func TestApplyProfileBatmanDisabledKeepsLanBridge(t *testing.T) {
	fake := &fakeUCI{}
	m := NewManager(nil, fake, Config{Radio: "radio0"})
	prof := models.Profile{HomeID: "h1", MeshSSID: "omm"}
	if err := m.ApplyProfile(context.Background(), prof); err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}
	if fake.sections["omm_mesh"]["network"] != "lan" {
		t.Errorf("omm_mesh.network = %q, want lan", fake.sections["omm_mesh"]["network"])
	}
	if _, ok := fake.sections["bat0"]; ok {
		t.Error("bat0 authored while batman disabled")
	}
}

// When batman is enabled but bat0 does not come up (module/proto absent), meshd
// must fall back to bridging the mesh directly onto lan and tear bat0 down —
// rather than wrongly degrading the (working) mesh.
func TestApplyProfileBatmanDownFallsBackToLanBridge(t *testing.T) {
	fake := &fakeUCI{}
	store := newStore(t)
	m := NewManager(store, fake, Config{
		Radio:              "radio0",
		BatmanEnable:       true,
		LanDevice:          "@device[0]",
		Mesh:               fakeMesh{up: true},
		Batman:             fakeBatman{up: false},
		MeshVerifyInterval: time.Nanosecond,
	})
	prof := models.Profile{HomeID: "h1", MeshSSID: "omm"}
	if err := m.ApplyProfile(context.Background(), prof); err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}

	if fake.sections["omm_mesh"]["network"] != "lan" {
		t.Errorf("after batman-down fallback, omm_mesh.network = %q, want lan", fake.sections["omm_mesh"]["network"])
	}
	deleted := false
	for _, s := range fake.deleted {
		if s == "bat0" {
			deleted = true
		}
	}
	if !deleted {
		t.Errorf("bat0 not torn down on batman-down fallback; deleted = %v", fake.deleted)
	}
	// The mesh itself is up, so the backhaul mode stays 802.11s (not degraded).
	state, err := store.GetBackhaulState(context.Background())
	if err != nil {
		t.Fatalf("GetBackhaulState: %v", err)
	}
	if state.Mode != models.BackhaulMode80211s {
		t.Errorf("backhaul mode = %q, want %q", state.Mode, models.BackhaulMode80211s)
	}
}
