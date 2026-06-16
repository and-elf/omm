package profiles

import (
	"context"

	"github.com/and-elf/omm/internal/ubus"
)

// UbusBatmanInspector implements BatmanInspector against a live OpenWrt by
// asking netifd whether the batman soft interface device (bat0) exists:
// `ubus call network.device status {"name":"bat0"}`. The bat0 netdev is
// materialized by the kernel only once the batman-adv module is loaded and a
// hard interface with a real device is enslaved to it, so its presence is the
// signal that the routing layer actually came up — distinct from a `proto
// batadv` interface that is merely configured. An absent device (rpcd returns
// an error) is reported as a clean (false, nil) "not up" so ApplyProfile
// degrades to the direct lan bridge rather than stranding the mesh on a bat0
// that never instantiated.
type UbusBatmanInspector struct {
	Ubus ubus.Client
}

// deviceStatus mirrors the fields of `network.device status` this check needs.
type deviceStatus struct {
	Up      bool `json:"up"`
	Present bool `json:"present"`
}

func (i UbusBatmanInspector) BatmanUp(ctx context.Context, iface string) (bool, error) {
	var st deviceStatus
	if err := i.Ubus.Call(ctx, "network.device", "status", map[string]string{"name": iface}, &st); err != nil {
		// netifd has no such device (the common "batman not up" case: module or
		// proto absent, or no hardif attached). Treat any failure to confirm the
		// device as not-up; the apply-time poll retries, so a transient blip
		// self-corrects.
		return false, nil
	}
	return st.Up || st.Present, nil
}
