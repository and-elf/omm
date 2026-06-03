import { ref, type Ref } from 'vue'

import { api, ApiClient, ApiError } from '@/api/client'
import { getNative } from '@/native'
import type { DiscoveredController } from '@/types'

/** Where the discovered controllers came from. */
export type DiscoverySource = 'mdns' | 'scan'

export interface UseDiscovery {
  controllers: Ref<DiscoveredController[] | null>
  source: Ref<DiscoverySource | null>
  scanning: Ref<boolean>
  error: Ref<string | null>
  discover: () => Promise<void>
}

/**
 * Discovers nearby controllers (Homes), preferring the native mDNS browser when
 * the app runs inside a native shell and falling back to the daemon's
 * `GET /scan` (its passive UDP-announcement cache) otherwise. `source` records
 * which path answered so the UI can label it.
 *
 * `client` is injected for tests and to allow discovering through a specific
 * device (e.g. a remote controller) rather than the shared same-origin client.
 */
export function useDiscovery(client: ApiClient = api): UseDiscovery {
  const controllers = ref<DiscoveredController[] | null>(null)
  const source = ref<DiscoverySource | null>(null)
  const scanning = ref(false)
  const error = ref<string | null>(null)

  const discover = async () => {
    scanning.value = true
    error.value = null
    try {
      const native = getNative()
      if (native.discovery.isAvailable()) {
        controllers.value = await native.discovery.discoverControllers()
        source.value = 'mdns'
      } else {
        controllers.value = await client.scanHomes()
        source.value = 'scan'
      }
    } catch (err) {
      controllers.value = null
      error.value = err instanceof ApiError || err instanceof Error ? err.message : String(err)
    } finally {
      scanning.value = false
    }
  }

  return { controllers, source, scanning, error, discover }
}
