import { ApiClient, createRemoteApi } from './client'
import { createUbusFetch } from './ubus'

// Reaching a controller's management API depends on its posture:
//   - Combined mode (bare meshd): management REST is open on the network — talk
//     to it directly (createRemoteApi).
//   - Split mode (luci-app-meshd installed): management binds to localhost and
//     is reachable only through LuCI's authenticated /ubus. The companion app
//     must log in (rpcd session.login with the router admin password) to get a
//     session token, then call meshd through /ubus carrying that token.
//
// This module provides the LuCI login and an authenticated client, plus a
// connectController() that picks the right path automatically.

type FetchFn = typeof fetch

/** The pre-login ubus session id (32 zeros), used only to call session.login. */
const ZERO_SESSION = '00000000000000000000000000000000'

/** Thrown when a controller cannot be reached or authenticated. */
export class ControllerAuthError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'ControllerAuthError'
  }
}

function ubusEndpoint(baseUrl: string): string {
  return `${baseUrl.trim().replace(/\/+$/, '')}/ubus`
}

/**
 * Logs into a controller's LuCI/rpcd and returns the ubus session token.
 * `baseUrl` is the controller's web origin (e.g. `http://192.168.1.1`).
 */
export async function loginController(
  baseUrl: string,
  username: string,
  password: string,
  fetchFn: FetchFn = fetch.bind(globalThis),
): Promise<string> {
  const endpoint = ubusEndpoint(baseUrl)
  const rpc = {
    jsonrpc: '2.0',
    id: 1,
    method: 'call',
    params: [ZERO_SESSION, 'session', 'login', { username, password }],
  }

  let res: Response
  try {
    res = await fetchFn(endpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(rpc),
    })
  } catch (err) {
    throw new ControllerAuthError(
      `cannot reach controller at ${endpoint}: ${err instanceof Error ? err.message : String(err)}`,
    )
  }

  if (!res.ok) throw new ControllerAuthError(`login transport failed with status ${res.status}`)

  const env = (await res.json()) as {
    result?: [number, { ubus_rpc_session?: string }?]
    error?: { message?: string }
  }
  if (env.error || !env.result) {
    throw new ControllerAuthError(env.error?.message ?? 'login failed')
  }
  const [code, data] = env.result
  // rpcd returns a non-zero ubus status (e.g. 6 = permission denied) for bad
  // credentials, with no session payload.
  if (code !== 0 || !data?.ubus_rpc_session) {
    throw new ControllerAuthError('invalid username or password')
  }
  return data.ubus_rpc_session
}

/** Builds a management client that talks to a controller's meshd over LuCI /ubus. */
export function createAuthedControllerApi(
  baseUrl: string,
  token: string,
  fetchFn?: FetchFn,
): ApiClient {
  return new ApiClient('', createUbusFetch({ token, endpoint: ubusEndpoint(baseUrl), fetchFn }))
}

export interface ConnectOptions {
  username?: string
  password?: string
  fetchFn?: FetchFn
}

/**
 * Connects to a controller's management API, choosing the posture automatically:
 * use the open REST API when it answers (combined mode), otherwise authenticate
 * via LuCI session.login (split mode). Throws if the controller is not openly
 * reachable and no credentials were supplied.
 */
export async function connectController(
  baseUrl: string,
  opts: ConnectOptions = {},
): Promise<ApiClient> {
  const fetchFn = opts.fetchFn ?? fetch.bind(globalThis)

  const plain = createRemoteApi(baseUrl, fetchFn)
  try {
    await plain.getStatus()
    return plain
  } catch {
    // Not openly reachable (split mode, or behind LuCI): fall through to login.
  }

  if (!opts.username) {
    throw new ControllerAuthError(
      'controller is not openly reachable; a LuCI username and password are required',
    )
  }
  const token = await loginController(baseUrl, opts.username, opts.password ?? '', fetchFn)
  return createAuthedControllerApi(baseUrl, token, fetchFn)
}
