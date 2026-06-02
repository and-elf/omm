# OpenWrt Integration & Packaging

How meshd installs and integrates on an OpenWrt device, and how it is packaged
for distribution. See the [README](../README.md) for the project overview and
[Releases & Installation](../README.md#releases--installation) for the prebuilt
download packages.

---

## On-device layout

| Path | Purpose |
|------|---------|
| `/usr/bin/meshd` | The daemon (static, CGO-free Go binary) |
| `/etc/init.d/meshd` | procd init script ([`package/meshd/files/meshd.init`](../package/meshd/files/meshd.init)) |
| `/etc/config/meshd` | UCI config consumed by the init script ([`package/meshd/files/meshd.config`](../package/meshd/files/meshd.config)) |
| `/etc/meshd/` | Database (`meshd.bolt`) and device identity (`identity/`) |

The init script maps UCI options to the `MESHD_*` environment the daemon reads.

### Management / mesh plane split

meshd separates two HTTP planes:

- **Management plane** — admin/UI: `/status`, `/setup*`, `/homes*`, `/nodes*`,
  `/active-home`, `/home-selection`, `/scan`, `/reset`, the pending-enrollment
  list + adopt/reject, `/topology` (GET), and the PWA. Intended to bind to
  **localhost** and be reached only through LuCI.
- **Mesh control plane** — node-to-node: `/enroll/{request,verify,…,ack}`,
  `/topology/report`, `GET /homes/{id}` (joined-Home metadata), `/health`. Stays
  **network-reachable** on every controller.

Configuration (UCI `meshd.main`, mapped to env):

| Mode | UCI | Env | Behaviour |
|------|-----|-----|-----------|
| Combined (default) | `http_addr` | `MESHD_HTTP_ADDR` | one server, both planes on one address |
| Split | `mgmt_addr` + `mesh_addr` (remove `http_addr`) | `MESHD_MGMT_ADDR` (`127.0.0.1:8080`) + `MESHD_MESH_ADDR` (`0.0.0.0:8081`) | management on localhost, mesh on the network |

The discovery announcement carries the **mesh-facing** address, so joining nodes
reach the control plane regardless of mode.

## LuCI integration (`luci-app-meshd`)

Lives under [`package/luci-app-meshd/`](../package/luci-app-meshd/):

- **rpcd exec plugin** (`/usr/libexec/rpcd/meshd`) — exposes meshd's management
  API as the `meshd` **ubus object**. rpcd registers it and `ubus`/LuCI calls
  proxy to meshd's local HTTP API.
- **ACL** (`/usr/share/rpcd/acl.d/luci-app-meshd.json`) — grants the LuCI app
  read access to the query methods and write access to the mutations.
- **menu + view** — an `admin/network/meshd` entry hosting the embedded PWA.

The plugin's request/response contract is exercised by tests in
[`internal/luci`](../internal/luci) (the script is run against a stub meshd
server), so the ubus surface and its ACL stay in sync.

## Authentication model — two trust boundaries

1. **Management plane** (an admin managing this device): authenticated by
   **LuCI** — the rpcd session plus the ACL above gate every `meshd` ubus call.
2. **Mesh control plane** (node ↔ controller enrollment and topology over the
   network): **not** covered by LuCI. This is meshd-to-meshd and needs the
   certificate-based mutual auth described in the [Security Model](security.md).

> **Status.** The plane split exists (see above) so the management API *can*
> bind to localhost today. It is not yet the *default* (the shipped config runs
> combined mode), and the LuCI view still iframes the PWA — which only works
> while management is network-reachable. Making localhost the default depends on
> the LuCI-native ubus UI (so the browser reaches management through LuCI's
> authenticated session, not a direct port); mesh-plane mTLS is the parallel
> next step. Until then the LuCI ACL gate is real but not yet the *only* door.

## Packaging

- **Prebuilt packages** for direct download are produced by the release
  workflow from [`scripts/package-ipk.sh`](../scripts/package-ipk.sh) and
  [`scripts/package-apk.sh`](../scripts/package-apk.sh); see
  [Releases & Installation](../README.md#releases--installation).
- **Feed packages** for the official OpenWrt / LuCI feeds are described by
  `Makefile`s — [`package/meshd/Makefile`](../package/meshd/Makefile) (the
  daemon, via `golang-package.mk`) and
  [`package/luci-app-meshd/Makefile`](../package/luci-app-meshd/Makefile) (the
  LuCI app, via `luci.mk`). OpenWrt's build infrastructure compiles these for
  every architecture, so the per-arch matrix in the release workflow is only
  needed for the direct-download packages. Bump `PKG_VERSION`/`PKG_HASH` in
  [`package/meshd/Makefile`](../package/meshd/Makefile) per release before
  submitting to a feed.
- **opkg feed.** The release workflow runs
  [`scripts/make-feed-index.sh`](../scripts/make-feed-index.sh) over the built
  `.ipk`s to publish an opkg index (`Packages`/`Packages.gz`) as release assets,
  so a release doubles as a feed (see
  [Releases & Installation](../README.md#releases--installation)).
- **Feed signing.** When the `OPKG_SIGN_KEY` repository secret is set, the
  workflow signs the index with usign ([`scripts/sign-feed.sh`](../scripts/sign-feed.sh))
  and attaches `Packages.sig`, which `opkg` verifies on `opkg update`. Without
  the secret the feed is published unsigned.

  Remaining follow-up: the feed URL is **per-release**; a stable rolling feed —
  e.g. a `gh-pages` branch aggregating versions — would let `opkg update` track
  new releases without editing `customfeeds.conf`.

### Enabling signed feeds (maintainer)

One-time setup with the [usign](https://github.com/openwrt/usign) tool:

```sh
usign -G -s omm-feed.sec -p omm-feed.pub    # generate the keypair
```

1. Store the **secret** key (`omm-feed.sec` contents, both lines) as the
   `OPKG_SIGN_KEY` Actions secret. Never commit it.
2. Publish the **public** key (`omm-feed.pub`) — e.g. as a release asset or in
   the repo — so devices can `opkg-key add omm-feed.pub` to trust the feed.
3. Future releases are signed automatically.

> Bootstrapping note: to install from a *signed* feed a device must already
> trust the key, so add it with `opkg-key add` (or bake it into the firmware
> image) before the first `opkg update`.
