package luci

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pluginPath locates the rpcd exec plugin relative to the repo root.
func pluginPath(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "package", "luci-app-meshd", "root", "usr", "libexec", "rpcd", "meshd")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root (go.mod)")
		}
		dir = parent
	}
}

// runPlugin executes `sh <plugin> <args...>` with stdin and MESHD_URL pointing
// at the stub server, returning stdout.
func runPlugin(t *testing.T, base, stdin string, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available")
	}
	cmd := exec.Command("sh", append([]string{pluginPath(t)}, args...)...)
	cmd.Env = append(os.Environ(), "MESHD_URL="+base)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("plugin %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestPluginListAdvertisesMethods(t *testing.T) {
	out := runPlugin(t, "http://127.0.0.1:1", "", "list")

	var methods map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &methods); err != nil {
		t.Fatalf("list output is not valid JSON: %v\n%s", err, out)
	}
	for _, want := range []string{"status", "homes", "nodes", "set_active_home", "delete_home", "reset"} {
		if _, ok := methods[want]; !ok {
			t.Fatalf("list missing method %q; got %v", want, out)
		}
	}
}

func TestPluginProxiesGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/status" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"state":"active","home":"Home"}`)
	}))
	defer srv.Close()

	out := runPlugin(t, srv.URL, "", "call", "status")
	if !strings.Contains(out, `"state":"active"`) {
		t.Fatalf("status not proxied through: %s", out)
	}
}

func TestPluginProxiesWriteBody(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{"home_id":"h1"}`)
	}))
	defer srv.Close()

	out := runPlugin(t, srv.URL, `{"home_id":"h1"}`, "call", "set_active_home")
	if gotMethod != http.MethodPut || gotPath != "/active-home" {
		t.Fatalf("set_active_home hit %s %s, want PUT /active-home", gotMethod, gotPath)
	}
	if !strings.Contains(gotBody, `"home_id":"h1"`) {
		t.Fatalf("request body not forwarded: %q", gotBody)
	}
	if !strings.Contains(out, `"home_id":"h1"`) {
		t.Fatalf("response not returned: %s", out)
	}
}

// When meshd answers with an HTTP error, the plugin must pass meshd's JSON
// error body back through (with a zero exit) so the PWA can surface the real
// reason. Otherwise rpcd reports a bare, meaningless ubus status (e.g. 5) and
// the user is left guessing ("access denied?").
func TestPluginPropagatesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"missing required fields: [id name]"}`)
	}))
	defer srv.Close()

	out := runPlugin(t, srv.URL, `{"id":"","name":""}`, "call", "create_home")
	if !strings.Contains(out, "missing required fields") {
		t.Fatalf("meshd error body not propagated; got %q", out)
	}
}

func TestPluginExtractsPathParam(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	runPlugin(t, srv.URL, `{"home_id":"home-1"}`, "call", "delete_home")
	if gotMethod != http.MethodDelete || gotPath != "/homes/home-1" {
		t.Fatalf("delete_home hit %s %s, want DELETE /homes/home-1", gotMethod, gotPath)
	}
}
