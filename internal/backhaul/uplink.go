package backhaul

import (
	"fmt"
	"strconv"
	"strings"
)

// The failover loop watches ONE physical port's carrier to decide wired-vs-mesh.
// On a node where "any ethernet jack is a relay", the jack facing the home is not
// fixed, so we auto-detect it: the wired uplink is the bridge port through which
// the node currently reaches its default gateway (the controller). Everything
// here is pure parsing over /proc and `bridge fdb` text so it is unit-testable;
// the daemon supplies the live readers.

// UplinkReaders supplies the three data sources used to locate the wired uplink
// port, injected so ResolveUplinkPort is testable without /proc or a bridge.
type UplinkReaders struct {
	// Route returns the contents of /proc/net/route.
	Route func() (string, error)
	// ARP returns the contents of /proc/net/arp.
	ARP func() (string, error)
	// FDB returns the output of `bridge fdb show` (mac -> dev lines).
	FDB func() (string, error)
}

// ResolveUplinkPort returns the physical ethernet port to watch for carrier, or
// "" when there is no wired uplink (a wireless-only node — its mesh then runs
// always-on rather than as a carrier-gated standby).
//
// An explicit configured port always wins. Otherwise it follows default route ->
// gateway IP -> gateway MAC (ARP) -> the bridge port that MAC is learned on, and
// rejects the batman soft interface / wireless vifs: a gateway reachable only
// over bat0 means the wire is already down, i.e. no wired uplink to watch.
func ResolveUplinkPort(explicit, batIface string, r UplinkReaders) string {
	if explicit != "" {
		return explicit
	}
	route, err := r.Route()
	if err != nil {
		return ""
	}
	gwIP := parseDefaultGateway(route)
	if gwIP == "" {
		return ""
	}
	arp, err := r.ARP()
	if err != nil {
		return ""
	}
	mac := parseARPMAC(arp, gwIP)
	if mac == "" {
		return ""
	}
	fdb, err := r.FDB()
	if err != nil {
		return ""
	}
	port := parseFDBPort(fdb, mac)
	if port == "" || port == batIface || strings.HasPrefix(port, "phy") {
		return ""
	}
	return port
}

// parseDefaultGateway extracts the IPv4 default gateway (dotted) from the
// contents of /proc/net/route: the row whose Destination is 00000000 and whose
// Gateway is non-zero. The Gateway field is little-endian hex.
func parseDefaultGateway(procNetRoute string) string {
	for _, line := range strings.Split(procNetRoute, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[1] != "00000000" || f[2] == "00000000" {
			continue
		}
		if ip := leHexToIP(f[2]); ip != "" {
			return ip
		}
	}
	return ""
}

// leHexToIP converts a little-endian 8-hex-digit string (as in /proc/net/route,
// e.g. "0102A8C0") to a dotted IPv4 address ("192.168.2.1").
func leHexToIP(h string) string {
	if len(h) != 8 {
		return ""
	}
	var b [4]uint64
	for i := 0; i < 4; i++ {
		v, err := strconv.ParseUint(h[i*2:i*2+2], 16, 8)
		if err != nil {
			return ""
		}
		b[i] = v
	}
	// little-endian: last byte pair is the first octet.
	return fmt.Sprintf("%d.%d.%d.%d", b[3], b[2], b[1], b[0])
}

// parseARPMAC returns the HW address for ip from the contents of /proc/net/arp,
// or "" if absent or incomplete (00:00:00:00:00:00).
func parseARPMAC(procNetArp, ip string) string {
	for _, line := range strings.Split(procNetArp, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[0] != ip {
			continue
		}
		mac := f[3]
		if mac == "00:00:00:00:00:00" {
			return ""
		}
		return mac
	}
	return ""
}

// parseFDBPort returns the bridge port a MAC is learned on from `bridge fdb show`
// output. Lines look like "f8:5e:3c:a0:57:8a dev lan1 master br-lan". The first
// entry that names a device via "dev" wins.
func parseFDBPort(fdbShow, mac string) string {
	mac = strings.ToLower(mac)
	for _, line := range strings.Split(fdbShow, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || strings.ToLower(f[0]) != mac {
			continue
		}
		for i := 1; i < len(f)-1; i++ {
			if f[i] == "dev" {
				return f[i+1]
			}
		}
	}
	return ""
}
