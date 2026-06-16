package config

import (
	"path/filepath"
	"testing"
)

// clearAddrEnv removes the address env vars so each case starts from defaults.
func clearAddrEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"MESHD_HTTP_ADDR", "MESHD_MGMT_ADDR", "MESHD_MESH_ADDR", "MESHD_API_ADVERTISE"} {
		t.Setenv(k, "")
	}
}

// The adopt policy defaults to onlink (zero-touch), MESHD_ADOPT_POLICY wins, and
// the legacy MESHD_AUTO_ADOPT is honored both ways when explicitly set —
// notably AUTO_ADOPT=0 must mean "off", not the new onlink default.
func TestAdoptPolicyResolution(t *testing.T) {
	cases := []struct{ policy, auto, want string }{
		{"", "", "onlink"},       // unset -> zero-touch default
		{"always", "", "always"}, // explicit policy wins
		{"off", "1", "off"},      // explicit policy wins over legacy
		{"", "1", "always"},      // legacy on -> always
		{"", "0", "off"},         // legacy explicit off -> off (regression guard)
		{"", "true", "always"},
	}
	for _, c := range cases {
		t.Setenv("MESHD_ADOPT_POLICY", c.policy)
		t.Setenv("MESHD_AUTO_ADOPT", c.auto)
		if got := Load().AdoptPolicy; got != c.want {
			t.Fatalf("policy=%q auto=%q => %q, want %q", c.policy, c.auto, got, c.want)
		}
	}
}

func TestDeriveHomeID(t *testing.T) {
	// Stable: derived from the node id's prefix.
	if got := DeriveHomeID("a7cf0e35468faaf6cf7b8d202c0d2d13"); got != "home-a7cf0e35468f" {
		t.Fatalf("DeriveHomeID = %q, want home-a7cf0e35468f", got)
	}
	// Distinct node ids yield distinct home ids (the whole point — no collision).
	a := DeriveHomeID("aaaaaaaaaaaaaaaa1111")
	b := DeriveHomeID("bbbbbbbbbbbbbbbb2222")
	if a == b {
		t.Fatalf("distinct node ids collided: %q", a)
	}
	// Degenerate input still yields a usable id.
	if got := DeriveHomeID(""); got != "home-unknown" {
		t.Fatalf("DeriveHomeID(\"\") = %q, want home-unknown", got)
	}
}

// home_id is unset by default so the daemon derives a unique one per device.
func TestConfigHomeIDUnsetByDefault(t *testing.T) {
	t.Setenv("MESHD_HOME_ID", "")
	if c := Load(); c.HomeID != "" {
		t.Fatalf("expected empty HomeID by default (derived later), got %q", c.HomeID)
	}
}

// The identity dir defaults to an absolute path so an env-less hand-launch
// reuses the deployed identity instead of minting a fresh keypair in whatever
// the current working directory happens to be (which silently changes the
// derived home id). MESHD_IDENTITY_DIR still overrides.
func TestIdentityDirDefaultsAbsolute(t *testing.T) {
	t.Setenv("MESHD_IDENTITY_DIR", "")
	c := Load()
	if c.IdentityDir != "/etc/meshd/identity" {
		t.Fatalf("IdentityDir default = %q, want /etc/meshd/identity", c.IdentityDir)
	}
	if !filepath.IsAbs(c.IdentityDir) {
		t.Fatalf("IdentityDir default %q is not absolute", c.IdentityDir)
	}

	t.Setenv("MESHD_IDENTITY_DIR", "/run/custom/id")
	if c := Load(); c.IdentityDir != "/run/custom/id" {
		t.Fatalf("IdentityDir override = %q, want /run/custom/id", c.IdentityDir)
	}
}

func TestConfigDefaultsToSplitListeners(t *testing.T) {
	clearAddrEnv(t)
	c := Load()

	if c.Combined() {
		t.Fatalf("expected split mode by default, Combined()=true (HTTPAddr=%q)", c.HTTPAddr)
	}
	if c.MgmtAddr != "127.0.0.1:8080" {
		t.Fatalf("MgmtAddr default = %q, want 127.0.0.1:8080", c.MgmtAddr)
	}
	if c.MeshAddr != "0.0.0.0:8081" {
		t.Fatalf("MeshAddr default = %q, want 0.0.0.0:8081", c.MeshAddr)
	}
	// Split mode announces the mesh-facing address (nodes reach the control
	// plane there), not the management listener.
	if c.AnnounceAddr() != c.MeshAddr {
		t.Fatalf("AnnounceAddr() = %q, want MeshAddr %q", c.AnnounceAddr(), c.MeshAddr)
	}
}

func TestConfigCombinedWhenHTTPAddrSet(t *testing.T) {
	clearAddrEnv(t)
	t.Setenv("MESHD_HTTP_ADDR", "0.0.0.0:8080")
	c := Load()

	if !c.Combined() {
		t.Fatal("expected Combined()=true when MESHD_HTTP_ADDR is set")
	}
	if c.AnnounceAddr() != "0.0.0.0:8080" {
		t.Fatalf("combined AnnounceAddr() = %q, want the HTTPAddr", c.AnnounceAddr())
	}
}

func TestSetupAPDefaultsOnAndCanBeDisabled(t *testing.T) {
	clearAddrEnv(t)

	t.Setenv("MESHD_SETUP_AP", "")
	if c := Load(); !c.SetupAPEnabled {
		t.Fatal("expected setup AP enabled by default")
	}
	if c := Load(); c.SetupAPRadio != "radio0" {
		t.Fatalf("SetupAPRadio default = %q, want radio0", c.SetupAPRadio)
	}

	t.Setenv("MESHD_SETUP_AP", "0")
	if c := Load(); c.SetupAPEnabled {
		t.Fatal("expected setup AP disabled when MESHD_SETUP_AP=0")
	}
}

func TestUbusSocketEmptyByDefault(t *testing.T) {
	// An empty default makes the ubus/uci CLI fall back to its own
	// compiled-in socket path, which tracks the OpenWrt release. A
	// hardcoded /var/run/ubus.sock broke on modern OpenWrt (socket moved to
	// /var/run/ubus/ubus.sock) with "Failed to connect to ubus".
	t.Setenv("MESHD_UBUS_SOCKET", "")
	if c := Load(); c.UbusSocket != "" {
		t.Fatalf("UbusSocket default = %q, want empty", c.UbusSocket)
	}

	t.Setenv("MESHD_UBUS_SOCKET", "/run/custom/ubus.sock")
	if c := Load(); c.UbusSocket != "/run/custom/ubus.sock" {
		t.Fatalf("UbusSocket override = %q, want /run/custom/ubus.sock", c.UbusSocket)
	}
}

func TestConfigAddrOverrides(t *testing.T) {
	clearAddrEnv(t)
	t.Setenv("MESHD_MGMT_ADDR", "127.0.0.1:9000")
	t.Setenv("MESHD_MESH_ADDR", "0.0.0.0:9001")
	c := Load()

	if c.Combined() {
		t.Fatal("split overrides should not imply combined mode")
	}
	if c.MgmtAddr != "127.0.0.1:9000" || c.MeshAddr != "0.0.0.0:9001" {
		t.Fatalf("overrides not applied: mgmt=%q mesh=%q", c.MgmtAddr, c.MeshAddr)
	}
}

func TestBatmanDefaultsOnAndConfigurable(t *testing.T) {
	for _, k := range []string{"MESHD_BATMAN", "MESHD_BATMAN_PORTS", "MESHD_BATMAN_ROUTING_ALGO"} {
		t.Setenv(k, "")
	}
	// batman-adv routing is on by default (auto-degrades when the module/proto is
	// absent), with the standard routing algorithm and no extra wired ports.
	c := Load()
	if !c.BatmanEnable {
		t.Error("BatmanEnable should default to true")
	}
	if c.BatmanRoutingAlgo != "BATMAN_IV" {
		t.Errorf("BatmanRoutingAlgo default = %q, want BATMAN_IV", c.BatmanRoutingAlgo)
	}
	if len(c.BatmanPorts) != 0 {
		t.Errorf("BatmanPorts default = %v, want empty", c.BatmanPorts)
	}

	// Explicit overrides.
	t.Setenv("MESHD_BATMAN", "0")
	t.Setenv("MESHD_BATMAN_PORTS", "eth0, eth1")
	t.Setenv("MESHD_BATMAN_ROUTING_ALGO", "BATMAN_V")
	c = Load()
	if c.BatmanEnable {
		t.Error("MESHD_BATMAN=0 should disable batman")
	}
	if c.BatmanRoutingAlgo != "BATMAN_V" {
		t.Errorf("BatmanRoutingAlgo = %q, want BATMAN_V", c.BatmanRoutingAlgo)
	}
	if len(c.BatmanPorts) != 2 || c.BatmanPorts[0] != "eth0" || c.BatmanPorts[1] != "eth1" {
		t.Errorf("BatmanPorts = %v, want [eth0 eth1]", c.BatmanPorts)
	}
}
