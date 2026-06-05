# Profiles

Per-Home configuration: what a profile contains, how it is stored on disk, and
how a node switches between Homes. See the [README](../README.md) for the
project overview.

---

## Scope

A profile carries the settings OMM orchestrates — node name, mesh SSID/key, and
an optional client-AP SSID/key — which it applies to UCI and reloads on the
running system when the Home becomes active (see
[Profile Switching](#profile-switching)).

When a Home becomes active, `ApplyProfile` authors two interfaces on the
configured radio (default `radio0`), both attached to the existing `lan` so
meshed nodes and AP clients share the controller's LAN and its DHCP:

- **`omm_mesh`** — the 802.11s backhaul, `mesh_id` = `mesh_ssid`, SAE-encrypted
  when `mesh_key` is set, so other nodes mesh in.
- **`omm_ap`** — a client-facing access point, `ssid` = `ap_ssid` (falling back
  to `mesh_ssid`) with `ap_key`/`mesh_key` as a WPA2 passphrase, so phones and
  laptops can join the Home and get an address.

Both use dedicated section names so an operator's own wifi-iface sections are
never touched, and the radio is enabled only when at least one SSID is set. The
sections are authored with create-or-update semantics, so a stock device with
no prior OMM wireless comes up correctly.

Lower-level, per-interface policy such as **VLANs and firewall rules** sits
below this and is left to OpenWrt's own tooling (LuCI / UCI). OMM does not
author that configuration: the `vlans` field on a profile is stored but not
applied, so an operator's LuCI-managed VLAN and firewall setup is never
overwritten.

## Profile System

Each Home has an independent profile.

Example:

```text
Home
 ├── Node Name: Garage
 ├── VLANs
 ├── Firewall
 └── Mesh Settings

Cottage
 ├── Node Name: Guest House
 ├── VLANs
 ├── Firewall
 └── Mesh Settings
```

---

## Profile Storage

Directory Layout:

```text
/etc/meshd/

homes/
 ├── home/
 ├── cottage/
 └── parents/

active-home
```

Each Home contains:

```text
metadata.json
wireless.json
network.json
mesh.json
```

---

## Profile Switching

Nodes scan for controllers.

Selection order:

1. Last Active Home
2. Strongest Signal
3. User Selection

Workflow:

```mermaid
flowchart TD

A[Boot]
--> B[Discover Controllers]

B --> C{Known Home?}

C -->|No| D[Remain Unclaimed]

C -->|Yes| E[Select Home]

E --> F[Load Profile]

F --> G[Apply UCI]

G --> H[Reload Network]
```
