import { describe, expect, it, vi } from 'vitest'

import { ApiClient } from '@/api/client'
import type { NativeBridge, SetupCredentials } from '@/native'
import { useOnboarding } from './useOnboarding'

const creds: SetupCredentials = { ssid: 'OMM-Setup-a51f', password: 'pw', serial: 'sn-1' }

function nativeBridge(over: {
  qrAvailable?: boolean
  wifiAvailable?: boolean
  scan?: () => Promise<SetupCredentials>
  join?: (ssid: string, password?: string) => Promise<void>
}): NativeBridge {
  return {
    isNative: true,
    discovery: { isAvailable: () => false, discoverControllers: async () => [] },
    wifi: {
      isAvailable: () => over.wifiAvailable ?? true,
      joinNetwork: over.join ?? (async () => {}),
    },
    qr: {
      isAvailable: () => over.qrAvailable ?? true,
      scanSetupLabel: over.scan ?? (async () => creds),
    },
  }
}

// A node client whose /setup returns nodeId and whose joinHome returns `status`.
function nodeClient(status: string, joinSpy?: ReturnType<typeof vi.fn>): ApiClient {
  return {
    getSetup: async () => ({
      setup_complete: false,
      node_id: 'node-1',
      serial: 'sn-1',
      home_id: '',
      home_name: '',
    }),
    joinHome: joinSpy ?? (async () => ({ status })),
  } as unknown as ApiClient
}

describe('useOnboarding', () => {
  it('runs scan → join → connect → enroll and auto-completes when the node is active', async () => {
    const join = vi.fn(async () => {})
    const native = nativeBridge({ join })
    const node = nodeClient('active')

    const ob = useOnboarding({
      controllerUrl: 'http://controller:8080',
      credentials: creds,
      native,
      createClient: () => node,
    })
    await ob.run()

    expect(ob.error.value).toBeNull()
    expect(ob.step.value).toBe('done')
    expect(ob.nodeId.value).toBe('node-1')
    expect(ob.adopted.value).toBe(true)
    expect(join).toHaveBeenCalledWith('OMM-Setup-a51f', 'pw')
  })

  it('scans the label when no credentials are supplied', async () => {
    const scan = vi.fn(async () => creds)
    const native = nativeBridge({ scan })

    const ob = useOnboarding({
      controllerUrl: 'http://controller:8080',
      native,
      createClient: () => nodeClient('active'),
    })
    await ob.run()

    expect(scan).toHaveBeenCalledOnce()
    expect(ob.step.value).toBe('done')
  })

  it('errors when no QR scanner and no credentials are available', async () => {
    const native = nativeBridge({ qrAvailable: false })

    const ob = useOnboarding({
      controllerUrl: 'http://controller:8080',
      native,
      createClient: () => nodeClient('active'),
    })
    await ob.run()

    expect(ob.step.value).toBe('scan')
    expect(ob.error.value).toMatch(/QR scanning is unavailable/)
  })

  it('polls the controller and adopts a pending node', async () => {
    let inInventory = false
    const adoptNode = vi.fn(async () => {
      inInventory = true
      return { status: 'approved' }
    })
    const controller = {
      listNodes: async () => (inInventory ? [{ id: 'node-1' }] : []),
      listPendingEnrollments: async () => [{ node_id: 'node-1', id: 'e1' }],
      adoptNode,
    } as unknown as ApiClient

    const ob = useOnboarding({
      controllerUrl: 'http://controller:8080',
      credentials: creds,
      native: nativeBridge({}),
      createClient: () => nodeClient('pending_approval'),
      controllerClient: controller,
      pollIntervalMs: 1,
      pollTimeoutMs: 200,
    })
    await ob.run()

    expect(adoptNode).toHaveBeenCalledWith('node-1')
    expect(ob.adopted.value).toBe(true)
    expect(ob.enrollStatus.value).toBe('active')
    expect(ob.step.value).toBe('done')
  })

  it('ends pending (not fatal) when the controller is unreachable for adoption', async () => {
    const ob = useOnboarding({
      controllerUrl: 'http://controller:8080',
      credentials: creds,
      native: nativeBridge({}),
      createClient: () => nodeClient('pending_approval'),
      // no controllerClient
    })
    await ob.run()

    expect(ob.error.value).toBeNull()
    expect(ob.step.value).toBe('done')
    expect(ob.adopted.value).toBe(false)
    expect(ob.note.value).toMatch(/approve this node on the controller/i)
  })

  it('skips WiFi join when unavailable and notes it', async () => {
    const join = vi.fn(async () => {})
    const native = nativeBridge({ wifiAvailable: false, join })

    const ob = useOnboarding({
      controllerUrl: 'http://controller:8080',
      credentials: creds,
      native,
      createClient: () => nodeClient('active'),
    })
    await ob.run()

    expect(join).not.toHaveBeenCalled()
    expect(ob.note.value).toMatch(/already on the node/i)
    expect(ob.step.value).toBe('done')
  })

  it('surfaces the failing step when the node is unreachable', async () => {
    const node = {
      getSetup: async () => {
        throw new Error('connection refused')
      },
    } as unknown as ApiClient

    const ob = useOnboarding({
      controllerUrl: 'http://controller:8080',
      credentials: creds,
      native: nativeBridge({}),
      createClient: () => node,
    })
    await ob.run()

    expect(ob.step.value).toBe('connectNode')
    expect(ob.error.value).toMatch(/connection refused/)
  })
})
