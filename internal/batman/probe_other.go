//go:build !linux

package batman

import (
	"context"
	"errors"
	"time"
)

// SniffBatadvPeer is Linux-only (raw AF_PACKET). On other platforms it reports an
// error so ResolveBackhaul treats the wire as having no batman peer (the safe
// default: keep it plain-bridged). meshd runs on Linux/OpenWrt; this stub only
// keeps the package building for off-target development.
func SniffBatadvPeer(ctx context.Context, dev string, timeout time.Duration) (bool, error) {
	return false, errors.New("batadv peer sniff unsupported on this platform")
}
