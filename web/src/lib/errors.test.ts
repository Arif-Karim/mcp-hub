import { describe, expect, it } from 'vitest'
import { ApiError } from '../api/client'
import { errorMessage } from './errors'

const err = (code: string, status = 400, message = 'raw server message') =>
  new ApiError(status, code as ApiError['code'], message)

describe('errorMessage', () => {
  it('explains checksum_mismatch', () => {
    const msg = errorMessage(err('checksum_mismatch'), 'service-directory')
    expect(msg).toBe("Install blocked — the download didn't match the catalogue's checksum.")
    expect(msg).not.toMatch(/notified/)
  })

  it('names the conflicting hub__ entry for config_conflict', () => {
    expect(errorMessage(err('config_conflict'), 'deepwiki')).toBe(
      "You already have a server called hub__deepwiki in your Claude config that mcp-hub didn't create — not overwritten.",
    )
  })

  it('explains config_invalid', () => {
    expect(errorMessage(err('config_invalid'))).toBe(
      "Your ~/.claude.json couldn't be parsed — mcp-hub won't edit it until it's fixed.",
    )
  })

  it.each([
    ['not_approved', /not approved/i],
    ['registry_unavailable', /registry/i],
    ['unsupported_platform', /platform/i],
    ['start_failed', /start/i],
    ['network', /daemon/i],
  ])('has a human message for %s', (code, pattern) => {
    const msg = errorMessage(err(code))
    expect(msg).toMatch(pattern)
    expect(msg).not.toBe('raw server message')
  })

  it('uses server wording for not_found only in a server context', () => {
    expect(errorMessage(err('not_found', 404), 'deepwiki')).toMatch(/no longer in the catalogue/i)
    const generic = errorMessage(err('not_found', 404))
    expect(generic).not.toMatch(/catalogue/i)
    expect(generic).toMatch(/404/)
  })

  it('treats a bare 404 (no contract body) outside a server context as generic too', () => {
    const msg = errorMessage(err('unknown', 404, 'Not Found'))
    expect(msg).not.toMatch(/catalogue/i)
    expect(msg).toMatch(/404/)
  })

  it('falls back to the server message for unmapped codes', () => {
    expect(errorMessage(err('internal', 500, 'boom'))).toBe('Something went wrong: boom')
  })

  it('handles non-ApiError values', () => {
    expect(errorMessage(new Error('kaput'))).toBe('Something went wrong: kaput')
  })
})
