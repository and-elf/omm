package topology

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestDnsmasqLeasesParsesFile(t *testing.T) {
	const file = "1718539200 aa:bb:cc:dd:ee:01 192.168.1.50 laptop 01:aa:bb:cc:dd:ee:01\n" +
		"1718539300 AA:BB:CC:DD:EE:02 192.168.1.51 * 01:aa:bb:cc:dd:ee:02\n" +
		"1718539400 aa:bb:cc:dd:ee:03 192.168.1.52 Phone\n" +
		"\n" + // blank line tolerated
		"garbage line\n"

	src := DnsmasqLeases{Path: "/tmp/dhcp.leases", Read: func(path string) ([]byte, error) {
		if path != "/tmp/dhcp.leases" {
			t.Fatalf("unexpected path %q", path)
		}
		return []byte(file), nil
	}}

	got := src.Leases(context.Background())
	want := map[string]Lease{
		"aa:bb:cc:dd:ee:01": {IP: "192.168.1.50", Hostname: "laptop"},
		"aa:bb:cc:dd:ee:02": {IP: "192.168.1.51", Hostname: ""}, // "*" hostname dropped; MAC lowercased
		"aa:bb:cc:dd:ee:03": {IP: "192.168.1.52", Hostname: "Phone"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("leases:\n got %#v\nwant %#v", got, want)
	}
}

func TestDnsmasqLeasesDefaultsPath(t *testing.T) {
	var seen string
	src := DnsmasqLeases{Read: func(path string) ([]byte, error) {
		seen = path
		return nil, nil
	}}
	src.Leases(context.Background())
	if seen != "/tmp/dhcp.leases" {
		t.Fatalf("default path = %q, want /tmp/dhcp.leases", seen)
	}
}

func TestDnsmasqLeasesDegradesOnReadError(t *testing.T) {
	src := DnsmasqLeases{Read: func(string) ([]byte, error) { return nil, errors.New("no such file") }}
	if got := src.Leases(context.Background()); got != nil {
		t.Fatalf("want nil on read error, got %#v", got)
	}
}

func TestLabelClientsFillsIPAndHostname(t *testing.T) {
	g := Graph{Clients: []Client{
		{MAC: "aa:bb:cc:dd:ee:01", AP: "self", Signal: -55},
		{MAC: "AA:BB:CC:DD:EE:02", AP: "self", Signal: -60}, // upper-case; must still match
		{MAC: "aa:bb:cc:dd:ee:99", AP: "self", Signal: -70}, // no lease: left untouched
	}}
	leases := map[string]Lease{
		"aa:bb:cc:dd:ee:01": {IP: "192.168.1.50", Hostname: "laptop"},
		"aa:bb:cc:dd:ee:02": {IP: "192.168.1.51"},
	}

	g = LabelClients(g, leases)

	if g.Clients[0].IP != "192.168.1.50" || g.Clients[0].Hostname != "laptop" {
		t.Errorf("client 0 = %+v", g.Clients[0])
	}
	if g.Clients[1].IP != "192.168.1.51" || g.Clients[1].Hostname != "" {
		t.Errorf("client 1 = %+v", g.Clients[1])
	}
	if g.Clients[2].IP != "" || g.Clients[2].Hostname != "" {
		t.Errorf("client 2 should be untouched, got %+v", g.Clients[2])
	}
}

func TestLabelClientsNoLeasesIsNoop(t *testing.T) {
	g := Graph{Clients: []Client{{MAC: "aa:bb:cc:dd:ee:01", AP: "self"}}}
	if got := LabelClients(g, nil); got.Clients[0].IP != "" || got.Clients[0].Hostname != "" {
		t.Fatalf("want untouched, got %+v", got.Clients[0])
	}
}
