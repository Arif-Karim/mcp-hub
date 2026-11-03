import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ServerCard from './ServerCard.vue'
import { fixture } from '../api/fixture'
import type { CatalogueItem } from '../api/types'

const item = (id: string): CatalogueItem => structuredClone(fixture.find((i) => i.id === id)!)

describe('ServerCard', () => {
  it('shows kind, classification and approval badges', () => {
    const w = mount(ServerCard, { props: { item: item('service-directory') } })
    expect(w.get('[data-test=badge-kind]').text()).toBe('Local')
    const cls = w.get('[data-test=badge-classification]')
    expect(cls.text()).toBe('Internal')
    expect(cls.classes()).toContain('badge--internal')
    expect(w.get('[data-test=badge-approval]').text()).toBe('Approved')
    expect(w.text()).toContain('platform-engineering')
  })

  it('gives each classification a distinct style', () => {
    const classes = ['deepwiki', 'service-directory', 'jira-rovo'].map(
      (id) => mount(ServerCard, { props: { item: item(id) } }).get('[data-test=badge-classification]').classes(),
    )
    expect(classes[0]).toContain('badge--public')
    expect(classes[1]).toContain('badge--internal')
    expect(classes[2]).toContain('badge--confidential')
  })

  it('enables the toggle for approved entries with no reason shown', async () => {
    const w = mount(ServerCard, { props: { item: item('deepwiki') } })
    const btn = w.get('[data-test=toggle]')
    expect(btn.text()).toBe('Enable')
    expect(btn.attributes('disabled')).toBeUndefined()
    expect(w.find('[data-test=reason]').exists()).toBe(false)
    await btn.trigger('click')
    expect(w.emitted('toggle')).toHaveLength(1)
    expect(w.emitted('open')).toBeUndefined()
  })

  it.each([
    ['jira-rovo', 'Pending', 'Pending security review'],
    ['payments-sandbox', 'Revoked', 'Revoked by platform team'],
  ])('disables the toggle for %s and explains why', (id, badge, reason) => {
    const w = mount(ServerCard, { props: { item: item(id) } })
    expect(w.get('[data-test=badge-approval]').text()).toBe(badge)
    expect(w.get('[data-test=toggle]').attributes('disabled')).toBeDefined()
    expect(w.get('[data-test=reason]').text()).toBe(reason)
  })

  it('shows Disable and the running status when enabled', () => {
    const i = item('service-directory')
    i.state = { ...i.state, enabled: true, status: 'running' }
    const w = mount(ServerCard, { props: { item: i } })
    expect(w.get('[data-test=toggle]').text()).toBe('Disable')
    expect(w.get('[data-test=status]').text()).toBe('Running')
  })

  it('shows a busy label and an inline error', () => {
    const w = mount(ServerCard, { props: { item: item('deepwiki'), busy: true, error: 'Nope.' } })
    const btn = w.get('[data-test=toggle]')
    expect(btn.text()).toBe('Enabling…')
    expect(btn.attributes('disabled')).toBeDefined()
    expect(btn.attributes('aria-busy')).toBe('true')
    expect(w.get('[role=alert]').text()).toContain('Nope.')
  })

  it('opens details when the title is clicked', async () => {
    const w = mount(ServerCard, { props: { item: item('deepwiki') } })
    await w.get('[data-test=open]').trigger('click')
    expect(w.emitted('open')).toHaveLength(1)
  })

  it('invites teams to publish on pending entries only', () => {
    const hint = mount(ServerCard, { props: { item: item('jira-rovo') } }).get('[data-test=pending-hint]')
    expect(hint.text()).toContain('Another team is building this')
    expect(hint.get('a').attributes('href')).toBe('/publish')
    for (const id of ['deepwiki', 'payments-sandbox']) {
      expect(mount(ServerCard, { props: { item: item(id) } }).find('[data-test=pending-hint]').exists()).toBe(false)
    }
  })
})
