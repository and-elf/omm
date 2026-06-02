# Discovery & Enrollment

The conceptual flow from an unclaimed device to an active mesh member:
controller discovery, first boot, creating or joining a Home, and the
enrollment state machine. For the wire-level contract (messages, sequence,
error handling) see the [Node Enrollment Protocol](enrollment.md). See the
[README](../README.md) for the project overview.

---

## Controller Discovery

Discovery methods:

1. mDNS
2. UDP Broadcast
3. Future: LLDP

Service Advertisement:

```text
_mesh._tcp.local
```

Controller broadcasts:

```json
{
  "home_id": "8f53dc9e",
  "name": "Home",
  "controller_id": "gw01",
  "api": "https://0.0.0.0:8081"
}
```

The `api` field tells joiners where to reach the controller's mesh control
plane. A controller announces its *bind* address, which is usually the
unspecified `0.0.0.0`; the listener fills the real host from the **UDP packet's
source address**, so a joiner gets a dialable URL with **no per-device
configuration**. (Set `MESHD_API_ADVERTISE` to override with an explicit URL.)

Because UDP broadcast is confined to a single L2 segment, this zero-config
discovery works between devices on the same bridged LAN — e.g. a wired
controller (even one with no radios, such as a Raspberry Pi) and a wifi AP
plugged into the same network. Across a router, use an explicit join URL
(`MESHD_JOIN` / the enrollment UI).

---

## First Boot

Default state:

```text
UNCLAIMED
```

Device starts:

```text
SSID:
OpenWrt-Setup-XXXX

IP:
192.168.254.1
```

Web UI options:

```text
Create New Home
Join Existing Home
Advanced OpenWrt Setup
```

---

## Create New Home

Workflow:

```mermaid
flowchart TD

A[Unclaimed Device]
--> B[Create New Home]

B --> C[Generate Home ID]
C --> D[Generate Certificates]
D --> E[Configure DHCP]
E --> F[Configure Mesh]
F --> G[Become Controller]
```

---

## Join Existing Home

Workflow:

```mermaid
flowchart TD

A[Unclaimed Device]
--> B[Scan Nearby WiFi]

B --> C[Join Existing LAN]

C --> D[Discover Controller]

D --> E[Pending Adoption]

E --> F[Controller Approval]

F --> G[Receive Profile]

G --> H[Apply Config]

H --> I[Join Mesh]
```

---

## Enrollment State Machine

```mermaid
stateDiagram-v2

[*] --> Unclaimed

Unclaimed --> Discovering
Discovering --> ControllerFound

ControllerFound --> Authenticating
Authenticating --> ReceivingProfile

ReceivingProfile --> ApplyingConfig

ApplyingConfig --> Active

Active --> ProfileSwitch

ProfileSwitch --> Active
```
