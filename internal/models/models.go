package models

type Home struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Controller  string `json:"controller"`
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
