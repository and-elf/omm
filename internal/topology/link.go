package topology

import (
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// SysfsIwLinkMetrics classifies a mesh link and measures it from the outgoing
// batman hard interface. A wireless interface (one with a phy80211 in sysfs)
// yields the peer's RSSI from `iw dev <iface> station dump`; a wired one yields
// its negotiated speed from `/sys/class/net/<iface>/speed`. Every dependency is
// injected so the source is testable without a real device, and any failure
// degrades to a zero-value (unknown) field per the package charter.
type SysfsIwLinkMetrics struct {
	Run    CommandRunner          // runs `iw`; nil => ExecRunner
	Read   fileReader             // reads sysfs; nil => os.ReadFile
	Exists func(path string) bool // tests a sysfs path; nil => os.Stat
}

func (s SysfsIwLinkMetrics) LinkMetrics(ctx context.Context, iface, nexthop string) LinkMetrics {
	if iface == "" {
		return LinkMetrics{}
	}
	if s.isWireless(iface) {
		return LinkMetrics{Kind: LinkWireless, Signal: s.signal(ctx, iface, nexthop)}
	}
	return LinkMetrics{Kind: LinkWired, SpeedMbps: s.speed(iface)}
}

func (s SysfsIwLinkMetrics) isWireless(iface string) bool {
	exists := s.Exists
	if exists == nil {
		exists = func(path string) bool { _, err := os.Stat(path); return err == nil }
	}
	return exists("/sys/class/net/" + iface + "/phy80211")
}

func (s SysfsIwLinkMetrics) speed(iface string) int {
	read := s.Read
	if read == nil {
		read = os.ReadFile
	}
	b, err := read("/sys/class/net/" + iface + "/speed")
	if err != nil {
		return 0
	}
	// A down interface reports -1; clamp anything non-positive to "unknown".
	if mbps, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && mbps > 0 {
		return mbps
	}
	return 0
}

func (s SysfsIwLinkMetrics) signal(ctx context.Context, iface, nexthop string) int {
	run := s.Run
	if run == nil {
		run = ExecRunner
	}
	out, err := run(ctx, "iw", "dev", iface, "station", "dump")
	if err != nil {
		return 0
	}
	return signalForStation(out, nexthop)
}

var iwSignalRe = regexp.MustCompile(`signal:\s*(-?\d+)`)

// signalForStation parses `iw … station dump` output and returns the RSSI (dBm)
// of the station whose MAC matches peer. Output is a sequence of "Station <mac>"
// blocks, each with a "signal:\t-58 … dBm" line; we return the signal in the
// block whose MAC matches.
func signalForStation(out []byte, peer string) int {
	peer = strings.ToLower(peer)
	var inBlock bool
	for _, line := range strings.Split(string(out), "\n") {
		if macs := macRe.FindAllString(line, -1); strings.HasPrefix(strings.TrimSpace(line), "Station") && len(macs) > 0 {
			inBlock = strings.ToLower(macs[0]) == peer
			continue
		}
		if inBlock {
			if m := iwSignalRe.FindStringSubmatch(line); m != nil {
				if v, err := strconv.Atoi(m[1]); err == nil {
					return v
				}
			}
		}
	}
	return 0
}
