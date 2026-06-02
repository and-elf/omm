//go:build e2e

// This file drives meshd through the *full* LuCI integration: a real OpenWrt
// userland with the built meshd + luci-app-meshd packages, the LuCI service
// stack (ubusd + rpcd + uhttpd with the ubus handler), and the operator
// workflows exercised over the authenticated /ubus endpoint exactly as the
// PWA's ubus transport (web/src/api/ubus.ts) does. See
// doc/luci-integration-testing.md.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// zeroSession is the pre-login ubus session id (32 zeros): it holds only the
// ACLs granted to the unauthenticated scope, so it must NOT reach meshd.
const zeroSession = "00000000000000000000000000000000"

// stubClientMAC is the station the stub hostapd.ap0 object advertises; meshd's
// topology read lower-cases it, so we assert against the lower-case form.
const stubClientMAC = "aa:bb:cc:dd:ee:ff"

// luciEntrypoint bootstraps the controller container: install the feed deps and
// our two packages, gate a LuCI login on ONLY the luci-app-meshd ACL, stub a
// hostapd object so the topology read has a known associated station, then run
// meshd (combined plane, manual adopt) behind ubusd/rpcd/uhttpd.
//
// meshd is started with explicit env (not via procd/uci), so the package's init
// script and uci-defaults are inert here — they cannot start a competing daemon.
const luciEntrypoint = `#!/bin/sh
set -e
mkdir -p /var/lock /var/run /var/run/ubus /usr/libexec/rpcd /usr/share/rpcd/acl.d /etc/meshd /www
opkg update >/dev/null 2>&1
opkg install uhttpd uhttpd-mod-ubus curl >/dev/null 2>&1 || { echo "FAIL: feed install"; exit 1; }
opkg install /tmp/meshd.ipk >/dev/null 2>&1 || { echo "FAIL: meshd ipk install"; exit 1; }
opkg install /tmp/luci-app-meshd.ipk >/dev/null 2>&1 || { echo "FAIL: luci-app-meshd ipk install"; exit 1; }

# A LuCI login granted ONLY the luci-app-meshd ACL scope: reaching meshd through
# it proves our ACL file grants the methods (not a blanket permission).
cat > /etc/config/rpcd <<'EOF'
config login
	option username 'root'
	option password '$p$root'
	list read 'luci-app-meshd'
	list write 'luci-app-meshd'
EOF
printf 'test\ntest\n' | passwd root >/dev/null 2>&1

# Stub hostapd.ap0 so meshd's topology read (hostapd.<iface> get_clients) sees a
# known associated station, exercising the wireless-client device path.
cat > /usr/libexec/rpcd/hostapd.ap0 <<'EOF'
#!/bin/sh
case "$1" in
list) echo '{"get_clients":{}}' ;;
call)
	case "$2" in
	get_clients) echo '{"freq":5180,"clients":{"AA:BB:CC:DD:EE:FF":{"signal":-55,"rx_rate_info":{"rate":866},"tx_rate_info":{"rate":866}}}}' ;;
	*) echo '{}' ;;
	esac ;;
esac
EOF
chmod +x /usr/libexec/rpcd/hostapd.ap0

# Seed the uci config meshd's profile apply writes to: a named 'mesh' wifi-iface
# (it sets wireless.mesh.ssid/key) and a system section (it sets the hostname).
# The real rpcd 'uci' object operates on these.
cat > /etc/config/wireless <<'EOF'
config wifi-iface 'mesh'
	option ssid 'old-mesh'
EOF
cat > /etc/config/system <<'EOF'
config system
	option hostname 'OpenWrt'
EOF

# Stub the netifd objects the apply reloads through (absent without netifd):
# meshd calls 'network reload' then 'network.wireless reconf' and only needs
# them to accept the request, exactly as netifd does on a real device.
for obj in network network.wireless; do
	cat > "/usr/libexec/rpcd/$obj" <<'EOF'
#!/bin/sh
case "$1" in
list) echo '{"reload":{},"reconf":{}}' ;;
call) echo '{}' ;;
esac
EOF
	chmod +x "/usr/libexec/rpcd/$obj"
done

/sbin/ubusd & sleep 1
# Combined plane on :8080 (mgmt + mesh): the rpcd plugin proxies to localhost
# and the joining node reaches the same address. Manual adopt so the enrollment
# lands pending and the test drives adoption over /ubus. The ubus socket path is
# this image's ubusd default (meshd's built-in default differs).
MESHD_HTTP_ADDR=0.0.0.0:8080 MESHD_AUTO_ADOPT=0 \
	MESHD_HOME_ID=home-e2e MESHD_HOME_NAME=Casa MESHD_SERIAL=controller \
	MESHD_DATABASE_PATH=/tmp/m.bolt MESHD_IDENTITY_DIR=/tmp/id \
	MESHD_AP_IFACES=ap0 MESHD_UBUS_SOCKET=/var/run/ubus/ubus.sock \
	MESHD_UDP_BROADCAST=127.0.0.1:45678 \
	/usr/bin/meshd >/tmp/meshd.log 2>&1 &
for i in $(seq 1 30); do curl -fs http://127.0.0.1:8080/health >/dev/null 2>&1 && break; sleep 1; done
/sbin/rpcd & sleep 1
# uhttpd in the foreground keeps the container alive; -u /ubus is the endpoint
# the PWA (and this test) call.
exec uhttpd -f -h /www -p 0.0.0.0:80 -u /ubus
`

// TestLuCIWorkflowE2E runs the operator workflows through the authenticated
// LuCI /ubus path against a real OpenWrt userland. See the package doc above.
func TestLuCIWorkflowE2E(t *testing.T) {
	tg := targets[0] // opkg-23.05: the image the LuCI feed packages target.
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	meshdIPK := filepath.Join(repoRoot(t), tg.pkgRel)
	luciIPK := filepath.Join(repoRoot(t), "build/luci-ipk/luci-app-meshd_0.1.0_all.ipk")
	for _, p := range []string{meshdIPK, luciIPK} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("package not built: %s (run ./scripts/build.sh, package-ipk.sh, package-luci-ipk.sh)", p)
		}
	}

	net, err := tcnetwork.New(ctx)
	if err != nil {
		t.Fatalf("create network: %v", err)
	}
	t.Cleanup(func() { _ = net.Remove(ctx) })

	// Controller: meshd + luci-app-meshd behind the full LuCI stack.
	controller, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          tg.image,
			Networks:       []string{net.Name},
			NetworkAliases: map[string][]string{net.Name: {"controller"}},
			ExposedPorts:   []string{"80/tcp", "8080/tcp"},
			Files: []testcontainers.ContainerFile{
				{HostFilePath: meshdIPK, ContainerFilePath: "/tmp/meshd.ipk", FileMode: 0o644},
				{HostFilePath: luciIPK, ContainerFilePath: "/tmp/luci-app-meshd.ipk", FileMode: 0o644},
			},
			Entrypoint: []string{"/bin/sh", "-c", "printf '%s' \"$LUCI_ENTRYPOINT\" > /run.sh && exec /bin/sh /run.sh"},
			Env:        map[string]string{"LUCI_ENTRYPOINT": luciEntrypoint},
			WaitingFor: wait.ForHTTP("/health").WithPort("8080/tcp").WithStartupTimeout(180 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start controller: %v", err)
	}
	t.Cleanup(func() { _ = controller.Terminate(ctx) })

	// Node: a plain meshd daemon (its own Home) that joins the controller over
	// the mesh plane. With manual adopt on the controller, its join blocks
	// pending approval — which the test grants over /ubus.
	h := &harness{t: t, ctx: ctx, tg: tg, net: net.Name, pkgPath: meshdIPK}
	h.startDaemon("node-1", map[string]string{
		"MESHD_HOME_ID": "home-node-1", "MESHD_SERIAL": "node-1",
		"MESHD_JOIN": "http://controller:8080",
	})

	luci := &luciSession{t: t, base: mappedURL(ctx, t, controller, "80/tcp")}
	luci.waitForUbus(ctx)
	luci.login(ctx, "root", "test")

	// 1. ACL gate: the granted session reaches meshd; the zero session does not.
	if got := luci.callRaw(ctx, luci.token, "status", nil); !strings.Contains(got, `"status":"ready"`) {
		t.Fatalf("granted session could not reach meshd.status: %s", got)
	}
	if got := luci.callRaw(ctx, zeroSession, "status", nil); strings.Contains(got, `"status":"ready"`) {
		t.Fatalf("unauthenticated session reached meshd (ACL not enforced): %s", got)
	}

	// 2. Node enrollment + adopt over /ubus.
	nodeID := luci.waitForPendingNode(ctx, 90*time.Second)
	t.Logf("pending enrollment for node %s; adopting over /ubus", nodeID)
	luci.call(ctx, "adopt_node", map[string]any{"node_id": nodeID})

	nodes := luci.waitForNode(ctx, nodeID, 30*time.Second)
	if nodes[nodeID] != "home-e2e" {
		t.Fatalf("adopted node %s in home %q, want home-e2e", nodeID, nodes[nodeID])
	}

	// 3. Home/profile lifecycle over /ubus.
	if homes := luci.call(ctx, "homes", nil)["homes"]; !containsHome(homes, "home-e2e") {
		t.Fatalf("homes list missing the controller Home: %v", homes)
	}
	luci.call(ctx, "create_home", map[string]any{"id": "home-guest", "name": "Guest"})
	if got := luci.call(ctx, "get_home", map[string]any{"home_id": "home-guest"}); got["name"] != "Guest" {
		t.Fatalf("get_home after create returned %v, want name=Guest", got)
	}
	luci.call(ctx, "save_profile", map[string]any{
		"home_id": "home-guest", "node_name": "gw", "mesh_ssid": "guest-mesh", "mesh_key": "supersecretkey",
	})
	prof, _ := luci.call(ctx, "get_profile", map[string]any{"home_id": "home-guest"})["profile"].(map[string]any)
	if prof["mesh_ssid"] != "guest-mesh" {
		t.Fatalf("get_profile returned %v, want mesh_ssid=guest-mesh", prof)
	}
	luci.call(ctx, "set_active_home", map[string]any{"home_id": "home-guest"})
	if got := luci.call(ctx, "active_home", nil); got["home_id"] != "home-guest" {
		t.Fatalf("active_home = %v, want home-guest", got["home_id"])
	}
	// Switch back before deleting: meshd refuses to delete the active Home.
	luci.call(ctx, "set_active_home", map[string]any{"home_id": "home-e2e"})
	luci.call(ctx, "delete_home", map[string]any{"home_id": "home-guest"})
	if homes := luci.call(ctx, "homes", nil)["homes"]; containsHome(homes, "home-guest") {
		t.Fatalf("home-guest still present after delete_home: %v", homes)
	}

	// 4. Wireless client devices surface through the topology read.
	topo := luci.call(ctx, "topology", nil)
	if !topologyHasClient(topo, stubClientMAC) {
		t.Fatalf("topology over /ubus missing the associated station %s: %v", stubClientMAC, topo)
	}

	t.Logf("LuCI /ubus path verified: ACL gate, node enroll+adopt, home/profile lifecycle, wireless client device")
}

// luciSession drives meshd over LuCI's authenticated /ubus JSON-RPC endpoint,
// mirroring web/src/api/ubus.ts.
type luciSession struct {
	t     *testing.T
	base  string
	token string
}

// waitForUbus blocks until uhttpd answers /ubus (it comes up after meshd's
// health gate the container waits on).
func (s *luciSession) waitForUbus(ctx context.Context) {
	s.t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		out := s.rpc(ctx, zeroSession, "session", "login", map[string]any{})
		if out != "" {
			return
		}
		time.Sleep(time.Second)
	}
	s.t.Fatal("uhttpd /ubus did not come up")
}

func (s *luciSession) login(ctx context.Context, user, pass string) {
	s.t.Helper()
	out := s.rpc(ctx, zeroSession, "session", "login", map[string]any{"username": user, "password": pass})
	var env struct {
		Result []json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil || len(env.Result) < 2 {
		s.t.Fatalf("login: unexpected response %s", out)
	}
	var data struct {
		Session string `json:"ubus_rpc_session"`
	}
	_ = json.Unmarshal(env.Result[1], &data)
	if data.Session == "" {
		s.t.Fatalf("login returned no session: %s", out)
	}
	s.token = data.Session
}

// call invokes a meshd ubus method with the granted session and returns the
// decoded data object, failing the test on a transport/permission error.
func (s *luciSession) call(ctx context.Context, method string, args map[string]any) map[string]any {
	s.t.Helper()
	out := s.callRaw(ctx, s.token, method, args)
	var data map[string]any
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		s.t.Fatalf("%s: response not a JSON object: %s", method, out)
	}
	if errMsg, ok := data["error"]; ok {
		s.t.Fatalf("%s returned meshd error: %v", method, errMsg)
	}
	return data
}

// callRaw performs a meshd ubus call and returns the raw data payload (result[1]
// re-encoded). An empty/missing data payload (e.g. a permission-denied result
// with only a status code) yields "{}".
func (s *luciSession) callRaw(ctx context.Context, token, method string, args map[string]any) string {
	s.t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	out := s.rpc(ctx, token, "meshd", method, args)
	var env struct {
		Result []json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		s.t.Fatalf("%s: invalid /ubus envelope: %s", method, out)
	}
	if env.Error != nil || len(env.Result) == 0 {
		return "{}" // JSON-RPC level error (e.g. denied) — no data
	}
	var code int
	_ = json.Unmarshal(env.Result[0], &code)
	if code != 0 || len(env.Result) < 2 {
		return "{}" // ubus status (e.g. 6 = permission denied) — no data
	}
	return string(env.Result[1])
}

// rpc POSTs one JSON-RPC ubus "call" and returns the raw response body.
func (s *luciSession) rpc(ctx context.Context, token, object, method string, args map[string]any) string {
	s.t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "call",
		"params": []any{token, object, method, args},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/ubus", bytes.NewReader(payload))
	if err != nil {
		s.t.Fatalf("build /ubus request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "" // not up yet (waitForUbus retries)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

// waitForPendingNode polls enrollments until the joining node appears awaiting
// approval, returning its node id.
func (s *luciSession) waitForPendingNode(ctx context.Context, timeout time.Duration) string {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data := s.call(ctx, "enrollments", nil)
		for _, e := range asSlice(data["enrollments"]) {
			m, _ := e.(map[string]any)
			if id, _ := m["node_id"].(string); id != "" {
				return id
			}
		}
		time.Sleep(2 * time.Second)
	}
	s.t.Fatalf("no pending enrollment within %s", timeout)
	return ""
}

// waitForNode polls nodes until nodeID is present, returning a node-id ->
// current-home map.
func (s *luciSession) waitForNode(ctx context.Context, nodeID string, timeout time.Duration) map[string]string {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		homes := map[string]string{}
		for _, n := range asSlice(s.call(ctx, "nodes", nil)["nodes"]) {
			m, _ := n.(map[string]any)
			id, _ := m["id"].(string)
			home, _ := m["current_home"].(string)
			homes[id] = home
		}
		if _, ok := homes[nodeID]; ok {
			return homes
		}
		time.Sleep(2 * time.Second)
	}
	s.t.Fatalf("node %s did not appear within %s", nodeID, timeout)
	return nil
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func containsHome(homes any, id string) bool {
	for _, h := range asSlice(homes) {
		if m, _ := h.(map[string]any); m["id"] == id {
			return true
		}
	}
	return false
}

func topologyHasClient(topo map[string]any, mac string) bool {
	for _, c := range asSlice(topo["clients"]) {
		if m, _ := c.(map[string]any); m["mac"] == mac {
			return true
		}
	}
	return false
}

// mappedURL returns the host URL for a container's exposed port.
func mappedURL(ctx context.Context, t *testing.T, c testcontainers.Container, port string) string {
	t.Helper()
	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	p, err := c.MappedPort(ctx, port)
	if err != nil {
		t.Fatalf("mapped port: %v", err)
	}
	return fmt.Sprintf("http://%s:%s", host, p.Port())
}
