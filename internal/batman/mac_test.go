package batman

import (
	"strconv"
	"testing"
)

func TestUniqueHardifMACIsLocallyAdministeredUnicast(t *testing.T) {
	got, err := uniqueHardifMAC("f8:5e:3c:a0:57:8a", "lan3")
	if err != nil {
		t.Fatalf("uniqueHardifMAC: %v", err)
	}
	first, err := parseFirstOctet(got)
	if err != nil {
		t.Fatalf("parse %q: %v", got, err)
	}
	if first&0x02 == 0 {
		t.Errorf("MAC %q is not locally-administered (LA bit unset)", got)
	}
	if first&0x01 != 0 {
		t.Errorf("MAC %q is multicast (must be unicast)", got)
	}
}

func TestUniqueHardifMACDiffersFromOriginal(t *testing.T) {
	// The mesh vif keeps the real MAC; the enslaved wired port must NOT collide
	// with it (the exact ZB lan3/phy0-mesh0 clash that gave batman a TQ0 link).
	real := "f8:5e:3c:a0:57:8a"
	got, _ := uniqueHardifMAC(real, "lan3")
	if got == real {
		t.Errorf("derived MAC equals the real MAC %q; would still collide with the mesh vif", real)
	}
}

func TestUniqueHardifMACDistinctPerPortOnSharedBase(t *testing.T) {
	// The ZB's DSA ports share one base MAC; enslaving several must yield distinct
	// MACs so they don't collide with EACH OTHER on bat0.
	base := "f8:5e:3c:a0:57:8a"
	seen := map[string]string{}
	for _, port := range []string{"lan1", "lan2", "lan3", "wan"} {
		m, err := uniqueHardifMAC(base, port)
		if err != nil {
			t.Fatalf("port %s: %v", port, err)
		}
		if other, dup := seen[m]; dup {
			t.Errorf("ports %s and %s derived the same MAC %q", port, other, m)
		}
		seen[m] = port
	}
}

func TestUniqueHardifMACIsDeterministic(t *testing.T) {
	// Must be stable across reboots (same inputs => same MAC), so the batman
	// identity doesn't churn.
	a, _ := uniqueHardifMAC("10:7b:44:d5:d3:33", "wan")
	b, _ := uniqueHardifMAC("10:7b:44:d5:d3:33", "wan")
	if a != b {
		t.Errorf("non-deterministic: %q != %q", a, b)
	}
}

func TestUniqueHardifMACPreservesNodeUniqueness(t *testing.T) {
	// Two different nodes enslaving a same-named port ("wan") must NOT collide —
	// node-uniqueness comes from the real MAC and must survive the transform.
	x, _ := uniqueHardifMAC("10:7b:44:d5:d3:33", "wan")
	y, _ := uniqueHardifMAC("10:7b:44:aa:bb:cc", "wan")
	if x == y {
		t.Errorf("distinct node MACs collided after transform: both %q", x)
	}
}

func TestUniqueHardifMACRejectsBadInput(t *testing.T) {
	if _, err := uniqueHardifMAC("not-a-mac", "wan"); err == nil {
		t.Error("expected error for malformed MAC")
	}
}

// parseFirstOctet is a tiny test helper to read the first octet of a MAC string.
func parseFirstOctet(mac string) (byte, error) {
	v, err := strconv.ParseUint(mac[0:2], 16, 8)
	if err != nil {
		return 0, err
	}
	return byte(v), nil
}
