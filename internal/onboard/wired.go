// Package onboard holds the policy for unattended onboarding of nodes. Its logic
// is kept here, separate from the daemon wiring in main, so it is unit-testable.
package onboard

import (
	"context"
	"log"
	"sort"
	"time"

	"github.com/and-elf/omm/internal/discovery"
	"github.com/and-elf/omm/internal/topology"
)

// Decide reports whether an unclaimed, ethernet-connected node should auto-enroll
// this tick and, if so, which discovered controller to enroll into.
//
// It acts only when every condition holds: the node is not yet claimed
// (!complete), no home has been chosen yet (activeHome empty), the backhaul is
// ethernet (a node on the wire is implicitly trusted to onboard unattended), and
// at least one controller other than this node's own Home has been discovered.
//
// When several controllers qualify it picks the lowest HomeID, so the choice is
// stable across ticks and reproducible in tests; on the wire link quality is
// irrelevant, and boot home-selection re-evaluates the active home across all
// recorded homes afterwards anyway.
func Decide(complete bool, activeHome, backhaul string, controllers []discovery.Announcement, selfHomeID string) (controllerURL string, act bool) {
	if complete || activeHome != "" || backhaul != topology.BackhaulEthernet {
		return "", false
	}

	candidates := make([]discovery.Announcement, 0, len(controllers))
	for _, a := range controllers {
		if a.HomeID == selfHomeID || a.API == "" {
			continue
		}
		candidates = append(candidates, a)
	}
	if len(candidates) == 0 {
		return "", false
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].HomeID < candidates[j].HomeID })
	return candidates[0].API, true
}

// Deps are the inputs to the wired auto-onboard loop, injected as closures so
// the loop is testable without a real store, sysfs, or controller.
type Deps struct {
	Interval      time.Duration                             // poll cadence (default 5s)
	SelfHomeID    string                                    // this node's own Home, never auto-joined
	SetupComplete func(ctx context.Context) (bool, error)   // is the node already claimed?
	ActiveHome    func(ctx context.Context) (string, error) // currently-active home (empty => none)
	Backhaul      func(ctx context.Context) string          // current backhaul type
	Discover      func() []discovery.Announcement           // controllers currently discovered
	// Join enrolls into the chosen controller and, on success, completes setup.
	// Returning nil means the node is now onboarded and the loop stops.
	Join func(ctx context.Context, controllerURL string) error
}

// Run polls the onboarding state and, when an unclaimed node is on the wire with
// a controller in reach, auto-enrolls it. It is a one-shot in spirit — it stops
// once the node is claimed (its own setup completes, or another path claims it)
// — but retries on transient failures and keeps watching in case the ethernet
// link or a controller only appears later. It returns when the node is onboarded
// or the context is cancelled.
func Run(ctx context.Context, d Deps) {
	interval := d.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		complete, err := d.SetupComplete(ctx)
		if err != nil {
			log.Printf("auto-onboard: read setup state failed: %v", err)
		} else if complete {
			return // claimed (here or by another path): nothing more to do
		}

		active, err := d.ActiveHome(ctx)
		if err != nil {
			log.Printf("auto-onboard: read active home failed: %v", err)
		}

		if url, act := Decide(complete, active, d.Backhaul(ctx), d.Discover(), d.SelfHomeID); act {
			if jerr := d.Join(ctx, url); jerr != nil {
				log.Printf("auto-onboard: join %s failed, will retry: %v", url, jerr)
			} else {
				log.Printf("auto-onboard: enrolled into %s over ethernet, setup complete", url)
				return
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
