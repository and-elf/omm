# LuCI integration testing

`luci-app-meshd` is the thin layer that lets the PWA reach meshd from inside
LuCI: the browser POSTs JSON-RPC to LuCI's authenticated `/ubus` endpoint, rpcd
checks the session against the `luci-app-meshd` ACL, and the `meshd` rpcd exec
plugin proxies the call to meshd's localhost management API. The unit tests
([`internal/luci/plugin_test.go`](../internal/luci/plugin_test.go)) cover the
exec plugin against an HTTP stub, but nothing exercised the *real* path
(uhttpd `/ubus` → session auth → rpcd ACL → exec plugin → meshd) for the
operator workflows the UI actually drives.

`TestLuCIWorkflowE2E` ([`internal/e2e/luci_e2e_test.go`](../internal/e2e/luci_e2e_test.go))
closes that gap. It boots a real OpenWrt userland in a container with the built
`meshd` + `luci-app-meshd` packages installed, brings up the full LuCI service
stack (ubusd, rpcd, uhttpd with the ubus handler), and drives meshd through the
authenticated `/ubus` endpoint exactly as the PWA's ubus transport
([`web/src/api/ubus.ts`](../web/src/api/ubus.ts)) does.

## What it verifies

The single test logs into a LuCI session (root, granted *only* the
`luci-app-meshd` ACL scope) and exercises three workflow layers over `/ubus`:

1. **ACL gate** — an authenticated session reaches `meshd.status`; an
   unauthenticated (zero) session is denied. This proves the shipped ACL file
   grants the methods the UI needs and nothing reaches meshd without it.

2. **Node enrollment + adopt** — a second `meshd` daemon (its own Home) joins
   the controller over the mesh plane and lands as a *pending* enrollment
   (controller runs with auto-adopt off). The test then, over `/ubus`:
   - lists `enrollments` and finds the pending node,
   - `adopt_node`s it,
   - lists `nodes` and confirms the node is now a member of the controller's
     Home — the exact "a node showed up, approve it" flow an operator performs
     in the UI.

3. **Home/profile lifecycle** — `status`, `homes`, `create_home`, `get_home`,
   `save_profile`/`get_profile`, `set_active_home`/`active_home` and
   `delete_home`, the management calls the PWA makes.

4. **Wireless client devices** — a stub `hostapd.ap0` ubus object advertises an
   associated station; the test reads `topology` over `/ubus` and confirms the
   client device surfaces in the graph, verifying meshd's hostapd read flows
   all the way to the UI transport.

## Running it

The test carries the `e2e` build tag and runs in the `e2e` CI job. Locally:

```sh
SKIP_FRONTEND=1 ./scripts/build.sh
./scripts/package-ipk.sh
./scripts/package-luci-ipk.sh
go test -tags e2e -run TestLuCIWorkflowE2E -timeout 25m -v ./internal/e2e/...
```

It needs a container runtime testcontainers can reach (Docker on CI, podman
locally) with network access to pull the OpenWrt rootfs image and install
`uhttpd`/`rpcd` from the feed (plus `curl`, used only by the harness's own
readiness/`/ubus` probes — the rpcd plugin itself talks to meshd via busybox
`nc`, so the shipped package has no curl dependency). The narrower
[`scripts/verify-luci-ubus.sh`](../scripts/verify-luci-ubus.sh) remains a quick
host-side check of the same `/ubus` path (status + ACL only).
