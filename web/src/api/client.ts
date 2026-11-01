import { ERROR_CODES } from './types'
import type { AuditEvent, CatalogueItem, CatalogueQuery, ErrorCode, Status } from './types'

/** Contract codes plus two client-side ones: daemon unreachable, and unrecognised codes. */
export type ApiErrorCode = ErrorCode | 'network' | 'unknown'

export class ApiError extends Error {
  readonly status: number
  readonly code: ApiErrorCode

  constructor(status: number, code: ApiErrorCode, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

export interface ApiClient {
  status(): Promise<Status>
  listCatalogue(query?: CatalogueQuery): Promise<CatalogueItem[]>
  getItem(id: string): Promise<CatalogueItem>
  enable(id: string): Promise<CatalogueItem>
  disable(id: string): Promise<CatalogueItem>
  audit(limit?: number): Promise<AuditEvent[]>
}

const BASE = '/api/v1'

function isErrorCode(v: unknown): v is ErrorCode {
  return typeof v === 'string' && (ERROR_CODES as readonly string[]).includes(v)
}

async function toApiError(res: Response): Promise<ApiError> {
  let code: ApiErrorCode = 'unknown'
  let message = res.statusText || `HTTP ${res.status}`
  try {
    const body = (await res.json()) as { error?: { code?: unknown; message?: unknown } }
    if (isErrorCode(body.error?.code)) code = body.error.code
    if (typeof body.error?.message === 'string') message = body.error.message
  } catch {
    // Not a contract error body (e.g. a proxy's HTML page) — keep the defaults.
  }
  return new ApiError(res.status, code, message)
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response
  try {
    res = await fetch(BASE + path, { headers: { Accept: 'application/json' }, ...init })
  } catch (e) {
    throw new ApiError(0, 'network', e instanceof Error ? e.message : 'network error')
  }
  if (!res.ok) throw await toApiError(res)
  return (await res.json()) as T
}

const enc = encodeURIComponent

export const httpClient: ApiClient = {
  status: () => request<Status>('/status'),
  async listCatalogue(query = {}) {
    const params = new URLSearchParams()
    if (query.q) params.set('q', query.q)
    if (query.kind) params.set('kind', query.kind)
    const qs = params.toString()
    const body = await request<{ items: CatalogueItem[] }>(`/catalogue${qs ? `?${qs}` : ''}`)
    return body.items
  },
  getItem: (id) => request<CatalogueItem>(`/catalogue/${enc(id)}`),
  enable: (id) => request<CatalogueItem>(`/servers/${enc(id)}/enable`, { method: 'POST' }),
  disable: (id) => request<CatalogueItem>(`/servers/${enc(id)}/disable`, { method: 'POST' }),
  async audit(limit = 50) {
    const body = await request<{ events: AuditEvent[] }>(`/audit?limit=${limit}`)
    return body.events
  },
}
