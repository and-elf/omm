package config

import "testing"

// clearAddrEnv removes the address env vars so each case starts from defaults.
func clearAddrEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"MESHD_HTTP_ADDR", "MESHD_MGMT_ADDR", "MESHD_MESH_ADDR", "MESHD_API_ADVERTISE"} {
		t.Setenv(k, "")
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
