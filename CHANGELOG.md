# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.0] - 2026-06-07

### Added
- **Backhaul connection type in the topology graph.** Each node now reports how
  it reaches the mesh — over a wired ethernet uplink or the wireless mesh —
  derived from a configured uplink interface under `/sys/class/net`
  (`carrier`/`operstate`). The value rides on the node's `self` vertex, is
  preserved by the controller-side aggregator when that node is merged into the
  mesh-wide graph, and surfaces as a `backhaul` field on a topology node; the
  Topology view marks wired nodes with a solid border and wireless with a dashed
  one. Set the uplink interface with `MESHD_BACKHAUL_IFACE` (UCI `backhaul_iface`);
  empty => `unknown`. New `internal/topology/backhaul.go`.
- **Status LED reflecting onboarding state.** `meshd` now drives a node's status
  LED from its onboarding state so an installer can read it off the device
  without a companion app: blinking while unclaimed, a heartbeat while it joins,
  solid once a home is active. The LED is the kernel sysfs LED named by
  `MESHD_LED_NAME` (UCI `led_name`, default `green:status`); a board lacking that
  LED is a graceful no-op, so the same build runs unchanged across hardware. Set
  `MESHD_LED=0` (UCI `led_enabled '0'`) to leave the LED alone. New
  `internal/deviceled` package.
- **Wired auto-onboard.** An unclaimed node that is on the wire (ethernet
  backhaul) can now enroll into a discovered controller unattended, with no setup
  wizard: when it is still unclaimed, its ethernet uplink is up, and a controller
  other than its own Home has been discovered, it joins (the lowest discovered
  `home_id`, chosen deterministically), applies the returned profile, marks setup
  complete and tears down the first-boot setup AP. Opt-in via
  `MESHD_AUTO_ONBOARD_WIRED` (UCI `auto_onboard_wired`, default off); requires
  `MESHD_BACKHAUL_IFACE` to be set, runs only when no explicit `MESHD_JOIN`
  controllers are configured, and completes unattended only when the controller
  auto-adopts. New `internal/onboard` package.

## [0.2.0] - 2026-06-03

### Added
- **First-boot setup access point.** While a device is unclaimed (setup not
  complete) `meshd` now brings up a known, label-printable WiFi AP
  (`OMM-Setup-<last4-of-node-id>`) on a small static network
  (`192.168.254.1/24`) serving its open management API, so a companion app can
  reach an out-of-the-box node before it has joined any network. The AP is torn
  down automatically once onboarding completes. Open by default; set
  `MESHD_SETUP_AP_KEY` for WPA2, `MESHD_SETUP_AP_RADIO` to choose the radio, or
  `MESHD_SETUP_AP=0` to disable (e.g. a radio-less wired controller). New
  `internal/setupap` package; `uci.Client` gained `SetSection`/`Delete`. Covered
  by a real-OpenWrt-container e2e (`TestSetupAPLifecycleE2E`) asserting the uci
  sections appear on boot and are removed once onboarding completes.
- **Companion onboarding app.** The Vue frontend now also builds as a
  cross-platform companion app (Capacitor: Android/iOS/desktop), whose first goal
  is to make adding a node a few-tap flow. It discovers controllers on the LAN
  (native mDNS `_mesh._tcp`, falling back to the daemon's `/scan`), can target a
  specific device over the LAN (an unclaimed node's setup-AP address or a
  controller's announced URL), and guides adding a node end to end (`/onboard`):
  read the device's setup label (QR — OMM-JSON, the `WIFI:` standard, or a bare
  SSID), join its setup AP, reach it, request enrollment (`/enroll/join`), and
  confirm adoption — including signing in to a split-mode controller (LuCI
  `session.login` over `/ubus`) when its management API is localhost-bound. The
  native capabilities (mDNS / WiFi-join / QR scan) sit behind a bridge that is a
  no-op in the browser, so the existing PWA is unchanged. See the design spec in
  [doc/companion-app.md](doc/companion-app.md).
- **Guided onboarding wizard + wireless-only enrollment.** The onboarding flow is
  now a three-page wizard — choose Home → choose device → confirm — that
  auto-progresses between steps and adopts the node in the background once the
  app holds a controller client, with no manual approve step. A node without an
  Ethernet uplink can now be enrolled over WiFi: `meshd` gains
  `POST /setup/uplink` (unclaimed devices only) which brings up a station
  `wifi-iface` + DHCP-client network from supplied home-WiFi credentials so the
  node can reach its controller, torn down with the setup AP once onboarding
  completes (`internal/setupap` `EnableUplink`). On Android the wizard offers a
  setup-AP picker (`OMM-Setup-*`); iOS/web keep the QR/manual path. Covered by a
  real-OpenWrt-container e2e (`TestSetupUplinkE2E`). See §13–§14 of
  [doc/companion-app.md](doc/companion-app.md).
- **Companion-app local dev workflow.** `scripts/run-dev-stack.sh` plus a Vite
  dev-server proxy for all meshd REST endpoints make it possible to drive the
  companion app against a local `meshd` for manual enrollment testing.
  `MESHD_DEV_CORS` (development only) lets a cross-origin app call the management
  API directly, logging a warning when enabled. The onboarding wizard can also
  target an explicit node URL (e.g. a wired node) instead of the setup-AP default.
- **Companion app packaging.** The frontend now builds as native Android/iOS apps
  (Capacitor) with the QR (`@capacitor-mlkit/barcode-scanning`) and WiFi-join
  (`@falconeta/capacitor-wifi-connect`) plugins wired in; desktop ships as the
  installable PWA. Native platform projects are generated on a dev machine
  (gitignored) via `npm run cap:*`. See the build steps, required permissions and
  the on-device verification matrix in
  [doc/companion-app-packaging.md](doc/companion-app-packaging.md). (Native mDNS
  awaits a Capacitor-8 plugin; until then discovery falls back to the daemon's
  `/scan`.)
- **Release publishes the Android companion app.** A `v*` tag now also builds
  and (with an `ANDROID_KEYSTORE_BASE64` secret) signs the Android APK and
  attaches it to the GitHub Release. The job is decoupled from the meshd package
  jobs, so it never blocks the OpenWrt release; iOS (App Store/TestFlight) and
  desktop (the installable PWA) are out of scope. Required secrets are listed in
  [doc/companion-app-packaging.md](doc/companion-app-packaging.md#release-publishing).
- **End-to-end test for the LuCI integration.** `TestLuCIWorkflowE2E` boots a
  real OpenWrt userland with the built `meshd` + `luci-app-meshd` packages and
  the full LuCI stack (ubusd + rpcd + uhttpd), then drives the operator
  workflows over the authenticated `/ubus` endpoint exactly as the PWA does:
  the ACL gate, node enrollment + adopt, the Home/profile lifecycle, and a
  wireless client device surfacing through the topology read. Runs in the `e2e`
  CI job; see [doc/luci-integration-testing.md](doc/luci-integration-testing.md).

### Fixed
- **Creating a Home through the setup wizard failed with ubus error 5.**
  Selecting a freshly created Home applied its (non-existent) profile, and the
  API treated the missing profile as a fatal 500 — even though meshd's own
  auto-select already treats it as non-fatal. Selecting a Home with no profile
  yet now succeeds; only real apply failures error.
- **Opaque ubus error 5 from the LuCI Mesh Manager.** The rpcd plugin used
  `curl -f`, which discarded meshd's JSON error body on any HTTP error, leaving
  rpcd to report a bare `NO_DATA` (5). The plugin now passes meshd's
  `{"error": …}` body back through so the PWA shows the real reason.
- **Local meshd builds now run on OpenWrt.** `scripts/build.sh` defaults to
  `CGO_ENABLED=0`, producing a statically-linked binary; the previous dynamic
  (glibc) build failed on OpenWrt's musl userland with
  `can't execute '/usr/bin/meshd': No such file or directory`.
- **Onboarding wizard's default node client.** A function-typed prop default was
  treated as a factory, so the default client was a function rather than a
  client; the wizard now reaches the node without an explicit `createClient`.

## [0.1.1] - 2026-06-02

### Fixed
- **LuCI Mesh Manager "Access denied".** The LuCI host view handed the embedded
  PWA `L.env.token` (LuCI's CSRF token) as the `/ubus` session token; rpcd does
  not recognise it as a session, so every meshd ubus call the PWA made was
  rejected. The view now passes `L.env.sessionid` (the rpcd ubus session id),
  so the Mesh Manager works inside LuCI.

## [0.1.0] - 2026-06-02

First release with the full secure OpenWrt integration: a mutual-TLS mesh
control plane, a native LuCI app, and signed opkg **and** apk package feeds.

### Added
- **Mesh mutual TLS.** The mesh control plane is served over mutual TLS on both
  the server and client side, using a CN-not-hostname identity model with
  trust-on-first-use.
- **Per-Home PKI.** Each Home is its own certificate authority; nodes are issued
  a Home-CA node certificate automatically at adoption.
- **Separate control planes.** Management and mesh control planes run on
  separate listeners, isolating the device-facing API from the inter-node mesh.
- **LuCI app (`luci-app-meshd`).** rpcd plugin, ACLs, and an embedded LuCI view;
  shipped in releases (architecture-independent). The PWA runs inside LuCI over
  an authenticated ubus session, auto-selecting the ubus transport when embedded.
- **opkg feed.** Every release publishes an opkg feed index
  (`Packages`/`Packages.gz`), usign-signed (`Packages.sig`) when a key is
  configured; the signing public key (`omm-feed.pub`) is published as an asset.
- **apk packages & feed.** Real, signed apk-v3 packages (`apk mkpkg`) plus a
  signed apk repository index (`packages.adb`) for OpenWrt's apk userland; the
  signing public key (`omm-apk.pub`) is published as an asset.
- **Profile application.** Home profiles are applied when the active Home
  changes, with a netifd reload so the changes take effect immediately.

### Changed
- On install, `uci-defaults` flips meshd to its secure posture automatically.
- Discovery derives the announced API host from the packet source, removing the
  need to configure it manually.

### Removed
- The Home/node-delete and factory-reset API endpoints.

### Fixed
- Release version strings are now valid for apk (stricter than opkg): non-tag
  builds use `0.0.0_git` instead of the apk-invalid `0.0.0-dev`.
- The release publish step is idempotent — re-running a release no longer fails
  if the release already exists.
- Profiles are re-applied when the active Home changes.
- Removed duplicate meshd init/config files left at old paths.

### Security
- Inter-node mesh traffic is authenticated and encrypted end-to-end (per-Home CA
  + mutual TLS).
- opkg and apk package feeds are cryptographically signed and verified on the
  device against published public keys.

## [0.0.1] - 2026-06-01

Initial tagged release.

[Unreleased]: https://github.com/and-elf/omm/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/and-elf/omm/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/and-elf/omm/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/and-elf/omm/compare/v0.0.1...v0.1.0
[0.0.1]: https://github.com/and-elf/omm/releases/tag/v0.0.1
