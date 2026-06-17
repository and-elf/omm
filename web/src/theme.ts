// Host-theme integration. When the PWA is embedded in LuCI it runs inside an
// iframe served from the same origin (luci-static), so it can read the host
// page's resolved colours and adopt them — the manager then looks like part of
// LuCI (light or dark) instead of imposing its own dark palette. Standalone
// (served by meshd on :8080) there is no distinct parent to read, so the
// built-in dark theme in styles.css stands.
//
// We only pull a few base colours (background, text, accent, font); the rest of
// the palette (surfaces, borders, muted text) is derived from them with
// color-mix() under the `.theme-host` selector, which keeps the result coherent
// whether the host is light or dark.

export interface HostTheme {
  bg: string
  text: string
  accent?: string
  fontFamily?: string
}

// Selectors for a LuCI action/primary element to borrow an accent from, most
// specific first. We fall back to the built-in accent when none resolve.
const ACCENT_SELECTORS = ['.cbi-button-action', '.cbi-button-save', '.btn', 'a']

// A colour is unusable as a theme base if it is fully transparent (e.g. an
// unstyled body that shows the page behind it).
function isVisibleColor(value: string | null | undefined): value is string {
  if (!value) return false
  const v = value.trim()
  if (v === 'transparent') return false
  // rgba(…, 0) / rgb(… / 0) with a zero alpha.
  const m = v.match(/rgba?\([^)]*[,/]\s*(0|0?\.0+)\s*\)$/i)
  return !m
}

function readAccent(parent: Window, doc: Document): string | undefined {
  for (const sel of ACCENT_SELECTORS) {
    const el = doc.querySelector(sel)
    if (!el) continue
    const color = parent.getComputedStyle(el as Element).color
    if (isVisibleColor(color)) return color
  }
  return undefined
}

// readHostTheme inspects the host (parent) document and returns its theme, or
// null when there is nothing usable to read: a standalone window, a cross-origin
// parent (access throws), or a host with no opaque background to build on.
export function readHostTheme(win: Window = window): HostTheme | null {
  try {
    const parent = win.parent
    if (!parent || parent === win) return null
    const doc = parent.document // throws on a cross-origin parent
    const body = doc.body
    if (!body) return null
    const cs = parent.getComputedStyle(body)
    if (!isVisibleColor(cs.backgroundColor)) return null
    return {
      bg: cs.backgroundColor,
      text: cs.color,
      accent: readAccent(parent, doc),
      fontFamily: cs.fontFamily || undefined,
    }
  } catch {
    return null
  }
}

// applyHostTheme writes the host colours onto the root element as CSS variables
// and tags it `theme-host`, which switches styles.css over to the derived
// (color-mix) palette built from these bases.
export function applyHostTheme(
  theme: HostTheme,
  root: HTMLElement = document.documentElement,
): void {
  root.style.setProperty('--bg', theme.bg)
  root.style.setProperty('--text', theme.text)
  if (theme.accent) root.style.setProperty('--accent', theme.accent)
  if (theme.fontFamily) root.style.fontFamily = theme.fontFamily
  root.classList.add('theme-host')
}

// importHostStyles clones the host page's stylesheets (and inline <style>
// blocks) into our document so LuCI's own theme styles components we mark up
// with LuCI's classes — e.g. the menu (ul.nav#topmenu) renders natively rather
// than us re-implementing it. They are inserted at the head of <head>, ahead of
// the app's own bundled CSS, so our component styles still win where they
// overlap while LuCI fills in the base look. Idempotent (deduped by href /
// inline text) and a no-op when standalone or cross-origin. Returns the list of
// imported sources.
export function importHostStyles(win: Window = window, target: Document = document): string[] {
  try {
    const parent = win.parent
    if (!parent || parent === win) return []
    const pdoc = parent.document // throws on a cross-origin parent
    const head = target.head
    if (!head) return []

    const seen = new Set<string>()
    head.querySelectorAll('[data-host-style]').forEach((node) => {
      seen.add(node.tagName === 'LINK' ? (node as HTMLLinkElement).href : (node.textContent ?? ''))
    })

    const frag = target.createDocumentFragment()
    const imported: string[] = []
    pdoc.querySelectorAll('link[rel~="stylesheet"], style').forEach((node) => {
      if (node.tagName === 'LINK') {
        const href = (node as HTMLLinkElement).href
        if (!href || seen.has(href)) return
        seen.add(href)
        const link = target.createElement('link')
        link.rel = 'stylesheet'
        link.href = href
        const media = (node as HTMLLinkElement).media
        if (media) link.media = media
        link.setAttribute('data-host-style', '')
        frag.appendChild(link)
        imported.push(href)
      } else {
        const css = node.textContent ?? ''
        if (!css.trim() || seen.has(css)) return
        seen.add(css)
        const style = target.createElement('style')
        style.textContent = css
        style.setAttribute('data-host-style', '')
        frag.appendChild(style)
        imported.push('<style>')
      }
    })

    // Ahead of our own CSS so the app's styles keep priority on overlap.
    head.insertBefore(frag, head.firstChild)
    return imported
  } catch {
    return []
  }
}

// isHostThemed reports whether the host theme has been adopted, i.e. the PWA is
// embedded in LuCI. Components use it to switch to LuCI's markup/classes.
export function isHostThemed(root: HTMLElement = document.documentElement): boolean {
  return root.classList.contains('theme-host')
}

// syncHostTheme reads and applies the host theme in one step: adopts the host
// colours and pulls in its stylesheets. Returns whether a host theme was adopted
// (true) or the built-in theme was kept (false).
export function syncHostTheme(
  win: Window = window,
  root: HTMLElement = document.documentElement,
): boolean {
  const theme = readHostTheme(win)
  if (!theme) return false
  applyHostTheme(theme, root)
  importHostStyles(win, root.ownerDocument ?? document)
  return true
}
