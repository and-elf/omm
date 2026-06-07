import type { Topology } from '@/types'

export interface CyElement {
  group: 'nodes' | 'edges'
  data: Record<string, unknown>
  classes?: string
}

/** Maps signal (RSSI dBm) to a coarse quality class for styling. */
export function signalClass(rssi: number): string {
  if (rssi === 0) return 'unknown'
  if (rssi >= -60) return 'good'
  if (rssi >= -72) return 'fair'
  return 'weak'
}

/** Maps batman TQ (0-255) to a quality class. */
export function tqClass(tq: number): string {
  if (tq >= 200) return 'good'
  if (tq >= 120) return 'fair'
  return 'weak'
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
    elements.push({
      group: 'nodes',
      data: { id: node.id, label: node.label || node.id, backhaul: node.backhaul },
      classes: classes.join(' '),
    })
  }

  for (const link of topo.links ?? []) {
    elements.push({
      group: 'edges',
      data: {
        id: `link:${link.source}->${link.target}`,
        source: link.source,
        target: link.target,
        label: `TQ ${link.tq}`,
      },
      classes: `link link--${tqClass(link.tq)}`,
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
