import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import EnrollView from './EnrollView.vue'
import { ApiClient } from '@/api/client'

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

  it('joins another home via the form', async () => {
    const client = clientWithPending([])
    const join = vi.spyOn(client, 'joinHome').mockResolvedValue({ status: 'active' })

    const wrapper = mount(EnrollView, { props: { client } })
    await flushPromises()

    await wrapper.find('[data-test="join-form"] input').setValue('http://other:8080')
    await wrapper.find('[data-test="join-form"]').trigger('submit')
    await flushPromises()

    expect(join).toHaveBeenCalledWith('http://other:8080', undefined)
    expect(wrapper.text()).toContain('status: active')
  })
})
