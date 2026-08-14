/**
 * client.ts tests.
 *
 * Issue #1514 (fix round 6): the dev-mode error logger in this file logged
 * `error.config?.url` verbatim. For the account-deletion cancellation endpoint
 * (`/api/v1/user/delete/cancel/{token}` — a PUBLIC endpoint the backend validates
 * by the token alone, see backend/internal/huma/operations/deletion.go), that
 * token is a bearer capability, not a resource id — logging it verbatim (even in
 * dev-only console output) is a real exposure, not a cosmetic one.
 *
 * This is the first test file in this package (no prior test infrastructure) —
 * added as part of this fix, per the repo owner's approval to include new test
 * infrastructure in this round. `bun test` runs it directly with no bunfig/config
 * needed: this only exercises a pure string-transform function, no module mocking.
 */
import { describe, expect, test } from 'bun:test'
import { redactCapabilityUrlPath } from './client'

describe('redactCapabilityUrlPath', () => {
  const TOKEN = 'eyJhbGciOiJIUzI1NiJ9.SENTINEL_CAPABILITY_TOKEN.abc123'

  test('redacts the account-deletion cancellation token path segment', () => {
    const url = `/api/v1/user/delete/cancel/${TOKEN}`
    const result = redactCapabilityUrlPath(url)
    expect(result).not.toContain(TOKEN)
    expect(result).toBe('/api/v1/user/delete/cancel/[redacted]')
  })

  test('redacts the household-invite token path segment, including trailing /accept', () => {
    const url = `/api/v1/households/invites/${TOKEN}/accept`
    const result = redactCapabilityUrlPath(url)
    expect(result).not.toContain(TOKEN)
    expect(result).toBe('/api/v1/households/invites/[redacted]/accept')
  })

  // Issue #1514 (fix round 7): round 6 only covered the API path shape. The frontend page
  // route the emailed link actually points at (`/cancel-deletion/{token}`, App.tsx) is a
  // separate shape this dev logger's `error.config?.url` would never carry (it only ever
  // logs API request URLs) — but this package is now the single source of truth for the
  // whole pattern list (see client.ts), so it's tested here regardless of whether this
  // particular call site can reach it, to keep the list itself correct.
  test('redacts the frontend cancel-deletion page route', () => {
    const url = `/cancel-deletion/${TOKEN}`
    const result = redactCapabilityUrlPath(url)
    expect(result).not.toContain(TOKEN)
    expect(result).toBe('/cancel-deletion/[redacted]')
  })

  test('redacts the legacy /household/invite/{token} alias route', () => {
    const url = `/household/invite/${TOKEN}`
    const result = redactCapabilityUrlPath(url)
    expect(result).not.toContain(TOKEN)
    expect(result).toBe('/household/invite/[redacted]')
  })

  test('leaves a URL with no known capability route unchanged', () => {
    const url = '/api/v1/accounts/acc_123'
    expect(redactCapabilityUrlPath(url)).toBe(url)
  })

  test('passes through undefined unchanged', () => {
    expect(redactCapabilityUrlPath(undefined)).toBeUndefined()
  })
})
