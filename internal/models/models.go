package models

type Home struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Controller  string `json:"controller"`
	BSSID       string `json:"bssid"`
	Certificate []byte `json:"certificate"`
	LastSeen    int64  `json:"last_seen"`
}

type Node struct {
	ID           string   `json:"id"`
	Serial       string   `json:"serial"`
	CurrentHome  string   `json:"current_home"`
	TrustedHomes []string `json:"trusted_homes"`
	LastSeen     int64    `json:"last_seen"`
}

type Profile struct {
	HomeID   string `json:"home_id"`
	NodeName string `json:"node_name"`
	MeshSSID string `json:"mesh_ssid"`
	MeshKey  string `json:"mesh_key"`
	// APSSID/APKey configure the client-facing access point a claimed home
	// broadcasts (so phones/laptops can join and get an address). When empty
	// they fall back to MeshSSID/MeshKey, so a home configured with only mesh
	// settings still comes up with a usable AP.
	APSSID string `json:"ap_ssid"`
	APKey  string `json:"ap_key"`
	// Band selects the radio by frequency ("2g", "5g" or "6g"); meshd resolves
	// it to the matching OpenWrt wifi-device. This is the friendly knob, since
	// radio names are device-specific. Radio is an advanced override naming the
	// wifi-device directly (e.g. "radio0"). Precedence: Radio, then Band, then
	// the daemon default.
	Band  string   `json:"band"`
	Radio string   `json:"radio"`
	VLANs []string `json:"vlans"`
	// MeshChannel/MeshHTMode pin the 802.11s backhaul's channel and width
	// home-wide, so every node's mesh lands on the same channel and lines up to
	// peer. Empty leaves the mesh radio's existing channel/width untouched. The
	// channel must be one the mesh radio supports (e.g. a 5 GHz-high channel for
	// a dedicated backhaul radio); pair with meshd's per-node mesh_radio setting.
	MeshChannel string `json:"mesh_channel"`
	MeshHTMode  string `json:"mesh_htmode"`
}

// Backhaul mode: the wireless-backhaul technology actually in effect on a node
// after a profile is applied. Distinct from topology's ethernet/wireless
// "backhaul" (the physical uplink); this is whether the 802.11s mesh formed or
// the node degraded to a wired multi-AP. See doc/network-model.md.
const (
	BackhaulMode80211s  = "802.11s"  // omm_mesh came up: true wireless mesh
	BackhaulModeMultiAP = "multi_ap" // AP only (mesh unavailable, or none configured)
	BackhaulModeUnknown = "unknown"  // no profile applied yet
)

// BackhaulState is the applied wireless-backhaul outcome for a node. When the
// node was configured for 802.11s but the mesh could not start (no mesh-capable
// wpad), Mode is multi_ap and Reason/Remediation explain the degrade so the UI
// can tell the operator what happened and how to fix it.
type BackhaulState struct {
	Mode        string `json:"mode"`
	Reason      string `json:"reason,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

// EnrollmentStatus tracks a node through the enrollment flow.
type EnrollmentStatus string

const (
	EnrollmentPendingVerification EnrollmentStatus = "pending_verification"
	EnrollmentPendingApproval     EnrollmentStatus = "pending_approval"
	EnrollmentApproved            EnrollmentStatus = "approved"
	EnrollmentActive              EnrollmentStatus = "active"
	EnrollmentRejected            EnrollmentStatus = "rejected"
)

// Enrollment is the controller-side record of a node enrolling into a Home.
type Enrollment struct {
	ID        string           `json:"id"`
	NodeID    string           `json:"node_id"`
	Serial    string           `json:"serial"`
	PublicKey []byte           `json:"public_key"`
	Challenge []byte           `json:"challenge"`
	Status    EnrollmentStatus `json:"status"`
	HomeID    string           `json:"home_id"`
	CreatedAt int64            `json:"created_at"`
}
