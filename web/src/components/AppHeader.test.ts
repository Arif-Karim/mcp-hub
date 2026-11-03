import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import AppHeader from './AppHeader.vue'
import { ApiError } from '../api/client'
import { daemonHealth } from '../lib/status'

const status = {
  version: '0.1.0',
  addr: '127.0.0.1:7420',
  claudeConfigPath: '/tmp/.claude.json',
  enabledCount: 2,
  catalogueSource: 'embedded',
  user: 'dev',
}

describe('daemonHealth', () => {
  it('is unreachable only for network failures', () => {
    expect(daemonHealth(new ApiError(0, 'network', 'Failed to fetch'))).toBe('unreachable')
    expect(daemonHealth(new TypeError('Failed to fetch'))).toBe('unreachable')
  })

  it('is degraded for HTTP errors from a reachable daemon', () => {
    expect(daemonHealth(new ApiError(404, 'not_found', 'no route'))).toBe('degraded')
    expect(daemonHealth(new ApiError(500, 'internal', 'boom'))).toBe('degraded')
  })
})

describe('AppHeader status pill', () => {
  it('shows version, enabled count and user when ok', () => {
    const w = mount(AppHeader, { props: { health: 'ok', status, tab: 'catalogue' } })
    expect(w.get('[data-test=status-pill]').text()).toContain('v0.1.0 · 2 enabled · dev')
  })

  it('shows a red unreachable pill on network failure', () => {
    const w = mount(AppHeader, { props: { health: 'unreachable', tab: 'catalogue' } })
    const pill = w.get('[data-test=status-pill]')
    expect(pill.text()).toBe('Daemon unreachable')
    expect(pill.classes()).toContain('pill--bad')
  })

  it('shows a neutral degraded pill when /status errors', () => {
    const w = mount(AppHeader, { props: { health: 'degraded', tab: 'catalogue' } })
    const pill = w.get('[data-test=status-pill]')
    expect(pill.text()).toBe('Status unavailable')
    expect(pill.classes()).not.toContain('pill--bad')
  })
})

describe('AppHeader publish link', () => {
  it('links to /publish and navigates client-side', async () => {
    history.replaceState(null, '', '/')
    const w = mount(AppHeader, { props: { health: 'ok', status, tab: 'catalogue' } })
    const link = w.get('[data-test=publish-cta]')
    expect(link.text()).toBe('Publish your server')
    expect(link.attributes('href')).toBe('/publish')
    expect(link.attributes('aria-current')).toBeUndefined()
    await link.trigger('click')
    expect(location.pathname).toBe('/publish')
    history.replaceState(null, '', '/')
  })

  it('marks the link current on the publish page', () => {
    const w = mount(AppHeader, { props: { health: 'ok', status, tab: 'publish' } })
    expect(w.get('[data-test=publish-cta]').attributes('aria-current')).toBe('page')
  })
})
