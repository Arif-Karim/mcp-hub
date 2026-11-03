import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'

vi.mock('./api', async () => {
  const { createMockClient } = await import('./api/mock')
  return { api: createMockClient({ latencyMs: 0 }) }
})

const { default: App } = await import('./App.vue')

let w: VueWrapper | undefined
async function mountAt(path: string) {
  history.replaceState(null, '', path)
  w = mount(App, { attachTo: document.body })
  await flushPromises()
  return w
}

describe('App routing', () => {
  afterEach(() => {
    w?.unmount()
    w = undefined
    history.replaceState(null, '', '/')
  })

  it('renders the publish page when opened at /publish', async () => {
    const app = await mountAt('/publish')
    expect(app.find('#publish-heading').exists()).toBe(true)
    expect(document.title).toBe('Publish your server · mcp-hub')
  })

  it('normalises unknown paths to the catalogue', async () => {
    const app = await mountAt('/nope/')
    expect(app.find('#catalogue-heading').exists()).toBe(true)
    expect(location.pathname).toBe('/')
    expect(document.title).toBe('Catalogue · mcp-hub')
  })

  it('switches view on Back (popstate)', async () => {
    const app = await mountAt('/publish')
    history.pushState(null, '', '/activity')
    window.dispatchEvent(new PopStateEvent('popstate'))
    await flushPromises()
    expect(app.find('#activity-heading').exists()).toBe(true)
    expect(app.find('#publish-heading').exists()).toBe(false)
  })

  it('pushes exactly one history entry per navigation', async () => {
    const app = await mountAt('/')
    const before = history.length
    await app.get('[data-test=publish-cta]').trigger('click')
    await flushPromises()
    expect(location.pathname).toBe('/publish')
    expect(history.length).toBe(before + 1)

    const activity = app.findAll('.tab').find((t) => t.text() === 'Activity')!
    await activity.trigger('click')
    await flushPromises()
    expect(location.pathname).toBe('/activity')
    expect(history.length).toBe(before + 2)
  })

  it('moves focus to the new view heading and updates the title', async () => {
    const app = await mountAt('/')
    await app.get('[data-test=publish-cta]').trigger('click')
    await flushPromises()
    expect(document.activeElement?.id).toBe('publish-heading')
    expect(document.title).toBe('Publish your server · mcp-hub')
  })
})
