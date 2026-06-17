import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import NodesView from './NodesView.vue'
import { ApiClient } from '@/api/client'

const stubs = { RouterLink: { template: '<a><slot /></a>' } }

function clientReturning(nodes: unknown) {
  const fetchFn = (() =>
    Promise.resolve({
      ok: true,
      status: 200,
      json: async () => ({ nodes }),
      text: async () => '',
    })) as unknown as typeof fetch
  return new ApiClient('', fetchFn)
}

const NODES = [
  { id: 'n1', serial: 'Garaget', current_home: 'home-1', trusted_homes: [], last_seen: 1 },
  { id: 'n2', serial: 'Kontoret', current_home: 'home-1', trusted_homes: [], last_seen: 2 },
]

describe('NodesView', () => {
  it('renders a row per node', async () => {
    const wrapper = mount(NodesView, { props: { client: clientReturning(NODES) }, global: { stubs } })
    await flushPromises()

    expect(wrapper.findAll('[data-test="node-row"]')).toHaveLength(2)
    expect(wrapper.text()).toContain('Garaget')
    expect(wrapper.text()).toContain('Kontoret')
  })

  it('disables the delete button until a node is selected', async () => {
    const wrapper = mount(NodesView, { props: { client: clientReturning(NODES) }, global: { stubs } })
    await flushPromises()

    const btn = wrapper.find('[data-test="delete-selected"]')
    expect(btn.attributes('disabled')).toBeDefined()

    await wrapper.findAll('[data-test="select-node"]')[0].setValue(true)
    expect(btn.attributes('disabled')).toBeUndefined()
    expect(btn.text()).toContain('(1)')
  })

  it('deletes every selected node after confirmation, then refreshes', async () => {
    const client = clientReturning(NODES)
    const deleteSpy = vi.spyOn(client, 'deleteNode').mockResolvedValue()
    vi.spyOn(window, 'confirm').mockReturnValue(true)

    const wrapper = mount(NodesView, { props: { client }, global: { stubs } })
    await flushPromises()

    await wrapper.find('[data-test="select-all"]').setValue(true)
    await wrapper.find('[data-test="delete-selected"]').trigger('click')
    await flushPromises()

    expect(deleteSpy).toHaveBeenCalledTimes(2)
    expect(deleteSpy).toHaveBeenCalledWith('n1')
    expect(deleteSpy).toHaveBeenCalledWith('n2')
  })

  it('does not delete when confirmation is declined', async () => {
    const client = clientReturning(NODES)
    const deleteSpy = vi.spyOn(client, 'deleteNode').mockResolvedValue()
    vi.spyOn(window, 'confirm').mockReturnValue(false)

    const wrapper = mount(NodesView, { props: { client }, global: { stubs } })
    await flushPromises()

    await wrapper.findAll('[data-test="select-node"]')[0].setValue(true)
    await wrapper.find('[data-test="delete-selected"]').trigger('click')
    await flushPromises()

    expect(deleteSpy).not.toHaveBeenCalled()
  })

  it('treats a 404 (already gone) as success and still removes the rest', async () => {
    const client = clientReturning(NODES)
    const deleteSpy = vi.spyOn(client, 'deleteNode').mockImplementation((id: string) => {
      if (id === 'n1') return Promise.reject(Object.assign(new Error('not found'), { status: 404 }))
      return Promise.resolve()
    })
    vi.spyOn(window, 'confirm').mockReturnValue(true)

    const wrapper = mount(NodesView, { props: { client }, global: { stubs } })
    await flushPromises()

    await wrapper.find('[data-test="select-all"]').setValue(true)
    await wrapper.find('[data-test="delete-selected"]').trigger('click')
    await flushPromises()

    expect(deleteSpy).toHaveBeenCalledTimes(2)
    // No error surfaced — the 404 was swallowed and n2 still deleted.
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
})
