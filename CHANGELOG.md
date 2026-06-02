# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/and-elf/omm/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/and-elf/omm/compare/v0.0.1...v0.1.0
[0.0.1]: https://github.com/and-elf/omm/releases/tag/v0.0.1
