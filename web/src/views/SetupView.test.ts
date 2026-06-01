import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import SetupView from './SetupView.vue'
import { ApiClient } from '@/api/client'

const push = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push }),
}))

function client() {
  const fetchFn = (() =>
    Promise.resolve({
      ok: true,
      status: 200,
      json: async () => ({ setup_complete: false, home_id: 'self-home', home_name: 'Home' }),
      text: async () => '',
    })) as unknown as typeof fetch
  return new ApiClient('', fetchFn)
}

describe('SetupView', () => {
  it('creates a home: renames self home, sets active, completes setup, navigates', async () => {
    push.mockClear()
    const c = client()
    const update = vi.spyOn(c, 'updateHome').mockResolvedValue({} as never)
    const setActive = vi.spyOn(c, 'setActiveHome').mockResolvedValue({ home_id: 'self-home' })
    const complete = vi.spyOn(c, 'completeSetup').mockResolvedValue({})

    const wrapper = mount(SetupView, { props: { client: c } })
    await flushPromises()

    await wrapper.find('[data-test="choice-create"]').trigger('click')
    await wrapper.find('[data-test="form-create"] input').setValue('Cottage')
    await wrapper.find('[data-test="form-create"]').trigger('submit')
    await flushPromises()

    expect(update).toHaveBeenCalledWith('self-home', { name: 'Cottage' })
    expect(setActive).toHaveBeenCalledWith('self-home')
    expect(complete).toHaveBeenCalled()
    expect(push).toHaveBeenCalledWith('/')
  })

  it('joins an existing home then completes setup', async () => {
    push.mockClear()
    const c = client()
    const join = vi.spyOn(c, 'joinHome').mockResolvedValue({ status: 'active' })
    const complete = vi.spyOn(c, 'completeSetup').mockResolvedValue({})

    const wrapper = mount(SetupView, { props: { client: c } })
    await flushPromises()

    await wrapper.find('[data-test="choice-join"]').trigger('click')
    await wrapper.find('[data-test="form-join"] input').setValue('http://ctrl:8080')
    await wrapper.find('[data-test="form-join"]').trigger('submit')
    await flushPromises()

    expect(join).toHaveBeenCalledWith('http://ctrl:8080', undefined)
    expect(complete).toHaveBeenCalled()
    expect(push).toHaveBeenCalledWith('/')
  })
})
