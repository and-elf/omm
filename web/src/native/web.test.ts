import { afterEach, describe, expect, it } from 'vitest'

import { getNative, resetNativeBridge, setNativeBridge } from './index'
import { NativeUnavailableError, type NativeBridge } from './types'

describe('native bridge', () => {
  afterEach(() => resetNativeBridge())

  it('defaults to the web fallback (not native, nothing available)', () => {
    const n = getNative()
    expect(n.isNative).toBe(false)
    expect(n.discovery.isAvailable()).toBe(false)
    expect(n.wifi.isAvailable()).toBe(false)
    expect(n.qr.isAvailable()).toBe(false)
  })

  it('rejects unavailable capabilities with NativeUnavailableError', async () => {
    const n = getNative()
    await expect(n.discovery.discoverControllers()).rejects.toBeInstanceOf(NativeUnavailableError)
    await expect(n.wifi.joinNetwork('OMM-Setup-abcd')).rejects.toBeInstanceOf(NativeUnavailableError)
    await expect(n.qr.scanSetupLabel()).rejects.toBeInstanceOf(NativeUnavailableError)
  })

  it('uses a registered native bridge once set', async () => {
    const fake: NativeBridge = {
      isNative: true,
      discovery: {
        isAvailable: () => true,
        discoverControllers: async () => [
          { home_id: 'h1', name: 'Home', controller_id: 'gw01', api: 'http://10.0.0.1:8080' },
        ],
      },
      wifi: { isAvailable: () => true, joinNetwork: async () => {} },
      qr: {
        isAvailable: () => true,
        scanSetupLabel: async () => ({ ssid: 'OMM-Setup-abcd', password: 'secret' }),
      },
    }
    setNativeBridge(fake)

    const n = getNative()
    expect(n.isNative).toBe(true)
    expect(n.discovery.isAvailable()).toBe(true)
    const controllers = await n.discovery.discoverControllers()
    expect(controllers).toHaveLength(1)
    expect(controllers[0].api).toBe('http://10.0.0.1:8080')
    const creds = await n.qr.scanSetupLabel()
    expect(creds.ssid).toBe('OMM-Setup-abcd')
  })

  it('resets back to the web fallback', () => {
    setNativeBridge({
      isNative: true,
      discovery: { isAvailable: () => true, discoverControllers: async () => [] },
      wifi: { isAvailable: () => true, joinNetwork: async () => {} },
      qr: { isAvailable: () => true, scanSetupLabel: async () => ({ ssid: 'x' }) },
    })
    resetNativeBridge()
    expect(getNative().isNative).toBe(false)
  })
})
