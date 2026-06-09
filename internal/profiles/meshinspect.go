package profiles

import (
	"context"

	"github.com/and-elf/omm/internal/ubus"
)

// UbusMeshInspector implements MeshInspector against a live OpenWrt by reading
// `ubus call network.wireless status`. A mesh wifi-iface is considered up when
// its radio is up, the radio's setup did not fail, and netifd assigned the
// section a runtime ifname — the signal that wpad actually instantiated the
// 802.11s interface (it fails to when the node lacks a mesh-capable wpad).
type UbusMeshInspector struct {
	Ubus ubus.Client
}

// wirelessDevice / wirelessIface mirror the fields of network.wireless status
// this check needs; the response carries more that we ignore.
type wirelessDevice struct {
	Up               bool            `json:"up"`
	RetrySetupFailed bool            `json:"retry_setup_failed"`
	Interfaces       []wirelessIface `json:"interfaces"`
}

type wirelessIface struct {
	Section string `json:"section"`
	Ifname  string `json:"ifname"`
}

func (i UbusMeshInspector) MeshUp(ctx context.Context, section string) (bool, error) {
	var status map[string]wirelessDevice
	if err := i.Ubus.Call(ctx, "network.wireless", "status", nil, &status); err != nil {
		return false, err
	}
	for _, dev := range status {
		for _, iface := range dev.Interfaces {
			if iface.Section == section {
				return dev.Up && !dev.RetrySetupFailed && iface.Ifname != "", nil
			}
		}
	}
	// The section isn't present in any radio's interface list: netifd never
	// instantiated it, so the mesh is not up.
	return false, nil
}
