package topology

import (
	"context"
	"errors"
	"testing"
)

func TestBatctlSelfAddrsCollectsEveryHardifMAC(t *testing.T) {
	// batman-adv lists a node's neighbours by the MAC of the hard interface the
	// OGMs arrive on. A node with a wired backhaul has several hardifs — the mesh
	// vif and each enslaved ethernet port, the latter carrying a unique
	// locally-administered MAC — so it appears under several originator MACs. Self
	// must report all of them (plus bat0's own) or the wired-port originator
	// survives reconcile as a phantom node (the "ethernet port = separate node"
	// bug).
	macs := map[string]string{
		"/sys/class/net/bat0/address":       "de:ad:be:ef:00:01\n",
		"/sys/class/net/phy0-mesh0/address": "de:ad:be:ef:00:01\n", // mesh vif == bat0 MAC
		"/sys/class/net/eth1/address":       "02:AD:BE:EF:00:99\n", // wired port, unique MAC
	}
	s := BatctlSelfAddrs{
		Iface: "bat0",
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "batctl" {
				t.Fatalf("unexpected command %q", name)
			}
			return []byte("phy0-mesh0: active\neth1: active\n"), nil
		},
		Read: func(path string) ([]byte, error) {
			if v, ok := macs[path]; ok {
				return []byte(v), nil
			}
			return nil, errors.New("no such file")
		},
	}

	got := s.SelfAddrs(context.Background())
	want := map[string]bool{"de:ad:be:ef:00:01": true, "02:ad:be:ef:00:99": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want the deduped lowercased union of bat0+hardif MACs %v", got, want)
	}
	for _, a := range got {
		if !want[a] {
			t.Errorf("unexpected address %q in %v", a, got)
		}
	}
}

func TestBatctlSelfAddrsDegradesWhenBatctlMissing(t *testing.T) {
	// With no batctl, fall back to just bat0's own address so the node still
	// reconciles its wireless originator (the common single-link case).
	s := BatctlSelfAddrs{
		Iface: "bat0",
		Run:   func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("not found") },
		Read: func(path string) ([]byte, error) {
			if path == "/sys/class/net/bat0/address" {
				return []byte("de:ad:be:ef:00:01\n"), nil
			}
			return nil, errors.New("no such file")
		},
	}
	got := s.SelfAddrs(context.Background())
	if len(got) != 1 || got[0] != "de:ad:be:ef:00:01" {
		t.Fatalf("got %v, want [de:ad:be:ef:00:01]", got)
	}
}

func TestBatctlSelfAddrsNoIface(t *testing.T) {
	if got := (BatctlSelfAddrs{}).SelfAddrs(context.Background()); got != nil {
		t.Fatalf("got %v, want nil with no interface", got)
	}
}
