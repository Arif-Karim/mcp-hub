const LOOPBACK = new Set(['localhost', '127.0.0.1', '[::1]'])

/**
 * Returns the URL only if it is safe to render as a link: https://, or http:// to loopback.
 * Catalogue data is publisher-supplied, so anything else (javascript:, plain http, junk) is dropped.
 */
export function safeDocsUrl(raw: string | undefined): string | undefined {
  if (!raw) return undefined
  let u: URL
  try {
    u = new URL(raw)
  } catch {
    return undefined
  }
  if (u.protocol === 'https:') return raw
  if (u.protocol === 'http:' && LOOPBACK.has(u.hostname)) return raw
  return undefined
}
