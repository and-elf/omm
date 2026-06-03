import { describe, expect, it, vi } from 'vitest'

import { ApiClient } from './client'
import {
  ControllerAuthError,
  connectController,
  createAuthedControllerApi,
  loginController,
} from './controller'

/**
 * A fetch double routing by (method, path, ubus-method) so one mock can serve
 * REST probes and /ubus JSON-RPC (login + meshd calls).
 */
function routedFetch(handlers: {
  status?: { ok: boolean; status?: number; body?: unknown }
  login?: { code: number; session?: string }
  meshd?: (method: string) => { code: number; data?: unknown }
}) {
  return vi.fn(async (url: string, init?: RequestInit) => {
    const method = (init?.method ?? 'GET').toUpperCase()
    if (method === 'GET' && url.endsWith('/status')) {
      const s = handlers.status ?? { ok: false, status: 404 }
      return {
        ok: s.ok,
        status: s.status ?? (s.ok ? 200 : 404),
        json: async () => s.body ?? { status: 'ready' },
        text: async () => '',
      }
    }
    // /ubus JSON-RPC
    const rpc = JSON.parse(String(init?.body)) as { params: [string, string, string, unknown] }
    const [, object, ubusMethod] = rpc.params
    if (object === 'session' && ubusMethod === 'login') {
      const l = handlers.login ?? { code: 6 }
      return {
        ok: true,
        status: 200,
        json: async () => ({ result: [l.code, l.session ? { ubus_rpc_session: l.session } : {}] }),
        text: async () => '',
      }
    }
    const m = handlers.meshd?.(ubusMethod) ?? { code: 0, data: {} }
    return {
      ok: true,
      status: 200,
      json: async () => ({ result: [m.code, m.data ?? {}] }),
      text: async () => '',
    }
  }) as unknown as typeof fetch
}

describe('loginController', () => {
  it('returns the ubus session token on success', async () => {
    const fetchFn = routedFetch({ login: { code: 0, session: 'tok-123' } })
    const token = await loginController('http://ctrl/', 'root', 'pw', fetchFn)

    expect(token).toBe('tok-123')
    // Posts session.login to the controller's /ubus with the zero session.
    const [url, init] = (fetchFn as unknown as ReturnType<typeof vi.fn>).mock.calls[0]
    expect(url).toBe('http://ctrl/ubus')
    const rpc = JSON.parse(String((init as RequestInit).body))
    expect(rpc.params[1]).toBe('session')
    expect(rpc.params[2]).toBe('login')
    expect(rpc.params[3]).toEqual({ username: 'root', password: 'pw' })
  })

  it('rejects bad credentials (non-zero ubus status)', async () => {
    const fetchFn = routedFetch({ login: { code: 6 } })
    await expect(loginController('http://ctrl', 'root', 'bad', fetchFn)).rejects.toBeInstanceOf(
      ControllerAuthError,
    )
  })

  it('wraps a transport error', async () => {
    const fetchFn = (async () => {
      throw new Error('ECONNREFUSED')
    }) as unknown as typeof fetch
    await expect(loginController('http://ctrl', 'root', 'pw', fetchFn)).rejects.toThrow(/cannot reach/)
  })
})

describe('createAuthedControllerApi', () => {
  it('calls meshd over /ubus carrying the session token', async () => {
    const fetchFn = routedFetch({ meshd: () => ({ code: 0, data: { status: 'ready' } }) })
    const client = createAuthedControllerApi('http://ctrl', 'tok-123', fetchFn)

    await expect(client.getStatus()).resolves.toEqual({ status: 'ready' })
    const [url, init] = (fetchFn as unknown as ReturnType<typeof vi.fn>).mock.calls[0]
    expect(url).toBe('http://ctrl/ubus')
    const rpc = JSON.parse(String((init as RequestInit).body))
    expect(rpc.params[0]).toBe('tok-123')
    expect(rpc.params[1]).toBe('meshd')
    expect(rpc.params[2]).toBe('status')
  })
})

describe('connectController', () => {
  it('uses the open REST API in combined mode', async () => {
    const fetchFn = routedFetch({ status: { ok: true, body: { status: 'ready' } } })
    const client = await connectController('http://ctrl:8080', { fetchFn })

    expect(client).toBeInstanceOf(ApiClient)
    await expect(client.getStatus()).resolves.toEqual({ status: 'ready' })
  })

  it('requires credentials when the controller is not openly reachable', async () => {
    const fetchFn = routedFetch({ status: { ok: false, status: 404 } })
    await expect(connectController('http://ctrl', { fetchFn })).rejects.toBeInstanceOf(
      ControllerAuthError,
    )
  })

  it('logs in via LuCI when REST is closed but credentials are given', async () => {
    const fetchFn = routedFetch({
      status: { ok: false, status: 404 },
      login: { code: 0, session: 'tok-xyz' },
      meshd: () => ({ code: 0, data: { status: 'ready' } }),
    })
    const client = await connectController('http://ctrl', {
      username: 'root',
      password: 'pw',
      fetchFn,
    })

    // The returned client talks meshd over /ubus with the obtained token.
    await expect(client.getStatus()).resolves.toEqual({ status: 'ready' })
  })
})
