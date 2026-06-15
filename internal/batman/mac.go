package batman

import (
	"fmt"
	"hash/fnv"
	"os"
	"strconv"
	"strings"
)

// uniqueHardifMAC derives a stable, locally-administered, unicast MAC for an
// enslaved wired port from its real MAC. batman-adv requires every hardif on a
// bat0 to have a distinct MAC; on shared-MAC DSA hardware (the ZB's lan1/2/3 and
// its mesh vif all share one MAC) two hardifs otherwise collide and the wired
// link goes to TQ0. This transform:
//   - sets the locally-administered bit and clears the multicast bit on octet 0,
//     so the result can never equal the real (globally-administered) MAC the mesh
//     vif keeps, and is a valid unicast address;
//   - folds the port name into the last octet, so several ports sharing one base
//     MAC (DSA switch ports) still differ from each other;
//   - leaves the remaining octets from the real MAC, so the result stays unique
//     across nodes (different real MACs => different results; XOR is per-byte
//     injective).
//
// It is deterministic (same inputs => same MAC) so the batman identity is stable
// across reboots.
func uniqueHardifMAC(realMAC, port string) (string, error) {
	parts := strings.Split(strings.TrimSpace(realMAC), ":")
	if len(parts) != 6 {
		return "", fmt.Errorf("malformed MAC %q", realMAC)
	}
	var b [6]byte
	for i, p := range parts {
		v, err := strconv.ParseUint(p, 16, 8)
		if err != nil {
			return "", fmt.Errorf("malformed MAC octet %q: %w", p, err)
		}
		b[i] = byte(v)
	}
	b[0] = (b[0] | 0x02) &^ 0x01 // locally-administered, unicast
	h := fnv.New32a()
	_, _ = h.Write([]byte(port))
	b[5] ^= byte(h.Sum32()) // port discriminator, preserves node-uniqueness
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3], b[4], b[5]), nil
}

// SysfsMAC reads a device's MAC from /sys/class/net/<dev>/address. read is
// injected for tests; nil uses os.ReadFile. It is the MAC source for the batman
// Manager's unique-hardif-MAC assignment.
func SysfsMAC(read func(path string) ([]byte, error)) func(dev string) (string, error) {
	if read == nil {
		read = os.ReadFile
	}
	return func(dev string) (string, error) {
		b, err := read("/sys/class/net/" + dev + "/address")
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
}
