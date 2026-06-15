package config

import (
	"os"
	"strings"
	"time"
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
	// Default wireless for this daemon's Home, used to seed a profile when the
	// Home has none yet (so onboarding pushes wifi without the wizard). Empty
	// MeshSSID derives a unique name from the Home id; empty MeshKey generates a
	// random one (persisted in the profile).
	MeshSSID string
	MeshKey  string
	// AdoptPolicy gates unattended adoption on the controller: "off" (operator
	// approves), "onlink" (auto-adopt only nodes enrolling from this controller's
	// LAN), or "always" (any node). Default "onlink" — safe (trust is the
	// verifiable on-link source) and enables zero-touch within a home.
	AdoptPolicy  string
	APIAdvertise string // API URL announced to clients (defaults to HTTPAddr)
	UDPBroadcast string // broadcast endpoint for announcements
	BSSID        string // controller mesh BSSID/MAC (explicit)
	MeshIface    string // interface to read the BSSID from when BSSID is empty
	MeshRadio    string // wifi-device hosting the 802.11s mesh (board-specific); empty = the AP radio

	// Device identity and homes to join at startup.
	IdentityDir string
	Serial      string
	Join        []string // controller URLs to enroll into on boot

	// AutoOnboardWired lets an unclaimed node that is on the wire (ethernet
	// backhaul) enroll into a discovered controller unattended, with no wizard.
	// Default true (zero-touch): a fresh wired node joins a home it finds, and
	// becomes its own controller if it finds none (grace fallback). Set false to
	// require the wizard. Adoption is still gated by the controller's AdoptPolicy.
	AutoOnboardWired bool

	// OnboardGrace is how long an unclaimed wired node lets discovery +
	// auto-onboarding try to claim it before falling back to selecting its own
	// (last-resort) Home and becoming its own controller. Prevents the node from
	// racing ahead and claiming itself before a controller is discovered.
	OnboardGrace time.Duration

	// Topology collection.
	BatmanIface   string   // batman-adv interface (e.g. bat0)
	APInterfaces  []string // hostapd interfaces to read clients from
	BackhaulIface string   // iface whose carrier classifies backhaul (default "br-lan"; e.g. eth0/wan); empty => unknown

	// batman-adv routing layer. When enabled, ApplyProfile authors a batman-adv
	// mesh (bat0 soft interface + a hard interface per backhaul link) and bridges
	// bat0 into the LAN, instead of bridging the 802.11s mesh straight onto lan —
	// giving loop-free multi-hop forwarding across any mix of wired and wireless
	// links. Default on; it auto-degrades to the direct mesh-on-lan bridge when
	// the batman-adv module/netifd proto is absent. BatmanPorts are wired backhaul
	// ethernet devices to enslave to bat0 (board-specific). See doc/network-model.md.
	BatmanEnable      bool
	BatmanPorts       []string
	BatmanRoutingAlgo string

	// Network posture: meshd manages network/dhcp/firewall by lifecycle state
	// (unclaimed -> Guest dumb-AP so discovery works; claimed controller ->
	// gateway; joined node -> mesh node). Opt-in (default false) because it
	// reconfigures the network and can strand a hand-wired device; enable only
	// after verifying the Guest transition on the target board. UplinkPort is the
	// wired uplink device bridged into br-lan in Guest posture; empty (default)
	// auto-detects it from network.wan.device, so any jack works without
	// per-board config (incl. a single-jack AP wired as `wan`). LanDevice is the
	// UCI section of the LAN bridge device whose `ports` it edits. See
	// doc/network-posture.md.
	ManageNetwork bool
	UplinkPort    string
	LanDevice     string

	// First-boot setup AP (brought up while the device is unclaimed).
	SetupAPEnabled bool   // bring up the setup AP while unclaimed (default true)
	SetupAPRadio   string // wifi-device hosting the setup AP (default radio0)
	SetupAPKey     string // WPA2 passphrase for the setup AP; empty => open

	// Status LED: blink while unclaimed, heartbeat while joining, solid once
	// active. The name is hardware-specific; an absent LED is a graceful no-op.
	LEDEnabled bool   // drive the status LED (default true)
	LEDName    string // /sys/class/leds/<name> to drive (default "green:status")

	// DevCORS adds permissive CORS headers to the management plane so a companion
	// app served from another origin (e.g. a Vite dev server) can call it
	// directly. Development only — the management API is unauthenticated, so this
	// must never be enabled on a network-reachable deployment.
	DevCORS bool
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
		// Empty by default so the ubus/uci CLI uses its own compiled-in
		// socket path, which is correct for whatever OpenWrt release is
		// running. Hardcoding /var/run/ubus.sock broke on modern OpenWrt,
		// where the socket moved to /var/run/ubus/ubus.sock. Override with
		// MESHD_UBUS_SOCKET only for a non-standard layout.
		UbusSocket: os.Getenv("MESHD_UBUS_SOCKET"),
		UbusBinary: envOr("MESHD_UBUS_BINARY", "ubus"),

		// Empty by default: main derives a unique id from the device identity
		// (DeriveHomeID) so fresh devices don't collide on a shared "default-home".
		HomeID:       os.Getenv("MESHD_HOME_ID"),
		HomeName:     envOr("MESHD_HOME_NAME", "Home"),
		ControllerID: envOr("MESHD_CONTROLLER_ID", "gw01"),
		MeshSSID:     os.Getenv("MESHD_MESH_SSID"),
		MeshKey:      os.Getenv("MESHD_MESH_KEY"),
		AdoptPolicy:  adoptPolicyEnv(),
		APIAdvertise: os.Getenv("MESHD_API_ADVERTISE"),
		UDPBroadcast: envOr("MESHD_UDP_BROADCAST", "255.255.255.255:45678"),
		BSSID:        os.Getenv("MESHD_BSSID"),
		MeshIface:    os.Getenv("MESHD_MESH_IFACE"),
		MeshRadio:    os.Getenv("MESHD_MESH_RADIO"),

		// Absolute default so an env-less hand-launch reuses the deployed
		// identity instead of creating a new keypair under the current working
		// directory (which silently changes the derived home id). The init
		// script sets MESHD_IDENTITY_DIR to this same path explicitly.
		IdentityDir:      envOr("MESHD_IDENTITY_DIR", "/etc/meshd/identity"),
		Serial:           envOr("MESHD_SERIAL", hostnameOr("unknown")),
		Join:             splitList(os.Getenv("MESHD_JOIN")),
		AutoOnboardWired: envBoolOr("MESHD_AUTO_ONBOARD_WIRED", true),
		OnboardGrace:     envDurationOr("MESHD_ONBOARD_GRACE", 20*time.Second),

		BatmanIface:   envOr("MESHD_BATMAN_IFACE", "bat0"),
		APInterfaces:  splitList(os.Getenv("MESHD_AP_IFACES")),
		BackhaulIface: envOr("MESHD_BACKHAUL_IFACE", "br-lan"),

		BatmanEnable:      envBoolOr("MESHD_BATMAN", true),
		BatmanPorts:       splitList(os.Getenv("MESHD_BATMAN_PORTS")),
		BatmanRoutingAlgo: envOr("MESHD_BATMAN_ROUTING_ALGO", "BATMAN_IV"),

		ManageNetwork: envBool("MESHD_MANAGE_NETWORK"),
		UplinkPort:    os.Getenv("MESHD_UPLINK_PORT"),
		LanDevice:     envOr("MESHD_LAN_DEVICE", "@device[0]"),

		SetupAPEnabled: envBoolOr("MESHD_SETUP_AP", true),
		SetupAPRadio:   envOr("MESHD_SETUP_AP_RADIO", "radio0"),
		SetupAPKey:     os.Getenv("MESHD_SETUP_AP_KEY"),

		LEDEnabled: envBoolOr("MESHD_LED", true),
		LEDName:    envOr("MESHD_LED_NAME", "green:status"),

		DevCORS: envBool("MESHD_DEV_CORS"),
	}
}

// adoptPolicyEnv resolves the adopt policy: MESHD_ADOPT_POLICY wins; otherwise
// the legacy MESHD_AUTO_ADOPT, when explicitly set, is authoritative (on ->
// "always", off -> "off"); only when neither is set do we fall back to the
// zero-touch default "onlink".
func adoptPolicyEnv() string {
	if p := os.Getenv("MESHD_ADOPT_POLICY"); p != "" {
		return p
	}
	if v, ok := os.LookupEnv("MESHD_AUTO_ADOPT"); ok && v != "" {
		if envBool("MESHD_AUTO_ADOPT") {
			return "always"
		}
		return "off"
	}
	return "onlink"
}

// envDurationOr parses a Go duration string (e.g. "20s", "1m") from key,
// returning fallback when unset or unparseable.
func envDurationOr(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

// DeriveHomeID builds a stable, unique default Home id from the device's node
// id, used when home_id is not explicitly configured. Every unconfigured device
// otherwise shares the literal "default-home", which makes them mutually
// invisible to discovery (a peer's announcement looks like the node's own Home)
// and unable to onboard. The node id is a hash of the device key, so this is
// stable across reboots and unique per device. See doc/network-model.md.
func DeriveHomeID(nodeID string) string {
	const n = 12
	short := nodeID
	if len(short) > n {
		short = short[:n]
	}
	if short == "" {
		return "home-unknown"
	}
	return "home-" + short
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
