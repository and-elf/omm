package topology

import (
	"context"
	"os"
	"strings"
)

// Backhaul classification values: how a node reaches the rest of the mesh.
const (
	BackhaulEthernet = "ethernet"
	BackhaulWireless = "wireless"
	BackhaulUnknown  = "unknown"
)

// BackhaulSource reports this node's backhaul type so the topology can show
// whether a node is wired or joins over the wireless mesh.
type BackhaulSource interface {
	Backhaul(ctx context.Context) string
}

// fileReader reads a file; injected so SysfsBackhaul is testable without sysfs.
type fileReader func(path string) ([]byte, error)

// SysfsBackhaul classifies the backhaul by inspecting a configured ethernet
// uplink interface under /sys/class/net (the same sysfs convention resolveBSSID
// uses to read a MAC). A link with carrier means the node is wired; a
// configured-but-down interface means it falls back to the wireless mesh; no
// interface configured (or an unreadable one) is reported as unknown.
//
// Reporting a bare string with no error keeps Collect simple and matches the
// package's "degrade gracefully" charter.
type SysfsBackhaul struct {
	Iface string     // ethernet uplink interface, e.g. "eth0"; empty => unknown
	Read  fileReader // nil => os.ReadFile
}

func (s SysfsBackhaul) Backhaul(ctx context.Context) string {
	if s.Iface == "" {
		return BackhaulUnknown
	}
	read := s.Read
	if read == nil {
		read = os.ReadFile
	}
	base := "/sys/class/net/" + s.Iface

	// carrier is the most direct signal: "1" means a cable is plugged and the
	// link is up, "0" means it is down. The kernel returns EINVAL for carrier
	// on an administratively-down interface, so fall back to operstate.
	if b, err := read(base + "/carrier"); err == nil {
		switch strings.TrimSpace(string(b)) {
		case "1":
			return BackhaulEthernet
		case "0":
			return BackhaulWireless
		}
	}
	if b, err := read(base + "/operstate"); err == nil {
		if strings.TrimSpace(string(b)) == "up" {
			return BackhaulEthernet
		}
		return BackhaulWireless
	}
	return BackhaulUnknown
}
