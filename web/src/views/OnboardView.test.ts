import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import OnboardView from './OnboardView.vue'
import { ApiClient } from '@/api/client'
import { resetNativeBridge, setNativeBridge, type NativeBridge } from '@/native'
import type { SetupCredentials, WifiNetwork } from '@/native'

// A native bridge with selectable capabilities. `scanNetworks` is omitted by
// default (iOS/web), matching the real fallback; pass it to exercise the
// Android-only SSID picker.
function nativeReady(over: {
  scanNetworks?: () => Promise<WifiNetwork[]>
  scanSetupLabel?: () => Promise<SetupCredentials>
} = {}): NativeBridge {
  return {
    isNative: true,
    discovery: { isAvailable: () => false, discoverControllers: async () => [] },
    wifi: {
      isAvailable: () => true,
      joinNetwork: async () => {},
      ...(over.scanNetworks ? { scanNetworks: over.scanNetworks } : {}),
    },
    qr: {
      isAvailable: () => true,
      scanSetupLabel:
        over.scanSetupLabel ?? (async () => ({ ssid: 'OMM-Setup-a51f', password: 'pw', serial: 'sn-1' })),
    },
  }
}

// Controller client used for discovery (and adoption, unused on the active path).
function controllerClient(): ApiClient {
  return {
    scanHomes: async () => [
      { home_id: 'h1', name: 'Casa', controller_id: 'gw01', api: 'http://controller:8080' },
    ],
  } as unknown as ApiClient
}

// Node client reached over the LAN: reports identity and enrolls active.
function nodeClient(status = 'active'): ApiClient {
  return {
    getSetup: async () => ({
      setup_complete: false,
      node_id: 'node-1',
      serial: '',
      home_id: '',
      home_name: '',
    }),
    joinHome: async () => ({ status }),
  } as unknown as ApiClient
}

function jsonResp(data: unknown) {
  return { ok: true, status: 200, json: async () => data, text: async () => '' } as Response
}

// Walk Page 1: discover, select the only Home, continue to Page 2.
async function gotoDevicePage(wrapper: ReturnType<typeof mount>) {
  await wrapper.find('[data-test="page-home"] .btn--primary').trigger('click')
  await flushPromises()
  await wrapper.find('[data-test="home-row"] .btn--primary').trigger('click')
  await wrapper.find('[data-test="home-continue"]').trigger('click')
  await flushPromises()
}

describe('OnboardView — 3-page wizard', () => {
  afterEach(() => {
    resetNativeBridge()
    vi.unstubAllGlobals()
  })

  it('starts on Page 1 (Choose Home); Pages 2 and 3 are not yet shown', () => {
    setNativeBridge(nativeReady())
    const wrapper = mount(OnboardView, { props: { client: controllerClient() } })

    expect(wrapper.find('[data-test="page-home"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="page-device"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="page-run"]').exists()).toBe(false)
  })

  it('Page 1: discovers Homes and only enables Continue once one is selected', async () => {
    setNativeBridge(nativeReady())
    const wrapper = mount(OnboardView, {
      props: { client: controllerClient(), createClient: () => nodeClient() },
    })

    await wrapper.find('[data-test="page-home"] .btn--primary').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-test="home-row"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('Casa')

    // Continue is disabled until a Home is chosen.
    expect(wrapper.find('[data-test="home-continue"]').attributes('disabled')).toBeDefined()
    await wrapper.find('[data-test="home-row"] .btn--primary').trigger('click')
    expect(wrapper.find('[data-test="home-continue"]').attributes('disabled')).toBeUndefined()
  })

  it('Page 2: shows the Android SSID picker filtered to OMM-Setup-* when scan is available', async () => {
    const scanNetworks = vi.fn(async (): Promise<WifiNetwork[]> => [
      { ssid: 'HomeNet' },
      { ssid: 'OMM-Setup-a51f' },
      { ssid: 'OMM-Setup-bbbb' },
    ])
    setNativeBridge(nativeReady({ scanNetworks }))
    const wrapper = mount(OnboardView, {
      props: { client: controllerClient(), createClient: () => nodeClient() },
    })
    await gotoDevicePage(wrapper)

    expect(wrapper.find('[data-test="page-device"]').exists()).toBe(true)
    const picker = wrapper.find('[data-test="ssid-picker"]')
    expect(picker.exists()).toBe(true)
    const rows = wrapper.findAll('[data-test="ssid-row"]')
    // Only the two OMM-Setup-* networks, not HomeNet.
    expect(rows).toHaveLength(2)
    expect(picker.text()).toContain('OMM-Setup-a51f')
    expect(picker.text()).not.toContain('HomeNet')
  })

  it('Page 2: hides the SSID picker and offers QR/manual when scan is unavailable (iOS/web)', async () => {
    setNativeBridge(nativeReady()) // no scanNetworks
    const wrapper = mount(OnboardView, {
      props: { client: controllerClient(), createClient: () => nodeClient() },
    })
    await gotoDevicePage(wrapper)

    expect(wrapper.find('[data-test="ssid-picker"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="manual-ssid"]').exists()).toBe(true)
  })

  it('Page 2: reveals home-WiFi fields only when the WiFi uplink is chosen, and provisions it', async () => {
    setNativeBridge(nativeReady())
    const provisionUplink = vi.fn(async () => ({ provisioned: true }))
    const node = {
      getSetup: async () => ({ setup_complete: false, node_id: 'node-1', serial: '', home_id: '', home_name: '' }),
      provisionUplink,
      joinHome: async () => ({ status: 'active' }),
    } as unknown as ApiClient

    const wrapper = mount(OnboardView, {
      props: { client: controllerClient(), createClient: () => node },
    })
    await gotoDevicePage(wrapper)

    // Default is wired: no home-WiFi credentials field.
    expect(wrapper.find('[data-test="home-wifi-ssid"]').exists()).toBe(false)

    // Choosing the WiFi uplink reveals the fields.
    await wrapper.find('[data-test="uplink-wifi"]').setValue(true)
    expect(wrapper.find('[data-test="home-wifi-ssid"]').exists()).toBe(true)
    await wrapper.find('[data-test="home-wifi-ssid"]').setValue('HomeNet')
    await wrapper.find('[data-test="home-wifi-password"]').setValue('home-pw')

    await wrapper.find('[data-test="device-start"]').trigger('click')
    await flushPromises()

    expect(provisionUplink).toHaveBeenCalledWith('HomeNet', 'home-pw')
    expect(wrapper.find('[data-test="onboard-done"]').exists()).toBe(true)
  })

  it('runs the full flow Page 1 → 2 → 3 to an adopted node, auto-progressing with no per-step buttons', async () => {
    setNativeBridge(nativeReady())
    const wrapper = mount(OnboardView, {
      props: { client: controllerClient(), createClient: () => nodeClient('active') },
    })
    await gotoDevicePage(wrapper)

    await wrapper.find('[data-test="device-start"]').trigger('click')
    await flushPromises()

    const run = wrapper.find('[data-test="page-run"]')
    expect(run.exists()).toBe(true)
    // The protocol steps advance without any user interaction between them.
    expect(wrapper.find('[data-test="step-enroll"]').attributes('data-state')).toBe('done')
    const done = wrapper.find('[data-test="onboard-done"]')
    expect(done.exists()).toBe(true)
    expect(done.text()).toContain('node-1')
  })

  it('signs in to a split-mode controller on Page 1 and adopts in the background (no approve tap)', async () => {
    setNativeBridge(nativeReady())

    let inInventory = false
    const adoptNode = vi.fn(async () => {
      inInventory = true
      return { status: 'approved' }
    })
    const authedController = {
      listNodes: async () => (inInventory ? [{ id: 'node-1' }] : []),
      listPendingEnrollments: async () => [{ node_id: 'node-1', id: 'e1' }],
      adoptNode,
    } as unknown as ApiClient
    const connect = vi.fn(async () => authedController)

    const wrapper = mount(OnboardView, {
      props: {
        client: controllerClient(),
        createClient: () => nodeClient('pending_approval'),
        connect,
      },
    })

    // Page 1: choose Home + controller sign-in, then continue.
    await wrapper.find('[data-test="page-home"] .btn--primary').trigger('click')
    await flushPromises()
    await wrapper.find('[data-test="home-row"] .btn--primary').trigger('click')
    await wrapper.find('[data-test="auth-username"]').setValue('root')
    await wrapper.find('[data-test="auth-password"]').setValue('pw')
    await wrapper.find('[data-test="home-continue"]').trigger('click')
    await flushPromises()

    // Page 2: start.
    await wrapper.find('[data-test="device-start"]').trigger('click')
    await flushPromises()

    expect(connect).toHaveBeenCalledWith('http://controller:8080', { username: 'root', password: 'pw' })
    // Adoption happens in the background — no approve button was clicked.
    expect(wrapper.find('[data-test="approve"]').exists()).toBe(false)
    expect(adoptNode).toHaveBeenCalledWith('node-1')
    expect(wrapper.find('[data-test="onboard-done"]').exists()).toBe(true)
  })

  it('targets the node URL from the field and reaches the node with the default createClient', async () => {
    setNativeBridge(nativeReady())
    const fetchMock = vi.fn(async (url: string | URL) => {
      const u = String(url)
      if (u.endsWith('/setup')) {
        return jsonResp({ setup_complete: false, node_id: 'node-1', serial: '', home_id: '', home_name: '' })
      }
      if (u.endsWith('/enroll/join')) return jsonResp({ status: 'active' })
      return jsonResp({})
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(OnboardView, { props: { client: controllerClient() } })
    await gotoDevicePage(wrapper)

    await wrapper.find('[data-test="node-url"]').setValue('http://127.0.0.1:8081')
    await wrapper.find('[data-test="device-start"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="onboard-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="onboard-done"]').exists()).toBe(true)
    expect(fetchMock).toHaveBeenCalledWith('http://127.0.0.1:8081/setup', expect.any(Object))
  })
})
