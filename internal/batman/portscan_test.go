package batman

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestEthernetPortsFiltersBatAndWifi(t *testing.T) {
	got := EthernetPorts([]string{"lan", "wan", "bat0", "phy0-ap0", "phy1-mesh0"}, "bat0")
	want := []string{"lan", "wan"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("EthernetPorts = %v, want %v (bat0 + phy* dropped)", got, want)
	}
}

func TestEthernetPortsDSALayout(t *testing.T) {
	got := EthernetPorts([]string{"lan1", "lan2", "lan3", "bat0"}, "bat0")
	want := []string{"lan1", "lan2", "lan3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("EthernetPorts = %v, want %v", got, want)
	}
}

func TestSysfsBridgePortsSortedFromBrif(t *testing.T) {
	got, err := SysfsBridgePorts("br-lan", func(path string) ([]string, error) {
		if path != "/sys/class/net/br-lan/brif" {
			t.Fatalf("unexpected path %q", path)
		}
		return []string{"wan", "bat0", "lan"}, nil
	})
	if err != nil {
		t.Fatalf("SysfsBridgePorts: %v", err)
	}
	want := []string{"bat0", "lan", "wan"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ports = %v, want %v (sorted)", got, want)
	}
}

func TestPortScanEnslavesOnlyPortsWithPeer(t *testing.T) {
	// lan3 + wan face OMM peers; lan1 is a plain client jack. Only the peered
	// ports become batman backhaul; the client jack stays out (plain bridge).
	scan := PortScan{
		Candidates: []string{"lan1", "lan3", "wan"},
		HasPeer: func(ctx context.Context, port string) (bool, error) {
			return port == "lan3" || port == "wan", nil
		},
	}
	got := scan.BackhaulPorts(context.Background())
	want := []string{"lan3", "wan"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BackhaulPorts = %v, want %v", got, want)
	}
}

func TestPortScanProbeErrorIsNoPeer(t *testing.T) {
	// A probe error must NOT enslave the port — keep it a plain bridge member
	// (safe: a wrongly-enslaved client port loses its LAN).
	scan := PortScan{
		Candidates: []string{"lan", "wan"},
		HasPeer: func(ctx context.Context, port string) (bool, error) {
			if port == "wan" {
				return false, errors.New("probe failed")
			}
			return true, nil
		},
	}
	got := scan.BackhaulPorts(context.Background())
	want := []string{"lan"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BackhaulPorts = %v, want %v (errored port not enslaved)", got, want)
	}
}

func TestPortScanNoPeersIsEmpty(t *testing.T) {
	scan := PortScan{
		Candidates: []string{"lan", "wan"},
		HasPeer:    func(ctx context.Context, port string) (bool, error) { return false, nil },
	}
	if got := scan.BackhaulPorts(context.Background()); len(got) != 0 {
		t.Errorf("BackhaulPorts = %v, want empty (no peers => all plain client ports)", got)
	}
}
