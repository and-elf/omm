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

> **Current limitation.** The LuCI app is in place, but meshd still serves its
> management API on the network (`0.0.0.0:8080`), so the LuCI gate is not yet
> the *only* way in. Binding the management plane to localhost — so rpcd/LuCI
> is the real front door — and adding mesh-plane mTLS are the next steps.

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
- **opkg feed.** The release workflow also runs
  [`scripts/make-feed-index.sh`](../scripts/make-feed-index.sh) over the built
  `.ipk`s to publish an opkg index (`Packages`/`Packages.gz`) as release assets,
  so a release doubles as a feed (see
  [Releases & Installation](../README.md#releases--installation)).

  Two follow-ups: the index is **unsigned** (add usign signing + ship the public
  key so `opkg` can verify it), and the feed URL is **per-release** (a stable
  rolling feed — e.g. a `gh-pages` branch aggregating versions — would let
  `opkg update` track new releases without editing `customfeeds.conf`).
