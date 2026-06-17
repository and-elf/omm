package topology

import (
	"context"
	"os"
	"strings"
)

// BatctlSelfAddrs reports every batman-adv address by which this node is known to
// its peers: bat0's own MAC plus the MAC of each enslaved hard interface. This
// matters because batman-adv keys originators by the hard interface the OGMs
// arrive on, so a node with a wired backhaul appears under several originator
// MACs — its mesh MAC and each ethernet port's unique hardif MAC (see
// batman.uniqueHardifMAC). Reporting only bat0's MAC leaves those wired-port
// originators unmapped, so reconcile keeps them as phantom "separate node"
// vertices. Hard interfaces are listed via `batctl if`; each device's MAC is read
// from sysfs. It degrades to just bat0's address when batctl is unavailable.
type BatctlSelfAddrs struct {
	Iface string        // batman soft interface, e.g. "bat0"
	Run   CommandRunner // nil => ExecRunner
	Read  fileReader    // nil => os.ReadFile
}

func (b BatctlSelfAddrs) SelfAddrs(ctx context.Context) []string {
	if b.Iface == "" {
		return nil
	}
	run := b.Run
	if run == nil {
		run = ExecRunner
	}

	seen := map[string]bool{}
	var addrs []string
	add := func(mac string) {
		if mac != "" && !seen[mac] {
			seen[mac] = true
			addrs = append(addrs, mac)
		}
	}

	// bat0's own MAC first — covers the common wireless case even with no batctl.
	add(readMAC(b.Read, b.Iface))

	// Each enslaved hard interface's MAC: this is what peers actually list as the
	// originator, and the only place the wired ports' unique MACs appear.
	if out, err := run(ctx, "batctl", "-m", b.Iface, "if"); err == nil {
		for _, dev := range parseHardifs(out) {
			add(readMAC(b.Read, dev))
		}
	}
	return addrs
}

// parseHardifs parses `batctl if` output, one hard interface per line in the form
// "<device>: <status>" (e.g. "eth1: active"). It returns the device names.
func parseHardifs(out []byte) []string {
	var devs []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if dev, _, ok := strings.Cut(line, ":"); ok {
			if dev = strings.TrimSpace(dev); dev != "" {
				devs = append(devs, dev)
			}
		}
	}
	return devs
}

// readMAC reads a device's MAC from /sys/class/net/<dev>/address, lowercased and
// trimmed; "" on any error or empty value.
func readMAC(read fileReader, dev string) string {
	if read == nil {
		read = os.ReadFile
	}
	b, err := read("/sys/class/net/" + dev + "/address")
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(string(b)))
}
