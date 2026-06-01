package config

import "os"

type Config struct {
	HTTPAddr     string
	DatabasePath string
	UDPListen    string
	UbusSocket   string
	UbusBinary   string
}

func Load() Config {
	addr := os.Getenv("MESHD_HTTP_ADDR")
	if addr == "" {
		addr = "0.0.0.0:8080"
	}

	dbPath := os.Getenv("MESHD_DATABASE_PATH")
	if dbPath == "" {
		dbPath = "./meshd.db"
	}

	udpListen := os.Getenv("MESHD_UDP_LISTEN")
	if udpListen == "" {
		udpListen = ":45678"
	}

	ubusSocket := os.Getenv("MESHD_UBUS_SOCKET")
	if ubusSocket == "" {
		ubusSocket = "/var/run/ubus.sock"
	}

	ubusBinary := os.Getenv("MESHD_UBUS_BINARY")
	if ubusBinary == "" {
		ubusBinary = "ubus"
	}

	return Config{
		HTTPAddr:     addr,
		DatabasePath: dbPath,
		UDPListen:    udpListen,
		UbusSocket:   ubusSocket,
		UbusBinary:   ubusBinary,
	}
}
