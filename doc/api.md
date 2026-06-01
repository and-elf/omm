# API

OMM exposes its functionality both over ubus (`meshd.*`) and as a REST API on
the same origin as the PWA. See the [README](../README.md) for the project
overview.

---

## Status

```text
meshd.status
```

Returns:

```json
{
  "state": "active",
  "home": "Home"
}
```

---

## Topology

```text
meshd.topology
```

Returns graph data. The HTTP API exposes this as `GET /topology`: mesh nodes,
batman-adv links with transmit quality (TQ), and associated clients with signal
(RSSI), band and tx/rx rates. Sources are `batctl o` (originators) and hostapd
`get_clients` over ubus; the PWA renders it with Cytoscape.js.

Topology is aggregated across the mesh: each node periodically pushes its local
view to the controllers it has joined (`POST /topology/report`), and a
controller's `GET /topology` merges its own view with fresh member reports
(deduplicating nodes/links/clients, keeping the strongest TQ per link). A leaf
node's `GET /topology` shows just its local view.

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
