import { describe, expect, it } from 'vitest'
import { signalClass, toElements, tqClass } from './topology'

describe('topology element builder', () => {
  it('classifies signal and tq', () => {
    expect(signalClass(-50)).toBe('good')
    expect(signalClass(-70)).toBe('fair')
    expect(signalClass(-85)).toBe('weak')
    expect(signalClass(0)).toBe('unknown')
    expect(tqClass(255)).toBe('good')
    expect(tqClass(150)).toBe('fair')
    expect(tqClass(50)).toBe('weak')
  })

  it('builds nodes, links and client associations', () => {
    const els = toElements({
      nodes: [
        { id: 'self', label: 'Gateway', role: 'self' },
        { id: 'n2', label: 'n2', role: 'node' },
      ],
      links: [{ source: 'self', target: 'n2', tq: 210 }],
      clients: [{ mac: 'aa:bb', ap: 'self', signal: -55, band: '5GHz' }],
    })

    const self = els.find((e) => e.data.id === 'self')
    expect(self?.classes).toContain('node--self')

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

  it('tolerates null arrays', () => {
    expect(toElements({ nodes: null, links: null, clients: null })).toEqual([])
  })
})
