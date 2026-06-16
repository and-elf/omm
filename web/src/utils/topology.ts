import type { Topology } from '@/types'

export interface CyElement {
  group: 'nodes' | 'edges'
  data: Record<string, unknown>
  classes?: string
}

/** Maps signal (RSSI dBm) to a four-tier quality class for styling. */
export function signalClass(rssi: number): string {
  if (rssi === 0) return 'unknown'
  if (rssi >= -50) return 'excellent'
  if (rssi >= -60) return 'good'
  if (rssi >= -70) return 'fair'
  return 'weak'
}

/** Maps batman TQ (0-255) to a quality class. */
export function tqClass(tq: number): string {
  if (tq >= 200) return 'good'
  if (tq >= 120) return 'fair'
  return 'weak'
}

/**
 * Formats a wired link speed (Mbps) as a compact label: 1000 -> "1G",
 * 2500 -> "2.5G", 10000 -> "10G", 100 -> "100M". Returns "" when unknown.
 */
export function speedLabel(mbps?: number): string {
  if (!mbps || mbps <= 0) return ''
  if (mbps >= 1000) {
    const g = mbps / 1000
    return `${Number.isInteger(g) ? g : g.toFixed(1)}G`
  }
  return `${mbps}M`
}

/** Maps a node's backhaul type to a styling class (empty when unknown). */
export function backhaulClass(backhaul?: string): string {
  if (backhaul === 'ethernet') return 'node--eth'
  if (backhaul === 'wireless') return 'node--wifi'
  return ''
}

/**
 * Builds Cytoscape elements from a topology graph: mesh nodes, client nodes,
 * mesh links labelled with TQ, and client links labelled with RSSI.
 */
export function toElements(topo: Topology): CyElement[] {
  const elements: CyElement[] = []

  for (const node of topo.nodes ?? []) {
    const classes = ['node']
    if (node.role === 'self') classes.push('node--self')
    const bh = backhaulClass(node.backhaul)
    if (bh) classes.push(bh)
    // A node that degraded to multi-AP gets a marked class and a ⚠ label
    // suffix, so per-node fallback is visible in the graph (the border already
    // encodes ethernet/wireless backhaul, an orthogonal axis).
    const label = node.label || node.id
    if (node.mesh_mode === 'multi_ap') classes.push('node--multiap')
    elements.push({
      group: 'nodes',
      data: {
        id: node.id,
        label: node.mesh_mode === 'multi_ap' ? `${label} ⚠` : label,
        backhaul: node.backhaul,
        mesh_mode: node.mesh_mode,
      },
      classes: classes.join(' '),
    })
  }

  for (const link of topo.links ?? []) {
    // A wired link draws solid and is labelled with its negotiated speed; a
    // wireless link draws dashed, coloured by RSSI quality and labelled with the
    // signal. A link of unknown medium falls back to the batman TQ (legacy view).
    const classes = ['link']
    let label: string
    if (link.kind === 'wired') {
      classes.push('link--wired')
      label = speedLabel(link.speed_mbps) || `TQ ${link.tq}`
    } else if (link.kind === 'wireless') {
      const quality = signalClass(link.signal ?? 0)
      classes.push('link--wireless', `link--${quality}`)
      label = link.signal ? `${link.signal} dBm · ${quality}` : `TQ ${link.tq}`
    } else {
      classes.push(`link--${tqClass(link.tq)}`)
      label = `TQ ${link.tq}`
    }
    elements.push({
      group: 'edges',
      data: {
        id: `link:${link.source}->${link.target}`,
        source: link.source,
        target: link.target,
        label,
      },
      classes: classes.join(' '),
    })
  }

  for (const client of topo.clients ?? []) {
    const id = `client:${client.mac}`
    const band = client.band ? ` ${client.band}` : ''
    elements.push({
      group: 'nodes',
      data: { id, label: client.mac },
      classes: 'client',
    })
    elements.push({
      group: 'edges',
      data: {
        id: `assoc:${client.mac}`,
        source: client.ap,
        target: id,
        label: `${client.signal} dBm${band}`,
      },
      classes: `assoc assoc--${signalClass(client.signal)}`,
    })
  }

  return elements
}
