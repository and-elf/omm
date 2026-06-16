//go:build !linux

package batman

import (
	"context"
	"errors"
	"time"
)

// SniffOMMBeacon is Linux-only (raw AF_PACKET). The stub reports an error so
// callers treat the port as having no peer (safe: leave it a plain bridge port).
func SniffOMMBeacon(ctx context.Context, dev string, udpPort int, timeout time.Duration) (bool, error) {
	return false, errors.New("OMM beacon sniff unsupported on this platform")
}
