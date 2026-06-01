import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import SettingsView from './SettingsView.vue'
import { ApiClient } from '@/api/client'

const stubs = { RouterLink: { template: '<a><slot /></a>' } }

function client() {
  const c = new ApiClient('')
  vi.spyOn(c, 'getSetup').mockResolvedValue({
    setup_complete: true,
    node_id: 'node-abc',
    serial: 'gw-1',
    home_id: 'garage',
    home_name: 'Garage',
  })
  vi.spyOn(c, 'getStatus').mockResolvedValue({ status: 'ready' })
  vi.spyOn(c, 'getHomeSelection').mockResolvedValue({
    recommended_home_id: 'cottage',
    active_home_id: 'garage',
    candidates: [
      { home_id: 'garage', signal: 0, last_active: 10, self_controlled: true },
      { home_id: 'cottage', signal: -55, last_active: 20, self_controlled: false },
    ],
  })
  return c
}

describe('SettingsView', () => {
  it('shows device info, the recommendation and candidates', async () => {
    const wrapper = mount(SettingsView, { props: { client: client() }, global: { stubs } })
    await flushPromises()

    expect(wrapper.text()).toContain('node-abc')
    expect(wrapper.text()).toContain('Garage')
    expect(wrapper.findAll('[data-test="candidate-row"]')).toHaveLength(2)
    expect(wrapper.text()).toContain('-55 dBm')
    expect(wrapper.text()).toContain('recommended')
  })

  it('sets the active home from a candidate', async () => {
    const c = client()
    const setActive = vi.spyOn(c, 'setActiveHome').mockResolvedValue({ home_id: 'cottage' })

    const wrapper = mount(SettingsView, { props: { client: c }, global: { stubs } })
    await flushPromises()

    // The cottage row (not active) exposes a Set active button.
    const cottageRow = wrapper.findAll('[data-test="candidate-row"]')[1]
    await cottageRow.find('button').trigger('click')

    expect(setActive).toHaveBeenCalledWith('cottage')
  })
})
