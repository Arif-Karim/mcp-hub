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

  it('shows only tool calls by default, newest first', async () => {
    const client = createMockClient({ latencyMs: 0 })
    await client.enable('service-directory')
    const w = mount(ActivityView, { props: { client } })
    await flushPromises()
    const rows = w.findAll('tbody tr')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('tools/call')
    expect(rows[0].text()).toContain('find_service')
    expect(rows[0].text()).toContain('service-directory')
    expect(w.text()).not.toContain('initialize')
    w.unmount()
  })

  it('reveals connection activity with friendly labels when the toggle is on', async () => {
    const client = createMockClient({ latencyMs: 0 })
    await client.enable('service-directory')
    const w = mount(ActivityView, { props: { client } })
    await flushPromises()
    const toggle = w.get('input[type=checkbox]')
    expect(w.get(`label[for="${toggle.attributes('id')}"]`).text()).toBe(
      'Show connection activity (handshake, listing, streams)',
    )
    expect((toggle.element as HTMLInputElement).checked).toBe(false)
    await toggle.setValue(true)
    const rows = w.findAll('tbody tr')
    expect(rows).toHaveLength(3)
    expect(rows[0].text()).toContain('find_service')
    expect(rows[1].text()).toContain('Listed tools')
    expect(rows[2].text()).toContain('Connected')
    expect(rows[2].text()).toContain('initialize')
    await toggle.setValue(false)
    expect(w.findAll('tbody tr')).toHaveLength(1)
    w.unmount()
  })

  it('explains when there is only connection traffic and no tool calls yet', async () => {
    const client = createMockClient({ latencyMs: 0 })
    client.audit = () =>
      Promise.resolve([
        { time: new Date().toISOString(), server: 'deepwiki', method: 'GET', status: 405, durationMs: 3, user: 'u' },
        { time: new Date().toISOString(), server: 'deepwiki', method: 'initialize', status: 200, durationMs: 3, user: 'u' },
      ])
    const w = mount(ActivityView, { props: { client } })
    await flushPromises()
    expect(w.find('table').exists()).toBe(false)
    expect(w.text()).toContain('No tool calls yet')
    expect(w.text()).toContain('2 connection events hidden')
    await w.get('input[type=checkbox]').setValue(true)
    const rows = w.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('Event stream')
    expect(rows[0].text()).toContain('405')
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
