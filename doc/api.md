# API

OMM exposes its functionality both over ubus (`meshd.*`) and as a REST API on
the same origin as the PWA. See the [README](../README.md) for the project
overview.

---

## Status

```text
meshd.status        # HTTP: GET /status
```

Returns daemon readiness plus the applied **wireless-backhaul** outcome:

```json
{
  "status": "ready",
  "backhaul": {
    "mode": "multi_ap",
    "reason": "802.11s mesh did not start — no mesh-capable wpad on this node",
    "remediation": "install wpad-mesh-wolfssl (or -mbedtls to match the image) and re-apply the profile"
  }
}
```

`backhaul.mode` is `802.11s` when the mesh formed, `multi_ap` when it degraded to
a wired multi-AP (see the [automatic fallback](network-model.md#automatic-fallback)),
or `unknown` before any profile is applied. `reason` and `remediation` are
present only on a degrade.

---

## Topology

```text
meshd.topology
```

Returns graph data. The HTTP API exposes this as `GET /topology`: mesh nodes,
batman-adv links with transmit quality (TQ), and associated clients with signal
(RSSI), band and tx/rx rates. Sources are `batctl o` (originators) and hostapd
`get_clients` over ubus; the PWA renders it with Cytoscape.js.

Each node's vertex also carries `backhaul` (ethernet/wireless — the physical
uplink) and `mesh_mode` (`802.11s`/`multi_ap` — the wireless-backhaul tier), so
the graph shows how every node reaches the mesh and whether it formed an 802.11s
mesh or degraded to a wired AP.

Topology is aggregated across the mesh: each node periodically pushes its local
view to the controllers it has joined (`POST /topology/report`), and a
controller's `GET /topology` merges its own view with fresh member reports
(deduplicating nodes/links/clients, keeping the strongest TQ per link, and
enriching a node with the `backhaul`/`mesh_mode` it self-reports). A leaf node's
`GET /topology` shows just its local view.

---

## Nodes

```text
meshd.nodes
```

Returns node inventory.

---

## Adopt

```text
meshd.adopt
```

Approves enrollment.

---

## Scan

```text
meshd.scan
```

Discovers controllers and networks. The HTTP API exposes this as `GET /scan`,
returning nearby controllers (Homes) the daemon has heard announce. Each daemon
passively listens for UDP announcements into a short-lived cache, so the scan
answers instantly; the PWA's enrollment screen lists the results to join with
one click instead of typing a controller URL.

---

## Lifecycle (REST)

Destructive operations for forgetting Homes/nodes and factory-resetting the
device. All return `204 No Content` on success.

| Endpoint | Effect |
|----------|--------|
| `DELETE /homes/{homeID}` | Forget a Home and everything scoped to it: its profile, its enrollment records, and every node's membership of it (current-home pointers and trusted-homes lists are cleared; the nodes themselves survive). Returns `409 Conflict` if it is the **active** Home — switch to another Home first — and `404` if unknown. |
| `DELETE /nodes/{nodeID}` | Decommission a node, removing it and its enrollment record. `404` if unknown. |
| `POST /reset` | Factory reset: clear all stored state (Homes, nodes, profiles, enrollments, active Home, setup flag), returning the device to its just-installed condition. Also used to reset state between e2e runs that reuse the same container. |
