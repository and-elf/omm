//go:build linux

package batman

import "testing"

// udpPacket builds a minimal IPv4 header (ihl words) + the start of a UDP header
// carrying destPort, for exercising udpDestPort's parsing.
func udpPacket(ihlWords int, proto byte, destPort int) []byte {
	ihl := ihlWords * 4
	p := make([]byte, ihl+4)
	p[0] = 0x40 | byte(ihlWords) // version 4, ihl
	p[9] = proto
	p[ihl+2] = byte(destPort >> 8)
	p[ihl+3] = byte(destPort & 0xff)
	return p
}

func TestUDPDestPort(t *testing.T) {
	if got := udpDestPort(udpPacket(5, 17, 45678)); got != 45678 {
		t.Errorf("standard IPv4/UDP: got %d, want 45678", got)
	}
	if got := udpDestPort(udpPacket(6, 17, 45678)); got != 45678 {
		t.Errorf("IPv4 with options (ihl=6): got %d, want 45678", got)
	}
	if got := udpDestPort(udpPacket(5, 6, 45678)); got != -1 {
		t.Errorf("TCP (proto 6) should not parse as UDP: got %d, want -1", got)
	}
	if got := udpDestPort([]byte{0x60, 0, 0}); got != -1 {
		t.Errorf("IPv6/short should be -1: got %d", got)
	}
	if got := udpDestPort(nil); got != -1 {
		t.Errorf("empty should be -1: got %d", got)
	}
}
