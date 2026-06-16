package batman

import (
	"context"
	"os"
	"sort"
	"strings"
)

// EthernetPorts filters a LAN bridge's port list down to the physical ethernet
// ports that are candidates for batman backhaul: it drops the bat0 soft
// interface and any wireless vif (phy*). What remains (lan, wan, lanN, eth*) are
// the wired ports to classify as backhaul-or-client by peer detection.
func EthernetPorts(bridgePorts []string, batIface string) []string {
	out := make([]string, 0, len(bridgePorts))
	for _, p := range bridgePorts {
		if p == batIface || strings.HasPrefix(p, "phy") {
			continue
		}
		out = append(out, p)
	}
	return out
}

// SysfsBridgePorts lists a bridge's member ports from /sys/class/net/<bridge>/brif,
// sorted for determinism. readDir is injected for tests; nil uses os.ReadDir.
// Combined with EthernetPorts it yields the wired backhaul candidates without
// parsing UCI list options.
func SysfsBridgePorts(bridge string, readDir func(string) ([]string, error)) ([]string, error) {
	if readDir == nil {
		readDir = func(path string) ([]string, error) {
			entries, err := os.ReadDir(path)
			if err != nil {
				return nil, err
			}
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			return names, nil
		}
	}
	names, err := readDir("/sys/class/net/" + bridge + "/brif")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

// PortScan classifies candidate wired ports into batman backhaul links vs plain
// client ports by probing each for an OMM peer (a beacon seen arriving on it).
type PortScan struct {
	// Candidates are the physical ethernet ports to classify (see EthernetPorts).
	Candidates []string
	// HasPeer reports whether an OMM peer is observed on a port (e.g. a presence
	// beacon sniffed on the device). An error is treated as "no peer" — the safe
	// direction, since wrongly enslaving a client port cuts it off the LAN.
	HasPeer func(ctx context.Context, port string) (bool, error)
}

// BackhaulPorts returns the candidate ports with an OMM peer — the wired ports to
// enslave to bat0. Ports with no peer (real client jacks) are omitted and stay
// plain bridge members. Order follows Candidates for determinism.
func (s PortScan) BackhaulPorts(ctx context.Context) []string {
	var out []string
	for _, port := range s.Candidates {
		if s.HasPeer == nil {
			continue
		}
		if ok, err := s.HasPeer(ctx, port); err == nil && ok {
			out = append(out, port)
		}
	}
	return out
}
