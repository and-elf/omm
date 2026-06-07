package profiles

import (
	"context"
	"fmt"

	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
	"github.com/and-elf/omm/internal/uci"
)

type ProfileManager interface {
	ApplyProfile(ctx context.Context, profile models.Profile) error
	ApplyProfileForHome(ctx context.Context, homeID string) error
}

const (
	// Dedicated section names so authoring a home's wireless never disturbs an
	// operator's own wifi-iface sections (matching the setupap convention). UCI
	// section names allow [A-Za-z0-9_] only, hence no hyphen.
	meshSection = "omm_mesh"
	apSection   = "omm_ap"
)

// Config tunes how a profile is applied to UCI.
type Config struct {
	// Radio is the wifi-device that hosts the home's mesh + client AP
	// (default "radio0").
	Radio string
}

func (c Config) withDefaults() Config {
	if c.Radio == "" {
		c.Radio = "radio0"
	}
	return c
}

type Manager struct {
	store     storage.Store
	uciClient uci.Client
	cfg       Config
}

func NewManager(store storage.Store, uciClient uci.Client, cfg Config) ProfileManager {
	return &Manager{store: store, uciClient: uciClient, cfg: cfg.withDefaults()}
}

// ApplyProfile authors the home's wireless from scratch and applies it. It
// creates (not just updates) the sections it owns, so it works on a stock
// device that has no meshd wireless yet — the earlier "apply profile: not
// found" was a plain `uci set` against a non-existent `mesh` section.
//
// Two interfaces are authored on the configured radio, both attached to `lan`
// so meshed nodes and AP clients share the controller's existing LAN and its
// DHCP:
//   - omm_mesh: the 802.11s backhaul (mesh_id = MeshSSID), so other nodes mesh in.
//   - omm_ap:   a client-facing AP (ssid = APSSID, defaulting to MeshSSID), so
//     phones/laptops can join and get an address.
//
// Each section is only authored when its SSID is set; absent both, the radio is
// left untouched.
func (m *Manager) ApplyProfile(ctx context.Context, profile models.Profile) error {
	// The client AP reuses the mesh SSID/key unless given explicit overrides,
	// so a mesh-only profile still yields a usable AP.
	apSSID, apKey := profile.APSSID, profile.APKey
	if apSSID == "" {
		apSSID, apKey = profile.MeshSSID, profile.MeshKey
	}

	radio, err := m.resolveRadio(ctx, profile)
	if err != nil {
		return err
	}

	wireless := false

	if profile.MeshSSID != "" {
		mesh := map[string]string{
			"device":  radio,
			"mode":    "mesh",
			"mesh_id": profile.MeshSSID,
			"network": "lan",
		}
		// 802.11s authenticates with SAE; an empty key leaves the mesh open.
		if profile.MeshKey != "" {
			mesh["encryption"] = "sae"
			mesh["key"] = profile.MeshKey
		} else {
			mesh["encryption"] = "none"
		}
		if err := m.uciClient.SetSection(ctx, "wireless", meshSection, "wifi-iface", mesh); err != nil {
			return fmt.Errorf("set mesh wifi-iface: %w", err)
		}
		wireless = true
	}

	if apSSID != "" {
		ap := map[string]string{
			"device":  radio,
			"mode":    "ap",
			"ssid":    apSSID,
			"network": "lan",
		}
		if apKey != "" {
			ap["encryption"] = "psk2"
			ap["key"] = apKey
		} else {
			ap["encryption"] = "none"
		}
		if err := m.uciClient.SetSection(ctx, "wireless", apSection, "wifi-iface", ap); err != nil {
			return fmt.Errorf("set ap wifi-iface: %w", err)
		}
		wireless = true
	}

	// A fresh OpenWrt radio ships disabled; enable it or neither interface
	// ever starts.
	if wireless {
		if err := m.uciClient.Set(ctx, "wireless", radio, "disabled", "0"); err != nil {
			return fmt.Errorf("enable radio: %w", err)
		}
	}

	if profile.NodeName != "" {
		if err := m.uciClient.Set(ctx, "system", "@system[0]", "hostname", profile.NodeName); err != nil {
			return fmt.Errorf("set hostname: %w", err)
		}
	}

	if err := m.uciClient.Commit(ctx, "wireless"); err != nil {
		return fmt.Errorf("commit wireless: %w", err)
	}

	if err := m.uciClient.Commit(ctx, "system"); err != nil {
		return fmt.Errorf("commit system: %w", err)
	}

	// Commits only stage the config files; reload so the new wireless actually
	// takes effect on the running system.
	if err := m.uciClient.Reload(ctx); err != nil {
		return fmt.Errorf("reload config: %w", err)
	}

	return nil
}

// resolveRadio picks the wifi-device for a profile. An explicit Radio wins;
// otherwise a Band ("2g"/"5g"/"6g") is resolved to the matching wifi-device by
// reading the live wireless config; otherwise the daemon default is used. A
// Band with no matching radio is an error rather than a silent wrong-band
// fallback, so the operator learns the device lacks that band.
func (m *Manager) resolveRadio(ctx context.Context, profile models.Profile) (string, error) {
	if profile.Radio != "" {
		return profile.Radio, nil
	}
	if profile.Band != "" {
		sections, err := m.uciClient.Sections(ctx, "wireless")
		if err != nil {
			return "", fmt.Errorf("list wireless devices: %w", err)
		}
		// Pick the lowest-numbered matching radio for determinism (radio0 <
		// radio1 sorts correctly as strings for these names).
		match := ""
		for name, opts := range sections {
			if opts[".type"] == "wifi-device" && opts["band"] == profile.Band {
				if match == "" || name < match {
					match = name
				}
			}
		}
		if match == "" {
			return "", fmt.Errorf("no radio for band %q on this device", profile.Band)
		}
		return match, nil
	}
	return m.cfg.Radio, nil
}

func (m *Manager) ApplyProfileForHome(ctx context.Context, homeID string) error {
	profile, err := m.store.GetProfile(ctx, homeID)
	if err != nil {
		return err
	}
	return m.ApplyProfile(ctx, profile)
}
