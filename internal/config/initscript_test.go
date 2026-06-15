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
