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

### Automatic fallback

The tier is chosen at apply time, not guessed up front. After authoring the
802.11s + AP interfaces and reloading, `ApplyProfile` verifies the mesh actually
came up (via `ubus call network.wireless status` — the mesh section must have a
running ifname on an up, non-failed radio). If it did not — the usual cause is a
node without a mesh-capable `wpad` — meshd **removes the mesh interface**, so the
radio re-sets cleanly with the AP alone, and records a degraded multi-AP state
with a reason and remediation. The outcome is persisted and surfaced:

* **`GET /status`** returns a `backhaul` object: `{mode, reason, remediation}`
  where `mode` is `802.11s`, `multi_ap` or `unknown`.
* **Topology** carries each node's `mesh_mode` on its vertex (like `backhaul`),
  so the controller — and the LuCI app — show every node's tier across the mesh.
* The **LuCI app** shows the controller's backhaul mode and, when degraded, the
  reason and the `wpad-mesh` remediation; degraded nodes are marked in the graph.

> **Status.** Implemented: the apply-time verification, automatic degrade,
> `/status` backhaul, and per-node `mesh_mode` in topology + LuCI. Still
> follow-up: declaring/installing a mesh-capable `wpad` from the package, and the
> batman-adv multi-hop layer (today the mesh is bridged directly onto `lan`, so
> chaining beyond a single hop is not yet guaranteed).

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

A unique id is essential: discovery treats an announcement whose `home_id`
matches the receiver's own as "self" and ignores it, so two devices sharing an
id are invisible to each other and cannot onboard. When `home_id` is not
configured, the daemon therefore derives a stable, unique one from the device
identity (`home-<node-id-prefix>`) rather than falling back to a shared literal;
the wizard / companion app replaces it with a friendly id when a Home is created
or joined.

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
