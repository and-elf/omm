import type {
  DiscoveryService,
  NativeBridge,
  QrService,
  SetupCredentials,
  WifiService,
} from './types'
import { NativeUnavailableError } from './types'
import type { DiscoveredController } from '../types'

// The fallback bridge used when the frontend runs as a plain browser PWA (no
// native shell). Every capability reports unavailable; callers should detect
// this (via isAvailable()) and degrade gracefully — e.g. fall back to the
// daemon's GET /scan or to manual controller-URL / SSID entry — rather than
// invoking a capability that will reject.

const webDiscovery: DiscoveryService = {
  isAvailable: () => false,
  discoverControllers: (): Promise<DiscoveredController[]> =>
    Promise.reject(new NativeUnavailableError('discovery')),
}

const webWifi: WifiService = {
  isAvailable: () => false,
  joinNetwork: (): Promise<void> => Promise.reject(new NativeUnavailableError('wifi.join')),
}

const webQr: QrService = {
  isAvailable: () => false,
  scanSetupLabel: (): Promise<SetupCredentials> =>
    Promise.reject(new NativeUnavailableError('qr')),
}

export const webBridge: NativeBridge = {
  discovery: webDiscovery,
  wifi: webWifi,
  qr: webQr,
  isNative: false,
}
