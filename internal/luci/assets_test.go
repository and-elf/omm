package luci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// appFile resolves a path inside package/luci-app-meshd/.
func appFile(t *testing.T, rel string) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "package", "luci-app-meshd", rel)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found")
		}
		dir = parent
	}
}

func readJSON(t *testing.T, rel string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(appFile(t, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s is not valid JSON: %v", rel, err)
	}
	return m
}

// The ACL must grant access to the meshd ubus object and cover every method the
// rpcd plugin advertises, split across read (queries) and write (mutations) —
// otherwise LuCI calls would be denied or, worse, a mutation would be missed.
func TestACLCoversPluginMethods(t *testing.T) {
	acl := readJSON(t, "root/usr/share/rpcd/acl.d/luci-app-meshd.json")
	entry, ok := acl["luci-app-meshd"].(map[string]any)
	if !ok {
		t.Fatal("ACL missing luci-app-meshd entry")
	}

	granted := map[string]bool{}
	for _, kind := range []string{"read", "write"} {
		section, _ := entry[kind].(map[string]any)
		ubus, _ := section["ubus"].(map[string]any)
		methods, ok := ubus["meshd"].([]any)
		if !ok {
			t.Fatalf("ACL %s grant missing meshd ubus methods", kind)
		}
		for _, m := range methods {
			granted[m.(string)] = true
		}
	}

	// Methods the plugin's `list` advertises must all be reachable.
	out := runPlugin(t, "http://127.0.0.1:1", "", "list")
	var advertised map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &advertised); err != nil {
		t.Fatalf("plugin list invalid: %v", err)
	}
	for m := range advertised {
		if !granted[m] {
			t.Errorf("plugin advertises %q but the ACL does not grant it", m)
		}
	}
}

func TestMenuEntryIsValid(t *testing.T) {
	menu := readJSON(t, "root/usr/share/luci/menu.d/luci-app-meshd.json")
	entry, ok := menu["admin/network/meshd"].(map[string]any)
	if !ok {
		t.Fatal("menu missing admin/network/meshd entry")
	}
	action, _ := entry["action"].(map[string]any)
	if action["type"] != "view" || action["path"] != "meshd/meshd" {
		t.Fatalf("unexpected menu action: %v", action)
	}
	// The referenced view file must exist.
	if _, err := os.Stat(appFile(t, "htdocs/luci-static/resources/view/meshd/meshd.js")); err != nil {
		t.Fatalf("menu references a missing view: %v", err)
	}
}
