import type { NativeBridge } from './types'
import { webBridge } from './web'

// A single, swappable native bridge. The web fallback is active by default so
// the PWA and tests work with no native code; a native (Capacitor) shell calls
// setNativeBridge() during app start to register its real implementations.

let bridge: NativeBridge = webBridge

/** Register the active native bridge (called by the native shell at startup). */
export function setNativeBridge(next: NativeBridge): void {
  bridge = next
}

/** The active native bridge (the web fallback unless one was registered). */
export function getNative(): NativeBridge {
  return bridge
}

/** Reset to the web fallback. Intended for tests. */
export function resetNativeBridge(): void {
  bridge = webBridge
}

export type {
  NativeBridge,
  DiscoveryService,
  WifiService,
  QrService,
  WifiNetwork,
  SetupCredentials,
} from './types'
export { NativeUnavailableError } from './types'
