import { createUbusFetch } from './ubus'
import type {
  ActiveHome,
  DiscoveredController,
  Enrollment,
  EnrollmentResult,
  Home,
  HomeSelection,
  Node,
  Profile,
  Setup,
  Status,
  Topology,
} from '@/types'

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

  getTopology(): Promise<Topology> {
    return this.request<Topology>('/topology')
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

  getSetup(): Promise<Setup> {
    return this.request<Setup>('/setup')
  }

  completeSetup(): Promise<unknown> {
    return this.request('/setup/complete', { method: 'POST' })
  }

  getHome(homeId: string): Promise<Home> {
    return this.request<Home>(`/homes/${encodeURIComponent(homeId)}`)
  }

  updateHome(homeId: string, fields: Partial<Pick<Home, 'name' | 'controller'>>): Promise<Home> {
    return this.request<Home>(`/homes/${encodeURIComponent(homeId)}`, {
      method: 'PUT',
      body: JSON.stringify(fields),
    })
  }

  getNode(nodeId: string): Promise<Node> {
    return this.request<Node>(`/nodes/${encodeURIComponent(nodeId)}`)
  }

  async getProfile(homeId: string): Promise<Profile> {
    const { profile } = await this.request<{ profile: Profile }>(
      `/homes/${encodeURIComponent(homeId)}/profile`,
    )
    return profile
  }

  async saveProfile(homeId: string, profile: Partial<Profile>): Promise<Profile> {
    const { profile: saved } = await this.request<{ profile: Profile }>(
      `/homes/${encodeURIComponent(homeId)}/profile`,
      { method: 'POST', body: JSON.stringify({ ...profile, home_id: homeId }) },
    )
    return saved
  }

  getHomeSelection(): Promise<HomeSelection> {
    return this.request<HomeSelection>('/home-selection')
  }

  async getActiveHome(): Promise<string> {
    const { home_id } = await this.request<ActiveHome>('/active-home')
    return home_id
  }

  setActiveHome(homeId: string): Promise<ActiveHome> {
    return this.request<ActiveHome>('/active-home', {
      method: 'PUT',
      body: JSON.stringify({ home_id: homeId }),
    })
  }

  async listPendingEnrollments(): Promise<Enrollment[]> {
    const { enrollments } = await this.request<{ enrollments: Enrollment[] | null }>('/enroll')
    return enrollments ?? []
  }

  adoptNode(nodeId: string): Promise<EnrollmentResult> {
    return this.request<EnrollmentResult>(`/nodes/${encodeURIComponent(nodeId)}/adopt`, {
      method: 'POST',
    })
  }

  rejectNode(nodeId: string): Promise<EnrollmentResult> {
    return this.request<EnrollmentResult>(`/nodes/${encodeURIComponent(nodeId)}/reject`, {
      method: 'POST',
    })
  }

  async scanHomes(): Promise<DiscoveredController[]> {
    const { controllers } = await this.request<{ controllers: DiscoveredController[] | null }>('/scan')
    return controllers ?? []
  }

  joinHome(controllerUrl: string, serial?: string): Promise<EnrollmentResult> {
    return this.request<EnrollmentResult>('/enroll/join', {
      method: 'POST',
      body: JSON.stringify({ controller_url: controllerUrl, serial: serial ?? '' }),
    })
  }
}

// __MESHD_UBUS__ is injected by the LuCI host page when the PWA is served
// inside LuCI, carrying the authenticated rpcd session token. When present the
// client talks to meshd through LuCI's /ubus endpoint instead of the REST API
// directly, so the management API never has to be reachable on the network.
declare global {
  interface Window {
    __MESHD_UBUS__?: { token: string; endpoint?: string }
  }
}

// ubusContext returns the LuCI ubus session when the PWA runs inside LuCI. The
// token arrives either as a pre-set global (__MESHD_UBUS__) or in the iframe
// URL hash (#ubus_token=…), which is how the LuCI view hands LuCI's session
// token to the embedded PWA.
function ubusContext(): { token: string; endpoint?: string } | undefined {
  if (typeof window === 'undefined') return undefined
  if (window.__MESHD_UBUS__?.token) return window.__MESHD_UBUS__
  const m = window.location?.hash?.match(/(?:^#|&)ubus_token=([^&]+)/)
  if (m) return { token: decodeURIComponent(m[1]) }
  return undefined
}

/**
 * Builds the shared client: a ubus-backed client when running inside LuCI
 * (a session token is injected), otherwise a same-origin REST client (the
 * standalone PWA served by meshd directly).
 */
export function createApi(): ApiClient {
  const ubus = ubusContext()
  if (ubus?.token) {
    return new ApiClient('', createUbusFetch({ token: ubus.token, endpoint: ubus.endpoint }))
  }
  return new ApiClient()
}

/** Shared client instance, selected for the runtime environment. */
export const api = createApi()
