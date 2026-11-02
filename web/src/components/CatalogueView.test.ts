import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CatalogueView from './CatalogueView.vue'
import { createMockClient } from '../api/mock'
import type { ApiClient } from '../api/client'
import { toasts } from '../lib/toast'

/** Wraps the mock so the test controls exactly when enable() settles. */
function gatedClient() {
  const inner = createMockClient({
    latencyMs: 0,
    failures: { deepwiki: { status: 409, code: 'config_conflict' } },
  })
  let release!: () => void
  const client: ApiClient = {
    ...inner,
    enable: async (id) => {
      await new Promise<void>((r) => (release = r))
      return inner.enable(id)
    },
  }
  return { client, release: () => release() }
}

const card = (w: ReturnType<typeof mount>, id: string) => w.get(`[data-test=card-${id}]`)

describe('CatalogueView', () => {
  afterEach(() => toasts.splice(0))

  it('enable shows a loading state, then the new state', async () => {
    const { client, release } = gatedClient()
    const w = mount(CatalogueView, { props: { client } })
    await flushPromises()

    const btn = () => card(w, 'service-directory').get('[data-test=toggle]')
    expect(btn().text()).toBe('Enable')
    await btn().trigger('click')
    expect(btn().text()).toBe('Enabling…')
    expect(btn().attributes('disabled')).toBeDefined()

    release()
    await flushPromises()
    expect(btn().text()).toBe('Disable')
    expect(card(w, 'service-directory').get('[data-test=status]').text()).toBe('Running')
    expect(w.emitted('changed')).toHaveLength(1)
  })

  it('shows a human error inline and as a toast when enable fails', async () => {
    const { client, release } = gatedClient()
    const w = mount(CatalogueView, { props: { client } })
    await flushPromises()

    await card(w, 'deepwiki').get('[data-test=toggle]').trigger('click')
    release()
    await flushPromises()

    const msg = "You already have a server called hub__deepwiki in your Claude config that mcp-hub didn't create — not overwritten."
    expect(card(w, 'deepwiki').get('[role=alert]').text()).toContain(msg)
    expect(toasts.map((t) => t.text)).toContain(msg)
    expect(card(w, 'deepwiki').get('[data-test=toggle]').text()).toBe('Enable')
  })

  it('filters by kind chip and enabled-only', async () => {
    const client = createMockClient({ latencyMs: 0 })
    await client.enable('deepwiki')
    const w = mount(CatalogueView, { props: { client } })
    await flushPromises()
    const ids = () => w.findAll('[data-test^=card-]').map((c) => c.attributes('data-test'))

    expect(ids()).toHaveLength(5)
    await w.get('[data-test=chip-local]').trigger('click')
    expect(ids()).toEqual(['card-service-directory', 'card-payments-sandbox'])
    await w.get('[data-test=chip-all]').trigger('click')
    await w.get('[data-test=enabled-only]').setValue(true)
    expect(ids()).toEqual(['card-deepwiki'])
  })

  it('searches and shows an empty state', async () => {
    const client = createMockClient({ latencyMs: 0 })
    const w = mount(CatalogueView, { props: { client, debounceMs: 0 } })
    await flushPromises()
    await w.get('input[type=search]').setValue('zzz-no-match')
    await new Promise((r) => setTimeout(r, 5))
    await flushPromises()
    expect(w.findAll('[data-test^=card-]')).toHaveLength(0)
    expect(w.text()).toContain('No servers match')
  })

  it('opens the detail drawer from a card', async () => {
    const client = createMockClient({ latencyMs: 0 })
    const w = mount(CatalogueView, { props: { client } })
    await flushPromises()
    expect(w.find('[role=dialog]').exists()).toBe(false)
    await card(w, 'context7').get('[data-test=open]').trigger('click')
    expect(w.get('[role=dialog]').text()).toContain('Context7')
  })

  it('shows the "Use mcp-hub from Claude Code" tip only once the skill is installed', async () => {
    localStorage.removeItem('mcp-hub.callout.dismissed')
    const client = createMockClient({ latencyMs: 0 })
    const path = '/Users/dev/.claude/skills/mcp-hub/SKILL.md'
    const w = mount(CatalogueView, { props: { client, skill: { installed: false, upToDate: false, path } } })
    await flushPromises()
    expect(w.text()).not.toContain('Use mcp-hub from Claude Code')
    expect(w.find('[data-test=skill-banner]').exists()).toBe(true)

    await w.setProps({ skill: { installed: true, upToDate: true, path } })
    expect(w.text()).toContain('Use mcp-hub from Claude Code')
    expect(w.find('[data-test=skill-banner]').exists()).toBe(false)
  })

  it('ends the grid with an invite card linking to /publish', async () => {
    const w = mount(CatalogueView, { props: { client: createMockClient({ latencyMs: 0 }) } })
    await flushPromises()
    const cards = w.findAll('.grid > *')
    const invite = cards[cards.length - 1]
    expect(invite.attributes('data-test')).toBe('invite-card')
    expect(invite.text()).toContain('Your service here')
    expect(invite.get('a').attributes('href')).toBe('/publish')
  })
})
