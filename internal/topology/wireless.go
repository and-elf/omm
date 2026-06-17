package topology

import (
	"context"
	"sort"
	"strings"

	"github.com/and-elf/omm/internal/ubus"
)

// UbusClients reads associated stations (and their RSSI) from hostapd over
// ubus: `hostapd.<iface> get_clients`. Interfaces may be set explicitly; when
// empty they are auto-discovered from `network.wireless status` (every AP-mode
// wifi-iface), so a node's clients always propagate without per-device config —
// the AP vif name is assigned by netifd and varies, so relying on a static list
// silently drops clients whenever it changes.
type UbusClients struct {
	Ubus       ubus.Client
	Interfaces []string
}

// wirelessStatus mirrors the fields of `network.wireless status` that AP
// discovery needs (each radio's interfaces, with their runtime ifname and mode).
type wirelessStatus struct {
	Interfaces []struct {
		Ifname string `json:"ifname"`
		Config struct {
			Mode string `json:"mode"`
		} `json:"config"`
	} `json:"interfaces"`
}

// apInterfaces returns the hostapd interfaces to read clients from: the explicit
// list when configured, else the AP-mode vifs discovered from the live wireless
// status. A discovery failure yields none (clients are best-effort).
func (u UbusClients) apInterfaces(ctx context.Context) []string {
	if len(u.Interfaces) > 0 {
		return u.Interfaces
	}
	var status map[string]wirelessStatus
	if err := u.Ubus.Call(ctx, "network.wireless", "status", nil, &status); err != nil {
		return nil
	}
	var ifaces []string
	for _, dev := range status {
		for _, iface := range dev.Interfaces {
			if iface.Config.Mode == "ap" && iface.Ifname != "" {
				ifaces = append(ifaces, iface.Ifname)
			}
		}
	}
	sort.Strings(ifaces) // map iteration is random; keep output deterministic
	return ifaces
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
	for _, iface := range u.apInterfaces(ctx) {
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
