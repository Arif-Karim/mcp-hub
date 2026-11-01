import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, httpClient } from './client'

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('httpClient', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('parses the contract error body into a typed ApiError', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        jsonResponse(403, { error: { code: 'not_approved', message: 'jira-rovo is pending' } }),
      ),
    )
    const err = await httpClient.enable('jira-rovo').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({ status: 403, code: 'not_approved', message: 'jira-rovo is pending' })
  })

  it('maps an unknown error code to "unknown"', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(jsonResponse(500, { error: { code: 'weird', message: 'x' } })),
    )
    const err = await httpClient.status().catch((e: unknown) => e)
    expect(err).toMatchObject({ status: 500, code: 'unknown', message: 'x' })
  })

  it('handles a non-JSON error body', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('Bad Gateway', { status: 502 })))
    const err = await httpClient.status().catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({ status: 502, code: 'unknown' })
  })

  it('reports a network failure as status 0 / network', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))
    const err = await httpClient.status().catch((e: unknown) => e)
    expect(err).toMatchObject({ status: 0, code: 'network' })
  })

  it('sends q and kind as query params and unwraps items', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { items: [] }))
    vi.stubGlobal('fetch', fetchMock)
    const items = await httpClient.listCatalogue({ q: 'service owner', kind: 'local' })
    expect(items).toEqual([])
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/catalogue?q=service+owner&kind=local')
  })

  it('POSTs enable and disable to the server routes', async () => {
    const fetchMock = vi.fn().mockImplementation(async () => jsonResponse(200, { id: 'deepwiki' }))
    vi.stubGlobal('fetch', fetchMock)
    await httpClient.enable('deepwiki')
    await httpClient.disable('deepwiki')
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/servers/deepwiki/enable')
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: 'POST' })
    expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/servers/deepwiki/disable')
  })

  it('unwraps audit events and passes limit', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { events: [{ server: 'x' }] }))
    vi.stubGlobal('fetch', fetchMock)
    const events = await httpClient.audit(20)
    expect(events).toEqual([{ server: 'x' }])
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/audit?limit=20')
  })
})
