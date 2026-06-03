import { describe, expect, it, vi } from 'vitest'

// Mock @capacitor/core so the bridge runs as if on a native platform, with a
// fake Zeroconf plugin that emits a couple of resolved mesh services.
vi.mock('@capacitor/core', () => {
  type WatchCb = (r: {
    action: string
    service: Record<string, unknown>
  }) => void
  const services = [
    {
      action: 'resolved',
      service: {
        name: 'Casa',
        type: '_mesh._tcp.',
        domain: 'local.',
        hostname: 'gw01.local.',
        ipv4Addresses: ['10.0.0.1'],
        ipv6Addresses: [],
        port: 8080,
        txtRecord: { home_id: 'h1', name: 'Casa', controller_id: 'gw01' },
      },
    },
    {
      action: 'resolved',
      service: {
        name: 'no-txt',
        type: '_mesh._tcp.',
        domain: 'local.',
        hostname: 'gw02.local.',
        ipv4Addresses: ['10.0.0.2'],
        ipv6Addresses: [],
        port: 8080,
        txtRecord: {},
      },
    },
  ]
  return {
    Capacitor: { isNativePlatform: () => true },
    // One fake covers every registerPlugin id used by the bridge (Zeroconf,
    // CapacitorWifiConnect, BarcodeScanning).
    registerPlugin: () => ({
      watch: async (_opts: unknown, cb: WatchCb) => {
        services.forEach((s) => cb(s))
        return { callbackId: '1' }
      },
      unwatch: async () => {},
      connect: async () => ({ value: true }),
      secureConnect: async () => ({ value: true }),
      scan: async () => ({ barcodes: [{ rawValue: '{"ssid":"OMM-Setup-a51f","password":"pw"}' }] }),
    }),
  }
})

import { capacitorBridge, serviceToController } from './capacitor'

describe('capacitor bridge', () => {
  it('reports native and exposes mDNS discovery', () => {
    expect(capacitorBridge.isNative).toBe(true)
    expect(capacitorBridge.discovery.isAvailable()).toBe(true)
  })

  it('maps resolved mesh services to controllers (txt, then ipv4:port)', async () => {
    const controllers = await capacitorBridge.discovery.discoverControllers(1)
    expect(controllers).toHaveLength(2)
    expect(controllers[0]).toEqual({
      home_id: 'h1',
      name: 'Casa',
      controller_id: 'gw01',
      api: 'http://10.0.0.1:8080',
    })
    // No TXT metadata: name falls back to the service name, api to ipv4:port.
    expect(controllers[1]).toEqual({
      home_id: '',
      name: 'no-txt',
      controller_id: '',
      api: 'http://10.0.0.2:8080',
    })
  })

  it('serviceToController prefers an explicit api TXT record', () => {
    const c = serviceToController({
      name: 'x',
      type: '_mesh._tcp.',
      domain: 'local.',
      hostname: 'h',
      ipv4Addresses: ['10.0.0.9'],
      ipv6Addresses: [],
      port: 9999,
      txtRecord: { api: 'https://controller.example:8443' },
    })
    expect(c.api).toBe('https://controller.example:8443')
  })

  it('exposes wifi join and qr label scan on native', async () => {
    expect(capacitorBridge.wifi.isAvailable()).toBe(true)
    expect(capacitorBridge.qr.isAvailable()).toBe(true)

    // joinNetwork resolves through the connect plugin without throwing.
    await expect(capacitorBridge.wifi.joinNetwork('OMM-Setup-a51f', 'pw')).resolves.toBeUndefined()

    // scanSetupLabel parses the scanned barcode payload into credentials.
    await expect(capacitorBridge.qr.scanSetupLabel()).resolves.toEqual({
      ssid: 'OMM-Setup-a51f',
      password: 'pw',
      serial: undefined,
    })
  })
})
