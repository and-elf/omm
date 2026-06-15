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
* **Chained nodes** — multi-hop paths (node → node → gateway).
* **Node mobility** — a relocated node re-attaches over the air.

#### batman-adv routing layer

When batman-adv is enabled (`MESHD_BATMAN`, default on where the kernel module
is present), `ApplyProfile` does **not** bridge the mesh interface straight onto
`lan`. Instead it authors a batman-adv mesh and bridges that:

* A **soft interface** `bat0` (`proto 'batadv'`) with **bridge-loop-avoidance**
  on. BLA is what lets a node carry *both* a wired link and the wireless mesh at
  once without a broadcast storm — batman detects the redundant L2 path and
  dedups it, where plain bridge STP could not (the consumer switches in the home
  kit do not forward STP BPDUs across the wired path).
* One **hard interface** (`proto 'batadv_hardif'`, `master 'bat0'`) per backhaul
  link enslaved to the mesh: the 802.11s `omm_mesh` vif **and** the node's wired
  backhaul uplink. batman-adv treats wired and wireless links uniformly and
  computes the best loop-free path across the mix.
* `bat0` is the only batman device bridged into `br-lan`; DHCP and clients ride
  on top of it.

**Auto backhaul detection.** A joined node does not need its backhaul port named
in config. At startup it resolves the wired uplink (in priority order:
`uplink_port` → the `network.wan.device` convention → a discrete `backhaul_iface`)
and, if that device has carrier, **gates enslavement on whether a batman peer
actually speaks on that wire** — a passive sniff for batman-adv OGM frames
(ethertype `0x4305`), which does not enslave or transmit, so it is safe while the
port is still a plain bridge member. This yields three cases:

* **Wireless-only** (no cabled uplink): nothing enslaved; the mesh is the only
  batman link.
* **batman wired** (a cabled uplink *with* a batman peer on it — a dedicated
  inter-node wired link): the uplink is enslaved to `bat0` and taken *out* of
  `br-lan` (a device that is both a bridge member and a batadv hardif is the
  redundant L2 path that storms). Client jacks stay normal `br-lan` ports. batman
  then selects the wired path while the cable is up and re-routes over the mesh
  within a second of a cable pull, preferring the wire again on replug — wired-
  primary + wireless-backup + multi-hop, natively, no bridge loop.
* **plain wired + standby mesh** (a cabled uplink with *no* batman peer — a node on
  the controller's **shared client LAN**, where the controller does not run batman
  on that switch port): the uplink stays a plain `br-lan` port (so it keeps L2
  reach to the controller), and the 802.11s mesh is held as an **admin standby** —
  the carrier-toggle failover enables it only on wire loss, so wired + mesh never
  bridge-loop.

> Why the gate: a batman hardif only carries traffic when the *other* end of the
> wire also runs batman on it. Enslaving a wire whose far end is a plain switch
> (the controller's client LAN) would silently kill that node's wired path and
> force it onto wireless. The peer-on-wire sniff distinguishes a dedicated
> inter-node link from a controller-LAN drop.

> Resolution runs on **joined nodes only**. A controller's `wan` is its *internet*
> uplink and must never be pulled into batman. `MESHD_BATMAN_PORTS` is an explicit
> override for deliberate wiring (e.g. a controller's dedicated downstream wired
> backhaul port) and, when set, enslaves those ports directly and skips resolution.

Because batman-adv forwards at layer 2.5 across *any* mix of enslaved links, a
node need not distinguish "edge" from "relay": a wired node automatically keeps
relaying for a wireless child, and a chain like

```text
controller ──wired──▶ AP ──wireless──▶ AP ──wired──▶ device
```

is just a route batman-adv selects. This supersedes the per-node
"disable the mesh while my own wire is up" failover (a workaround for the
bridge loop), which is **not used** when batman-adv is active.

> Without batman-adv (module absent, or `MESHD_BATMAN=0`) the mesh interface is
> bridged directly onto `lan` as before: single-hop wireless backhaul (house →
> garage) works, but multi-hop chaining and simultaneous wired+wireless on one
> node are not guaranteed.

The layer needs `kmod-batman-adv` (kernel module + netifd proto handlers) and
`batctl` on each device, and — because netifd loads proto handlers only at
startup — a netifd restart after installing them. See
[Routing layer requirements](openwrt.md#routing-layer-requirements-batman-adv).

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
> `/status` backhaul, per-node `mesh_mode` in topology + LuCI, and the
> **batman-adv routing layer** (`bat0` soft interface with bridge-loop-avoidance,
> a hard interface per wireless/wired backhaul link, `bat0` bridged into
> `br-lan`) authored by `ApplyProfile` when enabled. Still follow-up:
> declaring/installing a mesh-capable `wpad` from the package, and live
> multi-hop validation on the home kit.

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
