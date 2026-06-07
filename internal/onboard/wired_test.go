package onboard

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/and-elf/omm/internal/discovery"
	"github.com/and-elf/omm/internal/topology"
)

func ann(homeID, api string) discovery.Announcement {
	return discovery.Announcement{HomeID: homeID, API: api}
}

func TestDecide(t *testing.T) {
	selfHome := "my-home"
	others := []discovery.Announcement{ann("home-b", "https://b"), ann("home-a", "https://a")}

	tests := []struct {
		name        string
		complete    bool
		activeHome  string
		backhaul    string
		controllers []discovery.Announcement
		wantURL     string
		wantAct     bool
	}{
		{
			name:        "unclaimed wired with controllers => enroll deterministically",
			backhaul:    topology.BackhaulEthernet,
			controllers: others,
			wantURL:     "https://a", // lowest HomeID wins, stable across ticks
			wantAct:     true,
		},
		{
			name:        "already claimed => no act",
			complete:    true,
			backhaul:    topology.BackhaulEthernet,
			controllers: others,
			wantAct:     false,
		},
		{
			name:        "active home already set => no act",
			activeHome:  "home-a",
			backhaul:    topology.BackhaulEthernet,
			controllers: others,
			wantAct:     false,
		},
		{
			name:        "wireless backhaul => no act",
			backhaul:    topology.BackhaulWireless,
			controllers: others,
			wantAct:     false,
		},
		{
			name:        "unknown backhaul => no act",
			backhaul:    topology.BackhaulUnknown,
			controllers: others,
			wantAct:     false,
		},
		{
			name:        "no controllers => no act",
			backhaul:    topology.BackhaulEthernet,
			controllers: nil,
			wantAct:     false,
		},
		{
			name:        "only own home discovered => no act",
			backhaul:    topology.BackhaulEthernet,
			controllers: []discovery.Announcement{ann(selfHome, "https://self")},
			wantAct:     false,
		},
		{
			name:        "controller without API skipped",
			backhaul:    topology.BackhaulEthernet,
			controllers: []discovery.Announcement{ann("home-z", ""), ann("home-y", "https://y")},
			wantURL:     "https://y",
			wantAct:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, act := Decide(tt.complete, tt.activeHome, tt.backhaul, tt.controllers, selfHome)
			if act != tt.wantAct {
				t.Fatalf("act = %v, want %v", act, tt.wantAct)
			}
			if act && url != tt.wantURL {
				t.Fatalf("url = %q, want %q", url, tt.wantURL)
			}
		})
	}
}

func TestRunStopsWhenAlreadyComplete(t *testing.T) {
	joins := 0
	d := Deps{
		Interval:      time.Millisecond,
		SelfHomeID:    "self",
		SetupComplete: func(context.Context) (bool, error) { return true, nil },
		ActiveHome:    func(context.Context) (string, error) { return "", nil },
		Backhaul:      func(context.Context) string { return topology.BackhaulEthernet },
		Discover:      func() []discovery.Announcement { return []discovery.Announcement{ann("home-a", "https://a")} },
		Join:          func(context.Context, string) error { joins++; return nil },
	}
	Run(context.Background(), d)
	if joins != 0 {
		t.Fatalf("expected no join attempts for a claimed node, got %d", joins)
	}
}

func TestRunJoinsThenStops(t *testing.T) {
	var gotURL string
	joins := 0
	d := Deps{
		Interval:      time.Millisecond,
		SelfHomeID:    "self",
		SetupComplete: func(context.Context) (bool, error) { return false, nil },
		ActiveHome:    func(context.Context) (string, error) { return "", nil },
		Backhaul:      func(context.Context) string { return topology.BackhaulEthernet },
		Discover: func() []discovery.Announcement {
			return []discovery.Announcement{ann("home-b", "https://b"), ann("home-a", "https://a")}
		},
		Join: func(_ context.Context, url string) error { joins++; gotURL = url; return nil },
	}
	Run(context.Background(), d)
	if joins != 1 {
		t.Fatalf("expected exactly one successful join, got %d", joins)
	}
	if gotURL != "https://a" {
		t.Fatalf("expected deterministic pick https://a, got %q", gotURL)
	}
}

func TestRunRetriesAfterJoinFailure(t *testing.T) {
	joins := 0
	ctx, cancel := context.WithCancel(context.Background())
	d := Deps{
		Interval:      time.Millisecond,
		SelfHomeID:    "self",
		SetupComplete: func(context.Context) (bool, error) { return false, nil },
		ActiveHome:    func(context.Context) (string, error) { return "", nil },
		Backhaul:      func(context.Context) string { return topology.BackhaulEthernet },
		Discover:      func() []discovery.Announcement { return []discovery.Announcement{ann("home-a", "https://a")} },
		Join: func(context.Context, string) error {
			joins++
			if joins >= 3 {
				cancel() // give up the test after a few retries
			}
			return errors.New("unreachable")
		},
	}
	Run(ctx, d)
	if joins < 3 {
		t.Fatalf("expected the loop to retry after join failures, got %d attempts", joins)
	}
}

func TestRunSkipsWhenNotWired(t *testing.T) {
	joins, ticks := 0, 0
	ctx, cancel := context.WithCancel(context.Background())
	d := Deps{
		Interval:      time.Millisecond,
		SelfHomeID:    "self",
		SetupComplete: func(context.Context) (bool, error) { return false, nil },
		ActiveHome:    func(context.Context) (string, error) { return "", nil },
		Backhaul:      func(context.Context) string { return topology.BackhaulWireless },
		Discover: func() []discovery.Announcement {
			ticks++
			if ticks >= 3 {
				cancel()
			}
			return []discovery.Announcement{ann("home-a", "https://a")}
		},
		Join: func(context.Context, string) error { joins++; return nil },
	}
	Run(ctx, d)
	if joins != 0 {
		t.Fatalf("expected no join for a wireless node, got %d", joins)
	}
}
