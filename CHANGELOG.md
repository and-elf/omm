# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Dual-band / multi-band client AP (#36).** A claimed home previously
  broadcast its client AP on a single radio (typically 5 GHz), so 2.4 GHz-only
  devices had no AP to join. `ApplyProfile` now authors the client AP across
  multiple bands: the primary `omm_ap` stays on the `radio`/`band`-resolved
  radio, and each band in the new `ap_bands` profile field (`"2g"`/`"5g"`/`"6g"`)
  resolves to that node's matching radio, authored as `omm_ap_<band>` (e.g.
  `omm_ap_2g`, coexisting with `omm_mesh` on the same 2.4 GHz radio/channel).
  Empty `ap_bands` defaults to also broadcasting on 2.4 GHz, so every home gets a
  dual-band AP with no configuration; set a single band (e.g. `["5g"]`) to opt
  out. Bands absent on a node are skipped (not fatal), and narrowing `ap_bands`
  prunes the now-stale `omm_ap_<band>` sections on re-apply.
- **Xiaomi AX3600 as a build/deploy target.** `build-devices.sh` gains an
  `ax3600` label and `deploy.sh` recognises it. The board is a Qualcomm IPQ8071A
  (qualcommax/ipq807x, Cortex-A53) — the same `aarch64_cortex-a53` ISA group as
  the ZB8103AX, so the release feed's arm64 package already covered it; this just
  makes the local dev tooling first-class for it. Because two boards now share
  one ISA, `deploy.sh` disambiguates by `board_name` (`xiaomi,ax3600`) before
  falling back to `uname -m`. No profile changes were needed: the mesh radio is
  auto-selected by band, which picks the AX3600's 2.4 GHz radio (`radio2`).

### Changed
- **Mesh-node network posture now bridges into the home (single gateway).** A
  claimed satellite (`manage_network=1`) previously only stood down its
  authoritative DHCP, leaving its own routed/NAT'd `wan` up — so its bridged
  802.11s mesh was an island and mesh traffic could not reach the home WAN, which
  egresses only through the controller's gateway over the mesh. The Mesh-node
  posture now authors the same bridged shape as Guest: `lan` becomes a DHCP
  client, the routed `wan`/`wan6` are disabled, and authoritative DHCP is stood
  down, so the node's default route points at the controller. When batman-adv is
  the forwarding layer (`MESHD_BATMAN`, the default) the uplink is left to the
  batman port classifier instead of being folded into `br-lan` — `bat0` is the
  bridged backhaul and re-adding the uplink on every apply would fight the
  classifier and recreate the storming `br-lan`+`bat0` double path (the uplink is
  already a `br-lan` candidate from the Guest phase). Without batman the posture
  folds the uplink in itself; Guest folds regardless (no active home yet ⇒ no
  `bat0` to defer to). Still gated behind `manage_network` (off by default).
  Verified end-to-end on hardware: reset → Guest → wired auto-onboard → mesh-node,
  surviving both a meshd restart and a reboot.
- **Zero-touch defaults.** A fresh kit now self-forms with no configuration: the
  controller's `adopt_policy` defaults to `onlink` (auto-adopt only nodes
  verified to be on its own LAN) and a node's `auto_onboard_wired` defaults to on
  (with `backhaul_iface` defaulting to `br-lan` so the wired backhaul is
  classified), so powering the first device makes it a controller and cabling a
  node joins it. Set `adopt_policy=off` / `auto_onboard_wired=0` to require the
  wizard. Network posture management (`manage_network`) stays opt-in.

### Fixed
- **Topology view now draws lines between the real nodes (#27/#28/#33).**
  batman-adv lists neighbours by originator MAC while each node self-reports under
  a node ID, so links pointed at anonymous MAC blobs and the real nodes never
  connected. Each node now reports its batman address(es) (`addrs`) and the
  aggregator rewrites MAC-keyed link endpoints to the owning node ID, dropping the
  redundant MAC nodes. Because batman-adv keys originators by the hard interface
  the OGMs arrive on, a wired-backhaul node appears under several MACs (its mesh
  MAC plus each ethernet port's unique hardif MAC); reporting only the `bat0`
  address left those wired-port originators unmapped, so each ethernet port
  surfaced as a separate unconnected node. `BatctlSelfAddrs` now enumerates every
  enslaved hard interface via `batctl if` and reports each MAC (plus `bat0`),
  degrading to the `bat0` address alone when `batctl` is unavailable.
- **Wireless clients never reached the controller's topology (#33).** Client
  collection only read the explicitly-configured AP-interface list, which was
  never wired through the init script — so on deployed devices it was empty and
  every node aggregated zero clients. Nodes now auto-discover AP-mode vifs from
  `network.wireless status` (querying each hostapd object, skipping the mesh vif)
  when none are configured; an explicit list still wins, and `ap_interfaces` is
  now wired through the init for that override.
- **Empty topology graph over LuCI (#33).** The LuCI ubus transport
  (rpcd → busybox `nc`) half-closes the socket once the request is written, which
  Go reads as the client leaving and cancels the request context — killing the
  `batctl`/`iw` subprocesses mid-collect and returning an empty graph. Topology
  collection now runs to completion under its own timeout, detached from the
  request's cancellation. Onboarded-but-silent inventory nodes are also labelled
  by their serial instead of the raw 64-char node ID (falling back to the ID for
  records that predate serial capture).
- **Topology edge labels were illegible (#33).** The link speed/TQ/RSSI labels
  were small grey text with no background and washed out over the line and nodes;
  they now get a dark rounded halo, brighter text, and a slightly larger font.
- **Stale PWA baked into builds (#33).** `vite` runs with `emptyOutDir:false` so a
  no-frontend build still has a `dist` to embed, but hashed bundles accumulated
  across builds and the binary (`embed all:dist`) could bake in an outdated
  `index.html`/chunk and serve a stale PWA against a current backend. The build
  now clears the generated output before each frontend build, keeping the tracked
  `.gitkeep` placeholder.

### Added
- **PWA adopts the LuCI host theme when embedded.** Served inside LuCI the PWA
  runs in a same-origin iframe, so it now reads the host page's resolved colours,
  font and stylesheets and blends in (a `.theme-host` palette derived with
  `color-mix()`, host stylesheets imported ahead of our CSS) instead of imposing
  its own dark palette; the topology graph resolves its label/halo colours from
  CSS variables so it stays legible on a light host theme. Standalone (`meshd` on
  `:8080`) the built-in dark theme is unchanged. New `web/src/theme.ts`.
- **Remove nodes from the Nodes view (#33).** The Nodes view gains bulk node
  removal — per-row and select-all checkboxes and a "Delete selected" button that
  confirms once, deletes each node and prunes the selection to still-existing
  records on refresh — giving operators a way to clear stale/orphaned node records
  (e.g. ones left behind when a node re-enrolled under a fresh identity). Wires
  `DELETE /nodes/{id}` into the API client and the LuCI ubus transport
  (`delete_node`, already ACL-authorized).
- **Deploy prunes orphaned records and syncs the LuCI PWA (#33).** `--reset`
  wipes a node's identity, so on its next join it enrolls under a fresh node ID
  and its old record lingers as a "down" ghost; `scripts/deploy.sh --controller
  <host>` now reads the node's current ID (`ubus call meshd setup`) before the
  wipe and removes that record afterwards (`ubus call meshd delete_node`),
  best-effort so an unreachable controller never fails the deploy. Deploy also
  pushes the freshly built `web/dist` to the LuCI-served PWA copy
  (`/www/luci-static/resources/view/meshd/pwa`) when the luci-app is installed, so
  the embedded and standalone frontends update together.
- **Node liveness in the topology graph (#29).** An onboarded node that stops
  reporting — powered off, meshd down, or mesh failed to form — no longer
  silently vanishes from the map. The aggregator keeps onboarded nodes (from the
  controller's inventory, scoped to its own home) visible and tags each with a
  `status` (`alive`/`stale`/`down`) and `last_seen`. Stale and down nodes appear
  as isolated vertices (their stale links are not merged); the Topology view dims
  a stale node and crosses out a down one (`✕`), labelling both with how long ago
  they were last seen. A pure liveness signal — no dependency on internet
  connectivity. New `staleTTL` window on the aggregator (5 min default).
- **Link type and quality in the topology graph (#28).** Each mesh link now
  carries its medium and a metric: `kind` (`wired`/`wireless`, derived from the
  outgoing batman hard interface in `batctl o`), `speed_mbps` for wired links
  (read from `/sys/class/net/<iface>/speed`), and `signal` (RSSI) for wireless
  links (read from `iw … station dump`). The Topology view draws wired links solid
  with the speed (`1G`/`2.5G`/`5G`/`10G`) and wireless links dashed, coloured by a
  four-tier RSSI quality (`excellent`/`good`/`fair`/`weak`) and labelled with the
  signal. New `internal/topology/link.go` and `identity.go`.
- **Zero-touch backhaul (any topology).** Plug a node in over ethernet anywhere —
  into the controller, into another node, or leave it wireless — and it joins the
  batman fabric with no per-node config. Every meshd broadcasts a presence beacon;
  every node passively sniffs each wired (`br-lan`-member) port for a peer's beacon
  and enslaves only the ports that face an OMM peer to `bat0` (taking each out of
  `br-lan`, which is loop-safe on its own), leaving client jacks as plain bridge
  ports. The mesh is always-on and the mesh radio is auto-selected by band
  (2.4 GHz), so batman-adv + BLA own all path selection and loop avoidance — there
  is **no carrier-toggle failover** (pulling a wired uplink re-routes over the mesh
  in ~1s; replug restores wired-primary). Enslaved ports get a unique
  locally-administered MAC so shared-MAC DSA switch ports work. A reconcile loop
  keeps the classification live as cabling changes. `batman_ports` remains an
  explicit override that skips the scan. (Supersedes the earlier peer-on-wire /
  case-1-2-3 + failover approach.)
- **batman-adv multi-hop routing.** meshd now authors a batman-adv mesh as the
  forwarding layer instead of bridging the 802.11s mesh straight onto the LAN: a
  `bat0` soft interface with bridge-loop-avoidance, one batadv hard interface per
  backhaul link (the wireless mesh *and* each configured wired port,
  `batman_ports`), and `bat0` bridged into the LAN. batman-adv forwards loop-free
  across any mix of wired and wireless links, so chained nodes
  (controller → wired → AP → wireless → AP → wired → device) and simultaneous
  wired+wireless backhaul on one node now work without a bridge loop — superseding
  the carrier-toggle failover, which is disabled when batman is active. On by
  default (`batman`), auto-degrading to the direct mesh-on-LAN bridge when the
  batman-adv module/netifd proto is absent; configurable via `batman`,
  `batman_ports`, `batman_routing_algo`. Requires `kmod-batman-adv` (which ships
  the netifd proto handlers) and `batctl` on the image.
- **Mesh-capable `wpad` provisioning.** So 802.11s actually forms (instead of
  degrading to wired multi-AP): documented baking `wpad-mesh-*` into the firmware
  image — the reliable path that also covers offline nodes — and added
  `scripts/deploy.sh --install-dependencies`, which detects a live device's
  crypto variant and swaps `wpad-basic-*` for the matching `wpad-mesh-*` (and
  installs the `kmod-batman-adv`/`batctl` routing stack).
- **Zero-config wired onboarding.** A freshly-flashed kit now comes up hands-off:
  power the first device and it becomes its own controller; cable a node and it
  discovers, enrolls, and receives its wireless — no wizard, no per-device
  config. This is the sum of the items below.
- **Unique per-device Home id.** When `home_id` is unset, the daemon derives a
  stable, unique id from the device identity (`home-<node-id-prefix>`) instead of
  the shared literal `default-home`. Two unconfigured devices previously shared
  `default-home`, so discovery treated each other's announcements as "self" and
  ignored them — they were mutually invisible and could not onboard. The default
  config/init now ship an empty `home_id` to trigger derivation.
- **Default Home wireless profile.** A Home with no profile brought up no
  wireless and pushed nothing to a joining node. `meshd` now seeds a default
  profile at startup — an 802.11s mesh (and a client AP derived from it) with a
  mesh SSID (`MESHD_MESH_SSID`, else a unique `OMM-<home-suffix>`) and key
  (`MESHD_MESH_KEY`, else a generated random passphrase persisted in the
  profile). A wizard/operator-set profile is never overwritten.
- **Controller-side adoption policy.** Replaces the blanket auto-adopt boolean
  with `adopt_policy` (`MESHD_ADOPT_POLICY`): `off` (operator approves, default),
  `onlink` (auto-adopt only nodes whose enrollment arrives on the controller's
  own LAN subnet — verified from the request source), or `always` (any node).
  Adoption is the controller's decision, gated on a signal it can verify, never
  on the node's self-declared backhaul. Legacy `MESHD_AUTO_ADOPT=1` maps to
  `always`.
- **802.11s → wired multi-AP auto-fallback.** When the mesh interface does not
  come up (typically: no mesh-capable `wpad`), the node now degrades to a wired
  multi-AP instead of having no backhaul: it removes the mesh interface for a
  clean AP-only re-setup and records the outcome. Surfaced as a `backhaul`
  object on `GET /status` (`{mode, reason, remediation}`) and a per-node
  `mesh_mode` on the topology graph; the LuCI app shows the mode, a degrade
  notice with the `wpad-mesh` remediation, and marks degraded nodes.
- **Lifecycle-managed network posture.** Opt-in (`MESHD_MANAGE_NETWORK`, default
  off): an unclaimed node takes a **Guest** dumb-AP posture — its wired uplink
  (auto-detected from `network.wan.device`, so any jack works) bridged into
  `br-lan`, `lan` as a DHCP client, the routed `wan` disabled, authoritative DHCP
  stood down — so it is L2-adjacent to a controller and can actually receive
  discovery broadcasts (a routed, firewalled `wan` silently drops them). It
  returns to a gateway posture once it controls its Home. See
  [doc/network-posture.md](doc/network-posture.md). New `internal/netposture`.
- **Per-device build + deploy tooling.** `scripts/build-devices.sh`
  cross-compiles for the test boards (verifying the binary's ELF arch so a
  wrong-arch build can't ship), and `scripts/deploy.sh` deploys the package
  (binary + init, fresh config on `--reset`) to a live device over SSH with
  `--set`/`--join`/`--watch`.

### Fixed
- **802.11s mesh wrongly degraded to multi-AP even with a mesh-capable `wpad`.**
  Two causes, found on a live IPQ board: the first-boot setup AP lingered on the
  radio, so applying the mesh made it AP+AP+mesh on one radio (driver rejects it,
  `nl80211 -95`) — meshd now retires the setup AP when a home activates, before
  applying the profile; and the mesh-up check ran immediately after the wireless
  reload, before the mesh vif had instantiated — it now polls for a few seconds
  before concluding the mesh failed.
- **Controller announce failed on a segment with no default route.** Announce
  dialed a connected socket to the limited broadcast `255.255.255.255`, which
  needs a route a gateway-less segment lacks (`network is unreachable`), so the
  controller never announced. It now sends over a `SO_BROADCAST` socket to each
  interface's subnet-directed broadcast, which routes over the connected subnet.
- **`enroll/join` aborted with "context canceled" via LuCI.** The rpcd plugin
  reaches `meshd` over busybox `nc`, which half-closes after writing the request;
  `meshd` then cancelled the request context mid-handshake. The join now runs on
  a fresh, bounded context detached from the request.
- **Unattended wired onboard could never fire.** An unclaimed wired node
  self-selected its own Home at boot, before discovery surfaced a controller,
  which blocked auto-onboard. It now defers self-selection: the onboard loop
  enrolls into a discovered controller first and only becomes its own controller
  after a grace window (`MESHD_ONBOARD_GRACE`, default `20s`) with none found.

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

[Unreleased]: https://github.com/and-elf/omm/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/and-elf/omm/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/and-elf/omm/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/and-elf/omm/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/and-elf/omm/compare/v0.0.1...v0.1.0
[0.0.1]: https://github.com/and-elf/omm/releases/tag/v0.0.1
