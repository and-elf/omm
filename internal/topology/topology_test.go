package topology

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/and-elf/omm/internal/ubus"
)

type fakeMesh struct {
	neighbors []Neighbor
	err       error
}

func (f fakeMesh) Neighbors(context.Context) ([]Neighbor, error) { return f.neighbors, f.err }

type fakeClients struct {
	clients []Client
	err     error
}

func (f fakeClients) Clients(context.Context) ([]Client, error) { return f.clients, f.err }

func TestCollectAssemblesGraph(t *testing.T) {
	c := NewCollector("self-1", "Gateway",
		fakeMesh{neighbors: []Neighbor{{ID: "aa:bb", TQ: 240}, {ID: "cc:dd", TQ: 180}}},
		fakeClients{clients: []Client{{MAC: "11:22", Signal: -55, Band: "5GHz"}}},
	)
	g := c.Collect(context.Background())

	if len(g.Nodes) != 3 { // self + 2 neighbors
		t.Fatalf("expected 3 nodes, got %d: %+v", len(g.Nodes), g.Nodes)
	}
	if g.Nodes[0].Role != "self" || g.Nodes[0].Label != "Gateway" {
		t.Fatalf("expected self node first, got %+v", g.Nodes[0])
	}
	if len(g.Links) != 2 || g.Links[0].Source != "self-1" || g.Links[0].TQ != 240 {
		t.Fatalf("unexpected links: %+v", g.Links)
	}
	if len(g.Clients) != 1 || g.Clients[0].AP != "self-1" || g.Clients[0].Signal != -55 {
		t.Fatalf("expected client attached to self with rssi, got %+v", g.Clients)
	}
}

func TestCollectToleratesSourceErrors(t *testing.T) {
	c := NewCollector("self-1", "", fakeMesh{err: context.DeadlineExceeded}, fakeClients{err: context.DeadlineExceeded})
	g := c.Collect(context.Background())
	if len(g.Nodes) != 1 || len(g.Links) != 0 || len(g.Clients) != 0 {
		t.Fatalf("expected only self node on source errors, got %+v", g)
	}
}

func TestParseOriginators(t *testing.T) {
	// Representative `batctl o` output.
	out := `[B.A.T.M.A.N. adv 2023.1, MainIF/MAC: eth0/de:ad:be:ef:00:01 (bat0 BATMAN_IV)]
   Originator        last-seen (#/255)  Nexthop           [outgoingIF]
 * aa:bb:cc:dd:ee:01    0.480s   (255) aa:bb:cc:dd:ee:01 [     bat0]
   aa:bb:cc:dd:ee:02    1.020s   (181) aa:bb:cc:dd:ee:02 [     bat0]
 * aa:bb:cc:dd:ee:02    0.500s   (200) aa:bb:cc:dd:ee:02 [     bat0]
`
	neighbors := parseOriginators([]byte(out))
	if len(neighbors) != 2 {
		t.Fatalf("expected 2 originators, got %d: %+v", len(neighbors), neighbors)
	}
	if neighbors[0].ID != "aa:bb:cc:dd:ee:01" || neighbors[0].TQ != 255 {
		t.Fatalf("unexpected first originator: %+v", neighbors[0])
	}
	// Best (highest) TQ kept for the duplicated originator.
	if neighbors[1].ID != "aa:bb:cc:dd:ee:02" || neighbors[1].TQ != 200 {
		t.Fatalf("expected best TQ 200 for ee:02, got %+v", neighbors[1])
	}
}

// fakeUbus returns a canned get_clients reply.
type fakeUbus struct{ payload string }

func (f fakeUbus) Call(_ context.Context, object, method string, _, result interface{}) error {
	return json.Unmarshal([]byte(f.payload), result)
}
func (fakeUbus) Close() error { return nil }

var _ ubus.Client = fakeUbus{}

func TestUbusClientsParsesRSSI(t *testing.T) {
	src := UbusClients{
		Ubus:       fakeUbus{payload: `{"freq":5180,"clients":{"AA:BB:CC:DD:EE:FF":{"signal":-48,"rx_rate_info":{"rate":866},"tx_rate_info":{"rate":780}}}}`},
		Interfaces: []string{"wlan0"},
	}
	clients, err := src.Clients(context.Background())
	if err != nil {
		t.Fatalf("clients: %v", err)
	}
	if len(clients) != 1 {
		t.Fatalf("expected 1 client, got %d", len(clients))
	}
	c := clients[0]
	if c.MAC != "aa:bb:cc:dd:ee:ff" || c.Signal != -48 || c.Band != "5GHz" || c.TxRate != 780 || c.RxRate != 866 {
		t.Fatalf("unexpected client: %+v", c)
	}
}

func TestSignalByMAC(t *testing.T) {
	src := UbusClients{
		Ubus:       fakeUbus{payload: `{"freq":2412,"clients":{"AA:BB:CC:DD:EE:FF":{"signal":-61}}}`},
		Interfaces: []string{"wlan0"},
	}
	signals, err := src.SignalByMAC(context.Background())
	if err != nil {
		t.Fatalf("signal by mac: %v", err)
	}
	if signals["aa:bb:cc:dd:ee:ff"] != -61 {
		t.Fatalf("expected -61 for peer, got %+v", signals)
	}
}
