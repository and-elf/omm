package profiles

import (
	"context"
	"errors"
	"testing"

	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/uci"
)

// fakeUCI records the ordered sequence of UCI operations so tests can assert
// that a profile is committed and then applied (reloaded), plus the sections it
// authored (keyed by section name) and plain Set calls so tests can check the
// wireless layout.
type fakeUCI struct {
	ops       []string
	sections  map[string]map[string]string
	sets      []string // "pkg.section.option=value" for plain Set calls
	reloadErr error
}

var _ uci.Client = (*fakeUCI)(nil)

func (f *fakeUCI) Get(ctx context.Context, pkg, section, option string) (string, error) {
	return "", nil
}

func (f *fakeUCI) Set(ctx context.Context, pkg, section, option, value string) error {
	f.ops = append(f.ops, "set:"+pkg)
	f.sets = append(f.sets, pkg+"."+section+"."+option+"="+value)
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
	return nil
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

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
