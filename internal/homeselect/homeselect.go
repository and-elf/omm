// Package homeselect chooses which Home a multi-home device should activate on
// boot. A device is only ever active in one Home at a time.
//
// Selection priority (README "Profile Switching"):
//  1. an externally-controlled Home is always preferred over the device's own
//     Home — being your own controller is a last resort;
//  2. among same-kind candidates, the most recently active Home wins;
//  3. ties (and never-used Homes) are broken by strongest signal (RSSI).
package homeselect

import "sort"

// Candidate is a Home the device is a member of and that is currently
// reachable. Callers populate it from known memberships, the last-active record
// and (when available) a wireless scan.
type Candidate struct {
	HomeID         string
	ControllerURL  string
	LastActive     int64 // unix seconds; 0 = never activated
	Signal         int   // RSSI in dBm (negative); 0 = not measured
	SelfControlled bool  // true if this is the device's own Home
}

// Choose returns the Home to activate. An explicit user selection (e.g. set via
// the REST API) wins as long as it is still a reachable candidate; otherwise it
// falls back to the automatic policy in Select.
func Choose(explicitHomeID string, candidates []Candidate) (Candidate, bool) {
	if explicitHomeID != "" {
		for _, c := range candidates {
			if c.HomeID == explicitHomeID {
				return c, true
			}
		}
	}
	return Select(candidates)
}

// Select returns the best Home to activate, or ok=false if there are none.
func Select(candidates []Candidate) (Candidate, bool) {
	if len(candidates) == 0 {
		return Candidate{}, false
	}

	ranked := make([]Candidate, len(candidates))
	copy(ranked, candidates)

	sort.SliceStable(ranked, func(i, j int) bool {
		return less(ranked[i], ranked[j])
	})
	return ranked[0], true
}

// less reports whether a is a better choice than b.
func less(a, b Candidate) bool {
	// External homes always rank ahead of the device's own home.
	if a.SelfControlled != b.SelfControlled {
		return !a.SelfControlled
	}
	// Most recently active wins.
	if a.LastActive != b.LastActive {
		return a.LastActive > b.LastActive
	}
	// Otherwise the strongest measured signal wins.
	return signalRank(a.Signal) > signalRank(b.Signal)
}

// signalRank maps RSSI to a comparable strength, ranking an unmeasured signal
// (0) below any measured (negative) value.
func signalRank(rssi int) int {
	if rssi == 0 {
		return -1 << 30
	}
	return rssi
}
