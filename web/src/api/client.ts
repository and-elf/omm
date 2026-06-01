import type { Home, Node, Profile, Status } from '@/types'

/** Error thrown for non-2xx API responses, carrying the HTTP status code. */
export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

type FetchFn = typeof fetch

/**
 * Thin client over the meshd REST API.
 *
 * `baseUrl` is prepended to every path (empty string = same origin). `fetchFn`
 * is injected so the client can be unit-tested without a real network.
 */
export class ApiClient {
  constructor(
    private readonly baseUrl = '',
    private readonly fetchFn: FetchFn = fetch.bind(globalThis),
  ) {}

  private async request<T>(path: string, init?: RequestInit): Promise<T> {
    const res = await this.fetchFn(`${this.baseUrl}${path}`, {
      headers: { 'Content-Type': 'application/json' },
      ...init,
    })

    if (!res.ok) {
      let message = `request failed with status ${res.status}`
      try {
        const body = (await res.json()) as { error?: string }
        if (body?.error) {
          message = body.error
        }
      } catch {
        // Response body was not JSON; keep the default message.
      }
      throw new ApiError(res.status, message)
    }

    return (await res.json()) as T
  }

  getStatus(): Promise<Status> {
    return this.request<Status>('/status')
  }

  async listHomes(): Promise<Home[]> {
    const { homes } = await this.request<{ homes: Home[] | null }>('/homes')
    return homes ?? []
  }

  createHome(home: Partial<Home> & Pick<Home, 'id' | 'name'>): Promise<Home> {
    return this.request<Home>('/homes', {
      method: 'POST',
      body: JSON.stringify(home),
    })
  }

  async listNodes(): Promise<Node[]> {
    const { nodes } = await this.request<{ nodes: Node[] | null }>('/nodes')
    return nodes ?? []
  }

  getProfile(homeId: string): Promise<{ profile: Profile }> {
    return this.request<{ profile: Profile }>(`/homes/${encodeURIComponent(homeId)}/profile`)
  }
}

/** Shared client instance pointed at the same origin as the served PWA. */
export const api = new ApiClient()
