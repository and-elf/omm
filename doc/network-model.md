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
