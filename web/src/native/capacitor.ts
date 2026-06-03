import { Capacitor, registerPlugin } from '@capacitor/core'

import type { DiscoveredController } from '@/types'
import type {
  DiscoveryService,
  NativeBridge,
  QrService,
  SetupCredentials,
  WifiService,
} from './types'
import { NativeUnavailableError } from './types'

// The native bridge backed by Capacitor. mDNS discovery uses the
// @capacitor-community/zeroconf plugin, obtained through registerPlugin so the
// web build needs no dependency on the plugin package — the native platform
// supplies the implementation (and on web the proxy is simply never called,
// since isAvailable() is gated on a native platform).

/** The slice of the zeroconf plugin API used here. */
interface ZeroconfService {
  name: string
  type: string
  domain: string
  hostname: string
  ipv4Addresses: string[]
  ipv6Addresses: string[]
  port: number
  txtRecord: Record<string, string>
}
interface ZeroconfWatchResult {
  action: 'added' | 'removed' | 'resolved'
  service: ZeroconfService
}
interface ZeroconfPlugin {
  watch(
    opts: { type: string; domain: string },
    callback: (result: ZeroconfWatchResult) => void,
  ): Promise<{ callbackId: string }>
  unwatch(opts: { type: string; domain: string }): Promise<void>
}

const MESH_SERVICE = '_mesh._tcp.'
const MESH_DOMAIN = 'local.'
const DEFAULT_TIMEOUT_MS = 4000

const Zeroconf = registerPlugin<ZeroconfPlugin>('Zeroconf')

/**
 * Maps a resolved mDNS service to a controller. meshd publishes home_id, name
 * and controller_id as TXT records; the dialable api URL is an explicit `api`
 * TXT record when present, otherwise built from the resolved IPv4 and port.
 */
export function serviceToController(s: ZeroconfService): DiscoveredController {
  const txt = s.txtRecord ?? {}
  const ipv4 = s.ipv4Addresses?.[0]
  const api = txt.api || (ipv4 ? `http://${ipv4}:${s.port}` : '')
  return {
    home_id: txt.home_id ?? '',
    name: txt.name ?? s.name ?? '',
    controller_id: txt.controller_id ?? '',
    api,
  }
}

const discovery: DiscoveryService = {
  isAvailable: () => Capacitor.isNativePlatform(),
  async discoverControllers(timeoutMs = DEFAULT_TIMEOUT_MS): Promise<DiscoveredController[]> {
    const byApi = new Map<string, DiscoveredController>()
    await Zeroconf.watch({ type: MESH_SERVICE, domain: MESH_DOMAIN }, (result) => {
      if (result.action === 'resolved') {
        const c = serviceToController(result.service)
        if (c.api) byApi.set(c.api, c)
      }
    })
    await new Promise((resolve) => setTimeout(resolve, timeoutMs))
    await Zeroconf.unwatch({ type: MESH_SERVICE, domain: MESH_DOMAIN }).catch(() => {})
    return [...byApi.values()]
  },
}

// WiFi join and QR-label scan land in M3; expose them as unavailable for now so
// callers degrade gracefully (e.g. manual SSID/URL entry).
const wifi: WifiService = {
  isAvailable: () => false,
  joinNetwork: () => Promise.reject(new NativeUnavailableError('wifi.join')),
}
const qr: QrService = {
  isAvailable: () => false,
  scanSetupLabel: (): Promise<SetupCredentials> => Promise.reject(new NativeUnavailableError('qr')),
}

export const capacitorBridge: NativeBridge = {
  discovery,
  wifi,
  qr,
  isNative: Capacitor.isNativePlatform(),
}
