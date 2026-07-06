package topology

import (
	"context"
	"os"
	"strings"
)

// Lease is a client's DHCP-assigned identity: its IP and, when the client
// offered one, its hostname. Either field may be empty.
type Lease struct {
	IP       string
	Hostname string
}

// LeaseSource resolves associated-client MACs to their DHCP leases (IP +
// hostname). The home's authoritative DHCP runs on the controller, so leases
// are looked up where the merged graph is served — a member node is a bridged
// dumb AP and holds no leases of its own.
type LeaseSource interface {
	// Leases returns the current leases keyed by lower-case client MAC. It
	// degrades to a nil/empty map when the lease store is unavailable.
	Leases(ctx context.Context) map[string]Lease
}

// DnsmasqLeases reads dnsmasq's lease file (default /tmp/dhcp.leases on
// OpenWrt). Each line is:
//
//	<expiry-epoch> <mac> <ip> <hostname> <client-id>
//
// A hostname of "*" means the client offered none and is dropped. Short or
// malformed lines are skipped, so a partially-written file still yields the
// good leases. It degrades to nil when the file is unavailable.
type DnsmasqLeases struct {
	Path string     // default /tmp/dhcp.leases
	Read fileReader // nil => os.ReadFile
}

func (d DnsmasqLeases) Leases(_ context.Context) map[string]Lease {
	path := d.Path
	if path == "" {
		path = "/tmp/dhcp.leases"
	}
	read := d.Read
	if read == nil {
		read = os.ReadFile
	}
	b, err := read(path)
	if err != nil {
		return nil
	}
	out := map[string]Lease{}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue // need at least expiry, mac, ip, hostname
		}
		mac := strings.ToLower(fields[1])
		if mac == "" {
			continue
		}
		hostname := fields[3]
		if hostname == "*" {
			hostname = "" // dnsmasq marks "no hostname offered" with a bare *
		}
		out[mac] = Lease{IP: fields[2], Hostname: hostname}
	}
	return out
}

// LabelClients enriches a graph's clients with their DHCP-assigned IP and
// hostname from leases (keyed by lower-case MAC), so the topology view can show
// a recognizable name instead of a raw MAC. A client without a lease, or an
// already-populated field, is left untouched. It mutates and returns g.
func LabelClients(g Graph, leases map[string]Lease) Graph {
	if len(leases) == 0 {
		return g
	}
	for i := range g.Clients {
		l, ok := leases[strings.ToLower(g.Clients[i].MAC)]
		if !ok {
			continue
		}
		if g.Clients[i].IP == "" {
			g.Clients[i].IP = l.IP
		}
		if g.Clients[i].Hostname == "" {
			g.Clients[i].Hostname = l.Hostname
		}
	}
	return g
}
