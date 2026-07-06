package config

import (
	"os"
	"regexp"
	"testing"
)

// The OpenWrt init script (package/meshd/files/meshd.init) maps UCI options to
// the MESHD_* environment variables the daemon reads. If it exports a UCI value
// under a name the daemon does not read, the setting is silently dropped.
//
// Regression guard for the controller_url -> MESHD_JOIN mapping: the init script
// exported the configured controller as MESHD_CONTROLLER, but the daemon reads
// the join list only from MESHD_JOIN (see Load). The result was that setting
// `controller_url` in UCI never made a node join on boot — it re-derived its own
// home every restart instead of staying enrolled in its controller.
func TestInitScriptExportsControllerURLAsJoin(t *testing.T) {
	const initPath = "../../package/meshd/files/meshd.init"
	b, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatalf("read init script: %v", err)
	}
	init := string(b)

	// The daemon reads the controller/join URLs from MESHD_JOIN, so the init
	// must export the controller_url UCI option under that name.
	if !regexp.MustCompile(`MESHD_JOIN="\$controller_url"`).MatchString(init) {
		t.Errorf("meshd.init must export controller_url as MESHD_JOIN (the var the daemon reads); it does not")
	}

	// MESHD_CONTROLLER has no reader anywhere in the daemon — exporting the
	// controller_url under it silently drops the setting. (Distinct from
	// MESHD_CONTROLLER_ID, which is read and legitimate.)
	if regexp.MustCompile(`MESHD_CONTROLLER\b`).MatchString(init) {
		t.Errorf("meshd.init exports dead MESHD_CONTROLLER; the daemon reads MESHD_JOIN")
	}
}

// The batman-adv routing options must reach the daemon: the init script reads
// the UCI options and exports them under the MESHD_BATMAN* names Load() reads.
// Same failure mode as the controller_url bug — an unexported option is silently
// dropped and batman would never be configurable on-device.
func TestInitScriptExportsBatmanOptions(t *testing.T) {
	const initPath = "../../package/meshd/files/meshd.init"
	b, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatalf("read init script: %v", err)
	}
	init := string(b)

	for env, uci := range map[string]string{
		"MESHD_BATMAN":              "batman",
		"MESHD_BATMAN_PORTS":        "batman_ports",
		"MESHD_BATMAN_ROUTING_ALGO": "batman_routing_algo",
	} {
		if !regexp.MustCompile(env + `="\$` + uci + `"`).MatchString(init) {
			t.Errorf("meshd.init must export %s=\"$%s\"; it does not", env, uci)
		}
		if !regexp.MustCompile(`config_get ` + uci + ` main ` + uci).MatchString(init) {
			t.Errorf("meshd.init must read UCI option %q via config_get; it does not", uci)
		}
	}
}

// Network posture management defaults ON (opt-out): the init script's config_get
// fallback must be 1 so a device with no explicit manage_network still stands
// down its routed wan and makes every ethernet jack work (issue #42). A stale
// `0` fallback would silently keep the old opt-in behaviour on unconfigured
// devices even though the daemon default flipped.
func TestInitScriptManageNetworkDefaultsOn(t *testing.T) {
	const initPath = "../../package/meshd/files/meshd.init"
	b, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatalf("read init script: %v", err)
	}
	if !regexp.MustCompile(`config_get manage_network main manage_network 1\b`).MatchString(string(b)) {
		t.Error("meshd.init must default manage_network to 1 (opt-out); fallback is not 1")
	}
}
