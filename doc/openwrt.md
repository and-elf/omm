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
| `/etc/init.d/meshd` | procd init script ([`package/meshd.init`](../package/meshd.init)) |
| `/etc/config/meshd` | UCI config consumed by the init script ([`package/meshd.config`](../package/meshd.config)) |
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
  `Makefile`s (e.g. [`package/luci-app-meshd/Makefile`](../package/luci-app-meshd/Makefile)),
  which OpenWrt's build infrastructure compiles for every architecture. A
  `meshd` package `Makefile` (using `golang-package.mk`) is the next packaging
  step toward an official-feed submission.
