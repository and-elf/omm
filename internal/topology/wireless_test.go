package topology

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// fakeWifiUbus dispatches on (object, method): it answers network.wireless
// status with a canned topology and hostapd.<iface> get_clients per object, and
// records every call so a test can assert which interfaces were queried.
type fakeWifiUbus struct {
	statusJSON  string
	clientsJSON map[string]string // ubus object -> get_clients JSON
	calls       []string
}

func (f *fakeWifiUbus) Call(_ context.Context, object, method string, _, result interface{}) error {
	f.calls = append(f.calls, object+"."+method)
	switch {
	case object == "network.wireless" && method == "status":
		if f.statusJSON == "" {
			return errors.New("no status")
		}
		return json.Unmarshal([]byte(f.statusJSON), result)
	case method == "get_clients":
		j, ok := f.clientsJSON[object]
		if !ok {
			return errors.New("no such hostapd object")
		}
		return json.Unmarshal([]byte(j), result)
	}
	return errors.New("unexpected call " + object + "." + method)
}

func (f *fakeWifiUbus) Close() error { return nil }

func (f *fakeWifiUbus) called(c string) bool {
	for _, x := range f.calls {
		if x == c {
			return true
		}
	}
	return false
}

// With no explicit Interfaces, clients are discovered from the live wireless
// status: every AP-mode vif is queried, the mesh vif is not.
func TestUbusClientsAutoDiscoversAPInterfaces(t *testing.T) {
	fake := &fakeWifiUbus{
		statusJSON: `{"radio0":{"interfaces":[
			{"ifname":"phy0-ap0","config":{"mode":"ap"}},
			{"ifname":"phy0-mesh0","config":{"mode":"mesh"}}
		]}}`,
		clientsJSON: map[string]string{
			"hostapd.phy0-ap0": `{"freq":5180,"clients":{"AA:BB:CC:DD:EE:FF":{"signal":-55}}}`,
		},
	}

	clients, err := (UbusClients{Ubus: fake}).Clients(context.Background())
	if err != nil {
		t.Fatalf("Clients: %v", err)
	}
	if len(clients) != 1 || clients[0].MAC != "aa:bb:cc:dd:ee:ff" || clients[0].Signal != -55 || clients[0].Band != "5GHz" {
		t.Fatalf("expected one discovered 5GHz client, got %+v", clients)
	}
	if !fake.called("hostapd.phy0-ap0.get_clients") {
		t.Fatalf("AP vif should be queried; calls=%v", fake.calls)
	}
	if fake.called("hostapd.phy0-mesh0.get_clients") {
		t.Fatalf("mesh vif must not be queried for clients; calls=%v", fake.calls)
	}
}

// An explicit Interfaces list is authoritative and skips discovery entirely.
func TestUbusClientsExplicitInterfacesSkipDiscovery(t *testing.T) {
	fake := &fakeWifiUbus{
		clientsJSON: map[string]string{
			"hostapd.ap-fixed": `{"freq":2412,"clients":{"11:22:33:44:55:66":{"signal":-40}}}`,
		},
	}

	clients, err := (UbusClients{Ubus: fake, Interfaces: []string{"ap-fixed"}}).Clients(context.Background())
	if err != nil {
		t.Fatalf("Clients: %v", err)
	}
	if len(clients) != 1 || clients[0].MAC != "11:22:33:44:55:66" || clients[0].Band != "2.4GHz" {
		t.Fatalf("expected the configured AP's client, got %+v", clients)
	}
	if fake.called("network.wireless.status") {
		t.Fatalf("explicit interfaces must not trigger discovery; calls=%v", fake.calls)
	}
}

// A node serving no AP (e.g. discovery returns nothing) yields no clients and no
// error, so the collector degrades cleanly.
func TestUbusClientsNoAPInterfaces(t *testing.T) {
	fake := &fakeWifiUbus{statusJSON: `{"radio0":{"interfaces":[{"ifname":"phy0-mesh0","config":{"mode":"mesh"}}]}}`}

	clients, err := (UbusClients{Ubus: fake}).Clients(context.Background())
	if err != nil {
		t.Fatalf("Clients: %v", err)
	}
	if len(clients) != 0 {
		t.Fatalf("expected no clients, got %+v", clients)
	}
}
