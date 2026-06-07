// randomId returns a RFC-4122 v4 UUID.
//
// crypto.randomUUID() is only exposed in a secure context (https or
// localhost). The PWA is routinely served from a device over plain http
// (e.g. http://192.168.1.1 via LuCI), where randomUUID is undefined and a
// naive call throws — breaking Home creation entirely. crypto.getRandomValues
// is available in every context, so fall back to building the UUID from it.
export function randomId(): string {
  const c = globalThis.crypto
  // typeof guards the common insecure-context case (randomUUID is undefined);
  // the try/catch also covers locked-down contexts where it exists but throws.
  if (c && typeof c.randomUUID === 'function') {
    try {
      return c.randomUUID()
    } catch {
      // fall through to the getRandomValues implementation
    }
  }

  const bytes = new Uint8Array(16)
  c.getRandomValues(bytes)
  // Set the version (4) and variant (10xx) bits per RFC 4122.
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80

  const hex: string[] = []
  for (let i = 0; i < 256; i++) {
    hex.push((i + 0x100).toString(16).slice(1))
  }
  const b = bytes
  return (
    hex[b[0]] + hex[b[1]] + hex[b[2]] + hex[b[3]] + '-' +
    hex[b[4]] + hex[b[5]] + '-' +
    hex[b[6]] + hex[b[7]] + '-' +
    hex[b[8]] + hex[b[9]] + '-' +
    hex[b[10]] + hex[b[11]] + hex[b[12]] + hex[b[13]] + hex[b[14]] + hex[b[15]]
  )
}
