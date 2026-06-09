package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"syscall"
	"time"
)

// Announcement is the controller presence message broadcast over UDP.
type Announcement struct {
	HomeID       string `json:"home_id"`
	Name         string `json:"name"`
	ControllerID string `json:"controller_id"`
	API          string `json:"api"`
}

// Announce periodically broadcasts the controller announcement until the
// context is cancelled. address supplies the destination port and an optional
// explicit target host (e.g. "255.255.255.255:45678").
//
// It does NOT dial a connected socket to the limited broadcast 255.255.255.255:
// that needs a route for it, which a segment with no default gateway lacks
// (dial fails with "network is unreachable"). Instead it opens an unconnected,
// SO_BROADCAST-enabled socket and sends to each interface's *subnet-directed*
// broadcast (e.g. 192.168.2.255), which routes over the connected subnet route
// with no default gateway — plus the explicit target best-effort for the routed
// case.
func Announce(ctx context.Context, address string, ann Announcement, interval time.Duration) error {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("parse announce address: %w", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("parse announce port: %w", err)
	}

	pc, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return fmt.Errorf("open announce socket: %w", err)
	}
	defer pc.Close()
	conn := pc.(*net.UDPConn)

	// Permit sending to a broadcast address; without SO_BROADCAST the kernel
	// rejects broadcast sends with EACCES ("permission denied").
	if rc, cerr := conn.SyscallConn(); cerr == nil {
		_ = rc.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
		})
	}

	payload, err := json.Marshal(ann)
	if err != nil {
		return err
	}

	explicit := net.ParseIP(host)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Announce immediately, then on each tick, to every target. Per-target write
	// errors are ignored: an unroutable target (e.g. 255.255.255.255 with no
	// default route) must not stop announcing on the targets that do work.
	for {
		for _, ip := range broadcastTargets(explicit) {
			_, _ = conn.WriteTo(payload, &net.UDPAddr{IP: ip, Port: port})
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// broadcastTargets is the set of IPv4 destinations to announce to: every up,
// broadcast-capable interface's subnet-directed broadcast, plus the explicit
// target (when set) deduplicated. The directed broadcasts are what reach peers
// on a segment with no default route.
func broadcastTargets(explicit net.IP) []net.IP {
	var out []net.IP
	seen := map[string]bool{}
	add := func(ip net.IP) {
		if ip == nil {
			return
		}
		if k := ip.String(); !seen[k] {
			seen[k] = true
			out = append(out, ip)
		}
	}

	ifaces, _ := net.Interfaces()
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, ip := range directedBroadcasts(addrs) {
			add(ip)
		}
	}
	add(explicit)
	return out
}

// directedBroadcasts computes the subnet broadcast address for each IPv4
// address in addrs (host bits set: ip | ^mask).
func directedBroadcasts(addrs []net.Addr) []net.IP {
	var out []net.IP
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip4 := ipnet.IP.To4()
		if ip4 == nil {
			continue
		}
		mask := ipnet.Mask
		if len(mask) == net.IPv6len {
			mask = mask[12:]
		}
		if len(mask) != net.IPv4len {
			continue
		}
		b := make(net.IP, net.IPv4len)
		for i := range net.IPv4len {
			b[i] = ip4[i] | ^mask[i]
		}
		out = append(out, b)
	}
	return out
}

// DiscoverController listens on listenAddr for the first controller
// announcement carrying an API endpoint and returns it.
func DiscoverController(ctx context.Context, listenAddr string) (Announcement, error) {
	conn, err := net.ListenPacket("udp4", listenAddr)
	if err != nil {
		return Announcement{}, fmt.Errorf("listen for announcements: %w", err)
	}
	defer conn.Close()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	buf := make([]byte, 2048)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return Announcement{}, ctx.Err()
			}
			return Announcement{}, err
		}
		var ann Announcement
		if err := json.Unmarshal(buf[:n], &ann); err != nil {
			continue // ignore non-announcement traffic
		}
		if ann.API != "" {
			return ann, nil
		}
	}
}
