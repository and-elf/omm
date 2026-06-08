import { describe, expect, it } from 'vitest'
import { backhaulModeLabel, formatLastSeen } from './format'

describe('formatLastSeen', () => {
  it('renders "never" for a zero/absent timestamp', () => {
    expect(formatLastSeen(0)).toBe('never')
  })

  it('renders a localized date string for a real unix timestamp', () => {
    // 2021-01-01T00:00:00Z
    const result = formatLastSeen(1609459200)
    expect(result).not.toBe('never')
    expect(result).toMatch(/2021/)
  })
})

describe('backhaulModeLabel', () => {
  it('labels the known modes and falls back to Unknown', () => {
    expect(backhaulModeLabel('802.11s')).toBe('802.11s mesh')
    expect(backhaulModeLabel('multi_ap')).toBe('Wired multi-AP')
    expect(backhaulModeLabel('unknown')).toBe('Unknown')
    expect(backhaulModeLabel(undefined)).toBe('Unknown')
  })
})
