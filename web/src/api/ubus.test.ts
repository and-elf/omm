import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiClient, createApi } from './client'
import { createUbusFetch } from './ubus'

// ubusEnvelope mocks LuCI's /ubus JSON-RPC: result is [code, data].
function ubusEnvelope(data: unknown, code = 0) {
  return {
    ok: true,
    status: 200,
    json: async () => ({ jsonrpc: '2.0', id: 1, result: [code, data] }),
    text: async () => '',
  }
}

describe('createUbusFetch', () => {
  afterEach(() => vi.restoreAllMocks())

  it('maps a GET to a meshd ubus call and unwraps the result payload', async () => {
    const raw = vi.fn().mockResolvedValue(ubusEnvelope({ status: 'ready' }))
    const client = new ApiClient('', createUbusFetch({ token: 'tok', fetchFn: raw as unknown as typeof fetch }))

    await expect(client.getStatus()).resolves.toEqual({ status: 'ready' })

    expect(raw).toHaveBeenCalledTimes(1)
    const [url, init] = raw.mock.calls[0]
    expect(url).toBe('/ubus')
    expect(JSON.parse(init.body)).toEqual({
      jsonrpc: '2.0',
      id: 1,
      method: 'call',
      params: ['tok', 'meshd', 'status', {}],
    })
  })

  it('extracts path params into ubus args', async () => {
    const raw = vi.fn().mockResolvedValue(ubusEnvelope({ id: 'h1', name: 'Home' }))
    const client = new ApiClient('', createUbusFetch({ token: 'tok', fetchFn: raw as unknown as typeof fetch }))

    await client.getHome('h1')

    expect(JSON.parse(raw.mock.calls[0][1].body).params).toEqual(['tok', 'meshd', 'get_home', { home_id: 'h1' }])
  })

  it('forwards the request body as ubus args for writes', async () => {
    const raw = vi.fn().mockResolvedValue(ubusEnvelope({ home_id: 'h1' }))
    const client = new ApiClient('', createUbusFetch({ token: 'tok', fetchFn: raw as unknown as typeof fetch }))

    await client.setActiveHome('h1')

    expect(JSON.parse(raw.mock.calls[0][1].body).params).toEqual(['tok', 'meshd', 'set_active_home', { home_id: 'h1' }])
  })

  it('merges body and path params (save profile)', async () => {
    const raw = vi.fn().mockResolvedValue(ubusEnvelope({ profile: { home_id: 'h1', node_name: 'g' } }))
    const client = new ApiClient('', createUbusFetch({ token: 'tok', fetchFn: raw as unknown as typeof fetch }))

    await client.saveProfile('h1', { node_name: 'g' })

    const args = JSON.parse(raw.mock.calls[0][1].body).params[3]
    expect(args).toEqual({ node_name: 'g', home_id: 'h1' })
  })

  it('surfaces a meshd error payload as a failed response', async () => {
    const raw = vi.fn().mockResolvedValue(ubusEnvelope({ error: 'not found' }))
    const client = new ApiClient('', createUbusFetch({ token: 'tok', fetchFn: raw as unknown as typeof fetch }))

    await expect(client.getHome('missing')).rejects.toThrow('not found')
  })

  it('fails when the ubus status code is non-zero', async () => {
    const raw = vi.fn().mockResolvedValue(ubusEnvelope(null, 6))
    const client = new ApiClient('', createUbusFetch({ token: 'tok', fetchFn: raw as unknown as typeof fetch }))

    await expect(client.getStatus()).rejects.toThrow(/ubus status 6/)
  })
})

describe('createApi', () => {
  afterEach(() => {
    delete (window as Window).__MESHD_UBUS__
    vi.restoreAllMocks()
  })

  it('uses the ubus transport when LuCI injects a session token', async () => {
    ;(window as Window).__MESHD_UBUS__ = { token: 'sess-1' }
    const raw = vi.fn().mockResolvedValue(ubusEnvelope({ status: 'ready' }))
    vi.stubGlobal('fetch', raw)

    await createApi().getStatus()

    expect(raw).toHaveBeenCalledWith('/ubus', expect.any(Object))
    expect(JSON.parse(raw.mock.calls[0][1].body).params[0]).toBe('sess-1')
  })

  it('uses the same-origin REST transport when no token is present', async () => {
    const raw = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ status: 'ready' }) })
    vi.stubGlobal('fetch', raw)

    await createApi().getStatus()

    expect(raw).toHaveBeenCalledWith('/status', expect.any(Object))
  })
})
