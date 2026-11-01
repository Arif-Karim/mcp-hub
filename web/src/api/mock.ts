// In-memory implementation of ApiClient for `npm run dev:mock` and tests.
// Behaviour follows api/openapi.yaml: approval gate, local → running, remote → ready.
import { ApiError } from './client'
import type { ApiClient, ApiErrorCode } from './client'
import { fixture } from './fixture'
import { matchesQuery } from '../lib/filter'
import type { AuditEvent, CatalogueItem, Status } from './types'

export interface MockFailure {
  status: number
  code: ApiErrorCode
  message?: string
}

export interface MockOptions {
  /** Artificial latency per call so loading states are visible. */
  latencyMs?: number
  /** Entries whose next enable fails once with this error. */
  failures?: Record<string, MockFailure>
  /** Simulate Claude Code making occasional tool calls through enabled servers. */
  liveTraffic?: boolean
}

const USER = 'dev.user@example.test'
const ADDR = '127.0.0.1:7420'

export function createMockClient(opts: MockOptions = {}): ApiClient {
  const latency = opts.latencyMs ?? 400
  const failures = { ...opts.failures }
  const items: CatalogueItem[] = structuredClone(fixture)
  const events: AuditEvent[] = []

  const wait = <T>(fn: () => T): Promise<T> =>
    new Promise((resolve, reject) => {
      const run = () => {
        try {
          resolve(structuredClone(fn()))
        } catch (e) {
          reject(e)
        }
      }
      if (latency > 0) setTimeout(run, latency)
      else queueMicrotask(run)
    })

  const find = (id: string): CatalogueItem => {
    const item = items.find((i) => i.id === id)
    if (!item) throw new ApiError(404, 'not_found', `no catalogue entry "${id}"`)
    return item
  }

  const record = (item: CatalogueItem, method: string, tool?: string, at = Date.now()) => {
    events.unshift({
      time: new Date(at).toISOString(),
      server: item.id,
      method,
      tool,
      status: 200,
      durationMs: 5 + Math.floor(Math.random() * 120),
      user: USER,
    })
    events.splice(500)
  }

  const maybeTraffic = () => {
    if (!opts.liveTraffic) return
    const enabled = items.filter((i) => i.state.enabled)
    if (enabled.length === 0 || Math.random() > 0.5) return
    const item = enabled[Math.floor(Math.random() * enabled.length)]
    const tool = item.tools[Math.floor(Math.random() * item.tools.length)]
    record(item, 'tools/call', tool?.name)
  }

  return {
    status: () =>
      wait<Status>(() => ({
        version: '0.1.0-mock',
        addr: ADDR,
        claudeConfigPath: '/Users/dev/.claude.json',
        enabledCount: items.filter((i) => i.state.enabled).length,
        catalogueSource: 'mock',
        user: USER,
      })),

    listCatalogue: (query = {}) =>
      wait(() => {
        return items.filter((i) => (!query.kind || i.kind === query.kind) && matchesQuery(i, query.q ?? ''))
      }),

    getItem: (id) => wait(() => find(id)),

    enable: (id) =>
      wait(() => {
        const item = find(id)
        if (item.approval !== 'approved') {
          throw new ApiError(403, 'not_approved', `${id} is ${item.approval}`)
        }
        const fail = failures[id]
        if (fail) {
          delete failures[id]
          throw new ApiError(fail.status, fail.code, fail.message ?? `simulated ${fail.code}`)
        }
        if (!item.state.enabled) {
          item.state = {
            enabled: true,
            status: item.kind === 'local' ? 'running' : 'ready',
            configName: `hub__${id}`,
            proxyUrl: `http://${ADDR}/mcp/${id}`,
            enabledAt: new Date().toISOString(),
          }
          const now = Date.now()
          record(item, 'initialize', undefined, now - 2000)
          record(item, 'tools/list', undefined, now - 1000)
          record(item, 'tools/call', item.tools[0]?.name, now)
        }
        return item
      }),

    disable: (id) =>
      wait(() => {
        const item = find(id)
        item.state = { enabled: false, status: 'disabled', configName: `hub__${id}` }
        return item
      }),

    audit: (limit = 50) =>
      wait(() => {
        maybeTraffic()
        return events.slice(0, limit)
      }),
  }
}

/** Failures for demoing error UX: `?demoFail=checksum` or `?demoFail=conflict`. */
export function demoFailuresFromUrl(search: string): Record<string, MockFailure> {
  const mode = new URLSearchParams(search).get('demoFail')
  if (mode === 'checksum') return { 'service-directory': { status: 422, code: 'checksum_mismatch' } }
  if (mode === 'conflict') return { deepwiki: { status: 409, code: 'config_conflict' } }
  return {}
}
