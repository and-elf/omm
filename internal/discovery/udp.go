package discovery

import (
	"context"
	"net"
)

func ListenUDP(ctx context.Context, address string, handler func([]byte, *net.UDPAddr)) error {
	conn, err := net.ListenPacket("udp4", address)
	if err != nil {
		return err
	}
	defer conn.Close()

	buf := make([]byte, 2048)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return err
			}
			udpAddr, ok := addr.(*net.UDPAddr)
			if !ok {
				continue
			}
			handler(buf[:n], udpAddr)
		}
	}
}
