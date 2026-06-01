package topology

import (
	"context"
	"strings"

	"github.com/and-elf/omm/internal/ubus"
)

// UbusClients reads associated stations (and their RSSI) from hostapd over
// ubus: `hostapd.<iface> get_clients`. Interfaces are configured explicitly
// (auto-discovery via `ubus list` is a future improvement).
type UbusClients struct {
	Ubus       ubus.Client
	Interfaces []string
}

// getClientsResponse mirrors the hostapd get_clients ubus reply.
type getClientsResponse struct {
	Freq    int `json:"freq"`
	Clients map[string]struct {
		Signal     int `json:"signal"`
		RxRateInfo struct {
			Rate int `json:"rate"`
		} `json:"rx_rate_info"`
		TxRateInfo struct {
			Rate int `json:"rate"`
		} `json:"tx_rate_info"`
	} `json:"clients"`
}

func (u UbusClients) Clients(ctx context.Context) ([]Client, error) {
	var out []Client
	for _, iface := range u.Interfaces {
		var resp getClientsResponse
		if err := u.Ubus.Call(ctx, "hostapd."+iface, "get_clients", nil, &resp); err != nil {
			continue // an interface may be down; skip it
		}
		band := bandFromFreq(resp.Freq)
		for mac, info := range resp.Clients {
			out = append(out, Client{
				MAC:    strings.ToLower(mac),
				Signal: info.Signal,
				Band:   band,
				TxRate: info.TxRateInfo.Rate,
				RxRate: info.RxRateInfo.Rate,
			})
		}
	}
	return out, nil
}

// SignalByMAC returns the observed signal (RSSI, dBm) of associated peers keyed
// by lower-case MAC. It reuses the same hostapd get_clients read as the
// topology collector, so mesh peers (including a Home's controller) and their
// link signal can feed home selection.
func (u UbusClients) SignalByMAC(ctx context.Context) (map[string]int, error) {
	clients, err := u.Clients(ctx)
	if err != nil {
		return nil, err
	}
	signals := make(map[string]int, len(clients))
	for _, c := range clients {
		signals[c.MAC] = c.Signal
	}
	return signals, nil
}

// bandFromFreq maps a center frequency (MHz) to a human band label.
func bandFromFreq(freq int) string {
	switch {
	case freq == 0:
		return ""
	case freq < 2500:
		return "2.4GHz"
	case freq < 5925:
		return "5GHz"
	default:
		return "6GHz"
	}
}
