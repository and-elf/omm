package config

import (
	"os"
	"strings"
)

// Config holds the meshd daemon settings. A daemon always runs as a controller
// for its own Home and can additionally enroll into other controllers (as a
// node) at runtime or via Join.
type Config struct {
	// HTTPAddr, when set, runs a single "combined" server serving both the
	// management and mesh planes on one address (backward-compatible). When
	// empty, the daemon runs split listeners: MgmtAddr (admin/UI, localhost by
	// default) and MeshAddr (node-to-node control plane, network by default).
	HTTPAddr     string
	MgmtAddr     string
	MeshAddr     string
	DatabasePath string
	UDPListen    string
	UbusSocket   string
	UbusBinary   string

	// This daemon's own Home (controller capability).
	HomeID       string
	HomeName     string
	ControllerID string
	AutoAdopt    bool
	APIAdvertise string // API URL announced to clients (defaults to HTTPAddr)
	UDPBroadcast string // broadcast endpoint for announcements
	BSSID        string // controller mesh BSSID/MAC (explicit)
	MeshIface    string // interface to read the BSSID from when BSSID is empty

	// Device identity and homes to join at startup.
	IdentityDir string
	Serial      string
	Join        []string // controller URLs to enroll into on boot

	// Topology collection.
	BatmanIface  string   // batman-adv interface (e.g. bat0)
	APInterfaces []string // hostapd interfaces to read clients from

	// First-boot setup AP (brought up while the device is unclaimed).
	SetupAPEnabled bool   // bring up the setup AP while unclaimed (default true)
	SetupAPRadio   string // wifi-device hosting the setup AP (default radio0)
	SetupAPKey     string // WPA2 passphrase for the setup AP; empty => open
}

// Combined reports whether the daemon should serve a single server (both planes
// on HTTPAddr) rather than split management/mesh listeners.
func (c Config) Combined() bool { return c.HTTPAddr != "" }

// AnnounceAddr is the address other nodes should reach this controller's mesh
// control plane on: the combined address when combined, otherwise the mesh
// listener. It is the address wrapped into the discovery announcement when
// APIAdvertise is not set explicitly.
func (c Config) AnnounceAddr() string {
	if c.Combined() {
		return c.HTTPAddr
	}
	return c.MeshAddr
}

func Load() Config {
	return Config{
		HTTPAddr:     os.Getenv("MESHD_HTTP_ADDR"),
		MgmtAddr:     envOr("MESHD_MGMT_ADDR", "127.0.0.1:8080"),
		MeshAddr:     envOr("MESHD_MESH_ADDR", "0.0.0.0:8081"),
		DatabasePath: envOr("MESHD_DATABASE_PATH", "./meshd.bolt"),
		UDPListen:    envOr("MESHD_UDP_LISTEN", ":45678"),
		UbusSocket:   envOr("MESHD_UBUS_SOCKET", "/var/run/ubus.sock"),
		UbusBinary:   envOr("MESHD_UBUS_BINARY", "ubus"),

		HomeID:       envOr("MESHD_HOME_ID", "default-home"),
		HomeName:     envOr("MESHD_HOME_NAME", "Home"),
		ControllerID: envOr("MESHD_CONTROLLER_ID", "gw01"),
		AutoAdopt:    envBool("MESHD_AUTO_ADOPT"),
		APIAdvertise: os.Getenv("MESHD_API_ADVERTISE"),
		UDPBroadcast: envOr("MESHD_UDP_BROADCAST", "255.255.255.255:45678"),
		BSSID:        os.Getenv("MESHD_BSSID"),
		MeshIface:    os.Getenv("MESHD_MESH_IFACE"),

		IdentityDir: envOr("MESHD_IDENTITY_DIR", "./meshd-identity"),
		Serial:      envOr("MESHD_SERIAL", hostnameOr("unknown")),
		Join:        splitList(os.Getenv("MESHD_JOIN")),

		BatmanIface:  envOr("MESHD_BATMAN_IFACE", "bat0"),
		APInterfaces: splitList(os.Getenv("MESHD_AP_IFACES")),

		SetupAPEnabled: envBoolOr("MESHD_SETUP_AP", true),
		SetupAPRadio:   envOr("MESHD_SETUP_AP_RADIO", "radio0"),
		SetupAPKey:     os.Getenv("MESHD_SETUP_AP_KEY"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string) bool {
	switch os.Getenv(key) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// envBoolOr parses a boolean env var, returning fallback when the var is unset.
// An explicit falsey value (0/false/no/off) overrides a true fallback.
func envBoolOr(key string, fallback bool) bool {
	switch os.Getenv(key) {
	case "":
		return fallback
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func splitList(value string) []string {
	if value == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(value, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func hostnameOr(fallback string) string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return fallback
}
