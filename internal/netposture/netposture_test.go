package netposture

import "testing"

func TestDecideRole(t *testing.T) {
	tests := []struct {
		name       string
		activeHome string
		selfHomeID string
		want       Role
	}{
		{
			name:       "no active home yet -> guest (still discovering, scan-capable)",
			activeHome: "",
			selfHomeID: "home-a",
			want:       RoleGuest,
		},
		{
			name:       "settled on its own home -> controller (gateway)",
			activeHome: "home-a",
			selfHomeID: "home-a",
			want:       RoleController,
		},
		{
			name:       "joined another home -> mesh node",
			activeHome: "home-b",
			selfHomeID: "home-a",
			want:       RoleMeshNode,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecideRole(tt.activeHome, tt.selfHomeID); got != tt.want {
				t.Fatalf("DecideRole(%q, %q) = %q, want %q",
					tt.activeHome, tt.selfHomeID, got, tt.want)
			}
		})
	}
}
