import { afterEach, describe, expect, it } from 'vitest'

import {
  applyHostTheme,
  importHostStyles,
  isHostThemed,
  readHostTheme,
  syncHostTheme,
  type HostTheme,
} from './theme'

// Builds a stand-in for the LuCI host: a window whose `parent` is a different
// window exposing a styled <body> (and optionally an accent-bearing element),
// mirroring the same-origin LuCI page the PWA iframe is embedded in.
function makeHost(opts: {
  bg: string
  text: string
  font?: string
  accent?: string
  accentSelector?: string
  crossOrigin?: boolean
}): Window {
  const body = { tag: 'body' }
  const accentEl = opts.accent ? { tag: 'accent' } : null
  const doc = {
    get body() {
      return body
    },
    querySelector(sel: string) {
      return opts.accentSelector && sel.includes(opts.accentSelector) ? accentEl : null
    },
  }
  const getComputedStyle = (el: unknown) => {
    if (el === accentEl) return { color: opts.accent, backgroundColor: 'rgba(0, 0, 0, 0)' }
    return { backgroundColor: opts.bg, color: opts.text, fontFamily: opts.font ?? '' }
  }
  const parent = {
    get document() {
      if (opts.crossOrigin) throw new DOMException('Blocked a frame', 'SecurityError')
      return doc
    },
    getComputedStyle,
  } as unknown as Window
  return { parent } as unknown as Window
}

describe('readHostTheme', () => {
  it('returns null when the window has no distinct parent (standalone)', () => {
    const win = {} as unknown as Window
    ;(win as { parent: Window }).parent = win
    expect(readHostTheme(win)).toBeNull()
  })

  it('returns null when the parent document is cross-origin', () => {
    const win = makeHost({ bg: 'rgb(243, 243, 243)', text: 'rgb(0, 0, 0)', crossOrigin: true })
    expect(readHostTheme(win)).toBeNull()
  })

  it('returns null when the host body background is transparent', () => {
    const win = makeHost({ bg: 'rgba(0, 0, 0, 0)', text: 'rgb(0, 0, 0)' })
    expect(readHostTheme(win)).toBeNull()
  })

  it('reads the host background, text and font from a light LuCI theme', () => {
    const win = makeHost({
      bg: 'rgb(243, 243, 243)',
      text: 'rgb(33, 37, 41)',
      font: 'Helvetica, sans-serif',
    })
    expect(readHostTheme(win)).toEqual({
      bg: 'rgb(243, 243, 243)',
      text: 'rgb(33, 37, 41)',
      fontFamily: 'Helvetica, sans-serif',
    })
  })

  it('reads an accent colour from a host action element when present', () => {
    const win = makeHost({
      bg: 'rgb(255, 255, 255)',
      text: 'rgb(0, 0, 0)',
      accent: 'rgb(89, 116, 153)',
      accentSelector: 'cbi-button-action',
    })
    expect(readHostTheme(win)?.accent).toBe('rgb(89, 116, 153)')
  })
})

describe('applyHostTheme', () => {
  afterEach(() => {
    const root = document.documentElement
    root.removeAttribute('style')
    root.classList.remove('theme-host')
  })

  it('writes the host colours onto the root as CSS variables and tags it', () => {
    const theme: HostTheme = {
      bg: 'rgb(243, 243, 243)',
      text: 'rgb(33, 37, 41)',
      accent: 'rgb(89, 116, 153)',
      fontFamily: 'Helvetica, sans-serif',
    }
    const root = document.documentElement

    applyHostTheme(theme, root)

    expect(root.classList.contains('theme-host')).toBe(true)
    expect(root.style.getPropertyValue('--bg')).toBe('rgb(243, 243, 243)')
    expect(root.style.getPropertyValue('--text')).toBe('rgb(33, 37, 41)')
    expect(root.style.getPropertyValue('--accent')).toBe('rgb(89, 116, 153)')
    expect(root.style.fontFamily).toBe('Helvetica, sans-serif')
  })

  it('leaves the accent untouched when the host did not provide one', () => {
    const root = document.documentElement
    applyHostTheme({ bg: 'rgb(255,255,255)', text: 'rgb(0,0,0)' }, root)
    expect(root.style.getPropertyValue('--accent')).toBe('')
  })
})

// Builds a host window whose parent document exposes the given stylesheet
// links and inline styles, for the import path.
function makeStyleHost(opts: {
  links?: Array<{ href: string; media?: string }>
  styles?: string[]
  crossOrigin?: boolean
}): Window {
  const nodes = [
    ...(opts.links ?? []).map((l) => ({ tagName: 'LINK', href: l.href, media: l.media ?? '' })),
    ...(opts.styles ?? []).map((css) => ({ tagName: 'STYLE', textContent: css })),
  ]
  const doc = { querySelectorAll: () => nodes }
  const parent = {
    get document() {
      if (opts.crossOrigin) throw new DOMException('Blocked a frame', 'SecurityError')
      return doc
    },
  } as unknown as Window
  return { parent } as unknown as Window
}

describe('importHostStyles', () => {
  it('clones host stylesheet links and inline styles into the target head', () => {
    const target = document.implementation.createHTMLDocument('t')
    const win = makeStyleHost({
      links: [{ href: 'http://host/luci-static/bootstrap/cascade.css' }],
      styles: ['.x{color:red}'],
    })

    const imported = importHostStyles(win, target)

    expect(imported).toEqual(['http://host/luci-static/bootstrap/cascade.css', '<style>'])
    const link = target.head.querySelector('link[data-host-style]') as HTMLLinkElement
    expect(link.rel).toBe('stylesheet')
    expect(link.href).toBe('http://host/luci-static/bootstrap/cascade.css')
    expect(target.head.querySelector('style[data-host-style]')?.textContent).toBe('.x{color:red}')
  })

  it('inserts host styles ahead of the app’s own bundled CSS', () => {
    const target = document.implementation.createHTMLDocument('t')
    const ours = target.createElement('link')
    ours.rel = 'stylesheet'
    ours.href = 'http://app/assets/index.css'
    target.head.appendChild(ours)

    importHostStyles(win({ links: [{ href: 'http://host/cascade.css' }] }), target)

    const sheets = Array.from(target.head.querySelectorAll('link'))
    expect(sheets[0].getAttribute('data-host-style')).not.toBeNull()
    expect(sheets[sheets.length - 1].href).toBe('http://app/assets/index.css')
  })

  it('does not import the same stylesheet twice', () => {
    const target = document.implementation.createHTMLDocument('t')
    const host = win({ links: [{ href: 'http://host/cascade.css' }] })

    importHostStyles(host, target)
    const second = importHostStyles(host, target)

    expect(second).toEqual([])
    expect(target.head.querySelectorAll('link[data-host-style]').length).toBe(1)
  })

  it('returns an empty list when standalone or cross-origin', () => {
    const standalone = {} as unknown as Window
    ;(standalone as { parent: Window }).parent = standalone
    expect(importHostStyles(standalone, document.implementation.createHTMLDocument('t'))).toEqual(
      [],
    )
    expect(
      importHostStyles(
        makeStyleHost({ links: [{ href: 'x' }], crossOrigin: true }),
        document.implementation.createHTMLDocument('t'),
      ),
    ).toEqual([])
  })

  // Local alias keeping the link-only cases terse.
  function win(opts: { links?: Array<{ href: string }> }): Window {
    return makeStyleHost(opts)
  }
})

describe('isHostThemed', () => {
  it('reflects the theme-host class on the root', () => {
    const root = document.createElement('html')
    expect(isHostThemed(root)).toBe(false)
    root.classList.add('theme-host')
    expect(isHostThemed(root)).toBe(true)
  })
})

describe('syncHostTheme', () => {
  afterEach(() => {
    const root = document.documentElement
    root.removeAttribute('style')
    root.classList.remove('theme-host')
  })

  it('applies the host theme and reports true when embedded', () => {
    const win = makeHost({ bg: 'rgb(243, 243, 243)', text: 'rgb(33, 37, 41)' })
    const root = document.documentElement
    expect(syncHostTheme(win, root)).toBe(true)
    expect(root.classList.contains('theme-host')).toBe(true)
    expect(root.style.getPropertyValue('--bg')).toBe('rgb(243, 243, 243)')
  })

  it('does nothing and reports false when standalone', () => {
    const win = {} as unknown as Window
    ;(win as { parent: Window }).parent = win
    const root = document.documentElement
    expect(syncHostTheme(win, root)).toBe(false)
    expect(root.classList.contains('theme-host')).toBe(false)
  })
})
