# Companion App — Packaging & On-Device Verification

How the OMM frontend ships across platforms, and the manual checklist for
verifying the native capabilities CI cannot exercise. For the design and the
onboarding flow see [Companion App](companion-app.md).

The frontend is a single Vue codebase ([`web/`](../web)) with native capabilities
behind a bridge ([`web/src/native/`](../web/src/native)) that is a no-op in the
browser. It ships three ways:

| Target | How | Native capabilities |
|--------|-----|----------------------|
| Browser PWA / **desktop** (Win/macOS/Linux) | `npm run build`, served by `meshd` (or installed from the browser) | none — discovery falls back to `GET /scan`; onboarding uses manual entry |
| **Android** | Capacitor | mDNS (see gap below), WiFi-join, QR scan |
| **iOS** | Capacitor | mDNS (see gap below), WiFi-join (known SSID only), QR scan |

Desktop is intentionally the **installable PWA** — it rarely needs WiFi-join, and
the manifest already declares `display: standalone` + icons, so browsers offer
"Install". No Electron/Tauri toolchain is maintained.

---

## Building the mobile apps

Prerequisites are developer-machine tools, not part of this repo's CI:
Android Studio + SDK (Android), and macOS + Xcode + CocoaPods (iOS).

```sh
cd web
npm install
npm run build              # produces dist/ (Capacitor's webDir)

# One-time, per platform — generates the (gitignored) native project:
npm run cap:add:android
npm run cap:add:ios        # macOS only

# After any frontend change, copy the build + native plugins in:
npm run cap:sync

# Open the native IDE to run on a device/emulator:
npm run cap:open:android
npm run cap:open:ios
```

The generated `web/android/` and `web/ios/` projects are **gitignored** — they
are regenerated from the committed `capacitor.config.json`, dependencies and
scripts. (Capacitor docs suggest committing them; we keep the repo lean since
nothing here can build them.)

---

## Release publishing

The release workflow ([`.github/workflows/release.yml`](../.github/workflows/release.yml))
builds the **Android** companion app on a `v*` tag and attaches the APK to the
GitHub Release, alongside the meshd packages. It is **decoupled** from the
package jobs (`continue-on-error`), so a build hiccup here never blocks the
OpenWrt release — the APK is attached only when the job succeeds. The APK is
kept out of `dist/` so it is never mistaken for an OpenWrt apk package in the
feed index.

iOS and desktop are **not** published this way: iOS distribution is App
Store / TestFlight (a separate, credentialed pipeline), and desktop is the
installable PWA already served by the device.

**Signing (opt-in).** Without secrets the job publishes an *unsigned* APK
(`omm-companion-<version>-unsigned.apk`). To publish a signed one, store these
repository secrets (generate a keystore with `keytool -genkeypair -v -keystore
omm.jks -keyalg RSA -keysize 2048 -validity 10000 -alias omm`):

| Secret | Value |
|--------|-------|
| `ANDROID_KEYSTORE_BASE64` | `base64 -w0 omm.jks` |
| `ANDROID_KEYSTORE_PASSWORD` | keystore password |
| `ANDROID_KEY_ALIAS` | key alias (e.g. `omm`) |
| `ANDROID_KEY_PASSWORD` | key password |

> First-run note: this is the one job that can't be validated locally. The SDK /
> Android-Gradle-Plugin versions it installs (`platforms;android-35`,
> `build-tools;35.0.0`) may need bumping to match the Capacitor android template
> on its first real tag/`workflow_dispatch` run.

---

## Plugin wiring

Native capabilities are reached via `@capacitor/core`'s `registerPlugin(<id>)`,
so the JS never imports the plugin package — `cap sync` installs the native
implementation from the dependency. The bridge gates every call on
`Capacitor.isNativePlatform()`, so on web the proxies are never invoked.

| Capability | Plugin id | Package | Status |
|------------|-----------|---------|--------|
| QR / barcode | `BarcodeScanning` | `@capacitor-mlkit/barcode-scanning` | ✅ installed (Cap 8) |
| WiFi join | `CapacitorWifiConnect` | `@falconeta/capacitor-wifi-connect` | ✅ installed (Cap ≥7) |
| mDNS browse | `Zeroconf` | — | ⚠️ **gap — see below** |

### mDNS gap

There is currently **no maintained Capacitor-8 mDNS plugin**
(`@capacitor-community/zeroconf` is unpublished; `capacitor-zeroconf` tracks
Capacitor 4). Until one exists, native discovery is unavailable and
`useDiscovery` falls back to the daemon's `GET /scan` (or manual controller-URL
entry in the wizard). Options to close it, in order of effort:

1. Pin and test `capacitor-zeroconf@4` against Capacitor 8 (its native API may
   need patching).
2. Write a small first-party plugin wrapping Android NSD / iOS `NWBrowser` and
   register it as `Zeroconf` — the bridge needs no change
   ([`capacitor.ts`](../web/src/native/capacitor.ts) already maps the resolved
   service via `serviceToController`).

### Required native permissions / entitlements

To add to the generated projects (these live in the gitignored platform folders,
so document/script them):

- **Android** (`AndroidManifest.xml`): `ACCESS_FINE_LOCATION` (WiFi scan/connect
  on 9+), `CHANGE_WIFI_STATE`/`ACCESS_WIFI_STATE`, `CAMERA` (QR), and
  `INTERNET`. Request `ACCESS_FINE_LOCATION` at runtime before WiFi scan.
- **iOS** (`Info.plist` / entitlements): `NSCameraUsageDescription` (QR),
  `NSLocalNetworkUsageDescription` + a `NSBonjourServices` entry (`_mesh._tcp`)
  for any mDNS, and the **Hotspot Configuration** capability for
  `NEHotspotConfiguration` (join a known SSID). iOS cannot enumerate networks —
  the wizard's QR-label scan supplies the exact SSID.

---

## On-device verification matrix

The browser/serving paths, the API contract, the setup-AP lifecycle (real
OpenWrt e2e) and all app logic are covered by automated tests. The matrix below
is the **manual** check for what only runs on a device. Tick per release.

| # | Check | Android | iOS | Desktop PWA |
|---|-------|:------:|:---:|:-----------:|
| 1 | App installs / launches; PWA "Install" offered on desktop | ☐ | ☐ | ☐ |
| 2 | Discover Homes lists controllers (mDNS once available; else `/scan`/manual) | ☐ | ☐ | n/a (manual) |
| 3 | Scan a node's setup-label QR → credentials parsed | ☐ | ☐ | n/a |
| 4 | Join the node's `OMM-Setup-*` AP from the app | ☐ | ☐ | n/a |
| 5 | Reach the node at `192.168.254.1:8080` (CORS or native HTTP) | ☐ | ☐ | n/a |
| 6 | `/enroll/join` succeeds; node reaches `pending`/`active` | ☐ | ☐ | ☐ |
| 7 | Adopt: combined-mode controller (open REST) | ☐ | ☐ | ☐ |
| 8 | Adopt: split-mode controller via LuCI sign-in | ☐ | ☐ | ☐ |
| 9 | Network-context switch (setup AP → home network) per [companion-app.md §4](companion-app.md) | ☐ | ☐ | n/a |
| 10 | Full wired-uplink onboarding end to end | ☐ | ☐ | ☐ |

> Item 5 note: a browser PWA reaching `192.168.254.1` is subject to the device's
> CORS policy; the native build can use a CORS-free HTTP transport (inject it as
> the `fetchFn` into `createRemoteApi`). Item 9 is the part the spec calls out as
> needing hardware validation.
