# Network Model

The core domain concepts — Homes, Nodes, identity, controllers, and multi-home
membership. See the [README](../README.md) for the project overview.

---

## Home

A Home is a logical mesh deployment.

Examples:

* Main House
* Summer Cottage
* Parents House

Each Home has:

```text
Home ID
Name
Controller
Mesh Credentials
Certificates
Configuration Profiles
```

## Node

A Node is a physical OpenWrt device.

Each node has:

```text
Device Identity
Private Key
Certificate Store
Trusted Homes
Profiles
```

## Portable Nodes

Nodes may belong to multiple Homes.

Example:

```text
Node:
  Garage AP

Trusted Homes:
  Home
  Cottage

Current Home:
  Cottage
```

A node may only be active in one Home at a time.

---

## Backhaul & Mesh Model

How nodes interconnect depends on what the node's wireless stack can do. OMM
authors the same profile everywhere (see [Profiles](profiles.md)); the resulting
network is one of two tiers, decided by whether 802.11s is available on the node.

### Tier 1 — wireless mesh (802.11s)

When the node has a **mesh-capable `wpad`** (see
[Wireless backhaul requirements](openwrt.md#wireless-backhaul-requirements-80211s)),
the `omm_mesh` interface comes up in `mode 'mesh'` and forms a self-forming,
self-healing 802.11s backhaul. This is the tier you need when a node has **no
wired uplink** — e.g. a garage AP reached only over the air — and the only tier
that supports:

* **Wireless backhaul** — a node joins the network with no ethernet run.
* **Chained nodes** — multi-hop paths (node → node → gateway) via 802.11s HWMP.
* **Node mobility** — a relocated node re-attaches over the air.

> Multi-hop reliability and the topology view (`batctl`) assume **batman-adv**
> layered on the mesh interface. That layer is documented in
> [architecture.md](architecture.md) but not yet authored by `ApplyProfile`
> (today the mesh interface is bridged directly onto `lan`), so chaining beyond
> a single hop is not yet guaranteed. Single-hop wireless backhaul (house →
> garage) works with 802.11s alone.

### Tier 2 — wired multi-AP (degraded)

When the node lacks a mesh-capable `wpad` (the default `wpad-basic-*` cannot do
802.11s), the `omm_mesh` interface cannot start. The node still brings up
`omm_ap` bridged to `lan`, so it operates as a **plain access point on a wired
backhaul** — a standard multi-AP network sharing one SSID and DHCP. This is a
legitimate, robust deployment for nodes that *do* have ethernet, but it does
**not** provide wireless backhaul, chaining, or wireless node mobility; client
roaming is basic same-SSID roaming (no 802.11r/k/v).

A node with neither a mesh-capable `wpad` nor an ethernet uplink has no backhaul
at all and is effectively isolated.

> **Status.** The 802.11s and AP interfaces are both authored today
> ([`internal/profiles`](../internal/profiles)). The pieces that make the tiers
> explicit — declaring/installing a mesh-capable `wpad`, a preflight that
> detects 802.11s support and reports a clear error instead of a silent netifd
> failure, and the batman-adv multi-hop layer — are tracked as follow-up.

---

## Home Identity

Homes are identified by:

```text
UUID
```

Example:

```text
8f53dc9e-42e1-4d8d-a379-ef2f5c6c5f41
```

SSID names are not authoritative.

A Home may change:

* SSID
* Password
* Controller

without changing Home identity.

---

## Controller Model

A Controller is a node currently acting as coordinator.

Responsibilities:

* Profile distribution
* Enrollment approval
* Certificate signing
* Topology aggregation

Controllers are local-only.

No cloud component exists.

---

## Multi-Home Support

Supported:

```text
Home
Cottage
Parents
```

Unsupported:

```text
Simultaneous Active Membership
```

Reason:

Avoid conflicting:

* DHCP
* VLANs
* Firewall rules
* Routing
