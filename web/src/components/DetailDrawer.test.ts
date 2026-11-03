import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import DetailDrawer from './DetailDrawer.vue'
import { fixture } from '../api/fixture'
import type { CatalogueItem } from '../api/types'

const item = (id: string): CatalogueItem => structuredClone(fixture.find((i) => i.id === id)!)

describe('DetailDrawer', () => {
  it('explains auth, lists tools and hides config details while disabled', () => {
    const w = mount(DetailDrawer, { props: { item: item('service-directory') } })
    expect(w.text()).toContain('mcp-hub injects a short-lived token for you — nothing is stored in your Claude config')
    expect(w.text()).toContain('find_service')
    expect(w.text()).toContain('get_runbook')
    expect(w.text()).toContain('0.1.0')
    expect(w.find('[data-test=config-name]').exists()).toBe(false)
    expect(w.text()).not.toContain('hub__service-directory')
    expect(w.text()).not.toContain('/reload-plugins')
  })

  it('explains auth none', () => {
    const w = mount(DetailDrawer, { props: { item: item('deepwiki') } })
    expect(w.text()).toContain('No credentials are sent')
  })

  it('shows hub__ config name, proxy URL and restart note once enabled', () => {
    const i = item('service-directory')
    i.state = {
      enabled: true,
      status: 'running',
      configName: 'hub__service-directory',
      proxyUrl: 'http://127.0.0.1:7420/mcp/service-directory',
    }
    const w = mount(DetailDrawer, { props: { item: i } })
    expect(w.get('[data-test=config-name]').text()).toBe('hub__service-directory')
    expect(w.get('[data-test=proxy-url]').text()).toBe('http://127.0.0.1:7420/mcp/service-directory')
    expect(w.text()).toContain('Run /reload-plugins in Claude Code (or restart it) to load the new server.')
    expect(w.text()).toContain('After disabling, restart Claude Code to remove it')
    expect(w.findAll('[data-test=copy]')).toHaveLength(2)
  })

  it.each([
    ['https://docs.example.test/x', true],
    ['http://localhost:8080/docs', true],
    ['http://127.0.0.1:8080/docs', true],
    ['http://docs.example.test/x', false],
    ['javascript:alert(1)', false],
    ['not a url', false],
  ])('renders docsUrl %s as a link: %s', (url, shown) => {
    const i = item('deepwiki')
    i.docsUrl = url
    const w = mount(DetailDrawer, { props: { item: i } })
    const link = w.find('[data-test=docs]')
    expect(link.exists()).toBe(shown)
    if (shown) expect(link.attributes('href')).toBe(url)
  })

  it('closes on Escape and via the close button', async () => {
    const w = mount(DetailDrawer, { props: { item: item('deepwiki') }, attachTo: document.body })
    await w.get('[role=dialog]').trigger('keydown', { key: 'Escape' })
    await w.get('[data-test=close]').trigger('click')
    expect(w.emitted('close')).toHaveLength(2)
    w.unmount()
  })

  it('closes on Escape even when focus has left the drawer', async () => {
    const onClose = vi.fn()
    const w = mount(DetailDrawer, { props: { item: item('deepwiki'), onClose }, attachTo: document.body })
    document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(onClose).toHaveBeenCalledTimes(1)
    w.unmount()
    document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(onClose).toHaveBeenCalledTimes(1) // listener removed on unmount
  })

  it('moves focus into the drawer when opened', async () => {
    const w = mount(DetailDrawer, { props: { item: item('deepwiki') }, attachTo: document.body })
    await w.vm.$nextTick()
    expect(document.activeElement).toBe(w.get('[data-test=close]').element)
    w.unmount()
  })
})
