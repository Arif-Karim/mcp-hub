import { ApiError } from '../api/client'

export type DaemonHealth = 'loading' | 'ok' | 'degraded' | 'unreachable'

/** Unreachable means no response at all; an HTTP error means the daemon is up but /status failed. */
export function daemonHealth(err: unknown): DaemonHealth {
  if (err instanceof ApiError) return err.code === 'network' ? 'unreachable' : 'degraded'
  return err instanceof TypeError ? 'unreachable' : 'degraded'
}
