package config

import "os"

// Role determines whether meshd acts as a controller or an enrolling client.
type Role string

const (
	RoleController Role = "controller"
	RoleClient     Role = "client"
)

type Config struct {
	Role         Role
	HTTPAddr     string
	DatabasePath string
	UDPListen    string
	UbusSocket   string
	UbusBinary   string

	// Controller settings.
	HomeID       string
	HomeName     string
	ControllerID string
	AutoAdopt    bool
	APIAdvertise string // API URL announced to clients (defaults to HTTPAddr)
	UDPBroadcast string // broadcast endpoint for announcements

	// Client settings.
	IdentityDir   string
	ControllerURL string // explicit controller API URL; empty means UDP discovery
	Serial        string
}

func Load() Config {
	return Config{
		Role:         Role(envOr("MESHD_ROLE", string(RoleController))),
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

		IdentityDir:   envOr("MESHD_IDENTITY_DIR", "/etc/meshd/identity"),
		ControllerURL: os.Getenv("MESHD_CONTROLLER"),
		Serial:        envOr("MESHD_SERIAL", hostnameOr("unknown")),
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

func hostnameOr(fallback string) string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return fallback
}
