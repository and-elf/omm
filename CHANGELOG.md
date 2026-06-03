# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
- **Companion-app foundation (`web/src/native`).** A swappable native-capability
  bridge (mDNS discovery, WiFi-join, QR label scan) with a web fallback, so the
  same Vue frontend builds as a browser PWA today and as a Capacitor-wrapped
  cross-platform app. See the design spec in
  [doc/companion-app.md](doc/companion-app.md).
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

[Unreleased]: https://github.com/and-elf/omm/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/and-elf/omm/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/and-elf/omm/compare/v0.0.1...v0.1.0
[0.0.1]: https://github.com/and-elf/omm/releases/tag/v0.0.1
