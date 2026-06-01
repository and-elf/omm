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

  it('lists pending enrollments and unwraps the array', async () => {
    const enrollments = [{ id: 'e1', node_id: 'n1', serial: 'SN1', status: 'pending_approval' }]
    const client = new ApiClient('', mockFetch({ json: { enrollments } }))
    await expect(client.listPendingEnrollments()).resolves.toEqual(enrollments)
  })

  it('returns [] when there are no pending enrollments', async () => {
    const client = new ApiClient('', mockFetch({ json: { enrollments: null } }))
    await expect(client.listPendingEnrollments()).resolves.toEqual([])
  })

  it('adopts and rejects a node by id', async () => {
    const fetchFn = mockFetch({ json: { status: 'approved' } })
    const client = new ApiClient('', fetchFn)

    await client.adoptNode('node 1')
    expect(fetchFn).toHaveBeenCalledWith(
      '/nodes/node%201/adopt',
      expect.objectContaining({ method: 'POST' }),
    )
    await client.rejectNode('n2')
    expect(fetchFn).toHaveBeenCalledWith('/nodes/n2/reject', expect.objectContaining({ method: 'POST' }))
  })

  it('gets and sets the active home', async () => {
    const fetchFn = mockFetch({ json: { home_id: 'home-1' } })
    const client = new ApiClient('', fetchFn)

    await expect(client.getActiveHome()).resolves.toBe('home-1')

    await client.setActiveHome('home-2')
    expect(fetchFn).toHaveBeenCalledWith(
      '/active-home',
      expect.objectContaining({ method: 'PUT', body: JSON.stringify({ home_id: 'home-2' }) }),
    )
  })

  it('joins a controller with the given url and serial', async () => {
    const fetchFn = mockFetch({ json: { status: 'active' } })
    const client = new ApiClient('', fetchFn)

    await client.joinHome('http://other:8080', 'sn-1')
    expect(fetchFn).toHaveBeenCalledWith(
      '/enroll/join',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ controller_url: 'http://other:8080', serial: 'sn-1' }),
      }),
    )
  })

  it('saves a profile under the home id', async () => {
    const fetchFn = mockFetch({ json: { profile: { home_id: 'h1', node_name: 'Garage' } } })
    const client = new ApiClient('', fetchFn)

    const saved = await client.saveProfile('h1', { node_name: 'Garage' })
    expect(saved).toEqual({ home_id: 'h1', node_name: 'Garage' })
    expect(fetchFn).toHaveBeenCalledWith(
      '/homes/h1/profile',
      expect.objectContaining({ method: 'POST' }),
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
