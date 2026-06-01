// Package selection bridges stored Homes and live wireless signal into the
// homeselect policy, producing the Home a device should activate on boot.
package selection

import (
	"strings"

	"github.com/and-elf/omm/internal/homeselect"
	"github.com/and-elf/omm/internal/models"
)

// Signals maps a controller's lower-case MAC/BSSID to its observed RSSI (dBm).
type Signals map[string]int

// Candidates builds homeselect candidates from the Homes this device knows.
// A Home whose Controller matches an observed peer MAC is annotated with that
// peer's RSSI; the device's own Home is flagged so it is only a last resort.
func Candidates(homes []models.Home, selfHomeID string, signals Signals) []homeselect.Candidate {
	candidates := make([]homeselect.Candidate, 0, len(homes))
	for _, h := range homes {
		c := homeselect.Candidate{
			HomeID:         h.ID,
			LastActive:     h.LastSeen,
			SelfControlled: h.ID == selfHomeID,
		}
		if sig, ok := signals[strings.ToLower(h.Controller)]; ok {
			c.Signal = sig
		}
		candidates = append(candidates, c)
	}
	return candidates
}

// Recommend chooses the Home to activate: an explicit selection wins, otherwise
// the automatic policy (external over self, most recently active, strongest
// RSSI).
func Recommend(homes []models.Home, selfHomeID, explicitHomeID string, signals Signals) (homeselect.Candidate, bool) {
	return homeselect.Choose(explicitHomeID, Candidates(homes, selfHomeID, signals))
}
