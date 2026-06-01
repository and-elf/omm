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
	HomeID   string   `json:"home_id"`
	NodeName string   `json:"node_name"`
	MeshSSID string   `json:"mesh_ssid"`
	MeshKey  string   `json:"mesh_key"`
	VLANs    []string `json:"vlans"`
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
