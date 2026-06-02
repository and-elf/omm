/**
 * ubus transport for the meshd PWA.
 *
 * When the PWA runs inside LuCI the management API is not reachable directly
 * (it binds to localhost); instead LuCI exposes the `meshd` ubus object through
 * its authenticated `/ubus` JSON-RPC endpoint. createUbusFetch returns a
 * fetch-compatible function that maps the REST calls ApiClient makes onto
 * `meshd` ubus methods, so ApiClient itself is unchanged — construct it with
 * this fetch instead of the default.
 */

export interface UbusOptions {
  /** LuCI session token (rpcd ubus session id). */
  token: string
  /** JSON-RPC endpoint; defaults to '/ubus'. */
  endpoint?: string
  /** Underlying fetch (injected for tests). */
  fetchFn?: typeof fetch
}

type ArgFn = (caps: string[], body: Record<string, unknown>) => Record<string, unknown>

interface Route {
  method: string
  re: RegExp
  ubus: string
  args?: ArgFn
}

const dec = decodeURIComponent

// (HTTP method, path) -> meshd ubus method. More specific paths first so e.g.
// /homes/{id}/profile matches before /homes/{id}.
const ROUTES: Route[] = [
  { method: 'GET', re: /^\/status$/, ubus: 'status' },
  { method: 'GET', re: /^\/setup$/, ubus: 'setup' },
  { method: 'POST', re: /^\/setup\/complete$/, ubus: 'complete_setup' },
  { method: 'GET', re: /^\/homes$/, ubus: 'homes' },
  { method: 'POST', re: /^\/homes$/, ubus: 'create_home', args: (_c, b) => b },
  { method: 'GET', re: /^\/homes\/([^/]+)\/profile$/, ubus: 'get_profile', args: (c) => ({ home_id: dec(c[0]) }) },
  { method: 'POST', re: /^\/homes\/([^/]+)\/profile$/, ubus: 'save_profile', args: (c, b) => ({ ...b, home_id: dec(c[0]) }) },
  { method: 'GET', re: /^\/homes\/([^/]+)$/, ubus: 'get_home', args: (c) => ({ home_id: dec(c[0]) }) },
  { method: 'PUT', re: /^\/homes\/([^/]+)$/, ubus: 'update_home', args: (c, b) => ({ ...b, home_id: dec(c[0]) }) },
  { method: 'GET', re: /^\/nodes$/, ubus: 'nodes' },
  { method: 'POST', re: /^\/nodes\/([^/]+)\/adopt$/, ubus: 'adopt_node', args: (c) => ({ node_id: dec(c[0]) }) },
  { method: 'POST', re: /^\/nodes\/([^/]+)\/reject$/, ubus: 'reject_node', args: (c) => ({ node_id: dec(c[0]) }) },
  { method: 'GET', re: /^\/nodes\/([^/]+)$/, ubus: 'get_node', args: (c) => ({ node_id: dec(c[0]) }) },
  { method: 'GET', re: /^\/active-home$/, ubus: 'active_home' },
  { method: 'PUT', re: /^\/active-home$/, ubus: 'set_active_home', args: (_c, b) => b },
  { method: 'GET', re: /^\/home-selection$/, ubus: 'home_selection' },
  { method: 'GET', re: /^\/enroll$/, ubus: 'enrollments' },
  { method: 'GET', re: /^\/scan$/, ubus: 'scan' },
  { method: 'POST', re: /^\/enroll\/join$/, ubus: 'join_home', args: (_c, b) => b },
]

function resolve(
  method: string,
  path: string,
  body: Record<string, unknown>,
): { ubus: string; args: Record<string, unknown> } | null {
  for (const r of ROUTES) {
    if (r.method !== method) continue
    const m = r.re.exec(path)
    if (!m) continue
    return { ubus: r.ubus, args: r.args ? r.args(m.slice(1), body) : {} }
  }
  return null
}

/** A minimal Response shim carrying the data ApiClient.request reads. */
function jsonResponse(ok: boolean, status: number, data: unknown): Response {
  return {
    ok,
    status,
    json: async () => data,
    text: async () => JSON.stringify(data),
  } as Response
}

export function createUbusFetch(opts: UbusOptions): typeof fetch {
  const endpoint = opts.endpoint ?? '/ubus'
  const fetchFn = opts.fetchFn ?? fetch.bind(globalThis)

  return (async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = typeof input === 'string' ? input : input.toString()
    const path = url.replace(/^[a-z]+:\/\/[^/]+/i, '').replace(/\?.*$/, '')
    const method = (init?.method ?? 'GET').toUpperCase()

    const body = init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : {}
    const route = resolve(method, path, body)
    if (!route) {
      return jsonResponse(false, 404, { error: `no ubus mapping for ${method} ${path}` })
    }

    const rpc = {
      jsonrpc: '2.0',
      id: 1,
      method: 'call',
      params: [opts.token, 'meshd', route.ubus, route.args],
    }
    const res = await fetchFn(endpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(rpc),
    })
    if (!res.ok) {
      return jsonResponse(false, res.status, { error: `ubus transport status ${res.status}` })
    }
    const envelope = (await res.json()) as { result?: [number, unknown?]; error?: { message?: string } }
    if (envelope.error || !envelope.result) {
      return jsonResponse(false, 502, { error: envelope.error?.message ?? 'ubus call failed' })
    }
    const [code, data] = envelope.result
    if (code !== 0) {
      return jsonResponse(false, 502, { error: `ubus status ${code}` })
    }
    const payload = (data ?? {}) as { error?: string }
    if (payload && payload.error) {
      return jsonResponse(false, 400, payload)
    }
    return jsonResponse(true, 200, payload)
  }) as typeof fetch
}
