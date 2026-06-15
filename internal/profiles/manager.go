package profiles

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/and-elf/omm/internal/batman"
	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
	"github.com/and-elf/omm/internal/uci"
)

// DefaultProfile is the zero-config wireless profile for a freshly-created Home:
// an 802.11s mesh (and a client AP, which ApplyProfile derives from the same
// SSID/key) so the Home has working wireless — and pushes it to nodes that join
// — without the setup wizard. ssid falls back to a unique name derived from the
// Home id; an empty key leaves the mesh open (callers should supply a generated
// one). NodeName is left empty so applying it never renames the device.
func DefaultProfile(homeID, ssid, key string) models.Profile {
	if ssid == "" {
		ssid = "OMM-" + homeSuffix(homeID)
	}
	return models.Profile{HomeID: homeID, MeshSSID: ssid, MeshKey: key}
}

// homeSuffix is a short, stable tag from a Home id for a default SSID, e.g.
// "home-edb61002a448" -> "edb610".
func homeSuffix(homeID string) string {
	s := strings.TrimPrefix(homeID, "home-")
	if len(s) > 6 {
		s = s[:6]
	}
	if s == "" {
		return "mesh"
	}
	return s
}

// degradeReason / degradeRemediation explain a 802.11s -> multi-AP fallback to
// the operator (surfaced via /status and the LuCI app).
const (
	degradeReason      = "802.11s mesh did not start — no mesh-capable wpad on this node"
	degradeRemediation = "install wpad-mesh-wolfssl (or -mbedtls to match the image) and re-apply the profile"
)

// MeshInspector reports whether the mesh wifi-iface in the named UCI section is
// actually running. ApplyProfile uses it to verify the 802.11s backhaul came up
// and, if it did not, degrade to a wired multi-AP. Injected so the manager is
// testable off-OpenWrt; a nil inspector means "cannot verify, assume mesh".
type MeshInspector interface {
	MeshUp(ctx context.Context, section string) (bool, error)
}

// BatmanInspector reports whether the batman-adv soft interface (bat0) actually
// came up after the network was reloaded. ApplyProfile uses it to decide whether
// to wire the 802.11s mesh onto a batadv hard interface (multi-hop routing) or
// fall back to bridging it directly onto lan when the batman-adv module/proto is
// absent. Injected so the manager is testable off-OpenWrt; nil means "cannot
// verify, assume up".
type BatmanInspector interface {
	BatmanUp(ctx context.Context, iface string) (bool, error)
}

type ProfileManager interface {
	ApplyProfile(ctx context.Context, profile models.Profile) error
	ApplyProfileForHome(ctx context.Context, homeID string) error
}

const (
	// Dedicated section names so authoring a home's wireless never disturbs an
	// operator's own wifi-iface sections (matching the setupap convention). UCI
	// section names allow [A-Za-z0-9_] only, hence no hyphen. MeshSection is
	// exported so the daemon's backhaul failover can enable/disable the mesh.
	MeshSection = "omm_mesh"
	meshSection = MeshSection
	apSection   = "omm_ap"
)

// Config tunes how a profile is applied to UCI.
type Config struct {
	// Radio is the wifi-device that hosts the home's mesh + client AP
	// (default "radio0").
	Radio string
	// MeshRadio, when set, hosts the 802.11s mesh on a dedicated wifi-device
	// distinct from the client-AP radio. This is board-specific (radio names
	// differ per device) and so configured per node, not in the home profile:
	// on a tri-band board whose AP radio can't run mesh (e.g. the Lyra's
	// IPQ40xx radio0), the mesh goes on the dedicated backhaul radio (radio2).
	// Empty (default) keeps the mesh on the AP radio.
	MeshRadio string
	// Mesh verifies the 802.11s backhaul came up after apply, enabling the
	// automatic degrade to multi-AP. nil disables verification (assume mesh).
	Mesh MeshInspector
	// MeshVerifyAttempts/Interval bound how long to wait for the mesh vif to
	// instantiate before concluding it failed — a mesh point takes a few seconds
	// to come up after a wireless reload, so a single immediate check would
	// wrongly degrade. Defaults: 8 attempts, 1s apart (~8s). The same bound is
	// reused to wait for the batman soft interface.
	MeshVerifyAttempts int
	MeshVerifyInterval time.Duration

	// BatmanEnable authors a batman-adv routing layer (bat0 + a hard interface per
	// backhaul link) and wires the 802.11s mesh onto it, instead of bridging the
	// mesh straight onto lan — giving loop-free multi-hop forwarding across wired
	// and wireless links. It auto-degrades to the direct lan bridge when bat0 does
	// not come up (module/proto absent). Off by the zero value, so unit tests and
	// callers that don't opt in keep the prior single-hop behaviour.
	BatmanEnable bool
	// BatmanIface is the batman soft interface/netdev name (default "bat0").
	BatmanIface string
	// BatmanRoutingAlgo selects batman-adv's metric (default "BATMAN_IV").
	BatmanRoutingAlgo string
	// BatmanPorts are wired backhaul ethernet devices to enslave to bat0 as hard
	// interfaces (board-specific), so a wired hop is routed by batman-adv too.
	BatmanPorts []string
	// LanDevice is the UCI section of the LAN bridge device bat0 is bridged into
	// (e.g. "@device[0]"). Empty skips bridging bat0 into the LAN.
	LanDevice string
	// Batman verifies the bat0 soft interface came up after apply, gating the
	// mesh-on-batman vs mesh-on-lan decision. nil disables verification (assume up).
	Batman BatmanInspector
}

func (c Config) withDefaults() Config {
	if c.Radio == "" {
		c.Radio = "radio0"
	}
	if c.MeshVerifyAttempts == 0 {
		c.MeshVerifyAttempts = 8
	}
	if c.MeshVerifyInterval == 0 {
		c.MeshVerifyInterval = time.Second
	}
	if c.BatmanIface == "" {
		c.BatmanIface = "bat0"
	}
	if c.BatmanRoutingAlgo == "" {
		c.BatmanRoutingAlgo = "BATMAN_IV"
	}
	return c
}

type Manager struct {
	store     storage.Store
	uciClient uci.Client
	mesh      MeshInspector
	cfg       Config
}

func NewManager(store storage.Store, uciClient uci.Client, cfg Config) ProfileManager {
	return &Manager{store: store, uciClient: uciClient, mesh: cfg.Mesh, cfg: cfg.withDefaults()}
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

	apRadio, err := m.resolveRadio(ctx, profile)
	if err != nil {
		return err
	}
	// The mesh can live on a dedicated backhaul radio (board-specific) while the
	// client AP stays on the AP radio; absent that, both share the AP radio.
	meshRadio := apRadio
	if m.cfg.MeshRadio != "" {
		meshRadio = m.cfg.MeshRadio
	}

	// batman-adv routing layer: when enabled, the 802.11s mesh attaches to a
	// batadv hard interface (so batman-adv forwards loop-free, multi-hop across
	// any mix of wired and wireless links) instead of bridging straight onto lan.
	// It is authored before the wireless so we can verify bat0 actually came up
	// and wire the mesh accordingly — degrading to the direct lan bridge when the
	// batman-adv module/proto is absent, rather than stranding the mesh on a dead
	// hard interface.
	meshNetwork := "lan"
	var bm *batman.Manager
	if m.cfg.BatmanEnable && profile.MeshSSID != "" {
		bm = batman.NewManager(m.uciClient, batman.Config{
			Iface:       m.cfg.BatmanIface,
			RoutingAlgo: m.cfg.BatmanRoutingAlgo,
			WiredPorts:  m.cfg.BatmanPorts,
			LanDevice:   m.cfg.LanDevice,
		})
		if err := bm.Apply(ctx); err != nil {
			return fmt.Errorf("author batman-adv: %w", err)
		}
		if err := m.uciClient.Commit(ctx, "network"); err != nil {
			return fmt.Errorf("commit network: %w", err)
		}
		if err := m.uciClient.Reload(ctx); err != nil {
			return fmt.Errorf("reload after batman: %w", err)
		}
		if m.batmanUp(ctx) {
			meshNetwork = bm.MeshHardif()
		} else {
			log.Printf("backhaul: batman-adv did not come up; bridging mesh onto lan directly")
			if err := bm.Teardown(ctx); err != nil {
				return fmt.Errorf("tear down batman-adv: %w", err)
			}
			if err := m.uciClient.Commit(ctx, "network"); err != nil {
				return fmt.Errorf("commit network after batman teardown: %w", err)
			}
			if err := m.uciClient.Reload(ctx); err != nil {
				return fmt.Errorf("reload after batman teardown: %w", err)
			}
		}
	}

	// Radios to enable at the end (a fresh OpenWrt radio ships disabled; enable
	// it or its interfaces never start). Deduped so a shared radio is set once.
	enable := map[string]bool{}

	if profile.MeshSSID != "" {
		mesh := map[string]string{
			"device":  meshRadio,
			"mode":    "mesh",
			"mesh_id": profile.MeshSSID,
			"network": meshNetwork,
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
		// Pin the mesh channel/width on its radio so every node's mesh lands on
		// the same channel and can peer (a dedicated backhaul radio in particular
		// must be driven to the home's common mesh channel).
		if profile.MeshChannel != "" {
			if err := m.uciClient.Set(ctx, "wireless", meshRadio, "channel", profile.MeshChannel); err != nil {
				return fmt.Errorf("set mesh channel: %w", err)
			}
		}
		if profile.MeshHTMode != "" {
			if err := m.uciClient.Set(ctx, "wireless", meshRadio, "htmode", profile.MeshHTMode); err != nil {
				return fmt.Errorf("set mesh htmode: %w", err)
			}
		}
		enable[meshRadio] = true
	}

	if apSSID != "" {
		ap := map[string]string{
			"device":  apRadio,
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
		enable[apRadio] = true
	}

	for radio := range enable {
		if err := m.uciClient.Set(ctx, "wireless", radio, "disabled", "0"); err != nil {
			return fmt.Errorf("enable radio %s: %w", radio, err)
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

	// Verify the 802.11s backhaul actually came up and degrade to a wired
	// multi-AP if it did not, recording the outcome for /status and LuCI.
	state, err := m.verifyBackhaul(ctx, profile)
	if err != nil {
		return err
	}
	if m.store != nil {
		if err := m.store.SetBackhaulState(ctx, state); err != nil {
			return fmt.Errorf("record backhaul state: %w", err)
		}
	}

	return nil
}

// batmanUp polls the batman inspector until the bat0 soft interface is up or the
// verify window elapses. A nil inspector means "cannot verify": assume up rather
// than tear down a possibly-working layer. A probe error is treated the same way
// — the mesh verification downstream still catches a genuinely broken backhaul.
func (m *Manager) batmanUp(ctx context.Context) bool {
	if m.cfg.Batman == nil {
		return true
	}
	for attempt := 0; attempt <= m.cfg.MeshVerifyAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(m.cfg.MeshVerifyInterval)
		}
		up, err := m.cfg.Batman.BatmanUp(ctx, m.cfg.BatmanIface)
		if err != nil {
			log.Printf("backhaul: batman verification failed, assuming up: %v", err)
			return true
		}
		if up {
			return true
		}
	}
	return false
}

// verifyBackhaul determines the wireless-backhaul outcome after a profile is
// applied. When 802.11s was configured but the mesh interface did not start
// (typically: no mesh-capable wpad), it removes the mesh section so the radio
// re-sets cleanly with the AP alone, and returns a degraded multi-AP state with
// an operator-facing reason and remediation.
func (m *Manager) verifyBackhaul(ctx context.Context, profile models.Profile) (models.BackhaulState, error) {
	// No mesh configured: the node is a wired multi-AP by choice, not a degrade.
	if profile.MeshSSID == "" {
		return models.BackhaulState{Mode: models.BackhaulModeMultiAP}, nil
	}
	// Cannot verify (no inspector, or the probe failed): assume the configured
	// mesh is in effect rather than tear down a possibly-working backhaul.
	if m.mesh == nil {
		return models.BackhaulState{Mode: models.BackhaulMode80211s}, nil
	}
	// A mesh vif takes a few seconds to instantiate after the reload, so poll
	// before concluding it failed — checking once immediately would wrongly
	// degrade a mesh that is still coming up.
	for attempt := 0; attempt <= m.cfg.MeshVerifyAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(m.cfg.MeshVerifyInterval)
		}
		up, err := m.mesh.MeshUp(ctx, meshSection)
		if err != nil {
			log.Printf("backhaul: mesh verification failed, assuming 802.11s: %v", err)
			return models.BackhaulState{Mode: models.BackhaulMode80211s}, nil
		}
		if up {
			return models.BackhaulState{Mode: models.BackhaulMode80211s}, nil
		}
	}

	log.Printf("backhaul: 802.11s mesh did not start; degrading to wired multi-AP")
	if err := m.uciClient.Delete(ctx, "wireless", meshSection); err != nil {
		return models.BackhaulState{}, fmt.Errorf("disable mesh iface: %w", err)
	}
	if err := m.uciClient.Commit(ctx, "wireless"); err != nil {
		return models.BackhaulState{}, fmt.Errorf("commit wireless after degrade: %w", err)
	}
	if err := m.uciClient.Reload(ctx); err != nil {
		return models.BackhaulState{}, fmt.Errorf("reload after degrade: %w", err)
	}
	return models.BackhaulState{
		Mode:        models.BackhaulModeMultiAP,
		Reason:      degradeReason,
		Remediation: degradeRemediation,
	}, nil
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
