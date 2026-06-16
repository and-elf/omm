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

## Wireless backhaul requirements (802.11s)

OMM forms its wireless backhaul with **802.11s** (`mode 'mesh'`, SAE), authored
on the `omm_mesh` interface (see [Profiles](profiles.md) and the two-tier
[Backhaul & Mesh Model](network-model.md#backhaul--mesh-model)). 802.11s mesh
mode is **not** supported by the `wpad` variant that ships in stock OpenWrt
images, so — like [`mesh11sd`](https://github.com/openwrt/packages/tree/master/net/mesh11sd) —
a mesh node requires a **mesh-capable `wpad`**:

| `wpad` variant | 802.11s mesh? | Notes |
|----------------|---------------|-------|
| `wpad-basic-mbedtls` / `wpad-basic-wolfssl` | **No** | Default in images (23.05 / 22.03). AP+STA+SAE, but no mesh. |
| `wpad-mesh-mbedtls` / `wpad-mesh-wolfssl` / `wpad-mesh-openssl` | **Yes** | Adds 802.11s + SAE. Pick the variant matching your image's crypto backend. |
| `wpad-wolfssl` / `wpad-openssl` (full) | **Yes** | Everything, incl. mesh and WPA-Enterprise. |

Get a mesh-capable `wpad` onto each mesh device one of three ways:

- **Bake it into the firmware image (recommended for a fleet / offline nodes).**
  Add it to the image so every device — including nodes with no internet until
  they join — has it from first boot. With the image builder:

  ```sh
  make image PROFILE=<board> \
    PACKAGES="meshd luci-app-meshd -wpad-basic-mbedtls wpad-mesh-mbedtls"
  ```
  (Match your image's crypto backend: `-mbedtls`/`-wolfssl`/`-openssl`. The
  leading `-` drops the default basic variant it conflicts with.) For multi-hop
  routing add the batman-adv stack: `kmod-batman-adv` (the kernel module, which
  also ships the netifd `batadv`/`batadv_hardif` protocol handlers meshd authors
  — there is no separate userspace `batman-adv` package) and `batctl` (topology
  reads). Without these meshd falls back to bridging the mesh directly onto `lan`
  (single-hop only).

- **On a live device** (online — e.g. a controller, or a node after it has
  joined the home LAN): `scripts/deploy.sh <host> --install-dependencies` detects
  the package manager, swaps `wpad-basic-*` for the matching `wpad-mesh-*` (by
  the installed crypto variant), and installs the `kmod-batman-adv`/`batctl`
  routing stack. Or by hand:

  ```sh
  # OpenWrt 25+/snapshot (apk) — apk swaps the conflicting wpad provider for you:
  apk update && apk add wpad-mesh-mbedtls          # match your crypto

  # OpenWrt <=24.10 (opkg) — remove the basic variant first:
  opkg update && opkg remove wpad-basic-mbedtls && opkg install wpad-mesh-mbedtls
  ```

  An airgapped device with no internet on the LAN side can instead route through
  a laptop (share its uplink and point the device's default route at it), or
  install a downloaded package file locally (`apk add ./wpad-mesh-*.apk` /
  `opkg install ./wpad-mesh-*.ipk`).

> **Why it isn't a hard package dependency.** All `wpad`/`hostapd`/
> `wpa-supplicant` variants `PROVIDES: wpad` and conflict with one another, so a
> `DEPENDS: +wpad-mesh-*` would force-swap whatever the image shipped, break on a
> different crypto backend, and pull `wpad` onto radio-less wired controllers
> that don't need it. So it is **documented** (and the image-builder line above
> makes it explicit) rather than hard-pinned, matching `mesh11sd`'s approach.

**Without** a mesh-capable `wpad`, the `omm_mesh` interface cannot start and the
node degrades to the wired multi-AP tier (`omm_ap` on `lan`) — see
[Tier 2](network-model.md#tier-2--wired-multi-ap-degraded). A node reached only
over the air (e.g. a detached garage AP) therefore **requires** a mesh-capable
`wpad`.

## Routing layer requirements (batman-adv)

For loop-free **multi-hop** routing (and to run a wired link and the wireless
mesh on the same node without a bridge loop), meshd authors a **batman-adv**
layer — a `bat0` soft interface and a batadv hard interface per backhaul link —
instead of bridging the mesh straight onto `lan`. See the
[batman-adv routing layer](network-model.md#batman-adv-routing-layer). This needs
two packages on each mesh device:

| Package | Why |
|---------|-----|
| `kmod-batman-adv` | The kernel module **and** the netifd `batadv` / `batadv_hardif` protocol handlers (`/lib/netifd/proto/batadv*.sh`) that meshd drives via UCI. There is **no** separate userspace `batman-adv` package — the proto handlers ship inside the kmod. |
| `batctl` | Userspace control/inspection tool; meshd reads mesh originators (`batctl o`) for the topology view. |

Bake them into the image alongside `wpad-mesh-*` (see the image-builder line
above), or install on a live device with
`scripts/deploy.sh <host> --install-dependencies`.

> **Gotcha: restart netifd after installing.** netifd loads its protocol handlers
> **only at process start**. If `kmod-batman-adv` is installed *after* netifd is
> already running, the running netifd does not know the `batadv` proto: the
> `bat0` interface stays `proto none` and the device is never created, so meshd's
> verification degrades it to the direct `lan` bridge. A plain `network reload`
> (or `ubus call network reload`) does **not** reload proto handlers — only a
> netifd restart (`/etc/init.d/network restart`) or a reboot does.
> `--install-dependencies` restarts netifd for exactly this reason; a baked-in
> image avoids the problem entirely (the handlers are present at first boot).

> **Why it isn't a hard package dependency.** `kmod-batman-adv` is a
> board-/kernel-specific module, and a radio-less or single-uplink wired node
> never needs the routing layer. So — like the `wpad-mesh` requirement above — it
> is documented and opt-in rather than a `DEPENDS`. batman-adv stays **on by
> default** (`MESHD_BATMAN`) and **degrades gracefully**: when the module/proto is
> absent, meshd verifies that `bat0` did not come up and bridges the mesh directly
> onto `lan` (single-hop), so a node without the stack still works — it just
> cannot route multi-hop.

## Status LED

meshd drives a single status LED from the node's onboarding state so an
installer can read it off the device without a companion app:

| State | Condition | LED |
|-------|-----------|-----|
| Unclaimed | setup not complete | blinking (`timer` trigger) |
| Enrolling | claimed, no active home yet | pulsing (`heartbeat` trigger) |
| Active | active home applied | solid on |

The LED is the kernel sysfs LED named by `led_name` (`MESHD_LED_NAME`, default
`green:status`) under `/sys/class/leds/`. LED names are hardware-specific; if the
configured LED is absent the daemon simply does nothing (no error), so the same
build runs across boards. Set `led_enabled '0'` (`MESHD_LED=0`) to leave the LED
alone.

| UCI | Env | Default | Behaviour |
|-----|-----|---------|-----------|
| `led_enabled` | `MESHD_LED` | `1` | drive the status LED |
| `led_name` | `MESHD_LED_NAME` | `green:status` | `/sys/class/leds/<name>` to drive |

## LuCI integration (`luci-app-meshd`)

Lives under [`package/luci-app-meshd/`](../package/luci-app-meshd/):

- **rpcd exec plugin** (`/usr/libexec/rpcd/meshd`) — exposes meshd's management
  API as the `meshd` **ubus object**. rpcd registers it and `ubus`/LuCI calls
  proxy to meshd's local HTTP API.
- **ACL** (`/usr/share/rpcd/acl.d/luci-app-meshd.json`) — grants the LuCI app
  read access to the query methods and write access to the mutations.
- **menu + view + PWA** — an `admin/network/meshd` entry whose view iframes the
  PWA, which is shipped as LuCI static resources (`view/meshd/pwa/`) and served
  by uhttpd. The view passes LuCI's session token via the iframe URL hash; the
  PWA reads it and talks to meshd through LuCI's authenticated `/ubus`, so it
  works even when meshd's management API is localhost-bound.
- **uci-defaults** (`/etc/uci-defaults/99-luci-app-meshd`) — on install, flips a
  still-default (combined) meshd to the secure posture: management on localhost,
  mesh control plane network-facing with mutual TLS. Bare `meshd` (no LuCI) is
  left in combined mode so it stays remotely manageable.

The plugin's request/response contract is exercised by tests in
[`internal/luci`](../internal/luci) (the script is run against a stub meshd
server), so the ubus surface and its ACL stay in sync.

## Authentication model — two trust boundaries

1. **Management plane** (an admin managing this device): authenticated by
   **LuCI** — the rpcd session plus the ACL above gate every `meshd` ubus call.
2. **Mesh control plane** (node ↔ controller enrollment and topology over the
   network): secured by **mutual TLS** rooted in a per-Home CA (implemented; see
   the [Security Model](security.md)), independent of LuCI.

> **Status.** Complete. Bare `meshd` defaults to combined mode (single port,
> network-reachable PWA) so a headless node stays manageable. Installing
> `luci-app-meshd` flips the device (via uci-defaults) to **split + localhost
> management + mesh mutual-TLS**, and serves the PWA through LuCI's
> authenticated `/ubus` — so management is no longer reachable on the network,
> only through the LuCI session. The on-device browser behaviour (SPA in the
> iframe) is the one part not covered by automated tests; the static serving,
> the `/ubus` ACL path, the cert/mTLS layer and the config flip are all
> verified (unit + integration + real-OpenWrt container).

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
- **apk packages & feed.** The same release builds real, signed apk-v3 packages
  ([`scripts/package-apk.sh`](../scripts/package-apk.sh)) and a signed
  `packages.adb` apk index ([`scripts/make-apk-index.sh`](../scripts/make-apk-index.sh)),
  for OpenWrt's apk userland (snapshot/25.x). Signing is opt-in via the
  `APK_SIGN_KEY` secret. See [apk packages and signing](#apk-packages-and-signing-2410)
  below.

### apk packages and signing (24.10+)

Everything above is the **opkg** path (`.ipk`, OpenWrt ≤23.05 and 24.10's opkg).
OpenWrt's newer **apk** package manager (apk-tools 3, default on snapshot/25.x)
uses a separate scheme, fully supported here:

- The `.apk` files are **real, signed apk-v3 packages** built with `apk mkpkg`
  ([`scripts/package-apk.sh`](../scripts/package-apk.sh)) — installable with
  `apk add` and signature-verified, not the old extract-only tarball.
- apk verifies each **package** (and the index) against EC public keys in
  `/etc/apk/keys/`, matched by key *content* (the on-device filename is
  irrelevant). This is a different scheme from usign, so the apk keypair is
  independent of the `omm-feed.pub` usign key used for the opkg feed.
- The release also publishes a signed apk repository index — `packages.adb`
  ([`scripts/make-apk-index.sh`](../scripts/make-apk-index.sh)) — so the release
  doubles as an apk feed, just like the opkg `Packages` index.

apk-tools is a build-host tool (not shipped on-device); the scripts and CI build
it from upstream Alpine sources pinned to the version OpenWrt ships, via
[`scripts/get-apk-tools.sh`](../scripts/get-apk-tools.sh) — the same approach
used to build usign for opkg feed signing. The sign→verify round-trip (plus
rejection of a wrong key, in-body corruption, and a tampered index) is exercised
by [`scripts/verify-apk-signing.sh`](../scripts/verify-apk-signing.sh).

> On-device note: as of this writing no published OpenWrt rootfs image ships apk
> on-device yet (24.10 and snapshot still use opkg), so end-to-end `apk add` on a
> real device is verified manually until such an image exists. The host-side
> signing round-trip above is the automated CI gate.

### Enabling the signed opkg feed (maintainer, one-time)

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

### Trusting the opkg feed (device, one-time)

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

### Enabling apk signing (maintainer, one-time)

Analogous to the opkg setup, with an EC keypair instead of usign.

1. **Generate the keypair** with [`scripts/gen-apk-key.sh`](../scripts/gen-apk-key.sh)
   (pure `openssl`, EC `prime256v1` — OpenWrt's apk curve):

   ```sh
   ./scripts/gen-apk-key.sh         # writes omm-apk.key (secret) + omm-apk.pub
   ```

2. **Store the secret key** as the `APK_SIGN_KEY` Actions secret — never commit
   it; keep an offline backup:

   ```sh
   gh secret set APK_SIGN_KEY < omm-apk.key
   rm omm-apk.key                   # after backing it up
   ```

3. **Commit the public key** so the release workflow publishes it as an asset:

   ```sh
   cp omm-apk.pub package/omm-apk.pub
   git add package/omm-apk.pub && git commit -m "chore: add apk signing public key"
   ```

From then on every release signs each `.apk` and the `packages.adb` index, and
attaches `omm-apk.pub`. Without the secret the apk artifacts are built unsigned.

### Trusting the apk feed (device, one-time)

```sh
# fetch the published public key and drop it in apk's trusted-keys dir
wget https://github.com/and-elf/omm/releases/download/<tag>/omm-apk.pub
cp omm-apk.pub /etc/apk/keys/omm-apk.pub
# either install a single package directly…
wget https://github.com/and-elf/omm/releases/download/<tag>/meshd-<version>-<arch>.apk
apk add ./meshd-<version>-<arch>.apk
# …or add the release as a feed (signed packages.adb) and let apk resolve it
echo 'https://github.com/and-elf/omm/releases/download/<tag>' >> /etc/apk/repositories.d/customfeeds.list
apk update && apk add meshd
```

> apk matches a trusted key by its *contents*, so the filename under
> `/etc/apk/keys/` is arbitrary; the key must be present before installing, since
> it is what the package/index signature is verified against.
