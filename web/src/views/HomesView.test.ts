import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import HomesView from './HomesView.vue'
import { ApiClient } from '@/api/client'

const stubs = { RouterLink: { template: '<a><slot /></a>' } }

function clientReturning(homes: unknown) {
  const fetchFn = (() =>
    Promise.resolve({
      ok: true,
      status: 200,
      json: async () => ({ homes, home_id: '' }),
      text: async () => '',
    })) as unknown as typeof fetch
  return new ApiClient('', fetchFn)
}

describe('HomesView', () => {
  it('renders each home returned by the client', async () => {
    const client = clientReturning([
      { id: 'h1', name: 'Main House', controller: 'gw01', certificate: null, last_seen: 1609459200 },
      { id: 'h2', name: 'Cottage', controller: 'gw02', certificate: null, last_seen: 0 },
    ])

    const wrapper = mount(HomesView, { props: { client }, global: { stubs } })
    await flushPromises()

    expect(wrapper.text()).toContain('Main House')
    expect(wrapper.text()).toContain('Cottage')
    expect(wrapper.findAll('[data-test="home-row"]')).toHaveLength(2)
  })

  it('shows an empty state when there are no homes', async () => {
    const wrapper = mount(HomesView, { props: { client: clientReturning([]) }, global: { stubs } })
    await flushPromises()

    expect(wrapper.text().toLowerCase()).toContain('no homes')
  })

  it('creates a home from the form', async () => {
    const client = clientReturning([])
    const createSpy = vi.spyOn(client, 'createHome').mockResolvedValue({} as never)

    const wrapper = mount(HomesView, { props: { client }, global: { stubs } })
    await flushPromises()

    await wrapper.find('input').setValue('Cottage')
    await wrapper.find('[data-test="create-home"]').trigger('submit')
    await flushPromises()

    expect(createSpy).toHaveBeenCalledWith(
      expect.objectContaining({ name: 'Cottage' }),
    )
  })
})
