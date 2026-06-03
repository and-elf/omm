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

describe('OnboardView', () => {
  afterEach(() => resetNativeBridge())

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
})
