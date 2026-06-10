// Package backhaul holds the policy for switching a node between its wired
// (ethernet) and wireless (802.11s mesh) backhaul. Ethernet is always
// preferred: the wireless mesh is a standby that activates only when the wired
// uplink loses carrier, and is torn back down the moment the wire returns (also
// avoiding an ethernet+mesh bridging loop). The decision logic lives here,
// separate from the daemon wiring in main, so it is unit-testable.
package backhaul

import (
	"context"
	"log"
	"time"

	"github.com/and-elf/omm/internal/topology"
)

// Decide reports which backhaul the node should use this tick and whether that
// differs from what it is using now.
//
// Ethernet always wins when the wired uplink has carrier; the wireless mesh is
// used only when ethernet is down. An unknown carrier reading leaves the
// current backhaul unchanged, so a transient unreadable signal never flaps the
// backhaul (and a node with no uplink iface configured is left as-is).
func Decide(carrier, current string) (desired string, change bool) {
	switch carrier {
	case topology.BackhaulEthernet:
		desired = topology.BackhaulEthernet
	case topology.BackhaulWireless:
		desired = topology.BackhaulWireless
	default:
		return current, false
	}
	return desired, desired != current
}

// Deps are the inputs to the backhaul failover loop, injected as closures so the
// loop is testable without sysfs or a real wireless stack.
type Deps struct {
	// Interval is the poll cadence (default 5s).
	Interval time.Duration
	// Carrier reports the wired uplink's current state: ethernet (cable up),
	// wireless (cable down), or unknown.
	Carrier func(ctx context.Context) string
	// Activate brings the wireless mesh backhaul up (the wire is gone).
	Activate func(ctx context.Context) error
	// Deactivate tears the wireless mesh down so the wire is the sole path
	// (ethernet present) — keeping ethernet prioritized and avoiding a loop.
	Deactivate func(ctx context.Context) error
	// OnSwitch records the now-active backhaul after a successful switch (e.g. to
	// persist state for /status and the topology). Optional.
	OnSwitch func(ctx context.Context, backhaul string)
	// Initial is the backhaul assumed at start. Empty (unknown) makes the first
	// real carrier reading establish — and apply — the baseline.
	Initial string
}

// Run polls the wired uplink and switches the node's backhaul, keeping ethernet
// preferred and the wireless mesh as a standby. It applies a switch only on a
// transition (and retries the actuation on failure, since the next tick will
// see the same desired state). It returns when ctx is done.
func Run(ctx context.Context, d Deps) {
	interval := d.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	current := d.Initial
	for {
		if desired, change := Decide(d.Carrier(ctx), current); change {
			act, label := d.Deactivate, topology.BackhaulEthernet
			if desired == topology.BackhaulWireless {
				act, label = d.Activate, topology.BackhaulWireless
			}
			if err := act(ctx); err != nil {
				log.Printf("backhaul: switch to %s failed, will retry: %v", label, err)
			} else {
				log.Printf("backhaul: switched to %s", label)
				current = desired
				if d.OnSwitch != nil {
					d.OnSwitch(ctx, desired)
				}
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
