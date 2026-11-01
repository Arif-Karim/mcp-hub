import { describe, expect, it } from 'vitest'
import { ApiError } from '../api/client'
import { errorMessage } from './errors'

const err = (code: string, status = 400, message = 'raw server message') =>
  new ApiError(status, code as ApiError['code'], message)

describe('errorMessage', () => {
  it('explains checksum_mismatch', () => {
    expect(errorMessage(err('checksum_mismatch'), 'service-directory')).toBe(
      'Download failed integrity check — install blocked. The publisher has been notified.',
    )
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
    ['not_found', /no longer in the catalogue/i],
    ['network', /daemon/i],
  ])('has a human message for %s', (code, pattern) => {
    const msg = errorMessage(err(code))
    expect(msg).toMatch(pattern)
    expect(msg).not.toBe('raw server message')
  })

  it('falls back to the server message for unmapped codes', () => {
    expect(errorMessage(err('internal', 500, 'boom'))).toBe('Something went wrong: boom')
  })

  it('handles non-ApiError values', () => {
    expect(errorMessage(new Error('kaput'))).toBe('Something went wrong: kaput')
  })
})
