# OMM Companion App — Design Spec (v1: onboarding)

Status: **DRAFT / design**. This document specifies the cross-platform companion
app whose first goal is to **simplify node onboarding** using the OMM protocol,
plus the **meshd-side work** that onboarding depends on. It is written
spec-first: tests and code follow this spec, and this spec is updated before any
behavioural change (see the project workflow). For the protocol it builds on,
see [Discovery & Enrollment](discovery-enrollment.md), the
[Node Enrollment Protocol](enrollment.md), the [API](api.md), the
[Security Model](security.md), and [OpenWrt Integration](openwrt.md).

---

## 1. Goal & scope

**Primary goal:** make adding a node to a Home a guided, few-tap flow on a phone
(and, secondarily, on desktop), instead of typing IPs into a browser.

**v1 scope — onboarding-focused.** Discover → create/join a Home → enroll a new
node → approve it → confirm it joined. The remaining management surfaces (full
Homes/Nodes/Topology/Settings) are *reused later* from the existing PWA, not
rebuilt now.

**Decisions taken** (see the design conversation):

| Decision | Choice | Consequence |
|----------|--------|-------------|
| Shell | **Capacitor wrapping the existing Vue 3 PWA** | Reuse ~all tested views + API client; add native plugins for mDNS and WiFi |
| Bootstrap model | **Setup-AP + WiFi onboarding (OEM-style)** | Requires **new meshd work** (a first-boot setup AP — does not exist today) **and** native WiFi scan/join |
| Targets | Android, iOS, then desktop (Windows/macOS/Linux) | iOS WiFi limits are the dominant constraint (§6) |

**Non-goals (v1):** rebuilding Homes/Nodes/Topology/Settings natively; firmware
management; placement assistant; any cloud component.

---

## 2. What already exists (reuse, don't rebuild)

The recon over the codebase established that the workflow is **already
implemented** in the Vue PWA and the meshd REST API:

- **Vue 3 + TS + Vite PWA** with the full onboarding UI: `SetupView`
  (create/join Home), `EnrollView` (pending-approval queue + adopt/reject +
  scan-to-join), Homes/Nodes/Topology/Settings
  ([web/README.md](../web/README.md), [web/src/views/](../web/src/views/)).
- **A dual transport already baked into the API client**: same-origin REST, or
  ubus-over-LuCI using an injected session token, auto-selected at runtime
  ([web/src/api/client.ts:199](../web/src/api/client.ts#L199),
  [web/src/api/ubus.ts](../web/src/api/ubus.ts)). The companion app adds a
  **third** transport context (remote device over the LAN/setup-AP).
- **The full REST surface** for onboarding ([doc/api.md](api.md),
  [internal/api/router.go](../internal/api/router.go)) — see §7.

The companion app is therefore mostly a **native shell + two new capabilities**
(mDNS, WiFi) + a **remote-target API context**, not a new frontend.

---

## 3. The real onboarding flow (corrected)

The originally proposed flow assumed `root/root` login, a phone bridging two
networks, and a node that "reboots". None of those match the protocol. The
corrected model:

> **The app is a management-plane orchestrator, not a mesh participant.** It
> tells the new node *"join controller X"* and tells the controller *"adopt node
> Y"*. The **node ↔ controller** enrollment (challenge-response + mutual TLS) is
> done **daemon-to-daemon** — the phone never relays mesh traffic.

```
PHASE 0  Discover Homes (on the home network)
  └─ App browses mDNS _mesh._tcp / UDP :45678 → list of controllers (Homes),
     each with its api URL. User picks the target Home. App remembers its
     controller_url.            (native mDNS; meshd already announces)

PHASE 1  Reach the new (unclaimed) node
  └─ App joins the node's setup AP "OpenWrt-Setup-XXXX"  (native WiFi join)
  └─ App talks to the node's open management API at http://192.168.254.1:8080
       GET /setup   → node_id, serial, setup_complete=false
       GET /status  → state=unclaimed

PHASE 2  Trigger enrollment (one call, on the node)
  └─ POST /enroll/join { "controller_url": "<from PHASE 0>", "serial": "..." }
     The node now runs request→verify→poll→ack against the controller itself,
     using its own ECDSA identity and TOFU-pinned Home CA.   (daemon-to-daemon)

PHASE 3  Approve (on the controller)
  └─ App switches context to the controller (home network or its api URL)
       GET  /enroll?status=pending_approval   → the new node appears
       POST /nodes/{node_id}/adopt            → controller signs leaf cert,
                                                 returns profile to the node

PHASE 4  Confirm
  └─ Node applies the profile in place (NO reboot/factory reset) and reaches
     'active'. App polls GET /nodes / GET /status until the node shows active.
```

**State/endpoint truth** (verified): states are `pending_verification →
pending_approval → approved → active` ([internal/models/models.go:31](../internal/models/models.go#L31));
adopt is `POST /nodes/{id}/adopt` ([internal/api/enrollment.go:113](../internal/api/enrollment.go#L113));
join is `POST /enroll/join {controller_url, serial}`
([internal/api/enrollment.go:131](../internal/api/enrollment.go#L131)); no reboot
exists in the client ([internal/client/client.go](../internal/client/client.go)).

---

## 4. ⚠️ The chicken-and-egg this design must resolve

For PHASE 2 to work, **the node must be able to reach the controller** while it
is enrolling. But an unclaimed node broadcasting an *isolated* setup AP has no
uplink to the home network — so `/enroll/join` would have nothing to dial.

Three resolutions, in increasing complexity:

1. **Wired uplink during onboarding (v1 recommendation).** The new node is
   plugged into the home LAN by Ethernet *and* broadcasts its setup AP for the
   phone. The node reaches the controller over the wire; the phone configures it
   over WiFi. This is the standard "plug in, configure over the device's own
   WiFi" OEM pattern and needs no new node-as-station code.
2. **Phone provisions home-WiFi STA credentials (v2).** The phone, while on the
   setup AP, hands the node the home SSID/key; the node connects as a WiFi
   station, gains uplink, then enrolls. Enables fully-wireless onboarding but
   needs new meshd/UCI work to bring up a STA interface.
3. **Deferred enrollment.** The phone records the chosen Home on the node; the
   node enrolls opportunistically once it ever gains connectivity. Most robust,
   most state to manage.

**v1 picks (1).** (2) and (3) are tracked as follow-ups. This choice is the
single most important constraint on the meshd setup-AP work in §5.

---

## 5. New meshd work — the first-boot setup AP

Today **no code creates a setup AP**; `OpenWrt-Setup-XXXX` @ `192.168.254.1` is
only described in [discovery-enrollment.md:61](discovery-enrollment.md#L61) as
intended first-boot behaviour (confirmed absent by code search). v1's WiFi
onboarding requires building it. Spec:

- **Trigger:** device is `unclaimed` (no active Home, `setup_complete=false`).
- **AP:** SSID `OMM-Setup-<last4-of-node-id>` (stable, derivable, printable on a
  label/QR — see §6 for why the suffix must be discoverable out-of-band on iOS).
  Open or WPA2 with a label-printed/QR password (decision in §10).
- **Network:** AP bridged to a setup network, gateway `192.168.254.1/24`, DHCP
  for clients. Management API reachable at `http://192.168.254.1:8080` (combined
  mode is already unauthenticated on `0.0.0.0:8080`, so no auth change needed —
  [internal/config/config.go](../internal/config/config.go)).
- **Teardown:** the AP is torn down when the device leaves `unclaimed`
  (profile applied / active Home set).
- **Boundaries:** consistent with OMM's charter, meshd *orchestrates* OpenWrt
  (UCI wireless + dnsmasq) rather than reimplementing it
  ([architecture.md](architecture.md)). This is the first place meshd authors
  AP/DHCP config, so it must be reversible and must not clobber an operator's
  existing wireless.

This is a self-contained meshd feature with its own spec section, tests, and
e2e coverage (the e2e harness already runs real OpenWrt containers —
[enrollment.md:149](enrollment.md#L149)).

---

## 6. Native capabilities & platform constraints

The Vue frontend has **zero** native code today; everything is REST. The
companion app adds exactly two native capabilities, behind a Capacitor plugin
interface so the web build stays testable with mocks.

### 6.1 mDNS discovery (PHASE 0)
- Plugin: `@capacitor-community/zeroconf` (or equivalent). Browse `_mesh._tcp`.
- iOS: requires `NSLocalNetworkUsageDescription` + the iOS-14 local-network
  permission prompt and a `Bonjour services` plist entry.
- Android: NSD; mDNS works without location permission.
- Desktop: zeroconf via the desktop runtime (or fall back to manual URL entry).

### 6.2 WiFi scan/join (PHASE 1) — **highest risk**

| Platform | Scan (list SSIDs) | Join a known SSID | Bind app traffic to it |
|----------|-------------------|-------------------|------------------------|
| Android  | ✅ `WifiManager` scan — **needs `ACCESS_FINE_LOCATION` + location on**, throttled on 9+ | ✅ `WifiNetworkSpecifier` (10+) | ✅ app-scoped network — ideal: talk to node, keep cellular |
| iOS      | ❌ **No public scan API** (NEHotspotHelper needs a special Apple entitlement) | ✅ `NEHotspotConfiguration` joins a **named** SSID | ⚠️ system-routed; reach `192.168.254.1` over the joined WiFi |
| Win/macOS/Linux | ✅ via OS tooling | ✅ | n/a (single stack) |

**iOS implication (must design for):** the app **cannot enumerate** nearby
`OMM-Setup-*` networks. It can only *join a name it already knows*. Therefore
the setup-AP suffix must arrive **out-of-band**:
- **QR code on the device label** (recommended, OEM-standard): encodes
  SSID + password (+ optionally node serial). App scans it → joins exactly that
  AP. Works identically on iOS and Android and removes the scan dependency.
- Fallbacks: user reads the last-4 off the label and the app constructs the
  SSID; or user joins via iOS Settings, then returns to the app.

**Recommendation:** make the **QR-on-label** path primary on all platforms;
treat WiFi *scanning* as an Android-only convenience, never a requirement.

---

## 7. API surface the app uses (all verified present)

| Phase | Call | Auth context |
|-------|------|--------------|
| 0 | mDNS `_mesh._tcp` / UDP `:45678` (native), or `GET /scan` once on a meshd | unauth (LAN) |
| 1 | `GET /setup`, `GET /status` on the node | unclaimed node = open ([security.md](security.md)) |
| 2 | `POST /enroll/join {controller_url, serial}` on the node | open on unclaimed node |
| 3 | `GET /enroll?status=pending_approval`, `POST /nodes/{id}/adopt`, `POST /nodes/{id}/reject` on the controller | see §8 |
| 4 | `GET /nodes`, `GET /nodes/{id}`, `GET /status` | see §8 |

(Refs: [internal/api/router.go](../internal/api/router.go),
[internal/api/enrollment.go](../internal/api/enrollment.go),
[internal/api/scan.go](../internal/api/scan.go).)

---

## 8. Auth: meshd has none of its own — context decides

There is **no `/api/auth`, no `root/root`** in meshd
([security.md:38](security.md#L38)). Two postures the app must handle when
talking to the **controller** (PHASE 3–4):

1. **Combined mode** (bare meshd; and every *unclaimed* node): management REST
   is open on `0.0.0.0:8080`. Call it directly.
2. **Split mode** (after `luci-app-meshd` is installed
   — [openwrt.md:57](openwrt.md#L57)): management is **localhost-only**; the
   app must authenticate to **LuCI** (the router admin password → an rpcd ubus
   session token) and call meshd via `/ubus`. *This* is the only place a
   "password" exists — it is the OpenWrt/LuCI login, not meshd.

The existing client already speaks the `/ubus` envelope
([web/src/api/ubus.ts](../web/src/api/ubus.ts)); the app adds **LuCI
`session.login`** to obtain the token for a *remote* controller (today the token
is injected by the LuCI host page; the app must fetch it itself).

The **unclaimed node** (PHASE 1–2) is always open — no auth needed there.

---

## 9. Architecture

```
┌─────────────────────────── Capacitor app ───────────────────────────┐
│  Existing Vue 3 PWA (web/)                                            │
│   ├─ reused views (Setup/Enroll/...) + new onboarding wizard         │
│   └─ API client (web/src/api/client.ts)                              │
│        transports: same-origin REST | ubus(+LuCI login) | remote-LAN │
│                                                                      │
│  Native bridge (Capacitor plugins, mocked in web tests)              │
│   ├─ Discovery: mDNS browse (_mesh._tcp)                             │
│   ├─ WiFi: QR-scan → join named AP (scan = Android-only convenience) │
│   └─ QR scanner (camera)                                             │
└──────────────────────────────────────────────────────────────────────┘
        │ REST/ubus over LAN / setup-AP            ▲ daemon-to-daemon mTLS
        ▼                                          │
   Unclaimed node  ──POST /enroll/join──►  Controller (Home)
   (setup AP +                              announces over mDNS/UDP;
    open mgmt API)                          adopt → signs leaf cert
```

- **One web codebase**, packaged by Capacitor to Android/iOS and (Electron or
  the existing PWA) for desktop. Native features sit behind a thin TS interface
  with a web/no-op fallback so the PWA still builds and Vitest still runs.
- **New `remote-LAN` API context**: the API client gains a base-URL targeting a
  remote device (the node at `192.168.254.1:8080`, or a controller's announced
  `api` URL) instead of only same-origin.

---

## 10. Open decisions / risks

1. **Setup-AP security:** open AP (simplest, but anyone nearby can hit the open
   mgmt API of an unclaimed node) vs WPA2 with a label/QR password
   (recommended). The unclaimed mgmt API is unauthenticated, so an open setup AP
   = anyone in range can claim the node first → **lean WPA2 + QR**.
2. **Onboarding connectivity model:** §4 picks wired uplink for v1. Confirm this
   matches the intended user story before building the STA path.
3. **Desktop packaging:** Capacitor-Electron vs shipping desktop as the plain
   installable PWA (desktop rarely needs WiFi-join; mDNS may suffice).
4. **iOS distribution:** `NEHotspotConfiguration` + local-network are fine for
   the App Store; confirm no need for the restricted NEHotspotHelper entitlement
   (the QR-join design avoids it).
5. **Repo/CI:** org standard is GitLab + `glab`; this repo currently lives on
   GitHub (`github.com/and-elf/omm`). Decide where the app subproject and its
   pipeline live before first push.

---

## 11. Test strategy (TDD: red → green → refactor)

- **meshd setup-AP:** unit tests for the UCI/dnsmasq config authoring
  (table-driven, no device), integration against a stub, and an **e2e** case in
  the existing real-OpenWrt-container harness asserting an unclaimed device
  brings up `OMM-Setup-*` + serves `/setup` at `192.168.254.1:8080`, and tears
  it down on activation.
- **App logic:** Vitest unit tests for the onboarding state machine and the
  remote-LAN API context, with the native plugins **mocked** (the existing DI'd
  `fetchFn`/`baseUrl` pattern, [web/src/api/client.ts:35](../web/src/api/client.ts#L35)).
- **Native capabilities:** thin contract tests against the plugin interface;
  on-device manual verification for the WiFi-join and camera paths (the parts
  CI cannot exercise), mirroring how the on-device LuCI iframe is handled today.
- **Contract:** assert the app calls exactly the verified endpoints in §7 with
  the right bodies, so app and daemon stay in sync.

---

## 12. Milestones

1. **M0 — spec & skeleton:** ✅ *done.* This doc; the native-capability bridge
   (`web/src/native/`: discovery / WiFi / QR behind interfaces with a web
   fallback + Vitest tests) and `web/capacitor.config.json`. Web build + Vitest
   stay green. *Pending:* generating the `android/`/`ios/` platform projects
   (`npm i @capacitor/{core,cli,...}` + `npx cap add`) — needs the native SDKs,
   done on a dev machine, not in this repo.
2. **M1 — meshd setup AP:** ✅ *done.* `internal/setupap` (Enable/Disable),
   `uci.Client.SetSection`/`Delete`, wired into the daemon (up while unclaimed,
   torn down on `/setup/complete`), `MESHD_SETUP_AP*` config. Unit-tested **and**
   covered by a real-OpenWrt-container e2e (`TestSetupAPLifecycleE2E`) that
   asserts the uci sections appear on boot and are removed on setup completion.
3. **M2 — discovery + remote context:** ✅ *done.* `client.ts` gained
   `createRemoteApi(baseUrl)` (target a node's setup-AP address or a controller's
   announced URL); a `useDiscovery` composable that prefers native mDNS and falls
   back to the daemon's `/scan`; a Capacitor bridge (`web/src/native/capacitor.ts`,
   `@capacitor/core` + `registerPlugin('Zeroconf')`) registered at startup via
   `initNative()`; and the Enroll view's "pick a Home" now native-aware with a
   discovery-source badge. All unit-tested. *Pending (device-only):* the actual
   `@capacitor-community/zeroconf` install + `npx cap add android/ios` and
   on-device validation of the TXT-record mapping.
4. **M3 — onboarding wizard:** QR-join → reach node → `/enroll/join` →
   adopt → confirm, end to end against §4's wired-uplink model.
5. **M4 — controller auth:** LuCI `session.login` for split-mode controllers.
6. **M5 — desktop packaging** + on-device verification matrix.
</content>
</invoke>
