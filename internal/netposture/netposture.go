// Package netposture decides and (in later increments) applies a node's
// network/DHCP/firewall posture from its lifecycle state. The decision logic is
// kept here, pure and unit-testable, separate from the UCI authoring and the
// daemon wiring. See doc/network-posture.md.
package netposture

// Role is a node's network posture.
type Role string

const (
	// RoleGuest is the unclaimed dumb-AP posture: the wired uplink is bridged
	// into the LAN, the node is a DHCP client, and its own authoritative DHCP is
	// stood down — so it is L2-adjacent to a controller and can hear discovery
	// broadcasts instead of dropping them at a routed, firewalled WAN.
	RoleGuest Role = "guest"
	// RoleController is the gateway posture: routed WAN uplink, authoritative LAN
	// DHCP, locked WAN firewall (the stock OpenWrt router default).
	RoleController Role = "controller"
	// RoleMeshNode is a claimed satellite that joined another home: bridged into
	// the home with the controller serving DHCP.
	RoleMeshNode Role = "node"
)

// DecideRole maps the auto-determined role (which home this node settled on,
// from the boot scan + home selection) to the network posture it should hold:
//
//   - no active home yet -> Guest. This is the transient discovery posture that
//     lets the device scan at all; every device — including a future gateway —
//     passes through it until selection decides a role.
//   - active home is this node's own Home -> Controller (gateway: serves DHCP,
//     WAN stays locked so LuCI/SSH are not exposed).
//   - active home is another Home -> Mesh node.
//
// activeHome is persisted, so an established controller or node boots straight
// into its settled posture and never reverts to Guest — only an undecided
// device discovers. No static role config: the scan decides.
func DecideRole(activeHome, selfHomeID string) Role {
	switch {
	case activeHome == "":
		return RoleGuest
	case activeHome == selfHomeID:
		return RoleController
	default:
		return RoleMeshNode
	}
}
