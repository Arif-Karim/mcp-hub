// TypeScript mirror of the schemas in api/openapi.yaml (the source of truth).

export type Kind = 'local' | 'remote'
export type Approval = 'approved' | 'pending' | 'revoked'
export type Classification = 'public' | 'internal' | 'confidential'
export type AuthMode = 'none' | 'hub-jwt'
export type InstallStatus = 'disabled' | 'running' | 'stopped' | 'ready' | 'error'

export interface Tool {
  name: string
  description: string
}

export interface InstallState {
  enabled: boolean
  status: InstallStatus
  /** Key used in Claude Code `mcpServers`, always `hub__{id}`. */
  configName: string
  proxyUrl?: string
  enabledAt?: string
  lastError?: string
}

export interface CatalogueItem {
  id: string
  name: string
  description: string
  owner: string
  version: string
  kind: Kind
  classification: Classification
  approval: Approval
  auth: AuthMode
  tags: string[]
  tools: Tool[]
  docsUrl?: string
  /** Remote entries only. */
  upstreamUrl?: string
  state: InstallState
}

export interface Health {
  status: 'ok'
  version: string
}

/** The mcp-hub Claude Code skill. Without it Claude can't find or enable servers for the user. */
export interface SkillStatus {
  installed: boolean
  /** Installed copy matches the one embedded in the running daemon. */
  upToDate: boolean
  path: string
}

export interface Status {
  version: string
  addr: string
  claudeConfigPath: string
  enabledCount: number
  catalogueSource: string
  user: string
  /** Absent when the daemon cannot manage the skill (e.g. an older daemon). */
  skill?: SkillStatus
}

/** One proxied MCP request. Tool arguments and results are never recorded. */
export interface AuditEvent {
  time: string
  server: string
  method: string
  tool?: string
  status: number
  durationMs: number
  user: string
}

export const ERROR_CODES = [
  'not_found',
  'not_approved',
  'checksum_mismatch',
  'unsupported_platform',
  'registry_unavailable',
  'start_failed',
  'config_conflict',
  'config_invalid',
  'bad_request',
  'internal',
] as const

export type ErrorCode = (typeof ERROR_CODES)[number]

export interface ErrorBody {
  error: { code: ErrorCode; message: string }
}

export interface CatalogueQuery {
  q?: string
  kind?: Kind
}
