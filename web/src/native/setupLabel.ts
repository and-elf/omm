import type { SetupCredentials } from './types'

// A device's setup label (QR) carries the credentials needed to reach its
// first-boot setup AP. Three encodings are accepted, in order of preference:
//
//   1. OMM JSON:   {"ssid":"OMM-Setup-a51f","password":"…","serial":"…"}
//   2. WiFi URI:   WIFI:S:OMM-Setup-a51f;T:WPA;P:secret;;   (the de-facto QR
//                  standard printed by many label generators)
//   3. Bare SSID:  OMM-Setup-a51f                            (open networks)
//
// parseSetupLabel turns any of these into SetupCredentials, or throws if the
// text carries no usable SSID.

/** Thrown when a scanned label cannot be parsed into setup credentials. */
export class SetupLabelError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'SetupLabelError'
  }
}

export function parseSetupLabel(raw: string): SetupCredentials {
  const text = raw.trim()
  if (!text) throw new SetupLabelError('empty setup label')

  if (text.startsWith('{')) return fromJSON(text)
  if (/^WIFI:/i.test(text)) return fromWifiURI(text)
  // A bare token with no scheme/delimiters is treated as an open-network SSID.
  if (!text.includes(':') && !text.includes(';')) return { ssid: text }

  throw new SetupLabelError('unrecognised setup label format')
}

function fromJSON(text: string): SetupCredentials {
  let obj: Record<string, unknown>
  try {
    obj = JSON.parse(text) as Record<string, unknown>
  } catch {
    throw new SetupLabelError('setup label is not valid JSON')
  }
  const ssid = str(obj.ssid)
  if (!ssid) throw new SetupLabelError('setup label JSON has no ssid')
  return {
    ssid,
    password: str(obj.password) || undefined,
    serial: str(obj.serial) || undefined,
  }
}

// Parse the `WIFI:S:…;T:…;P:…;;` URI. Fields are `KEY:value` separated by `;`;
// values may backslash-escape `;`, `:`, `,` and `\`.
function fromWifiURI(text: string): SetupCredentials {
  const body = text.slice('WIFI:'.length)
  const fields: Record<string, string> = {}
  let key: string | null = null
  let buf = ''
  let escaped = false

  const commit = () => {
    if (key !== null) fields[key.toUpperCase()] = buf
    key = null
    buf = ''
  }

  for (const ch of body) {
    if (escaped) {
      buf += ch
      escaped = false
    } else if (ch === '\\') {
      escaped = true
    } else if (ch === ':' && key === null) {
      key = buf
      buf = ''
    } else if (ch === ';') {
      commit()
    } else {
      buf += ch
    }
  }
  commit()

  const ssid = fields.S
  if (!ssid) throw new SetupLabelError('WiFi URI has no SSID (S) field')
  return {
    ssid,
    password: fields.P || undefined,
    // Some labels carry the node serial in a vendor field.
    serial: fields.SN || fields.SERIAL || undefined,
  }
}

function str(v: unknown): string {
  return typeof v === 'string' ? v.trim() : ''
}
