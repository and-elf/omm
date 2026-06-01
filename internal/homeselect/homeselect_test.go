package homeselect

import "testing"

func TestPrefersLastActiveAmongExternal(t *testing.T) {
	best, ok := Select([]Candidate{
		{HomeID: "a", Signal: -40, LastActive: 100},
		{HomeID: "b", Signal: -80, LastActive: 200}, // older signal but more recently active
	})
	if !ok || best.HomeID != "b" {
		t.Fatalf("expected last-active home b, got %+v ok=%v", best, ok)
	}
}

func TestFallsBackToStrongestSignalWhenNoHistory(t *testing.T) {
	best, ok := Select([]Candidate{
		{HomeID: "a", Signal: -75},
		{HomeID: "b", Signal: -55}, // stronger
		{HomeID: "c", Signal: -90},
	})
	if !ok || best.HomeID != "b" {
		t.Fatalf("expected strongest-signal home b, got %+v ok=%v", best, ok)
	}
}

func TestSelfControlledIsLastResort(t *testing.T) {
	// Even though the self-controlled home was most recently active and has the
	// strongest signal, an external home is preferred.
	best, ok := Select([]Candidate{
		{HomeID: "self", SelfControlled: true, Signal: -30, LastActive: 999},
		{HomeID: "ext", Signal: -85, LastActive: 0},
	})
	if !ok || best.HomeID != "ext" {
		t.Fatalf("expected external home ext, got %+v ok=%v", best, ok)
	}
}

func TestSelfControlledChosenWhenOnlyOption(t *testing.T) {
	best, ok := Select([]Candidate{
		{HomeID: "self", SelfControlled: true, Signal: -50, LastActive: 10},
	})
	if !ok || best.HomeID != "self" {
		t.Fatalf("expected self home, got %+v ok=%v", best, ok)
	}
}

func TestUnknownSignalRanksBelowMeasured(t *testing.T) {
	best, ok := Select([]Candidate{
		{HomeID: "unknown", Signal: 0}, // 0 = not measured
		{HomeID: "weak", Signal: -95},
	})
	if !ok || best.HomeID != "weak" {
		t.Fatalf("expected measured weak signal over unknown, got %+v ok=%v", best, ok)
	}
}

func TestNoCandidates(t *testing.T) {
	if _, ok := Select(nil); ok {
		t.Fatal("expected ok=false for no candidates")
	}
}

func TestChooseHonorsExplicitSelection(t *testing.T) {
	candidates := []Candidate{
		{HomeID: "ext", Signal: -50, LastActive: 500},
		{HomeID: "self", SelfControlled: true, Signal: -40},
	}
	// Explicit user selection wins even though the policy would pick "ext".
	best, ok := Choose("self", candidates)
	if !ok || best.HomeID != "self" {
		t.Fatalf("expected explicit self, got %+v ok=%v", best, ok)
	}
}

func TestChooseFallsBackWhenExplicitUnavailable(t *testing.T) {
	candidates := []Candidate{{HomeID: "ext", Signal: -50}}
	best, ok := Choose("gone", candidates) // explicit no longer reachable
	if !ok || best.HomeID != "ext" {
		t.Fatalf("expected fallback to ext, got %+v ok=%v", best, ok)
	}
}
