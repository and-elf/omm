package config

import "testing"

// clearAddrEnv removes the address env vars so each case starts from defaults.
func clearAddrEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"MESHD_HTTP_ADDR", "MESHD_MGMT_ADDR", "MESHD_MESH_ADDR", "MESHD_API_ADVERTISE"} {
		t.Setenv(k, "")
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
