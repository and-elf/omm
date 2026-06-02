# OpenWrt Mesh Manager (OMM)

## Overview

OpenWrt Mesh Manager (OMM) is a local-first mesh networking management platform for OpenWrt.

Its goal is to make multi-node OpenWrt deployments as easy to manage as commercial mesh products while remaining:

* Open source
* Cloud-independent
* Vendor-neutral
* Self-hosted
* Fully functional without Internet access

OMM provides:

* Simple node enrollment
* Automatic mesh configuration
* Visual topology mapping
* Client-to-AP visibility
* Multi-home portable nodes
* LuCI integration
* Progressive Web App (PWA) support

---

# Goals

## Primary Goals

* Eliminate manual mesh configuration
* Eliminate factory resets when moving nodes between networks
* Eliminate cloud dependencies
* Provide a visual network map
* Provide simple onboarding and adoption workflows

## Non-Goals

* Replace OpenWrt networking stack
* Replace batman-adv
* Replace hostapd
* Replace netifd
* Replace UCI

OMM orchestrates existing OpenWrt services.

---

# How It Works

OMM centers on the `meshd` daemon running on each OpenWrt device. A device boots
**unclaimed** and is then either turned into a controller for a new **Home** (a
logical mesh deployment, identified by a stable UUID) or joins an existing one.
Controllers discover each other over mDNS/UDP, approve enrolling nodes, sign
certificates, distribute per-Home configuration **profiles**, and aggregate the
live mesh **topology**. Nodes can be members of several Homes and switch between
them without a factory reset — but are only ever active in one at a time. The
same Vue PWA is served directly from the `meshd` binary and embedded in LuCI.

See the [documentation](#documentation) below for the detail behind each of
these concepts.

---

# Releases & Installation

Pushing a `v*` tag (e.g. `v0.2.0`) triggers the release workflow, which
cross-compiles `meshd` as a static, CGO-free binary for four ISA groups and
publishes a GitHub Release with per-architecture OpenWrt packages attached.

A single binary is ABI-compatible across every OpenWrt subtarget in its ISA
group, so the same binary is repackaged under each CPU-tuned arch name that
`opkg`/`apk` checks against. The release covers the dominant real-world
subtargets:

| Architecture | OpenWrt package arch (covers) |
|--------------|-------------------------------|
| x86_64 | `x86_64` (VMs, x86 routers) |
| arm64 | `aarch64_generic`, `aarch64_cortex-a53`, `aarch64_cortex-a72` (mvebu, bcm27xx/RPi, mediatek filogic) |
| armv7 | `arm_cortex-a7_neon-vfpv4`, `arm_cortex-a9` (ipq40xx and similar) |
| mipsle | `mipsel_24kc`, `mipsel_74kc` (ramips/mt7621) |

Find your device's arch and install the matching package:

```sh
opkg print-architecture          # 23.05 and earlier
opkg install meshd_<version>_<arch>.ipk

apk info --print-arch            # 24.10+
apk add --allow-untrusted meshd-<version>-<arch>.apk
```

If your subtarget is not listed above, the binary is still compatible within
its ISA group; install with `opkg install --force-architecture`.

### As an opkg feed

Each release also ships an opkg index (`Packages`/`Packages.gz`), so you can add
the release as a feed and let `opkg` pick the matching arch and resolve updates:

```sh
echo 'src/gz omm https://github.com/and-elf/omm/releases/download/v0.2.0' >> /etc/opkg/customfeeds.conf
opkg update
opkg install meshd luci-app-meshd   # luci-app-meshd is optional (LuCI UI)
```

If the release was signed (a `Packages.sig` asset is present), trust the feed's
public key once, then `opkg update` verifies the index automatically:

```sh
opkg-key add omm-feed.pub        # the maintainer's published usign public key
```

If a release is **unsigned** (no `Packages.sig`), add `option check_signature 0`
to that feed line instead. Generating the key and turning on signing is a
maintainer step; see [OpenWrt Integration & Packaging](doc/openwrt.md). A stable
rolling feed URL (vs the per-release URL above) is still planned.

Build the single-binary product (frontend + daemon) locally with
`./scripts/build.sh` — see [Architecture & Components](doc/architecture.md).

---

# Documentation

| Document | Contents |
|----------|----------|
| [Architecture & Components](doc/architecture.md) | System diagram; the meshd daemon, LuCI app, and PWA |
| [Network Model](doc/network-model.md) | Homes, Nodes, Home identity, controllers, multi-home membership |
| [Discovery & Enrollment](doc/discovery-enrollment.md) | Controller discovery, first boot, create/join Home, enrollment state machine |
| [Node Enrollment Protocol](doc/enrollment.md) | Wire-level discovery + enrollment contract exercised by the e2e tests |
| [Profiles](doc/profiles.md) | Per-Home profile contents, on-disk storage, profile switching |
| [Topology](doc/topology.md) | Topology collection, aggregation, visualization, client mapping |
| [API](doc/api.md) | ubus (`meshd.*`) and REST endpoints |
| [Security Model](doc/security.md) | mutual TLS, Home-issued certificates, trust model |
| [OpenWrt Integration & Packaging](doc/openwrt.md) | on-device layout, the LuCI app, the auth model, packaging |
| [Roadmap](doc/roadmap.md) | Planned features not yet implemented |
| [Go Implementation Appendix](doc/implementation.md) | Implementation guidance for the Go codebase |
| [PWA (`web/README.md`)](web/README.md) | Frontend stack, serving model, development, current status |

---

# Success Criteria

A user should be able to:

1. Flash OpenWrt
2. Open setup page
3. Create Home
4. Add additional node
5. Approve adoption
6. View topology

without manually configuring:

* 802.11s
* batman-adv
* Bridges
* Firewall zones
* Routing
* VLANs

The user should think in terms of:

```text
Homes
Nodes
Clients
```

rather than networking internals.
