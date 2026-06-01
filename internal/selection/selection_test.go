package selection

import (
	"testing"

	"github.com/and-elf/omm/internal/models"
)

var homes = []models.Home{
	{ID: "self", Controller: "00:00:00:00:00:01", LastSeen: 500},
	{ID: "cottage", Controller: "aa:bb:cc:dd:ee:01", LastSeen: 100},
	{ID: "parents", Controller: "AA:BB:CC:DD:EE:02", LastSeen: 200},
}

func TestCandidatesAnnotatesSignalByControllerMAC(t *testing.T) {
	signals := Signals{"aa:bb:cc:dd:ee:01": -55, "aa:bb:cc:dd:ee:02": -80}
	c := Candidates(homes, "self", signals)

	byID := map[string]int{}
	self := false
	for _, cand := range c {
		byID[cand.HomeID] = cand.Signal
		if cand.HomeID == "self" {
			self = cand.SelfControlled
		}
	}
	if byID["cottage"] != -55 || byID["parents"] != -80 {
		t.Fatalf("signal not mapped by controller MAC: %+v", byID)
	}
	if !self {
		t.Fatal("self home should be flagged SelfControlled")
	}
}

func TestRecommendPrefersStrongestExternalSignal(t *testing.T) {
	// No history difference relevant; cottage has the stronger RSSI.
	signals := Signals{"aa:bb:cc:dd:ee:01": -55, "aa:bb:cc:dd:ee:02": -80}
	best, ok := Recommend([]models.Home{
		{ID: "cottage", Controller: "aa:bb:cc:dd:ee:01"},
		{ID: "parents", Controller: "aa:bb:cc:dd:ee:02"},
	}, "self", "", signals)
	if !ok || best.HomeID != "cottage" {
		t.Fatalf("expected cottage (strongest), got %+v ok=%v", best, ok)
	}
}

func TestRecommendExplicitWins(t *testing.T) {
	signals := Signals{"aa:bb:cc:dd:ee:02": -80}
	best, ok := Recommend(homes, "self", "parents", signals)
	if !ok || best.HomeID != "parents" {
		t.Fatalf("expected explicit parents, got %+v ok=%v", best, ok)
	}
}

func TestRecommendSelfIsLastResort(t *testing.T) {
	// Self has strong signal + recent activity, but an external home exists.
	best, ok := Recommend(homes, "self", "", Signals{"00:00:00:00:00:01": -30})
	if !ok || best.SelfControlled {
		t.Fatalf("expected an external home over self, got %+v", best)
	}
}
