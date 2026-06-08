# Network Posture (lifecycle-managed)

How a node configures its **network, DHCP and firewall** depends on where it is
in its lifecycle. A freshly-flashed OpenWrt device is a *router* — authoritative
DHCP on `br-lan`, a routed `wan` with `input REJECT` — which is correct for a
home gateway but **breaks discovery**: a controller's UDP broadcast
(`255.255.255.255:45678`) arriving on the unclaimed node's `wan` is dropped by
the firewall before the listener sees it, so `GET /scan` is always empty and
wired auto-onboard can never find a home. See the
[discovery flow](discovery-enrollment.md) and
[network model](network-model.md#backhaul--mesh-model).

meshd therefore manages a node's network posture as a function of lifecycle
state, the same apply-and-verify way it manages [wireless](profiles.md).

---

## Postures

The role is **auto-determined** — there is no static gateway/node config. Every
device boots, scans for homes ([discovery](discovery-enrollment.md)), and the
home it settles on decides its role; the posture follows. The active home is
persisted, so an established device boots straight into its settled posture.

| Role | When (auto) | Network | DHCP | Firewall |
|------|-------------|---------|------|----------|
| **Guest** (dumb-AP) | no active home yet — still discovering | wired uplink bridged into `br-lan`; `wan`/`wan6` disabled; `lan` is a DHCP **client** | LAN DHCP server **off** (`dhcp.lan.ignore=1`) | single `lan` zone, `input ACCEPT` |
| **Controller** (gateway) | active home is this node's **own** Home | routed `wan` uplink; `lan` static gateway | authoritative LAN DHCP server | `wan input REJECT` — LuCI/SSH not exposed on WAN (stock; Guest never modifies firewall, so re-enabling `wan` restores the lockdown) |
| **Mesh node** | active home is **another** Home | bridged into the home, uplink to the controller | LAN DHCP server off (controller serves) | `lan` accept |

The **Guest** posture is the fix for the discovery gap: bridging the uplink into
`lan` makes the node L2-adjacent to the controller, so broadcasts arrive on a
`lan`-zone (input-accept) interface, and standing down its own authoritative
DHCP removes the two-servers-on-one-segment conflict. This matches the
discovery doc's "same bridged LAN" assumption — which the default router posture
silently violated.

### Any port (cabling)

In Guest posture the operator should not have to care which jack they use:

* **LAN ports** are already members of `br-lan`, which Guest runs as a DHCP
  client — so plugging into any LAN port works, and which one does not matter.
* The **WAN port** is auto-detected (`network.wan.device`) and bridged into
  `br-lan` too, so it works the same as a LAN port.
* A **single-jack AP** (a range-extender-style device with one ethernet jack)
  works whether that lone port is wired as `wan` (auto-detected and bridged in)
  or as `lan` (already in `br-lan`).

So whichever ethernet jack the installer plugs into, the node lands on the home
L2 and discovers the controller.

The setup AP keeps working in Guest posture: it has its **own** network/dhcp
sections ([`internal/setupap`](../internal/setupap)), independent of `lan`, so a
companion app still reaches an unclaimed node before it is on any network.

## Lifecycle transitions

```text
          first boot (no active home)
               │
               ▼
          ┌─────────┐   selected own home (no other found)   ┌────────────┐
          │  Guest  │ ─────────────────────────────────────▶ │ Controller │
          │ dumb-AP │                                         │  gateway   │
          │ (scans) │ ──┐ joined another home                 └────────────┘
          └─────────┘   ▼
                   ┌────────────┐
                   │ Mesh node  │
                   └────────────┘
```

Role is derived from the auto-determined active home (`DecideRole`), not
configured directly:

* **no active home yet** → **Guest** — the transient discovery posture that lets
  the device scan; every device passes through it on a fresh boot.
* active home is **this node's own** Home → **Controller**.
* active home is **another** Home → **Mesh node**.

Because the active home is persisted, an established controller or node boots
straight into its settled posture and never reverts to Guest — only an undecided
device discovers, which removes any risk of a running gateway tearing down its
own DHCP/WAN on reboot. The setup-AP rescue path is on its own network, so it
works in every posture.

## Safety (do not strand the device)

Reconfiguring `network`/`firewall` on a live device can lock out an operator, so:

* **Idempotent + verified.** Apply authors the target posture and verifies it
  took (interface up, DHCP-client lease or static address present) the same way
  the mesh apply verifies 802.11s. A failed apply is logged and the previous
  posture left in place.
* **Opt-out.** `option manage_network '0'` (`MESHD_MANAGE_NETWORK=0`) disables
  posture management entirely, for operators who wire the device by hand.
* **Setup AP preserved.** Guest posture never touches the `ommsetup` sections,
  so the rescue path (companion app over the setup SSID) always remains.
* **Transition only on state change**, never re-applied on every boot tick, to
  avoid churning a working network.

## Configuration

| UCI (`meshd.main`) | Env | Default | Behaviour |
|--------------------|-----|---------|-----------|
| `manage_network` | `MESHD_MANAGE_NETWORK` | `0` | Enable lifecycle posture management. |
| `uplink_port` | `MESHD_UPLINK_PORT` | _(empty = auto)_ | Wired uplink device bridged into `br-lan` in Guest posture. Empty auto-detects it from `network.wan.device`, so **any jack works** without per-board config — including a single-jack AP whose only port is wired as `wan`. Set explicitly only to override. |
| `lan_device` | `MESHD_LAN_DEVICE` | `@device[0]` | UCI section of the LAN bridge device whose `ports` list is edited. |

> **Status.** Implemented and unit-tested
> ([`internal/netposture`](../internal/netposture)): `DecideRole`
> (lifecycle → posture), `Apply` (authors the Guest/Controller/Mesh-node UCI via
> the new list-valued `uci` `add_list`/`del_list`), and the daemon wiring (apply
> at boot and on the claim/join transitions). **Off by default** — verify the
> Guest transition on the target board before enabling, since bridging the
> uplink and disabling the routed WAN can strand a hand-wired device. The
> Mesh-node posture currently only stands down authoritative DHCP; fuller mesh
> bridging and the batman-adv layer are follow-ups.
