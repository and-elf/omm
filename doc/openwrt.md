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
   network): secured by **mutual TLS** rooted in a per-Home CA (implemented; see
   the [Security Model](security.md)), independent of LuCI.

> **Status.** The plane split and mesh-plane mutual TLS both exist: in split
> mode the mesh listener serves mTLS and rejects uncertified clients on
> post-enrollment routes, and the management API *can* bind to localhost. What
> remains is making localhost the *default* — the shipped config still runs
> combined mode and the LuCI view iframes the PWA (which only works while
> management is network-reachable). That flip depends on the LuCI-native ubus UI
> (so the browser reaches management through LuCI's authenticated session rather
> than a direct port). Until then the LuCI ACL gate is real but not the *only*
> door to management.

## Packaging

- **Prebuilt packages** for direct download are produced by the release
  workflow: the `meshd` daemon (per arch) from
  [`scripts/package-ipk.sh`](../scripts/package-ipk.sh) /
  [`scripts/package-apk.sh`](../scripts/package-apk.sh), and the
  architecture-independent `luci-app-meshd` from
  [`scripts/package-luci-ipk.sh`](../scripts/package-luci-ipk.sh). All are
  attached to the GitHub Release and listed in the feed index, so
  `opkg install meshd luci-app-meshd` works from the release feed. See
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

### Enabling signed feeds (maintainer, one-time)

The release workflow already signs the feed index *when* a key is configured;
this is the one-time setup to provide it.

1. **Generate the keypair** with [`scripts/gen-feed-key.sh`](../scripts/gen-feed-key.sh)
   (uses local `usign`, or generates inside an OpenWrt container):

   ```sh
   ./scripts/gen-feed-key.sh        # writes omm-feed.sec (secret) + omm-feed.pub
   ```

2. **Store the secret key** as the `OPKG_SIGN_KEY` Actions secret — never commit
   it; keep an offline backup:

   ```sh
   gh secret set OPKG_SIGN_KEY < omm-feed.sec
   rm omm-feed.sec                  # after backing it up
   ```

3. **Commit the public key** so the release workflow publishes it as an asset:

   ```sh
   cp omm-feed.pub package/omm-feed.pub
   git add package/omm-feed.pub && git commit -m "chore: add feed signing public key"
   ```

From then on every release signs `Packages` (→ `Packages.sig`) and attaches
`omm-feed.pub`. The signing is also exercised independently of opkg by
[`scripts/verify-luci-ubus.sh`](../scripts/verify-luci-ubus.sh)'s sibling check:
`usign -G`/`-S`/`-V` round-trips and a tampered index is rejected.

### Trusting the feed (device, one-time)

```sh
# fetch the published public key, then trust it
wget https://github.com/and-elf/omm/releases/download/<tag>/omm-feed.pub
opkg-key add omm-feed.pub
echo 'src/gz omm https://github.com/and-elf/omm/releases/download/<tag>' >> /etc/opkg/customfeeds.conf
opkg update            # now signature-verified
```

> Bootstrapping note: a device must trust the key (`opkg-key add`, or bake the
> key into the firmware image) **before** the first `opkg update`, since the
> index it downloads is what the signature protects.
