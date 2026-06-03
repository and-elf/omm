// Package setupap brings up and tears down the first-boot "setup" access point.
//
// An unclaimed OMM device has no mesh profile yet, so a companion app needs a
// way to reach its (open) management API before the device has joined any
// network. The setup AP is that bootstrap: a known, label-printable SSID on a
// small static network whose gateway serves meshd's management API. Once the
// device is claimed (setup completes) the AP is torn down.
//
// Consistent with OMM's charter, this orchestrates OpenWrt's own wireless,
// network and dhcp config via UCI rather than reimplementing any of it. All
// sections it authors use dedicated names (omm_setup / ommsetup) so an
// operator's existing wireless is never touched, and Disable removes exactly
// those sections.
package setupap

import (
	"context"
	"fmt"

	"github.com/and-elf/omm/internal/uci"
)

const (
	// wifiSection is the wireless wifi-iface section name; networkSection and
	// dhcpSection name the matching network/dhcp sections. UCI section names
	// allow [A-Za-z0-9_] only, hence no hyphen.
	wifiSection    = "omm_setup"
	networkSection = "ommsetup"
	dhcpSection    = "ommsetup"

	// uplinkWifiSection / uplinkNetSection are the optional station (client) WiFi
	// and its DHCP-client network, authored by EnableUplink so a wireless-only
	// node can reach its controller while it enrolls.
	uplinkWifiSection = "omm_uplink"
	uplinkNetSection  = "ommuplink"
)

// Config tunes the setup AP. Zero values fall back to sensible defaults.
type Config struct {
	Radio      string // wifi-device to host the AP (default "radio0")
	IP         string // gateway IP for the setup network (default 192.168.254.1)
	Netmask    string // default 255.255.255.0
	Key        string // WPA2 passphrase; empty => open network (discouraged)
	SSIDPrefix string // default "OMM-Setup-"
}

func (c Config) withDefaults() Config {
	if c.Radio == "" {
		c.Radio = "radio0"
	}
	if c.IP == "" {
		c.IP = "192.168.254.1"
	}
	if c.Netmask == "" {
		c.Netmask = "255.255.255.0"
	}
	if c.SSIDPrefix == "" {
		c.SSIDPrefix = "OMM-Setup-"
	}
	return c
}

// Manager authors the setup AP via UCI.
type Manager struct {
	uci uci.Client
	cfg Config
	// uplinkActive records whether EnableUplink authored a station interface, so
	// Disable only tears down uplink sections that actually exist (deleting an
	// absent section errors on a real device).
	uplinkActive bool
}

// New returns a setup-AP manager bound to a UCI client.
func New(uciClient uci.Client, cfg Config) *Manager {
	return &Manager{uci: uciClient, cfg: cfg.withDefaults()}
}

// SSID returns the setup SSID for a node: the prefix plus the last 4 characters
// of the node ID. It is deterministic so the value can be printed on a device
// label / QR code for out-of-band join — iOS offers no WiFi-scan API, so the
// app must know the exact SSID to join it.
func (m *Manager) SSID(nodeID string) string {
	suffix := nodeID
	if len(suffix) > 4 {
		suffix = suffix[len(suffix)-4:]
	}
	return m.cfg.SSIDPrefix + suffix
}

// Enable authors and applies the setup AP for the given node. It is idempotent:
// re-running it overwrites the same sections.
func (m *Manager) Enable(ctx context.Context, nodeID string) error {
	// Static setup network with the management gateway.
	if err := m.uci.SetSection(ctx, "network", networkSection, "interface", map[string]string{
		"proto":   "static",
		"ipaddr":  m.cfg.IP,
		"netmask": m.cfg.Netmask,
	}); err != nil {
		return fmt.Errorf("set setup network: %w", err)
	}

	// DHCP pool so a phone gets an address on the setup network.
	if err := m.uci.SetSection(ctx, "dhcp", dhcpSection, "dhcp", map[string]string{
		"interface": networkSection,
		"start":     "100",
		"limit":     "150",
		"leasetime": "12h",
	}); err != nil {
		return fmt.Errorf("set setup dhcp: %w", err)
	}

	// The AP itself. Open by default; WPA2 when a key is configured.
	wifi := map[string]string{
		"device":  m.cfg.Radio,
		"mode":    "ap",
		"ssid":    m.SSID(nodeID),
		"network": networkSection,
	}
	if m.cfg.Key != "" {
		wifi["encryption"] = "psk2"
		wifi["key"] = m.cfg.Key
	} else {
		wifi["encryption"] = "none"
	}
	if err := m.uci.SetSection(ctx, "wireless", wifiSection, "wifi-iface", wifi); err != nil {
		return fmt.Errorf("set setup wifi-iface: %w", err)
	}

	// A fresh OpenWrt radio ships disabled; enable it or the AP never starts.
	if err := m.uci.Set(ctx, "wireless", m.cfg.Radio, "disabled", "0"); err != nil {
		return fmt.Errorf("enable radio: %w", err)
	}

	return m.commitAndReload(ctx)
}

// EnableUplink joins the node to a home WiFi network as a station, so a
// wireless-only (un-wired) node gains a route to its controller and can enroll.
// It authors a station wifi-iface and a DHCP-client network alongside the setup
// AP (concurrent AP+STA on the same radio), using dedicated section names so an
// operator's wireless is untouched. Empty key => open network. Idempotent.
func (m *Manager) EnableUplink(ctx context.Context, ssid, key string) error {
	// DHCP-client network: the uplink takes its address from the home network.
	if err := m.uci.SetSection(ctx, "network", uplinkNetSection, "interface", map[string]string{
		"proto": "dhcp",
	}); err != nil {
		return fmt.Errorf("set uplink network: %w", err)
	}

	wifi := map[string]string{
		"device":  m.cfg.Radio,
		"mode":    "sta",
		"ssid":    ssid,
		"network": uplinkNetSection,
	}
	if key != "" {
		wifi["encryption"] = "psk2"
		wifi["key"] = key
	} else {
		wifi["encryption"] = "none"
	}
	if err := m.uci.SetSection(ctx, "wireless", uplinkWifiSection, "wifi-iface", wifi); err != nil {
		return fmt.Errorf("set uplink wifi-iface: %w", err)
	}

	// A fresh OpenWrt radio ships disabled; enable it or the station never joins.
	if err := m.uci.Set(ctx, "wireless", m.cfg.Radio, "disabled", "0"); err != nil {
		return fmt.Errorf("enable radio: %w", err)
	}

	if err := m.commitAndReload(ctx); err != nil {
		return err
	}
	m.uplinkActive = true
	return nil
}

// Disable removes the setup AP and its network/dhcp sections (and the uplink
// station, if one was provisioned), then reapplies.
func (m *Manager) Disable(ctx context.Context) error {
	if err := m.uci.Delete(ctx, "wireless", wifiSection); err != nil {
		return fmt.Errorf("delete setup wifi-iface: %w", err)
	}
	if err := m.uci.Delete(ctx, "network", networkSection); err != nil {
		return fmt.Errorf("delete setup network: %w", err)
	}
	if err := m.uci.Delete(ctx, "dhcp", dhcpSection); err != nil {
		return fmt.Errorf("delete setup dhcp: %w", err)
	}
	if m.uplinkActive {
		if err := m.uci.Delete(ctx, "wireless", uplinkWifiSection); err != nil {
			return fmt.Errorf("delete uplink wifi-iface: %w", err)
		}
		if err := m.uci.Delete(ctx, "network", uplinkNetSection); err != nil {
			return fmt.Errorf("delete uplink network: %w", err)
		}
		m.uplinkActive = false
	}
	return m.commitAndReload(ctx)
}

// commitAndReload stages every touched config then reloads so the changes take
// effect. Reload must run after all commits (a commit only rewrites files).
func (m *Manager) commitAndReload(ctx context.Context) error {
	for _, pkg := range []string{"network", "dhcp", "wireless"} {
		if err := m.uci.Commit(ctx, pkg); err != nil {
			return fmt.Errorf("commit %s: %w", pkg, err)
		}
	}
	if err := m.uci.Reload(ctx); err != nil {
		return fmt.Errorf("reload config: %w", err)
	}
	return nil
}
