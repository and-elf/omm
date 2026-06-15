package backhaul

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/and-elf/omm/internal/topology"
)

func TestDecidePrefersEthernet(t *testing.T) {
	cases := []struct {
		carrier, current, wantDesired string
		wantChange                    bool
	}{
		// Ethernet up: always ethernet; switch only if not already on it.
		{topology.BackhaulEthernet, topology.BackhaulWireless, topology.BackhaulEthernet, true},
		{topology.BackhaulEthernet, topology.BackhaulEthernet, topology.BackhaulEthernet, false},
		// Wire down: fall back to wireless; switch only if not already on it.
		{topology.BackhaulWireless, topology.BackhaulEthernet, topology.BackhaulWireless, true},
		{topology.BackhaulWireless, topology.BackhaulWireless, topology.BackhaulWireless, false},
		// From unknown start, the first real reading establishes (and applies) it.
		{topology.BackhaulEthernet, "", topology.BackhaulEthernet, true},
		{topology.BackhaulWireless, "", topology.BackhaulWireless, true},
		// Unknown carrier never flaps the current backhaul.
		{topology.BackhaulUnknown, topology.BackhaulEthernet, topology.BackhaulEthernet, false},
		{topology.BackhaulUnknown, topology.BackhaulWireless, topology.BackhaulWireless, false},
		{topology.BackhaulUnknown, "", "", false},
	}
	for _, c := range cases {
		desired, change := Decide(c.carrier, c.current)
		if desired != c.wantDesired || change != c.wantChange {
			t.Errorf("Decide(%q,%q) = (%q,%v), want (%q,%v)",
				c.carrier, c.current, desired, change, c.wantDesired, c.wantChange)
		}
	}
}

// A node booting on the wire deactivates the standby mesh (ethernet prioritized);
// when the wire drops it activates the mesh; when the wire returns it deactivates
// again. Each transition fires exactly once and is recorded.
func TestRunFailsOverAndBack(t *testing.T) {
	var mu sync.Mutex
	carrier := topology.BackhaulEthernet
	var activations, deactivations int
	var recorded []string

	setCarrier := func(v string) { mu.Lock(); carrier = v; mu.Unlock() }

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})

	go func() {
		Run(ctx, Deps{
			Interval: time.Millisecond,
			Carrier:  func(context.Context) string { mu.Lock(); defer mu.Unlock(); return carrier },
			Activate: func(context.Context) error { mu.Lock(); activations++; mu.Unlock(); return nil },
			Deactivate: func(context.Context) error {
				mu.Lock()
				deactivations++
				mu.Unlock()
				return nil
			},
			OnSwitch: func(_ context.Context, b string) { mu.Lock(); recorded = append(recorded, b); mu.Unlock() },
		})
		close(done)
	}()

	wait := func(cond func() bool) {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			ok := cond()
			mu.Unlock()
			if ok {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Fatal("condition not met in time")
	}

	// Boot on the wire: mesh deactivated, backhaul recorded ethernet.
	wait(func() bool { return deactivations == 1 })
	// Cable pulled -> activate mesh (wireless).
	setCarrier(topology.BackhaulWireless)
	wait(func() bool { return activations == 1 })
	// Cable back -> deactivate mesh (ethernet again).
	setCarrier(topology.BackhaulEthernet)
	wait(func() bool { return deactivations == 2 })

	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(recorded) != 3 ||
		recorded[0] != topology.BackhaulEthernet ||
		recorded[1] != topology.BackhaulWireless ||
		recorded[2] != topology.BackhaulEthernet {
		t.Fatalf("recorded switches = %v, want [ethernet wireless ethernet]", recorded)
	}
}

// On a wired node the mesh can be re-enabled behind the failover's back — a
// profile re-apply on a joined node's boot recreates the mesh enabled. With only
// carrier-transition tracking the loop would never notice (carrier never
// changed) and leave a standing ethernet+mesh bridging loop. With Current wired
// in, the loop reconciles against the actual mesh state and disables it again.
func TestRunReconcilesExternalMeshEnable(t *testing.T) {
	var mu sync.Mutex
	// actual is the real backhaul the reconciler observes: it starts wireless
	// because something (a profile re-apply) left the mesh enabled while wired.
	actual := topology.BackhaulWireless
	var deactivations int

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})

	go func() {
		Run(ctx, Deps{
			Interval: time.Millisecond,
			Carrier:  func(context.Context) string { return topology.BackhaulEthernet }, // wire always up
			Current:  func(context.Context) string { mu.Lock(); defer mu.Unlock(); return actual },
			Activate: func(context.Context) error { mu.Lock(); actual = topology.BackhaulWireless; mu.Unlock(); return nil },
			Deactivate: func(context.Context) error {
				mu.Lock()
				deactivations++
				actual = topology.BackhaulEthernet
				mu.Unlock()
				return nil
			},
		})
		close(done)
	}()

	wait := func(cond func() bool) {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			ok := cond()
			mu.Unlock()
			if ok {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Fatal("condition not met in time")
	}

	// Wire is up but the mesh was left enabled -> reconcile by disabling it.
	wait(func() bool { return deactivations == 1 })
	// Simulate a profile re-apply re-enabling the mesh behind the loop's back.
	mu.Lock()
	actual = topology.BackhaulWireless
	mu.Unlock()
	// The loop must disable it again from the observed state, not wait for a
	// (never-coming) carrier transition.
	wait(func() bool { return deactivations == 2 })

	cancel()
	<-done
}

// A failed actuation does not advance state, so the next tick retries it.
func TestRunRetriesOnActuationError(t *testing.T) {
	var mu sync.Mutex
	var calls int
	failFirst := true

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})

	go func() {
		Run(ctx, Deps{
			Interval: time.Millisecond,
			Carrier:  func(context.Context) string { return topology.BackhaulWireless },
			Activate: func(context.Context) error {
				mu.Lock()
				defer mu.Unlock()
				calls++
				if failFirst {
					failFirst = false
					return errors.New("reload failed")
				}
				return nil
			},
			Deactivate: func(context.Context) error { return nil },
		})
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		ok := calls >= 2
		mu.Unlock()
		if ok {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if calls < 2 {
		t.Fatalf("expected Activate to be retried after failure, got %d calls", calls)
	}
}
