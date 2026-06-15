//go:build linux

package batman

import (
	"context"
	"net"
	"syscall"
	"time"
)

// ethPBatman is the batman-adv ethertype. A node running batman on a link
// broadcasts OGM (originator) frames with this ethertype, so seeing one on a
// device means a batman peer speaks on that wire.
const ethPBatman = 0x4305

// SniffBatadvPeer passively listens for a batman-adv frame on dev for up to
// timeout, returning true as soon as one arrives. It does NOT enslave the device
// or transmit anything, so it is safe to probe a port that is still a plain
// bridge member. A timeout with no frame is (false, nil) — no peer on the wire.
//
// It is the PeerOnWire gate for ResolveBackhaul: only a wire with a batman peer
// is enslaved to bat0; a controller's shared LAN (no batman on the wire) stays
// plain-bridged. Any setup error is returned so the caller can treat it as
// "no peer" (the safe default).
func SniffBatadvPeer(ctx context.Context, dev string, timeout time.Duration) (bool, error) {
	iface, err := net.InterfaceByName(dev)
	if err != nil {
		return false, err
	}
	proto := int(htons(ethPBatman))
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, proto)
	if err != nil {
		return false, err
	}
	defer syscall.Close(fd)

	if err := syscall.Bind(fd, &syscall.SockaddrLinklayer{
		Protocol: htons(ethPBatman),
		Ifindex:  iface.Index,
	}); err != nil {
		return false, err
	}

	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	tv := syscall.NsecToTimeval(timeout.Nanoseconds())
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
		return false, err
	}

	// One batadv frame is enough to confirm a peer (OGMs broadcast roughly every
	// second). The socket is bound to the batadv ethertype, so any frame received
	// is an OGM from another node — we transmit none on this wire (not enslaved).
	buf := make([]byte, 256)
	n, _, err := syscall.Recvfrom(fd, buf, 0)
	if err != nil {
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			return false, nil // timed out: no batman peer on this wire
		}
		return false, err
	}
	return n > 0, nil
}

// htons converts a uint16 to network byte order for AF_PACKET protocol fields.
func htons(v uint16) uint16 { return v<<8 | v>>8 }
