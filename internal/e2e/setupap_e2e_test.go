//go:build e2e

// This file verifies the first-boot setup AP against a real OpenWrt userland.
// A container can't bring up an actual radio, but meshd authors the AP purely
// through OpenWrt's own uci (wireless/network/dhcp), so the meaningful contract
// — that an unclaimed device writes the setup sections on boot and removes them
// once setup completes — is fully observable via uci on a real rpcd/ubusd
// stack. See internal/setupap and doc/companion-app.md.
package e2e

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// setupAPEntrypoint boots ubusd + rpcd (with the real uci object) + meshd in
// combined mode. meshd starts unclaimed (setup not complete), so its boot path
// brings the setup AP up; POST /setup/complete later tears it down. netifd is
// absent, so its reload objects are stubbed exactly as the LuCI e2e does.
const setupAPEntrypoint = `#!/bin/sh
set -e
mkdir -p /var/lock /var/run /var/run/ubus /usr/libexec/rpcd
opkg update >/dev/null 2>&1 || true
# rpcd provides the built-in 'uci' ubus object meshd writes through; pull it from
# the feed only if the base image lacks it.
[ -x /sbin/rpcd ] || opkg install rpcd >/dev/null 2>&1 || { echo "FAIL: rpcd install"; exit 1; }
opkg install /tmp/meshd.ipk >/dev/null 2>&1 || { echo "FAIL: meshd ipk install"; exit 1; }

# A radio for the AP to attach to, and empty network/dhcp the AP writes into.
cat > /etc/config/wireless <<'EOF'
config wifi-device 'radio0'
	option type 'mac80211'
	option disabled '1'
EOF
: > /etc/config/network
: > /etc/config/dhcp

# Stub the netifd objects meshd reloads through (absent without netifd): it
# calls 'network reload' then 'network.wireless reconf' and only needs them to
# accept the request, exactly as netifd does on a real device.
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
# rpcd provides the 'uci' object meshd writes through; it must be up before
# meshd's boot brings the setup AP online.
/sbin/rpcd & sleep 1
exec env \
	MESHD_HTTP_ADDR=0.0.0.0:8080 MESHD_AUTO_ADOPT=0 MESHD_AUTO_ONBOARD_WIRED=0 \
	MESHD_HOME_ID=home-setup MESHD_SERIAL=setup-dev \
	MESHD_DATABASE_PATH=/tmp/m.bolt MESHD_IDENTITY_DIR=/tmp/id \
	MESHD_UBUS_SOCKET=/var/run/ubus/ubus.sock \
	MESHD_UDP_BROADCAST=127.0.0.1:45678 \
	/usr/bin/meshd
`

// startSetupDevContainer boots an unclaimed combined-mode meshd on a real
// OpenWrt userland (ubusd + rpcd + uci) and returns the running container.
func startSetupDevContainer(ctx context.Context, t *testing.T) testcontainers.Container {
	t.Helper()
	tg := targets[0] // opkg-23.05

	meshdIPK := filepath.Join(repoRoot(t), tg.pkgRel)
	if _, err := os.Stat(meshdIPK); err != nil {
		t.Fatalf("package not built: %s (run ./scripts/build.sh and package-ipk.sh first)", meshdIPK)
	}

	net, err := tcnetwork.New(ctx)
	if err != nil {
		t.Fatalf("create network: %v", err)
	}
	t.Cleanup(func() { _ = net.Remove(ctx) })

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          tg.image,
			Networks:       []string{net.Name},
			NetworkAliases: map[string][]string{net.Name: {"setup-dev"}},
			ExposedPorts:   []string{"8080/tcp"},
			Files: []testcontainers.ContainerFile{
				{HostFilePath: meshdIPK, ContainerFilePath: "/tmp/meshd.ipk", FileMode: 0o644},
			},
			Entrypoint: []string{"/bin/sh", "-c", "printf '%s' \"$SETUP_AP_ENTRYPOINT\" > /run.sh && exec /bin/sh /run.sh"},
			Env:        map[string]string{"SETUP_AP_ENTRYPOINT": setupAPEntrypoint},
			WaitingFor: wait.ForHTTP("/health").WithPort("8080/tcp").WithStartupTimeout(180 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start setup-dev: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(ctx) })
	return c
}

// TestSetupAPLifecycleE2E asserts an unclaimed device authors the setup AP uci
// sections on boot and removes them when onboarding completes.
func TestSetupAPLifecycleE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	c := startSetupDevContainer(ctx, t)

	// 1. On boot, the setup AP sections appear with the expected values.
	ssid := waitUCI(ctx, t, c, "wireless.omm_setup.ssid", 60*time.Second)
	if !strings.HasPrefix(ssid, "OMM-Setup-") {
		t.Fatalf("setup AP ssid = %q, want OMM-Setup-* prefix", ssid)
	}
	if ip := uciGet(ctx, t, c, "network.ommsetup.ipaddr"); ip != "192.168.254.1" {
		t.Fatalf("setup network ipaddr = %q, want 192.168.254.1", ip)
	}
	if iface := uciGet(ctx, t, c, "dhcp.ommsetup.interface"); iface != "ommsetup" {
		t.Fatalf("setup dhcp interface = %q, want ommsetup", iface)
	}
	t.Logf("setup AP up on boot: ssid=%s", ssid)

	// 2. Completing setup tears the AP down.
	base := mappedURL(ctx, t, c, "8080/tcp")
	resp, err := http.Post(base+"/setup/complete", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /setup/complete: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /setup/complete status %d", resp.StatusCode)
	}

	waitUCIAbsent(ctx, t, c, "wireless.omm_setup", 30*time.Second)
	assertUCIAbsent(ctx, t, c, "network.ommsetup")
	assertUCIAbsent(ctx, t, c, "dhcp.ommsetup")
	t.Logf("setup AP torn down after /setup/complete")
}

// TestSetupUplinkE2E asserts an unclaimed device, asked to join a home WiFi over
// POST /setup/uplink, authors the station wifi-iface + DHCP-client network so it
// can reach its controller, and that completing setup tears those down too.
func TestSetupUplinkE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	c := startSetupDevContainer(ctx, t)

	// Wait until the boot-time setup AP is up, so the daemon is fully ready.
	waitUCI(ctx, t, c, "wireless.omm_setup.ssid", 60*time.Second)

	// Provision a WiFi uplink to the home network.
	base := mappedURL(ctx, t, c, "8080/tcp")
	resp, err := http.Post(base+"/setup/uplink", "application/json",
		strings.NewReader(`{"ssid":"HomeNet","password":"home-secret"}`))
	if err != nil {
		t.Fatalf("POST /setup/uplink: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /setup/uplink status %d", resp.StatusCode)
	}

	// The station interface and its DHCP-client network appear with our values.
	if mode := waitUCI(ctx, t, c, "wireless.omm_uplink.mode", 30*time.Second); mode != "sta" {
		t.Fatalf("uplink mode = %q, want sta", mode)
	}
	if ssid := uciGet(ctx, t, c, "wireless.omm_uplink.ssid"); ssid != "HomeNet" {
		t.Fatalf("uplink ssid = %q, want HomeNet", ssid)
	}
	if enc := uciGet(ctx, t, c, "wireless.omm_uplink.encryption"); enc != "psk2" {
		t.Fatalf("uplink encryption = %q, want psk2", enc)
	}
	if proto := uciGet(ctx, t, c, "network.ommuplink.proto"); proto != "dhcp" {
		t.Fatalf("uplink network proto = %q, want dhcp", proto)
	}
	t.Logf("uplink station provisioned: ssid=HomeNet")

	// Completing setup tears down the uplink (and the setup AP) — the applied
	// profile owns the node's real network config from here on.
	resp, err = http.Post(base+"/setup/complete", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /setup/complete: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /setup/complete status %d", resp.StatusCode)
	}

	waitUCIAbsent(ctx, t, c, "wireless.omm_uplink", 30*time.Second)
	assertUCIAbsent(ctx, t, c, "network.ommuplink")
	t.Logf("uplink torn down after /setup/complete")
}

// uciExec runs `uci -q get <key>` (or show) in the container, returning the
// trimmed stdout and the exit code (uci exits non-zero for a missing entry).
func uciExec(ctx context.Context, t *testing.T, c testcontainers.Container, args ...string) (string, int) {
	t.Helper()
	// Multiplexed() demuxes Docker's stream framing so we get clean stdout.
	code, reader, err := c.Exec(ctx, append([]string{"uci"}, args...), tcexec.Multiplexed())
	if err != nil {
		t.Fatalf("exec uci %v: %v", args, err)
	}
	out, _ := io.ReadAll(reader)
	return strings.TrimSpace(string(out)), code
}

func uciGet(ctx context.Context, t *testing.T, c testcontainers.Container, key string) string {
	t.Helper()
	out, code := uciExec(ctx, t, c, "-q", "get", key)
	if code != 0 {
		t.Fatalf("uci get %s: exit %d (%q)", key, code, out)
	}
	// `uci get` output is prefixed by the exec multiplexer with no key; the value
	// is the whole trimmed line.
	return out
}

func waitUCI(ctx context.Context, t *testing.T, c testcontainers.Container, key string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if out, code := uciExec(ctx, t, c, "-q", "get", key); code == 0 && out != "" {
			return out
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("uci key %s did not appear within %s", key, timeout)
	return ""
}

func waitUCIAbsent(ctx context.Context, t *testing.T, c testcontainers.Container, section string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, code := uciExec(ctx, t, c, "-q", "show", section); code != 0 {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("uci section %s still present after %s", section, timeout)
}

func assertUCIAbsent(ctx context.Context, t *testing.T, c testcontainers.Container, section string) {
	t.Helper()
	if out, code := uciExec(ctx, t, c, "-q", "show", section); code == 0 {
		t.Fatalf("uci section %s still present: %q", section, out)
	}
}
