# Security Model

How OMM authenticates nodes and establishes trust between Homes — all without a
cloud component. See the [README](../README.md) for the project overview.

---

## Mesh control plane (node ↔ controller)

The mesh plane runs **mutual TLS** rooted in a **per-Home CA**:

* Each controller generates and persists a Home CA (`identity.LoadOrCreateCA`,
  under the identity dir) and publishes its certificate in the Home record.
* On adoption the controller signs a **leaf certificate** for the node's public
  key (CN = node ID), valid for both client and server TLS, and returns it with
  the Home CA in the enrollment result.
* Post-enrollment mesh endpoints (`GET /homes/{id}`, `/topology/report`) require
  a verified client certificate; the bootstrap `/enroll/*` and `/health` do not.
* Certificate identity is the node ID (CN), not a DNS name, so verification
  checks "issued by the pinned Home CA" rather than a hostname.

**Trust bootstrap (TOFU):** a node's very first enrollment is over encrypted but
unverified TLS; it pins the Home CA returned in the enrollment result and
verifies every later connection against it.

Enrollment also remains gated by the existing challenge-response (the node
proves it controls its key) and by **controller approval** (manual, or
auto-adopt for tests).

## Management plane (admin ↔ device)

The management API is intended to bind to **localhost** and be reached only
through LuCI's authenticated session (the `meshd` rpcd object) — see
[OpenWrt Integration](openwrt.md). The listener split is in place; making
localhost the default depends on the LuCI-native UI.

No cloud authentication exists; trust is entirely Home-based and local.
