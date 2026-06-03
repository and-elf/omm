import { afterEach, describe, expect, it } from 'vitest'

import { ApiClient } from '@/api/client'
import { resetNativeBridge, setNativeBridge, type NativeBridge } from '@/native'
import type { DiscoveredController } from '@/types'
import { useDiscovery } from './useDiscovery'

const flush = () => new Promise((resolve) => setTimeout(resolve, 0))

const home: DiscoveredController = {
  home_id: 'h1',
  name: 'Home',
  controller_id: 'gw01',
  api: 'http://10.0.0.1:8080',
}

function fakeClient(scan: () => Promise<DiscoveredController[]>): ApiClient {
  return { scanHomes: scan } as unknown as ApiClient
}

function nativeWithDiscovery(discover: () => Promise<DiscoveredController[]>): NativeBridge {
  return {
    isNative: true,
    discovery: { isAvailable: () => true, discoverControllers: discover },
    wifi: { isAvailable: () => false, joinNetwork: async () => {} },
    qr: { isAvailable: () => false, scanSetupLabel: async () => ({ ssid: 'x' }) },
  }
}

describe('useDiscovery', () => {
  afterEach(() => resetNativeBridge())

  it('falls back to the daemon /scan when no native discovery is available', async () => {
    const client = fakeClient(async () => [home])
    const { controllers, source, scanning, discover } = useDiscovery(client)

    const p = discover()
    expect(scanning.value).toBe(true)
    await p
    await flush()

    expect(controllers.value).toEqual([home])
    expect(source.value).toBe('scan')
    expect(scanning.value).toBe(false)
  })

  it('prefers native mDNS discovery when available', async () => {
    setNativeBridge(nativeWithDiscovery(async () => [home]))
    // The injected client must NOT be consulted when native discovery wins.
    const client = fakeClient(async () => {
      throw new Error('scanHomes should not be called')
    })
    const { controllers, source, discover } = useDiscovery(client)

    await discover()
    await flush()

    expect(controllers.value).toEqual([home])
    expect(source.value).toBe('mdns')
  })

  it('captures an error message and clears results on failure', async () => {
    const client = fakeClient(async () => {
      throw new Error('network down')
    })
    const { controllers, error, discover } = useDiscovery(client)

    await discover()
    await flush()

    expect(controllers.value).toBeNull()
    expect(error.value).toBe('network down')
  })
})
