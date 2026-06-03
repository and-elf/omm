import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import EnrollView from './EnrollView.vue'
import { ApiClient } from '@/api/client'
import { resetNativeBridge, setNativeBridge, type NativeBridge } from '@/native'

function clientWithPending(pending: unknown) {
  const fetchFn = (() =>
    Promise.resolve({
      ok: true,
      status: 200,
      json: async () => ({ enrollments: pending }),
      text: async () => '',
    })) as unknown as typeof fetch
  return new ApiClient('', fetchFn)
}

describe('EnrollView', () => {
  afterEach(() => resetNativeBridge())

  it('lists pending devices and approves one', async () => {
    const client = clientWithPending([
      { id: 'e1', node_id: 'abcdef0123456789aa', serial: 'SN1', status: 'pending_approval', created_at: 1 },
    ])
    const adopt = vi.spyOn(client, 'adoptNode').mockResolvedValue({ status: 'approved' })

    const wrapper = mount(EnrollView, { props: { client } })
    await flushPromises()

    expect(wrapper.findAll('[data-test="pending-row"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('SN1')

    await wrapper.find('[data-test="pending-row"] .btn--primary').trigger('click')
    expect(adopt).toHaveBeenCalledWith('abcdef0123456789aa')
  })

  it('shows an empty state when nothing is pending', async () => {
    const wrapper = mount(EnrollView, { props: { client: clientWithPending([]) } })
    await flushPromises()
    expect(wrapper.text().toLowerCase()).toContain('no devices')
  })

  it('scans for homes and joins a discovered one', async () => {
    const client = clientWithPending([])
    vi.spyOn(client, 'scanHomes').mockResolvedValue([
      { home_id: 'h1', name: 'Cottage', controller_id: 'gw01', api: 'http://other:8080' },
    ])
    const join = vi.spyOn(client, 'joinHome').mockResolvedValue({ status: 'active' })

    const wrapper = mount(EnrollView, { props: { client } })
    await flushPromises()

    // Scan, then join the discovered Home.
    await wrapper.find('[data-test="join-section"] .btn--primary').trigger('click')
    await flushPromises()

    const rows = wrapper.findAll('[data-test="found-row"]')
    expect(rows).toHaveLength(1)
    expect(wrapper.text()).toContain('Cottage')

    await rows[0].find('button').trigger('click')
    await flushPromises()

    expect(join).toHaveBeenCalledWith('http://other:8080')
    expect(wrapper.text()).toContain('status: active')
  })

  it('discovers via native mDNS when available and labels the source', async () => {
    const native: NativeBridge = {
      isNative: true,
      discovery: {
        isAvailable: () => true,
        discoverControllers: async () => [
          { home_id: 'h2', name: 'Garage', controller_id: 'gw02', api: 'http://10.0.0.2:8080' },
        ],
      },
      wifi: { isAvailable: () => false, joinNetwork: async () => {} },
      qr: { isAvailable: () => false, scanSetupLabel: async () => ({ ssid: 'x' }) },
    }
    setNativeBridge(native)

    const client = clientWithPending([])
    // mDNS must win: the daemon scan must not be consulted.
    const scan = vi.spyOn(client, 'scanHomes')

    const wrapper = mount(EnrollView, { props: { client } })
    await flushPromises()

    await wrapper.find('[data-test="join-section"] .btn--primary').trigger('click')
    await flushPromises()

    expect(scan).not.toHaveBeenCalled()
    expect(wrapper.findAll('[data-test="found-row"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('Garage')
    expect(wrapper.find('[data-test="discovery-source"]').text()).toContain('mDNS')
  })
})
