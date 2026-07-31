/**
 * overrideStatus tests
 * Issue #1413/#1414: Admin UI — stop lying about override state
 *
 * Unit tests for the override-liveness predicate and the override-status label
 * derivation, extracted from UsersPage.tsx so they're covered directly instead of
 * only incidentally through page rendering.
 */

import { describe, it, expect } from 'bun:test'
import { isOverrideActive, getOverrideStatus } from './overrideStatus'
import { KNOWN_TIER_NAMES } from './knownTiers'

describe('isOverrideActive', () => {
  it('is false when there is no override', () => {
    expect(isOverrideActive(null, null)).toBe(false)
    expect(isOverrideActive(undefined, undefined)).toBe(false)
  })

  it('is false for an empty-string override', () => {
    expect(isOverrideActive('', null)).toBe(false)
  })

  it('is true for a permanent override (no expiry)', () => {
    expect(isOverrideActive('premium', null)).toBe(true)
  })

  it('is true when expiry is in the future', () => {
    const now = new Date('2026-07-27T00:00:00Z')
    const future = new Date('2026-08-01T00:00:00Z').toISOString()
    expect(isOverrideActive('premium', future, undefined, now)).toBe(true)
  })

  it('is false when expiry is in the past', () => {
    const now = new Date('2026-07-27T00:00:00Z')
    const past = new Date('2026-01-01T00:00:00Z').toISOString()
    expect(isOverrideActive('premium', past, undefined, now)).toBe(false)
  })

  it('is true at the exact-now tie (backend uses >=, not >)', () => {
    const now = new Date('2026-07-27T12:00:00.000Z')
    expect(isOverrideActive('premium', now.toISOString(), undefined, now)).toBe(true)
  })

  // ─── Issue #1413 (this task) — tier-name validation ────────────────────────────

  it('is true for a known tier name when knownTierNames is supplied', () => {
    expect(isOverrideActive('premium', null, new Set(['premium', 'household_2']))).toBe(true)
  })

  it('is false for a value naming no configured tier when knownTierNames is supplied', () => {
    expect(isOverrideActive('not_a_tier', null, new Set(['premium', 'household_2']))).toBe(false)
  })

  it('accepts a plain array for knownTierNames, not just a Set', () => {
    expect(isOverrideActive('premium', null, ['premium', 'household_2'])).toBe(true)
    expect(isOverrideActive('not_a_tier', null, ['premium', 'household_2'])).toBe(false)
  })

  it('is false for a malformed override even when its expiry is still in the future', () => {
    const now = new Date('2026-07-27T00:00:00Z')
    const future = new Date('2026-08-01T00:00:00Z').toISOString()
    expect(isOverrideActive('not_a_tier', future, new Set(['premium']), now)).toBe(false)
  })

  it('skips tier-name validation when knownTierNames is omitted (nil-safe, matches backend convention)', () => {
    expect(isOverrideActive('not_a_tier', null)).toBe(true)
  })
})

describe('getOverrideStatus', () => {
  it('returns null when there is no override', () => {
    expect(getOverrideStatus(null, null)).toBeNull()
    expect(getOverrideStatus(undefined, undefined)).toBeNull()
  })

  it('labels a permanent override', () => {
    expect(getOverrideStatus('premium', null)).toEqual({ label: 'Permanent', variant: 'permanent' })
  })

  it('labels a future expiry as "Expires in Nd"', () => {
    const result = getOverrideStatus('premium', new Date(Date.now() + 5 * 24 * 60 * 60 * 1000).toISOString())
    expect(result?.variant).toBe('expiring')
    expect(result?.label).toMatch(/^Expires in \d+d$/)
  })

  it('labels a past expiry as "Expired"', () => {
    expect(getOverrideStatus('premium', '2020-01-01T00:00:00Z')).toEqual({ label: 'Expired', variant: 'expired' })
  })

  // Every comparison against NaN is false, so the naive `expiresAt < now` form fell through to the
  // day-arithmetic branch and rendered the literal string "Expires in NaNd" — while styling the row
  // as a live, expiring comp. Found by Codex adversarial review of the round-7 diff (Issue #1413).
  it('reports an unparseable expiry instead of rendering "Expires in NaNd"', () => {
    const result = getOverrideStatus('premium', 'not-a-date')
    expect(result?.label).toBe('Invalid expiry')
    expect(result?.variant).toBe('expired')
    expect(result?.label).not.toMatch(/NaN/)
  })

  // Issue #1413 round 9, Part 2: SubscriptionOverrideModal.tsx used to call `new Date()` once for
  // this helper and again, separately, for isOverrideActive — two independent reads of the clock
  // that could disagree if an expiry lapsed in the gap between them. Accepting `now` (mirroring
  // isOverrideActive's existing parameter) lets a single call site evaluate both against the same
  // instant instead of each reading the clock itself.
  //
  // Both endsAt values below are deliberately placed BETWEEN the real clock and the injected
  // `now` — computed from Date.now() so this can never drift onto the wrong branch as real time
  // passes (a hardcoded literal in this exact file previously did exactly that). If the
  // implementation reads `new Date()` internally instead of the `now` parameter, these assertions
  // fail regardless of what day this suite runs on: that's the point.
  it('accepts an injected `now` so callers can evaluate expiry against a fixed instant', () => {
    const DAY_MS = 24 * 60 * 60 * 1000
    const futureNow = new Date(Date.now() + 400 * DAY_MS)
    const endsAtBeforeFutureNow = new Date(Date.now() + 200 * DAY_MS).toISOString()
    expect(getOverrideStatus('premium', endsAtBeforeFutureNow, futureNow)?.variant).toBe('expired')

    const pastNow = new Date(Date.now() - 400 * DAY_MS)
    const endsAtAfterPastNow = new Date(Date.now() - 200 * DAY_MS).toISOString()
    expect(getOverrideStatus('premium', endsAtAfterPastNow, pastNow)?.variant).toBe('expiring')
  })
})

// The representation axis of the three-axis malformed-override rule: when the data does not let us
// establish a live comp, we must not claim one. An unparseable expiry is exactly that case, and the
// naive comparison form failed OPEN — reporting the override as live — because NaN comparisons are
// always false. These two must also agree with each other: it would be incoherent for
// isOverrideActive to call a row live while getOverrideStatus labels the same row invalid.
describe('malformed expiry fails closed (Issue #1413, Codex round-7 finding)', () => {
  it('isOverrideActive is false for an unparseable expiry, even with a valid known tier', () => {
    expect(isOverrideActive('premium', 'not-a-date', KNOWN_TIER_NAMES)).toBe(false)
  })

  it('isOverrideActive is false for an unparseable expiry when tier validation is skipped', () => {
    expect(isOverrideActive('premium', 'garbage')).toBe(false)
  })

  it('the two helpers agree that an unparseable expiry is not a live comp', () => {
    const endsAt = 'definitely-not-a-timestamp'
    expect(isOverrideActive('premium', endsAt, KNOWN_TIER_NAMES)).toBe(false)
    expect(getOverrideStatus('premium', endsAt)?.variant).not.toBe('expiring')
  })
})
