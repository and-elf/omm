//go:build linux

package batman

import (
	"context"
	"net"
	"syscall"
	"time"
)

// htons converts a uint16 to network byte order for AF_PACKET protocol fields.
func htons(v uint16) uint16 { return v<<8 | v>>8 }

const ethPIP = 0x0800 // IPv4 ethertype

// SniffOMMBeacon listens for an INGRESS UDP broadcast to udpPort arriving on dev
// for up to timeout, returning true as soon as one does. Every meshd broadcasts
// a presence beacon to the discovery UDP port, so a beacon arriving on a wired
// port means an OMM node is physically on that wire — the bootstrap signal for
// per-port batman enslavement, independent of batman state. It neither enslaves
// the port nor transmits (non-disruptive), and filters PACKET_OUTGOING so our
// own beacon flooded out the port doesn't count — only a peer on the port does.
func SniffOMMBeacon(ctx context.Context, dev string, udpPort int, timeout time.Duration) (bool, error) {
	iface, err := net.InterfaceByName(dev)
	if err != nil {
		return false, err
	}
	// SOCK_DGRAM strips the link header, so the buffer starts at the IP packet.
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_DGRAM, int(htons(ethPIP)))
	if err != nil {
		return false, err
	}
	defer syscall.Close(fd)
	if err := syscall.Bind(fd, &syscall.SockaddrLinklayer{
		Protocol: htons(ethPIP),
		Ifindex:  iface.Index,
	}); err != nil {
		return false, err
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	buf := make([]byte, 1500)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false, nil
		}
		tv := syscall.NsecToTimeval(remaining.Nanoseconds())
		if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
			return false, err
		}
		n, from, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil {
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
				return false, nil // timed out: no beacon on this wire
			}
			if err == syscall.EINTR {
				continue
			}
			return false, err
		}
		// Ignore our own beacon flooded OUT this port — only a peer's INGRESS
		// beacon means an OMM node is physically on the wire.
		if ll, ok := from.(*syscall.SockaddrLinklayer); ok && ll.Pkttype == packetOutgoing {
			continue
		}
		if udpDestPort(buf[:n]) == udpPort {
			return true, nil
		}
	}
}

// packetOutgoing is linux PACKET_OUTGOING (frames we transmit), filtered so a
// node's own broadcast beacon doesn't self-classify its ports.
const packetOutgoing = 4

// udpDestPort returns the UDP destination port of an IPv4 packet, or -1 if the
// packet is not IPv4/UDP or is too short to parse.
func udpDestPort(ip []byte) int {
	if len(ip) < 20 || ip[0]>>4 != 4 {
		return -1
	}
	ihl := int(ip[0]&0x0f) * 4
	if ihl < 20 || ip[9] != 17 { // proto 17 = UDP
		return -1
	}
	if len(ip) < ihl+4 {
		return -1
	}
	return int(ip[ihl+2])<<8 | int(ip[ihl+3])
}
