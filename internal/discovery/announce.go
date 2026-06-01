package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Announcement is the controller presence message broadcast over UDP.
type Announcement struct {
	HomeID       string `json:"home_id"`
	Name         string `json:"name"`
	ControllerID string `json:"controller_id"`
	API          string `json:"api"`
}

// Announce periodically broadcasts the controller announcement to address until
// the context is cancelled. address is typically a broadcast endpoint such as
// "255.255.255.255:45678".
func Announce(ctx context.Context, address string, ann Announcement, interval time.Duration) error {
	udpAddr, err := net.ResolveUDPAddr("udp4", address)
	if err != nil {
		return fmt.Errorf("resolve announce address: %w", err)
	}
	conn, err := net.DialUDP("udp4", nil, udpAddr)
	if err != nil {
		return fmt.Errorf("dial announce address: %w", err)
	}
	defer conn.Close()

	payload, err := json.Marshal(ann)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Announce immediately, then on each tick.
	for {
		if _, err := conn.Write(payload); err != nil {
			return fmt.Errorf("write announce: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
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
