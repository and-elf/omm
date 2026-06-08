import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import DashboardView from './DashboardView.vue'
import { ApiClient } from '@/api/client'
import type { Backhaul } from '@/types'

function client(backhaul?: Backhaul) {
  const c = new ApiClient('')
  vi.spyOn(c, 'getStatus').mockResolvedValue({ status: 'ready', backhaul })
  vi.spyOn(c, 'listHomes').mockResolvedValue([])
  vi.spyOn(c, 'listNodes').mockResolvedValue([])
  return c
}

describe('DashboardView', () => {
  it('shows the backhaul mode and no degrade notice when the mesh is up', async () => {
    const wrapper = mount(DashboardView, { props: { client: client({ mode: '802.11s' }) } })
    await flushPromises()

    expect(wrapper.text()).toContain('802.11s mesh')
    expect(wrapper.find('.notice--warn').exists()).toBe(false)
  })

  it('surfaces the reason and remediation when degraded to multi-AP', async () => {
    const wrapper = mount(DashboardView, {
      props: {
        client: client({
          mode: 'multi_ap',
          reason: '802.11s mesh did not start',
          remediation: 'install wpad-mesh-wolfssl',
        }),
      },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Wired multi-AP')
    const notice = wrapper.find('.notice--warn')
    expect(notice.exists()).toBe(true)
    expect(notice.text()).toContain('802.11s mesh did not start')
    expect(notice.text()).toContain('install wpad-mesh-wolfssl')
  })
})
