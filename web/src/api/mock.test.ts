import { describe, expect, it } from 'vitest'
import { ApiError } from './client'
import { createMockClient } from './mock'

const fast = () => createMockClient({ latencyMs: 0 })

describe('mockClient', () => {
  it('lists the five sample entries, all disabled', async () => {
    const items = await fast().listCatalogue()
    expect(items.map((i) => i.id)).toEqual([
      'service-directory',
      'deepwiki',
      'context7',
      'jira-rovo',
      'payments-sandbox',
    ])
    expect(items.every((i) => !i.state.enabled && i.state.status === 'disabled')).toBe(true)
    expect(items[0].state.configName).toBe('hub__service-directory')
  })

  it('enabling an approved local entry makes it running with a proxy URL', async () => {
    const c = fast()
    const item = await c.enable('service-directory')
    expect(item.state).toMatchObject({
      enabled: true,
      status: 'running',
      configName: 'hub__service-directory',
      proxyUrl: 'http://127.0.0.1:7420/mcp/service-directory',
    })
    expect(item.state.enabledAt).toBeTruthy()
    expect((await c.status()).enabledCount).toBe(1)
  })

  it('enabling an approved remote entry makes it ready', async () => {
    const item = await fast().enable('deepwiki')
    expect(item.state.status).toBe('ready')
    expect(item.state.enabled).toBe(true)
  })

  it.each(['jira-rovo', 'payments-sandbox'])('refuses to enable %s with 403 not_approved', async (id) => {
    const err = await fast().enable(id).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({ status: 403, code: 'not_approved' })
  })

  it('unknown id is 404 not_found', async () => {
    const err = await fast().enable('nope').catch((e: unknown) => e)
    expect(err).toMatchObject({ status: 404, code: 'not_found' })
  })

  it('disable returns the entry to disabled', async () => {
    const c = fast()
    await c.enable('context7')
    const item = await c.disable('context7')
    expect(item.state).toMatchObject({ enabled: false, status: 'disabled' })
    expect(item.state.proxyUrl).toBeUndefined()
    expect((await c.status()).enabledCount).toBe(0)
  })

  it('filters by q (including tool names) and kind', async () => {
    const c = fast()
    expect((await c.listCatalogue({ q: 'runbook' })).map((i) => i.id)).toEqual(['service-directory'])
    expect((await c.listCatalogue({ q: 'GET-LIBRARY' })).map((i) => i.id)).toEqual(['context7'])
    expect((await c.listCatalogue({ kind: 'local' })).map((i) => i.id)).toEqual([
      'service-directory',
      'payments-sandbox',
    ])
  })

  it('a configured failure fires once, then the enable succeeds', async () => {
    const c = createMockClient({
      latencyMs: 0,
      failures: { 'service-directory': { status: 422, code: 'checksum_mismatch' } },
    })
    const err = await c.enable('service-directory').catch((e: unknown) => e)
    expect(err).toMatchObject({ status: 422, code: 'checksum_mismatch' })
    expect((await c.enable('service-directory')).state.status).toBe('running')
  })

  it('enabling generates synthetic audit events, newest first', async () => {
    const c = fast()
    expect(await c.audit()).toEqual([])
    await c.enable('service-directory')
    const events = await c.audit()
    expect(events.length).toBeGreaterThan(0)
    expect(events.every((e) => e.server === 'service-directory')).toBe(true)
    const times = events.map((e) => Date.parse(e.time))
    expect([...times].sort((a, b) => b - a)).toEqual(times)
  })

  it('returns copies so callers cannot mutate mock state', async () => {
    const c = fast()
    const [first] = await c.listCatalogue()
    first.state.enabled = true
    expect((await c.getItem('service-directory')).state.enabled).toBe(false)
  })

  it('reports the skill as not installed until installSkill() is called', async () => {
    const c = fast()
    expect((await c.status()).skill).toEqual({
      installed: false,
      upToDate: false,
      path: '/Users/dev/.claude/skills/mcp-hub/SKILL.md',
    })
    expect(await c.installSkill()).toMatchObject({ installed: true, upToDate: true })
    expect((await c.status()).skill).toMatchObject({ installed: true, upToDate: true })
  })

  it('can start with an outdated skill', async () => {
    const c = createMockClient({ latencyMs: 0, skill: { installed: true, upToDate: false } })
    expect((await c.status()).skill).toMatchObject({ installed: true, upToDate: false })
  })
})
