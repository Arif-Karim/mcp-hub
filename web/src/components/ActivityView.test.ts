import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api/client'
import type { ApiClient } from '../api/client'
import { flushPromises, mount } from '@vue/test-utils'
import ActivityView from './ActivityView.vue'
import { createMockClient } from '../api/mock'

describe('ActivityView', () => {
  it('explains the empty state, including what is never recorded', async () => {
    const w = mount(ActivityView, { props: { client: createMockClient({ latencyMs: 0 }) } })
    await flushPromises()
    expect(w.find('table').exists()).toBe(false)
    expect(w.text()).toContain('Tool calls made by Claude Code through mcp-hub appear here.')
    expect(w.text()).toContain('Arguments and results are never recorded.')
    w.unmount()
  })

  it('renders events newest first', async () => {
    const client = createMockClient({ latencyMs: 0 })
    await client.enable('service-directory')
    const w = mount(ActivityView, { props: { client } })
    await flushPromises()
    const rows = w.findAll('tbody tr')
    expect(rows).toHaveLength(3)
    expect(rows[0].text()).toContain('tools/call')
    expect(rows[0].text()).toContain('find_service')
    expect(rows[2].text()).toContain('initialize')
    expect(rows[0].text()).toContain('service-directory')
    w.unmount()
  })

  describe('failures and polling', () => {
    afterEach(() => vi.useRealTimers())

    it('shows a generic 404 message (not catalogue wording) and no empty state alongside it', async () => {
      const client = createMockClient({ latencyMs: 0 })
      client.audit = () => Promise.reject(new ApiError(404, 'not_found', 'no route'))
      const w = mount(ActivityView, { props: { client } })
      await flushPromises()
      const alert = w.get('[role=alert]').text()
      expect(alert).not.toMatch(/catalogue/i)
      expect(alert).toMatch(/404/)
      expect(w.text()).not.toContain('Tool calls made by Claude Code')
      w.unmount()
    })

    it('does not start a new poll while the previous one is in flight', async () => {
      vi.useFakeTimers()
      let calls = 0
      const client = {
        ...createMockClient({ latencyMs: 0 }),
        audit: () => {
          calls++
          return new Promise<never>(() => {}) // never settles
        },
      } as ApiClient
      const w = mount(ActivityView, { props: { client, intervalMs: 100 } })
      await vi.advanceTimersByTimeAsync(550)
      expect(calls).toBe(1)
      w.unmount()
    })
  })
})
