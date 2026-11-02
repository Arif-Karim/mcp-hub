import type { Approval, AuthMode, InstallStatus } from '../api/types'

export const cap = (s: string): string => s.charAt(0).toUpperCase() + s.slice(1)

export const statusLabel: Record<InstallStatus, string> = {
  disabled: 'Not enabled',
  running: 'Running',
  stopped: 'Stopped',
  ready: 'Ready',
  error: 'Error',
}

/** Why the toggle is unavailable; undefined when the entry can be enabled. */
export const blockedReason: Record<Approval, string | undefined> = {
  approved: undefined,
  pending: 'Pending security review',
  revoked: 'Revoked by platform team',
}

export const authExplainer: Record<AuthMode, string> = {
  'hub-jwt': 'mcp-hub injects a short-lived token for you — nothing is stored in your Claude config.',
  none: 'No credentials are sent.',
}
