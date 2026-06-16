import { describe, expect, it } from 'vitest'
import { signalClass, speedLabel, toElements, tqClass } from './topology'

describe('topology element builder', () => {
  it('classifies signal across four tiers', () => {
    expect(signalClass(-45)).toBe('excellent')
    expect(signalClass(-55)).toBe('good')
    expect(signalClass(-68)).toBe('fair')
    expect(signalClass(-85)).toBe('weak')
    expect(signalClass(0)).toBe('unknown')
  })

  it('classifies tq', () => {
    expect(tqClass(255)).toBe('good')
    expect(tqClass(150)).toBe('fair')
    expect(tqClass(50)).toBe('weak')
  })

  it('formats wired link speed', () => {
    expect(speedLabel(10000)).toBe('10G')
    expect(speedLabel(5000)).toBe('5G')
    expect(speedLabel(2500)).toBe('2.5G')
    expect(speedLabel(1000)).toBe('1G')
    expect(speedLabel(100)).toBe('100M')
    expect(speedLabel(0)).toBe('')
    expect(speedLabel(undefined)).toBe('')
  })

  it('renders wired links solid with a speed label and wireless dashed with rssi', () => {
    const els = toElements({
      nodes: [
        { id: 'ctrl', label: 'Gateway', role: 'self' },
        { id: 'n1', label: 'Kitchen', role: 'node' },
        { id: 'n2', label: 'Office', role: 'node' },
      ],
      links: [
        { source: 'ctrl', target: 'n1', tq: 255, kind: 'wired', speed_mbps: 2500 },
        { source: 'ctrl', target: 'n2', tq: 200, kind: 'wireless', signal: -58 },
      ],
      clients: null,
    })

    const wired = els.find((e) => e.data.id === 'link:ctrl->n1')
    expect(wired?.classes).toContain('link--wired')
    expect(wired?.classes).not.toContain('link--wireless')
    expect(wired?.data.label).toBe('2.5G')

    const wireless = els.find((e) => e.data.id === 'link:ctrl->n2')
    expect(wireless?.classes).toContain('link--wireless')
    expect(wireless?.classes).toContain('link--good')
    expect(wireless?.data.label).toBe('-58 dBm · good')
  })

  it('builds nodes, links and client associations', () => {
    const els = toElements({
      nodes: [
        { id: 'self', label: 'Gateway', role: 'self', backhaul: 'ethernet' },
        { id: 'n2', label: 'n2', role: 'node', backhaul: 'wireless' },
      ],
      links: [{ source: 'self', target: 'n2', tq: 210 }],
      clients: [{ mac: 'aa:bb', ap: 'self', signal: -55, band: '5GHz' }],
    })

    const self = els.find((e) => e.data.id === 'self')
    expect(self?.classes).toContain('node--self')
    expect(self?.classes).toContain('node--eth')

    const n2 = els.find((e) => e.data.id === 'n2')
    expect(n2?.classes).toContain('node--wifi')
    expect(n2?.data.backhaul).toBe('wireless')

    const link = els.find((e) => e.data.id === 'link:self->n2')
    expect(link?.group).toBe('edges')
    expect(link?.data.label).toBe('TQ 210')
    expect(link?.classes).toContain('link--good')

    expect(els.find((e) => e.data.id === 'client:aa:bb')?.classes).toBe('client')
    const assoc = els.find((e) => e.data.id === 'assoc:aa:bb')
    expect(assoc?.data.source).toBe('self')
    expect(assoc?.data.label).toBe('-55 dBm 5GHz')
    expect(assoc?.classes).toContain('assoc--good')
  })

  it('marks a node that degraded to multi-AP and carries mesh_mode', () => {
    const els = toElements({
      nodes: [
        { id: 'self', label: 'Gateway', role: 'self', backhaul: 'wireless', mesh_mode: 'multi_ap' },
        { id: 'n2', label: 'n2', role: 'node', mesh_mode: '802.11s' },
      ],
      links: null,
      clients: null,
    })

    const self = els.find((e) => e.data.id === 'self')
    expect(self?.classes).toContain('node--multiap')
    expect(self?.data.mesh_mode).toBe('multi_ap')
    expect(self?.data.label).toBe('Gateway ⚠')

    const n2 = els.find((e) => e.data.id === 'n2')
    expect(n2?.classes).not.toContain('node--multiap')
    expect(n2?.data.mesh_mode).toBe('802.11s')
    expect(n2?.data.label).toBe('n2')
  })

  it('tolerates null arrays', () => {
    expect(toElements({ nodes: null, links: null, clients: null })).toEqual([])
  })
})
