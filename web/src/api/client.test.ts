import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiClient, ApiError } from './client'

function mockFetch(response: {
  ok?: boolean
  status?: number
  json?: unknown
  text?: string
}) {
  const fn = vi.fn().mockResolvedValue({
    ok: response.ok ?? true,
    status: response.status ?? 200,
    json: async () => response.json ?? {},
    text: async () => response.text ?? '',
  })
  return fn as unknown as typeof fetch
}

describe('ApiClient', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('fetches status from the configured base url', async () => {
    const fetchFn = mockFetch({ json: { status: 'ready' } })
    const client = new ApiClient('/api-base', fetchFn)

    const status = await client.getStatus()

    expect(status).toEqual({ status: 'ready' })
    expect(fetchFn).toHaveBeenCalledWith('/api-base/status', expect.any(Object))
  })

  it('returns the homes array from the wrapped response', async () => {
    const homes = [{ id: 'h1', name: 'Home', controller: 'gw01', certificate: null, last_seen: 1 }]
    const client = new ApiClient('', mockFetch({ json: { homes } }))

    await expect(client.listHomes()).resolves.toEqual(homes)
  })

  it('returns an empty array when the homes field is null', async () => {
    const client = new ApiClient('', mockFetch({ json: { homes: null } }))
    await expect(client.listHomes()).resolves.toEqual([])
  })

  it('returns the nodes array from the wrapped response', async () => {
    const nodes = [
      { id: 'n1', serial: 'SN1', current_home: 'h1', trusted_homes: ['h1'], last_seen: 2 },
    ]
    const client = new ApiClient('', mockFetch({ json: { nodes } }))

    await expect(client.listNodes()).resolves.toEqual(nodes)
  })

  it('creates a home by POSTing JSON', async () => {
    const fetchFn = mockFetch({ status: 201, json: { id: 'h1', name: 'Home' } })
    const client = new ApiClient('', fetchFn)

    await client.createHome({ id: 'h1', name: 'Home' })

    expect(fetchFn).toHaveBeenCalledWith(
      '/homes',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ id: 'h1', name: 'Home' }),
      }),
    )
  })

  it('throws an ApiError carrying the status code on failure', async () => {
    const client = new ApiClient('', mockFetch({ ok: false, status: 500, json: { error: 'boom' } }))

    await expect(client.listHomes()).rejects.toMatchObject({
      name: 'ApiError',
      status: 500,
      message: 'boom',
    })
    await expect(client.listHomes()).rejects.toBeInstanceOf(ApiError)
  })
})
