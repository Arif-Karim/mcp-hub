import { describe, expect, it } from 'vitest'
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
})
