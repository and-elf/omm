import { afterEach, describe, expect, it, vi } from 'vitest'

import { randomId } from './uuid'

const V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/

describe('randomId', () => {
  afterEach(() => vi.restoreAllMocks())

  it('uses crypto.randomUUID when available', () => {
    vi.spyOn(globalThis.crypto, 'randomUUID').mockReturnValue(
      '11111111-1111-4111-8111-111111111111',
    )
    expect(randomId()).toBe('11111111-1111-4111-8111-111111111111')
  })

  it('falls back to getRandomValues in a non-secure context (no randomUUID)', () => {
    // jsdom may not expose randomUUID; force the fallback regardless.
    vi.spyOn(globalThis.crypto, 'randomUUID').mockImplementation(() => {
      throw new TypeError('not available')
    })
    // Make the fallback deterministic.
    vi.spyOn(globalThis.crypto, 'getRandomValues').mockImplementation((arr) => {
      const u8 = arr as Uint8Array
      for (let i = 0; i < u8.length; i++) u8[i] = i
      return arr
    })

    const id = randomId()
    expect(id).toMatch(V4)
    // Version nibble is 4 and variant nibble is 8..b regardless of input.
    expect(id[14]).toBe('4')
    expect('89ab').toContain(id[19])
  })

  it('produces distinct ids across calls', () => {
    expect(randomId()).not.toBe(randomId())
  })
})
