package config

import (
	"os"
	"strings"
)

// Config holds the meshd daemon settings. A daemon always runs as a controller
// for its own Home and can additionally enroll into other controllers (as a
// node) at runtime or via Join.
type Config struct {
	HTTPAddr     string
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

	// Device identity and homes to join at startup.
	IdentityDir string
	Serial      string
	Join        []string // controller URLs to enroll into on boot

	// Topology collection.
	BatmanIface  string   // batman-adv interface (e.g. bat0)
	APInterfaces []string // hostapd interfaces to read clients from
}

func Load() Config {
	return Config{
		HTTPAddr:     envOr("MESHD_HTTP_ADDR", "0.0.0.0:8080"),
		DatabasePath: envOr("MESHD_DATABASE_PATH", "./meshd.db"),
		UDPListen:    envOr("MESHD_UDP_LISTEN", ":45678"),
		UbusSocket:   envOr("MESHD_UBUS_SOCKET", "/var/run/ubus.sock"),
		UbusBinary:   envOr("MESHD_UBUS_BINARY", "ubus"),

		HomeID:       envOr("MESHD_HOME_ID", "default-home"),
		HomeName:     envOr("MESHD_HOME_NAME", "Home"),
		ControllerID: envOr("MESHD_CONTROLLER_ID", "gw01"),
		AutoAdopt:    envBool("MESHD_AUTO_ADOPT"),
		APIAdvertise: os.Getenv("MESHD_API_ADVERTISE"),
		UDPBroadcast: envOr("MESHD_UDP_BROADCAST", "255.255.255.255:45678"),

		IdentityDir: envOr("MESHD_IDENTITY_DIR", "./meshd-identity"),
		Serial:      envOr("MESHD_SERIAL", hostnameOr("unknown")),
		Join:        splitList(os.Getenv("MESHD_JOIN")),

		BatmanIface:  envOr("MESHD_BATMAN_IFACE", "bat0"),
		APInterfaces: splitList(os.Getenv("MESHD_AP_IFACES")),
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
