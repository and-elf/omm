package profiles

import (
	"context"

	"github.com/and-elf/omm/internal/ubus"
)

// UbusBatmanInspector implements BatmanInspector against a live OpenWrt by
// reading `ubus call network.interface dump` and checking whether the batman
// soft interface (bat0) is up. It uses the dump (not a per-interface status
// call) so an absent interface — netifd never created bat0 because the
// batman-adv kernel module or netifd proto handler is missing — is reported as a
// clean (false, nil) "not up", distinct from a transport error.
type UbusBatmanInspector struct {
	Ubus ubus.Client
}

// interfaceDump mirrors the fields of `network.interface dump` this check needs;
// the response carries more that we ignore.
type interfaceDump struct {
	Interface []struct {
		Interface string `json:"interface"`
		Up        bool   `json:"up"`
	} `json:"interface"`
}

func (i UbusBatmanInspector) BatmanUp(ctx context.Context, iface string) (bool, error) {
	var dump interfaceDump
	if err := i.Ubus.Call(ctx, "network.interface", "dump", nil, &dump); err != nil {
		return false, err
	}
	for _, in := range dump.Interface {
		if in.Interface == iface {
			return in.Up, nil
		}
	}
	// The interface isn't present: netifd never instantiated bat0 (no batman-adv
	// module/proto), so the routing layer is not up — degrade to the lan bridge.
	return false, nil
}
