//go:build e2e

// Package e2e runs the enrollment flow against real OpenWrt container images
// with the built package installed. Every container runs the same role-less
// meshd daemon; the test itself drives who enrolls into whom.
//
// Run with:
//
//	./scripts/build.sh && ./scripts/package-ipk.sh && ./scripts/package-apk.sh
//	go test -tags e2e -timeout 25m ./internal/e2e/...
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
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

func init() {
	// Rootless podman generally needs the resource reaper disabled.
	if os.Getenv("TESTCONTAINERS_RYUK_DISABLED") == "" {
		_ = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
	}
}

// target is one OpenWrt image + the way to install the package on it.
type target struct {
	name      string
	image     string
	pkgRel    string // package path relative to repo root
	destPath  string // where to copy it inside the container
	installSh string // shell that installs the copied package
}

var targets = []target{
	{
		name:     "opkg-23.05",
		image:    "docker.io/openwrt/rootfs:x86-64-23.05.5",
		pkgRel:   "build/ipk/meshd_0.1.0_all.ipk",
		destPath: "/tmp/meshd.ipk",
		// /var/lock is normally created by procd at boot; create it so opkg can
		// take its lock in the bare container.
		installSh: "mkdir -p /var/lock /var/run && opkg install /tmp/meshd.ipk",
	},
	{
		name:     "apk-24.10",
		image:    "docker.io/openwrt/rootfs:x86_64-24.10.7",
		pkgRel:   "build/meshd-0.1.0-x86_64.apk",
		destPath: "/tmp/meshd.apk",
		// The repo's .apk is a plain gzip tar (not a signed apk package), so we
		// install it by extracting onto the real 24.10 userland.
		installSh: "tar -C / -xzf /tmp/meshd.apk",
	},
}

func TestEnrollmentE2E(t *testing.T) {
	clients := 5
	if v := os.Getenv("OMM_E2E_CLIENTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			clients = n
		}
	}

	for _, tg := range targets {
		tg := tg
		t.Run(tg.name, func(t *testing.T) {
			runE2E(t, tg, clients)
		})
	}
}

func runE2E(t *testing.T, tg target, clients int) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	pkgPath := filepath.Join(repoRoot(t), tg.pkgRel)
	if _, err := os.Stat(pkgPath); err != nil {
		t.Fatalf("package not built: %s (run ./scripts/build.sh and the package scripts first)", pkgPath)
	}

	net, err := tcnetwork.New(ctx)
	if err != nil {
		t.Fatalf("create network: %v", err)
	}
	t.Cleanup(func() { _ = net.Remove(ctx) })

	h := &harness{t: t, ctx: ctx, tg: tg, net: net.Name, pkgPath: pkgPath}

	// One daemon acts as the home-e2e controller; the rest are identical
	// daemons that the test will tell to enroll into it.
	controller := h.startDaemon("controller", map[string]string{
		"MESHD_HOME_ID": "home-e2e", "MESHD_SERIAL": "controller",
	})

	clientC := make([]testcontainers.Container, clients)
	var wg sync.WaitGroup
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			alias := fmt.Sprintf("client-%d", i)
			clientC[i] = h.startDaemon(alias, map[string]string{
				"MESHD_HOME_ID": "home-" + alias, "MESHD_SERIAL": alias,
			})
		}(i)
	}
	wg.Wait()

	// All clients enroll into the controller concurrently — the topology is
	// decided here, not by the container config.
	errs := make(chan error, clients)
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := h.join(clientC[i], "http://controller:8080", fmt.Sprintf("client-%d", i))
			if err != nil {
				errs <- fmt.Errorf("client-%d join: %w", i, err)
				return
			}
			if res.Status != "active" {
				errs <- fmt.Errorf("client-%d status %q", i, res.Status)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("%v", err)
	}

	// The controller's Home now holds exactly the N enrolled nodes, all unique.
	nodes := h.nodes(controller)
	if len(nodes) != clients {
		t.Fatalf("expected %d enrolled nodes, got %d: %+v", clients, len(nodes), nodes)
	}
	seen := map[string]bool{}
	for _, n := range nodes {
		if n.ID == "" || seen[n.ID] {
			t.Fatalf("missing/duplicate node id in %+v", nodes)
		}
		seen[n.ID] = true
		if n.CurrentHome != "home-e2e" {
			t.Fatalf("node %s in unexpected home %q", n.ID, n.CurrentHome)
		}
	}

	// A device is both: have the controller enroll into client-0's Home. The
	// controller now appears as a node in client-0's inventory while still
	// hosting home-e2e — both at once, just active in only one home.
	if _, err := h.join(controller, "http://client-0:8080", "controller"); err != nil {
		t.Fatalf("controller join client-0: %v", err)
	}
	if got := len(h.nodes(clientC[0])); got != 1 {
		t.Fatalf("client-0 should hold exactly the controller as a node, got %d", got)
	}
	t.Logf("%s: %d nodes enrolled concurrently; controller is also a node in client-0", tg.name, clients)
}

// harness builds and queries daemon containers for one target.
type harness struct {
	t       *testing.T
	ctx     context.Context
	tg      target
	net     string
	pkgPath string
}

func (h *harness) startDaemon(alias string, env map[string]string) testcontainers.Container {
	h.t.Helper()
	base := map[string]string{
		"MESHD_HTTP_ADDR":     "0.0.0.0:8080",
		"MESHD_AUTO_ADOPT":    "1",
		"MESHD_DATABASE_PATH": "/tmp/meshd.bolt",
		"MESHD_IDENTITY_DIR":  "/tmp/id",
		"MESHD_UDP_BROADCAST": "127.0.0.1:45678",
	}
	for k, v := range env {
		base[k] = v
	}

	c, err := testcontainers.GenericContainer(h.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          h.tg.image,
			Networks:       []string{h.net},
			NetworkAliases: map[string][]string{h.net: {alias}},
			ExposedPorts:   []string{"8080/tcp"},
			Env:            base,
			Files: []testcontainers.ContainerFile{{
				HostFilePath: h.pkgPath, ContainerFilePath: h.tg.destPath, FileMode: 0o644,
			}},
			Entrypoint: []string{"/bin/sh", "-c", h.tg.installSh + " && exec /usr/bin/meshd"},
			WaitingFor: wait.ForHTTP("/health").WithPort("8080/tcp").WithStartupTimeout(120 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		h.t.Fatalf("start daemon %s: %v", alias, err)
	}
	h.t.Cleanup(func() { _ = c.Terminate(h.ctx) })
	return c
}

type joinResult struct {
	Status string `json:"status"`
}

func (h *harness) join(c testcontainers.Container, controllerURL, serial string) (joinResult, error) {
	h.t.Helper()
	body, _ := json.Marshal(map[string]string{"controller_url": controllerURL, "serial": serial})
	resp, err := http.Post(h.baseURL(c)+"/enroll/join", "application/json", bytes.NewReader(body))
	if err != nil {
		return joinResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return joinResult{}, fmt.Errorf("status %d: %s", resp.StatusCode, msg)
	}
	var r joinResult
	return r, json.NewDecoder(resp.Body).Decode(&r)
}

type node struct {
	ID          string `json:"id"`
	CurrentHome string `json:"current_home"`
}

func (h *harness) nodes(c testcontainers.Container) []node {
	h.t.Helper()
	resp, err := http.Get(h.baseURL(c) + "/nodes")
	if err != nil {
		h.t.Fatalf("get nodes: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		Nodes []node `json:"nodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		h.t.Fatalf("decode nodes: %v", err)
	}
	return body.Nodes
}

func (h *harness) baseURL(c testcontainers.Container) string {
	h.t.Helper()
	host, err := c.Host(h.ctx)
	if err != nil {
		h.t.Fatalf("host: %v", err)
	}
	port, err := c.MappedPort(h.ctx, "8080/tcp")
	if err != nil {
		h.t.Fatalf("port: %v", err)
	}
	return fmt.Sprintf("http://%s:%s", host, port.Port())
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
