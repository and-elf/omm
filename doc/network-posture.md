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

### Any port (cabling) — issue #42

On a **non-controller** node the operator should not have to care which jack they
use, for either connecting a computer or extending the network with another node:

* **LAN ports** are already members of `br-lan` — so plugging into any LAN port
  works, and which one does not matter.
* The **WAN port** is stood down as a routed uplink (`network.wan.disabled=1`)
  and folded into `br-lan` too, so it is a plain bridge **relay** port exactly
  like a LAN port. Both Guest and the claimed mesh-node posture fold it.
* A **single-jack AP** (a range-extender-style device with one ethernet jack)
  works whether that lone port is wired as `wan` (stood down and bridged in) or as
  `lan` (already in `br-lan`).

So whichever ethernet jack the installer plugs into, the node lands on the home
L2 — a computer gets a controller DHCP lease and another node extends the network
by plain L2 bridging over the fast wire. The 802.11s mesh is a **carrier-loss
backhaul standby**: the daemon's failover brings it up only if the node's wired
uplink drops (see [network model](network-model.md)), so the wire is always
preferred and a wired+wireless bridge loop never forms.

A **controller** is the exception: its **WAN port is the routed internet uplink**
and is never folded into `br-lan` or offered to the classifier. Its LAN ports
still take clients and wired backhaul like any other node.

> Because folding the wan jack requires standing down the routed wan (a netdev
> cannot be both `network.wan`'s device and a `br-lan` member), and a satellite
> that keeps its own DHCP would be a rogue server on the home segment, "any port
> works" is inseparable from posture management — which is why `manage_network`
> defaults **on** (see below). `manage_network=0` opts a hand-wired device out
> entirely, leaving its ports as the operator cabled them.

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
| `manage_network` | `MESHD_MANAGE_NETWORK` | `1` | Lifecycle posture management. **On by default** — it is what makes any ethernet jack work on a non-controller (#42). Set `0` to opt a hand-wired device out entirely. |
| `uplink_port` | `MESHD_UPLINK_PORT` | _(empty = auto)_ | Wired uplink device bridged into `br-lan` in Guest posture. Empty auto-detects it from `network.wan.device`, so **any jack works** without per-board config — including a single-jack AP whose only port is wired as `wan`. Set explicitly only to override. |
| `lan_device` | `MESHD_LAN_DEVICE` | `@device[0]` | UCI section of the LAN bridge device whose `ports` list is edited. |

> **Status.** Implemented and unit-tested
> ([`internal/netposture`](../internal/netposture)): `DecideRole`
> (lifecycle → posture), `Apply` (authors the Guest/Controller/Mesh-node UCI via
> the new list-valued `uci` `add_list`/`del_list`), and the daemon wiring (apply
> at boot and on the claim/join transitions). **On by default** (`manage_network`,
> opt-out) — a non-controller stands down its routed WAN and every ethernet jack
> joins the home L2 so any device works on any port (#42), while a controller
> keeps the stock routed-WAN gateway untouched. `manage_network=0` opts a
> hand-wired device out entirely. The
> Mesh-node posture authors the **bridged shape** (`lan` as a DHCP client,
> routed `wan` disabled, authoritative DHCP off) so a claimed node is a pure L2
> bridge into the home and its mesh traffic egresses via the controller's single
> gateway — a claimed node that keeps its own routed/NAT'd `wan` leaves the
> bridged mesh an island that can't reach the home WAN.
>
> **Every ethernet jack is a plain `br-lan` relay port.** Both Guest and the
> Mesh-node posture fold the wan jack into `br-lan` (LAN jacks are already there),
> so any device works on any port (#42). Wired ports are never enslaved to
> batman-adv by default — the 802.11s mesh + `bat0` is a **carrier-loss backhaul
> standby** the daemon's failover brings up only when the wired uplink drops, so
> the fast wire stays primary and wired + mesh never bridge-loop (see
> [network model](network-model.md)). An explicit `MESHD_BATMAN_PORTS` still
> enslaves named ports for a deliberate wired batman backhaul.
