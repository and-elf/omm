//go:build e2e

// Package e2e runs the enrollment flow against real OpenWrt container images
// with the built package installed. It is gated behind the `e2e` build tag and
// requires a Docker-compatible runtime (Docker or a podman socket).
//
// Run with:
//
//	./scripts/build.sh && ./scripts/package-ipk.sh && ./scripts/package-apk.sh
//	go test -tags e2e -timeout 20m ./internal/e2e/...
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
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
		pkgRel:   "build/meshd-0.1.0.apk",
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

	pkgFile := testcontainers.ContainerFile{
		HostFilePath:      pkgPath,
		ContainerFilePath: tg.destPath,
		FileMode:          0o644,
	}

	// Controller.
	controllerScript := fmt.Sprintf(
		"%s && MESHD_ROLE=controller MESHD_HTTP_ADDR=0.0.0.0:8080 "+
			"MESHD_DATABASE_PATH=/tmp/meshd.db MESHD_HOME_ID=home-e2e MESHD_AUTO_ADOPT=1 "+
			"exec /usr/bin/meshd", tg.installSh)

	controller, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          tg.image,
			Networks:       []string{net.Name},
			NetworkAliases: map[string][]string{net.Name: {"controller"}},
			ExposedPorts:   []string{"8080/tcp"},
			Files:          []testcontainers.ContainerFile{pkgFile},
			Entrypoint:     []string{"/bin/sh", "-c", controllerScript},
			WaitingFor:     wait.ForHTTP("/health").WithPort("8080/tcp").WithStartupTimeout(120 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start controller: %v", err)
	}
	t.Cleanup(func() { _ = controller.Terminate(ctx) })

	// N clients enrolling concurrently.
	var wg sync.WaitGroup
	errs := make(chan error, clients)
	containers := make([]testcontainers.Container, clients)
	var mu sync.Mutex

	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			clientScript := fmt.Sprintf(
				"%s && MESHD_ROLE=client MESHD_CONTROLLER=http://controller:8080 "+
					"MESHD_IDENTITY_DIR=/tmp/id MESHD_SERIAL=client-%d "+
					"exec /usr/bin/meshd", tg.installSh, i)

			c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
				ContainerRequest: testcontainers.ContainerRequest{
					Image:      tg.image,
					Networks:   []string{net.Name},
					Files:      []testcontainers.ContainerFile{pkgFile},
					Entrypoint: []string{"/bin/sh", "-c", clientScript},
					WaitingFor: wait.ForLog("node active").WithStartupTimeout(150 * time.Second),
				},
				Started: true,
			})
			if err != nil {
				errs <- fmt.Errorf("client-%d: %w", i, err)
				return
			}
			mu.Lock()
			containers[i] = c
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	close(errs)

	for _, c := range containers {
		if c != nil {
			c := c
			t.Cleanup(func() { _ = c.Terminate(ctx) })
		}
	}
	for err := range errs {
		t.Fatalf("client enrollment failed: %v", err)
	}

	// Assert the controller inventory contains exactly the N enrolled nodes.
	base, err := controllerBaseURL(ctx, controller)
	if err != nil {
		t.Fatalf("controller url: %v", err)
	}

	nodes := fetchNodes(t, base)
	if len(nodes) != clients {
		t.Fatalf("expected %d enrolled nodes, got %d: %+v", clients, len(nodes), nodes)
	}
	seen := map[string]bool{}
	for _, n := range nodes {
		if n.ID == "" {
			t.Fatalf("node with empty id: %+v", n)
		}
		if seen[n.ID] {
			t.Fatalf("duplicate node id %s", n.ID)
		}
		seen[n.ID] = true
		if n.CurrentHome != "home-e2e" {
			t.Fatalf("node %s in unexpected home %q", n.ID, n.CurrentHome)
		}
	}
	t.Logf("%s: %d nodes enrolled concurrently, all unique", tg.name, len(nodes))
}

type node struct {
	ID          string `json:"id"`
	Serial      string `json:"serial"`
	CurrentHome string `json:"current_home"`
}

func controllerBaseURL(ctx context.Context, c testcontainers.Container) (string, error) {
	host, err := c.Host(ctx)
	if err != nil {
		return "", err
	}
	port, err := c.MappedPort(ctx, "8080/tcp")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("http://%s:%s", host, port.Port()), nil
}

func fetchNodes(t *testing.T, base string) []node {
	t.Helper()
	resp, err := http.Get(base + "/nodes")
	if err != nil {
		t.Fatalf("get nodes: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		Nodes []node `json:"nodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode nodes: %v", err)
	}
	return body.Nodes
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine caller")
	}
	// internal/e2e/e2e_test.go -> repo root is two levels up.
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
