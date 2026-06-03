import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'

import OnboardView from './OnboardView.vue'
import { ApiClient } from '@/api/client'
import { resetNativeBridge, setNativeBridge, type NativeBridge } from '@/native'

function nativeReady(): NativeBridge {
  return {
    isNative: true,
    discovery: { isAvailable: () => false, discoverControllers: async () => [] },
    wifi: { isAvailable: () => true, joinNetwork: async () => {} },
    qr: {
      isAvailable: () => true,
      scanSetupLabel: async () => ({ ssid: 'OMM-Setup-a51f', password: 'pw', serial: 'sn-1' }),
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
function nodeClient(): ApiClient {
  return {
    getSetup: async () => ({
      setup_complete: false,
      node_id: 'node-1',
      serial: '',
      home_id: '',
      home_name: '',
    }),
    joinHome: async () => ({ status: 'active' }),
  } as unknown as ApiClient
}

function jsonResp(data: unknown) {
  return { ok: true, status: 200, json: async () => data, text: async () => '' } as Response
}

describe('OnboardView', () => {
  afterEach(() => {
    resetNativeBridge()
    vi.unstubAllGlobals()
  })

  it('reaches the node with the default createClient (no createClient prop)', async () => {
    // Regression: function-typed prop defaults must BE the function, not a
    // factory — otherwise createClient(nodeUrl) returns a function and
    // nodeClient.getSetup is not a function.
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

    // Provide only `client` (for discovery); createClient/connect use defaults.
    const wrapper = mount(OnboardView, { props: { client: controllerClient() } })

    await wrapper.find('[data-test="choose-home"] .btn--primary').trigger('click')
    await flushPromises()
    await wrapper.find('[data-test="home-row"] .btn--primary').trigger('click')
    await wrapper.find('[data-test="node-url"]').setValue('http://127.0.0.1:8081')
    await wrapper.find('[data-test="ready"] .btn--primary').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="onboard-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="onboard-done"]').exists()).toBe(true)
    // The default createClient reached the node over HTTP.
    expect(fetchMock).toHaveBeenCalledWith('http://127.0.0.1:8081/setup', expect.any(Object))
  })

  it('discovers a Home and offers to start once one is selected', async () => {
    setNativeBridge(nativeReady())
    const wrapper = mount(OnboardView, {
      props: { client: controllerClient(), createClient: () => nodeClient() },
    })

    await wrapper.find('[data-test="choose-home"] .btn--primary').trigger('click')
    await flushPromises()

    expect(wrapper.findAll('[data-test="home-row"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('Casa')

    // Selecting the Home reveals the Start action.
    await wrapper.find('[data-test="home-row"] .btn--primary').trigger('click')
    expect(wrapper.find('[data-test="ready"]').exists()).toBe(true)
  })

  it('targets the node URL from the field (overriding the setup-AP default)', async () => {
    setNativeBridge(nativeReady())
    const seen: string[] = []
    const createClient = (baseUrl: string) => {
      seen.push(baseUrl)
      return nodeClient()
    }
    const wrapper = mount(OnboardView, {
      props: { client: controllerClient(), createClient },
    })

    await wrapper.find('[data-test="choose-home"] .btn--primary').trigger('click')
    await flushPromises()
    await wrapper.find('[data-test="home-row"] .btn--primary').trigger('click')
    await wrapper.find('[data-test="node-url"]').setValue('http://127.0.0.1:8081')
    await wrapper.find('[data-test="ready"] .btn--primary').trigger('click')
    await flushPromises()

    expect(seen).toContain('http://127.0.0.1:8081')
  })

  it('runs the full flow to an adopted node', async () => {
    setNativeBridge(nativeReady())
    const wrapper = mount(OnboardView, {
      props: { client: controllerClient(), createClient: () => nodeClient() },
    })

    await wrapper.find('[data-test="choose-home"] .btn--primary').trigger('click')
    await flushPromises()
    await wrapper.find('[data-test="home-row"] .btn--primary').trigger('click')
    await wrapper.find('[data-test="ready"] .btn--primary').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="step-enroll"]').attributes('data-state')).toBe('done')
    const done = wrapper.find('[data-test="onboard-done"]')
    expect(done.exists()).toBe(true)
    expect(done.text()).toContain('node-1')
  })

  it('signs in to a split-mode controller to confirm adoption', async () => {
    setNativeBridge(nativeReady())

    // Node enrolls pending; the authenticated controller client adopts it.
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
    const pendingNode = {
      getSetup: async () => ({
        setup_complete: false,
        node_id: 'node-1',
        serial: '',
        home_id: '',
        home_name: '',
      }),
      joinHome: async () => ({ status: 'pending_approval' }),
    } as unknown as ApiClient

    const connect = vi.fn(async () => authedController)

    const wrapper = mount(OnboardView, {
      props: { client: controllerClient(), createClient: () => pendingNode, connect },
    })

    await wrapper.find('[data-test="choose-home"] .btn--primary').trigger('click')
    await flushPromises()
    await wrapper.find('[data-test="home-row"] .btn--primary').trigger('click')

    // Provide controller credentials, then start.
    await wrapper.find('[data-test="auth-username"]').setValue('root')
    await wrapper.find('[data-test="auth-password"]').setValue('pw')
    await wrapper.find('[data-test="ready"] .btn--primary').trigger('click')
    await flushPromises()

    expect(connect).toHaveBeenCalledWith('http://controller:8080', {
      username: 'root',
      password: 'pw',
    })
    expect(adoptNode).toHaveBeenCalledWith('node-1')
    expect(wrapper.find('[data-test="onboard-done"]').exists()).toBe(true)
  })
})
