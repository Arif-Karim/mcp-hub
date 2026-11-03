import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PublishView from './PublishView.vue'
import { GUIDE_URL, TEMPLATE_URL, exampleEntry } from '../lib/publish'

describe('PublishView', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('renders the five publishing steps in order', () => {
    const steps = mount(PublishView).findAll('[data-test=steps] > li')
    expect(steps).toHaveLength(5)
    expect(steps[0].text()).toContain('template')
    expect(steps[1].text()).toContain('checksums')
    expect(steps[2].text()).toContain('approval: pending')
    expect(steps[3].text()).toContain('Platform team')
    expect(steps[4].text()).toMatch(/approved/i)
  })

  it('says which repo each step happens in', () => {
    const steps = mount(PublishView).findAll('[data-test=steps] > li').map((s) => s.text())
    expect(steps[0]).toContain("your team's own repo")
    expect(steps[1]).toContain('publish-mcp-server.yml')
    expect(steps[2]).toContain('mcp-hub')
    expect(steps[2]).toContain('internal/catalogue/default.yaml')
  })

  it('does not imply approval or revocation is instant', () => {
    const w = mount(PublishView)
    expect(w.findAll('[data-test=steps] > li')[4].text()).toContain('next mcp-hub release')
    expect(w.findAll('[data-test=steps] > li')[3].text()).toContain('CODEOWNERS')
    const revoked = w.findAll('[data-test=lifecycle] .lifecycle__row')[2].text()
    expect(revoked).toContain('after the next mcp-hub release')
  })

  it('points to the Claude Code skills near the top', () => {
    const callout = mount(PublishView).get('.publish__intro [data-test=ask-claude]')
    expect(callout.text()).toContain('Claude Code')
    expect(callout.text()).toContain('mcp-hub-build-server')
    expect(callout.text()).toContain('mcp-hub-publish-server')
  })

  it('gives the Copy button an accessible name', () => {
    expect(mount(PublishView).get('[data-test=copy]').attributes('aria-label')).toBe('Copy example catalogue entry')
  })

  it('selects the example for manual copy when copying fails', async () => {
    vi.stubGlobal('navigator', {})
    const w = mount(PublishView, { attachTo: document.body })
    await w.get('[data-test=copy]').trigger('click')
    await flushPromises()
    expect(window.getSelection()?.toString()).toContain('id: my-service')
    w.unmount()
  })

  it('compares local and remote servers', () => {
    const rows = mount(PublishView).findAll('[data-test=kinds] tbody tr')
    expect(rows.map((r) => r.get('th').text())).toEqual(['Local', 'Remote'])
  })

  it('shows the lifecycle with the UI labels', () => {
    const text = mount(PublishView).get('[data-test=lifecycle]').text()
    expect(text).toContain('Pending security review')
    expect(text).toContain('Revoked by platform team')
  })

  it('opens external links safely in a new tab', () => {
    const w = mount(PublishView)
    for (const [test, href] of [
      ['guide-link', GUIDE_URL],
      ['template-link', TEMPLATE_URL],
    ]) {
      const a = w.get(`[data-test=${test}]`)
      expect(a.attributes('href')).toBe(href)
      expect(a.attributes('target')).toBe('_blank')
      expect(a.attributes('rel')).toBe('noopener noreferrer')
    }
    expect(GUIDE_URL).toBe('https://github.com/Arif-Karim/mcp-hub/blob/main/docs/publishing.md')
    expect(TEMPLATE_URL).toBe('https://github.com/Arif-Karim/mcp-hub/tree/main/examples/mcp-server-template')
  })

  it('shows the example entry and copies it to the clipboard', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const w = mount(PublishView)
    expect(w.get('[data-test=example]').text()).toBe(exampleEntry.trim())
    expect(exampleEntry).toContain('approval: pending')
    const live = w.get('[data-test=copy-status]')
    expect(live.attributes('aria-live')).toBe('polite')
    expect(live.text()).toBe('')
    await w.get('[data-test=copy]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(exampleEntry)
    expect(live.text()).toBe('Copied to clipboard')
  })

  it('asks the user to copy manually when the clipboard is unavailable', async () => {
    vi.stubGlobal('navigator', {})
    const w = mount(PublishView)
    await w.get('[data-test=copy]').trigger('click')
    await flushPromises()
    expect(w.get('[data-test=copy-status]').text()).toBe("Couldn't copy — select the text and copy it manually")
  })

  it('asks the user to copy manually when the clipboard rejects', async () => {
    vi.stubGlobal('navigator', { clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } })
    const w = mount(PublishView)
    await w.get('[data-test=copy]').trigger('click')
    await flushPromises()
    expect(w.get('[data-test=copy-status]').text()).toBe("Couldn't copy — select the text and copy it manually")
  })
})
