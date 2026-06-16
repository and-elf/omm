//go:build harness

// Package e2e — backhaul harness. Validates the zero-touch wired batman backhaul
// in containers (no radios, no hardware): a wired chain
//
//	ctrl ──link-cn── node1 ──link-nn── node2     (+ a "mgmt" net for control)
//
// Every meshd broadcasts a presence beacon; each node sniffs its wired
// (br-lan-member) ports for a peer beacon and enslaves the peered ports to bat0.
// This asserts that classification end-to-end with REAL beacons over REAL
// container networks, by reading each daemon's own backhaul scan log.
//
// Two tiers:
//   - DETECTION (always): each node detects the peer on its inter-node link(s)
//     and logs the wired backhaul port set. Runs under rootless podman.
//   - FORWARDING (only when the host batman_adv module is loaded): bat0 comes up
//     and forwards. Load it first:  sudo modprobe batman_adv
//
// Run with (needs a podman API socket, e.g. `podman system service`):
//
//	TESTCONTAINERS_RYUK_DISABLED=true \
//	DOCKER_HOST=unix://$XDG_RUNTIME_DIR/podman/podman.sock \
//	go test -tags harness -run TestBackhaulHarness -timeout 15m ./internal/e2e/...
package e2e

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

const harnessImage = "docker.io/openwrt/rootfs:x86-64-23.05.5"

// Every container and network this harness creates carries this label so we can
// force-reap them by selector. testcontainers' t.Cleanup covers the happy path,
// but Ryuk is disabled under rootless podman (see TestMain), so a run killed
// before cleanup — timeout, OOM, panic, Ctrl-C — would otherwise leak host
// bridges and containers. That leak previously degraded host networking; the
// reaper below makes it self-healing.
const (
	harnessLabelKey = "omm.harness"
	harnessLabelVal = "backhaul"
)

// entrypoint runs inside each container: start ubusd+rpcd (so meshd's uci writes
// land), author the LAN bridge in UCI — a br-lan bridge device whose member
// ports are every ethernet port EXCEPT the mgmt one, plus an L3 address so the
// beacon's subnet-directed broadcast floods the links — then run netifd so it
// owns br-lan, and finally meshd. netifd ownership matters: meshd's reconcile
// does enslave→commit→`network reload`, and that reload only succeeds with
// netifd up (it provides the `network` ubus object); without it the classified
// port set is never recorded. The member ports are the batman backhaul
// candidates.
//
// br-lan gets EXACTLY the ethernet ports that carry no default route. The wired
// links are internal podman networks (no gateway, no default route); mgmt is the
// one routed network. So "no default route" == "a wired link", robustly: a
// routed port (mgmt) can never be bridged regardless of how many default routes
// exist or what order the kernel lists routes in. That structural guarantee is
// load-bearing — bridging mgmt, a segment shared by every node, closes an L2
// loop that broadcast-storms the host. Two busybox gotchas this avoids: `ip
// route show default` does NOT filter (it prints the whole table), and route
// line order is not stable, so picking "the default-route interface" by field
// position is unreliable; instead we test each interface for a default route.
//
// We first poll for the mgmt default route to appear — podman may not have
// installed it this early in boot, and acting before it exists would bridge mgmt
// (it would look route-less) and storm. If it never appears, ABORT.
const harnessEntrypoint = `set -e
mkdir -p /var/run /var/lock
: > /etc/config/network
ubusd & sleep 1
rpcd & sleep 1
i=0
while [ $i -lt 30 ]; do
  ip route 2>/dev/null | grep -qE '^default ' && break
  i=$((i+1)); sleep 1
done
ip route 2>/dev/null | grep -qE '^default ' || { echo "FATAL: no default route (mgmt) after 30s; refusing to bridge (a route-less mgmt would L2-loop and storm)"; exit 1; }
uci set network.hb=device
uci set network.hb.name=br-lan
uci set network.hb.type=bridge
bridged=0
for d in $(ls /sys/class/net | grep -E '^eth'); do
  ip route 2>/dev/null | grep -qE "^default .* dev $d( |\$)" && continue
  uci add_list network.hb.ports="$d"
  bridged=$((bridged+1))
done
[ $bridged -gt 0 ] || { echo "FATAL: no route-less (wired link) ports to bridge"; exit 1; }
uci set network.lan=interface
uci set network.lan.device=br-lan
uci set network.lan.proto=static
uci set network.lan.ipaddr="` + "`" + `echo ${BR_IP}` + "`" + `"
uci set network.lan.netmask=255.255.255.0
uci commit network
netifd & sleep 2
exec /usr/bin/meshd
`

func TestMain(m *testing.M) {
	// Ryuk (testcontainers' reaper) is flaky under rootless podman, so we disable
	// it and reap our own labeled resources instead — at startup (clearing stale
	// leftovers from any previously-killed run) and after the suite (a safety net
	// beyond each test's t.Cleanup).
	if os.Getenv("TESTCONTAINERS_RYUK_DISABLED") == "" {
		_ = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
	}
	ctx := context.Background()
	if c, n := reapHarnessResources(ctx); c > 0 || n > 0 {
		fmt.Fprintf(os.Stderr, "harness: reaped %d stale container(s) and %d network(s) before run\n", c, n)
	}
	code := m.Run()
	if c, n := reapHarnessResources(ctx); c > 0 || n > 0 {
		fmt.Fprintf(os.Stderr, "harness: reaped %d container(s) and %d network(s) after run\n", c, n)
	}
	os.Exit(code)
}

// reapHarnessResources force-removes every container and network tagged with the
// harness label, returning the counts removed. It is intentionally best-effort:
// any per-resource error is skipped so a partial daemon hiccup can't wedge the
// suite, and a missing daemon simply reaps nothing.
func reapHarnessResources(ctx context.Context) (containers, networks int) {
	cli, err := testcontainers.NewDockerClientWithOpts(ctx)
	if err != nil {
		return 0, 0
	}
	defer func() { _ = cli.Close() }()

	sel := make(client.Filters).Add("label", harnessLabelKey+"="+harnessLabelVal)

	if cl, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: sel}); err == nil {
		for _, c := range cl.Items {
			if _, err := cli.ContainerRemove(ctx, c.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); err == nil {
				containers++
			}
		}
	}
	if nl, err := cli.NetworkList(ctx, client.NetworkListOptions{Filters: sel}); err == nil {
		for _, n := range nl.Items {
			if _, err := cli.NetworkRemove(ctx, n.ID, client.NetworkRemoveOptions{}); err == nil {
				networks++
			}
		}
	}
	return containers, networks
}

// TestBackhaulHarness brings up the wired chain and asserts each node auto-detects
// its wired backhaul links from peer beacons.
func TestBackhaulHarness(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	meshd := buildMeshd(t)

	mk := func(internal bool) string {
		opts := []tcnetwork.NetworkCustomizer{tcnetwork.WithLabels(map[string]string{harnessLabelKey: harnessLabelVal})}
		if internal {
			opts = append(opts, tcnetwork.WithInternal())
		}
		n, err := tcnetwork.New(ctx, opts...)
		if err != nil {
			t.Fatalf("create network: %v", err)
		}
		t.Cleanup(func() { _ = n.Remove(ctx) })
		return n.Name
	}
	// mgmt is the routed control network — it carries the container's only default
	// route, which is how the entrypoint identifies it and keeps it OUT of br-lan.
	// The link networks are INTERNAL (no gateway), so they add no default route
	// and only ever carry the wired backhaul beacons. mgmt must NOT end up in
	// br-lan: all nodes share that segment, so bridging it closes an L2 loop that
	// broadcast-storms the host.
	mgmt := mk(false)
	linkCN := mk(true)
	linkNN := mk(true)

	// ctrl ─linkCN─ node1 ─linkNN─ node2. node1 sits on both links.
	ctrl := startBackhaulNode(t, ctx, meshd, "ctrl", "10.9.9.1", []string{mgmt, linkCN})
	node1 := startBackhaulNode(t, ctx, meshd, "node1", "10.9.9.2", []string{mgmt, linkCN, linkNN})
	node2 := startBackhaulNode(t, ctx, meshd, "node2", "10.9.9.3", []string{mgmt, linkNN})

	// Each node must classify EXACTLY its inter-node link(s) as wired backhaul:
	// ctrl and node2 have one link each, node1 sits between both. The count is
	// exact, not a lower bound — an extra port means mgmt leaked into br-lan
	// (the L2-loop/storm regression), which must fail the test, not pass it.
	assertDetectsWiredBackhaul(t, ctx, ctrl, "ctrl", 1)
	assertDetectsWiredBackhaul(t, ctx, node1, "node1", 2)
	assertDetectsWiredBackhaul(t, ctx, node2, "node2", 1)

	if batmanModuleLoaded() {
		t.Log("batman_adv loaded — FORWARDING tier could be exercised (batctl/bat0); detection asserted above")
	} else {
		t.Log("FORWARDING tier skipped: host batman_adv not loaded (sudo modprobe batman_adv)")
	}
}

func buildMeshd(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "meshd")
	cmd := exec.Command("go", "build", "-o", out, "./meshd/cmd/meshd/")
	cmd.Dir = harnessRepoRoot(t)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build meshd: %v\n%s", err, b)
	}
	return out
}

func startBackhaulNode(t *testing.T, ctx context.Context, meshd, alias, brIP string, nets []string) testcontainers.Container {
	t.Helper()
	aliases := map[string][]string{}
	for _, n := range nets {
		aliases[n] = []string{alias}
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          harnessImage,
			Networks:       nets,
			NetworkAliases: aliases,
			Labels:         map[string]string{harnessLabelKey: harnessLabelVal},
			Env: map[string]string{
				"BR_IP":               brIP,
				"MESHD_HTTP_ADDR":     "0.0.0.0:8080",
				"MESHD_SETUP_AP":      "0",
				"MESHD_BATMAN":        "1",
				"MESHD_DATABASE_PATH": "/tmp/m.bolt",
				"MESHD_IDENTITY_DIR":  "/tmp/id",
				"MESHD_HOME_ID":       "home-" + alias,
			},
			Files: []testcontainers.ContainerFile{{
				HostFilePath: meshd, ContainerFilePath: "/usr/bin/meshd", FileMode: 0o755,
			}},
			Entrypoint: []string{"/bin/sh", "-c", harnessEntrypoint},
			HostConfigModifier: func(hc *container.HostConfig) {
				hc.CapAdd = []string{"NET_ADMIN", "NET_RAW"}
			},
			WaitingFor: wait.ForLog("meshd up").WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start %s: %v", alias, err)
	}
	t.Cleanup(func() { _ = c.Terminate(ctx) })
	return c
}

// wiredPortsRe matches meshd's backhaul scan/reconcile log lines and captures the
// resolved wired-port list, e.g. "initial wired backhaul ports=[eth1 eth2]" or
// "reconciled wired ports +[eth2] -[] => [eth1 eth2]".
var wiredPortsRe = regexp.MustCompile(`(?:wired backhaul ports=|=> )\[([^\]]*)\]`)

// assertDetectsWiredBackhaul polls a node's logs until its backhaul scan reports
// EXACTLY wantPorts enslaved wired ports (peer beacons detected on exactly that
// many links), or fails after a window covering a reconcile cycle. The match is
// exact: a node classifying more ports than it has inter-node links means a
// non-link interface (mgmt) leaked into br-lan — the L2-loop regression — and
// must fail rather than satisfy a lower bound.
func assertDetectsWiredBackhaul(t *testing.T, ctx context.Context, c testcontainers.Container, alias string, wantPorts int) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	var lastLine, allLogs string
	for time.Now().Before(deadline) {
		rc, err := c.Logs(ctx)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		allLogs = string(b)
		for _, line := range strings.Split(allLogs, "\n") {
			if !strings.Contains(line, "backhaul") {
				continue
			}
			m := wiredPortsRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			lastLine = line
			ports := strings.Fields(m[1])
			if len(ports) == wantPorts {
				t.Logf("%s detected wired backhaul ports %v: %s", alias, ports, strings.TrimSpace(line))
				return
			}
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatalf("%s: expected exactly %d wired backhaul ports detected; last match %q\n--- logs ---\n%s",
		alias, wantPorts, lastLine, allLogs)
}

func batmanModuleLoaded() bool {
	b, err := os.ReadFile("/proc/modules")
	return err == nil && strings.Contains(string(b), "batman_adv ")
}

func harnessRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
