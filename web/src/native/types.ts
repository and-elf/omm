import type { DiscoveredController } from '../types'

// The companion app wraps this same web frontend in a native (Capacitor) shell
// to add two capabilities a browser PWA cannot provide: mDNS discovery of OMM
// controllers on the local network, and joining a node's first-boot setup AP.
// Everything is expressed behind these interfaces so the web build (and Vitest)
// runs unchanged with a no-op fallback, and the native implementations are
// registered at app start only when running on a device.

/** A WiFi network surfaced by a scan (Android-only convenience). */
export interface WifiNetwork {
  ssid: string
  bssid?: string
}

/**
 * Credentials read from a device's setup label (typically a QR code): the exact
 * setup-AP SSID to join and its passphrase. iOS has no WiFi-scan API, so the
 * SSID must arrive out-of-band like this rather than being discovered.
 */
export interface SetupCredentials {
  ssid: string
  password?: string
  serial?: string
}

/** Discovers OMM controllers on the local network via mDNS (`_mesh._tcp`). */
export interface DiscoveryService {
  /** Whether mDNS discovery is available in this runtime. */
  isAvailable(): boolean
  /** Browse for controllers (announced Homes) on the local segment. */
  discoverControllers(timeoutMs?: number): Promise<DiscoveredController[]>
}

/** Joins (and optionally lists) WiFi networks during onboarding. */
export interface WifiService {
  isAvailable(): boolean
  /**
   * Join a known setup-AP SSID. The name must be known up front (e.g. scanned
   * from the label), since iOS cannot enumerate nearby networks.
   */
  joinNetwork(ssid: string, password?: string): Promise<void>
  /** List nearby SSIDs. Android-only convenience; absent on iOS and web. */
  scanNetworks?(): Promise<WifiNetwork[]>
}

/** Reads a device's setup label (QR) to obtain its setup-AP credentials. */
export interface QrService {
  isAvailable(): boolean
  scanSetupLabel(): Promise<SetupCredentials>
}

/** The aggregate of native capabilities the onboarding flow may use. */
export interface NativeBridge {
  discovery: DiscoveryService
  wifi: WifiService
  qr: QrService
  /** True when running inside a native (Capacitor) shell. */
  isNative: boolean
}

/** Thrown when a native capability is used in a runtime that lacks it. */
export class NativeUnavailableError extends Error {
  constructor(capability: string) {
    super(`native capability "${capability}" is unavailable in this runtime`)
    this.name = 'NativeUnavailableError'
  }
}
