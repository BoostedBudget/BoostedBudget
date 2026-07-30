import { describe, it, expect } from 'bun:test'
import { getSubscriptionPresentation, presentationFromSubscription } from './subscriptionPresentation'

describe('getSubscriptionPresentation', () => {
  describe('comped', () => {
    it('isComped wins outright, even with a raw status of inactive', () => {
      const result = getSubscriptionPresentation({ status: 'inactive', isComped: true })

      expect(result.kind).toBe('comped')
      expect(result.isComped).toBe(true)
    })

    it('grants write access', () => {
      const result = getSubscriptionPresentation({ status: 'inactive', isComped: true })

      expect(result.hasWriteAccess).toBe(true)
    })

    it('carries compEndsAt when the comp has an end date', () => {
      const result = getSubscriptionPresentation({
        status: 'inactive',
        isComped: true,
        compEndsAt: '2026-12-31T00:00:00Z',
      })

      expect(result.compEndsAt).toBe('2026-12-31T00:00:00Z')
    })

    it('omits compEndsAt for a permanent comp', () => {
      const result = getSubscriptionPresentation({ status: 'inactive', isComped: true })

      expect(result.compEndsAt).toBeUndefined()
    })

    it('badges as info/Complimentary', () => {
      const result = getSubscriptionPresentation({ status: 'inactive', isComped: true })

      expect(result.badgeVariant).toBe('info')
      expect(result.badgeLabel).toBe('Complimentary')
    })
  })

  describe('effectiveStatus resolution', () => {
    it('falls back to raw status when effectiveStatus is absent (backend not yet deployed)', () => {
      const result = getSubscriptionPresentation({ status: 'active' })

      expect(result.kind).toBe('active')
    })

    it('prefers effectiveStatus over raw status when both are present', () => {
      const result = getSubscriptionPresentation({ status: 'inactive', effectiveStatus: 'active' })

      expect(result.kind).toBe('active')
    })

    it('falls back to raw status when effectiveStatus is unrecognised, never straight to unknown', () => {
      const result = getSubscriptionPresentation({
        status: 'active',
        effectiveStatus: 'some_future_enum_value',
      })

      expect(result.kind).toBe('active')
      expect(result.isUnknown).toBe(false)
    })
  })

  describe('write access by status', () => {
    it.each(['active', 'trial', 'trialing'] as const)(
      'grants write access for "%s" and never flags it unknown',
      (status) => {
        const result = getSubscriptionPresentation({ status })

        expect(result.hasWriteAccess).toBe(true)
        expect(result.isUnknown).toBe(false)
      },
    )

    it.each(['inactive', 'cancelled', 'past_due'] as const)(
      'denies write access for "%s"',
      (status) => {
        const result = getSubscriptionPresentation({ status })

        expect(result.hasWriteAccess).toBe(false)
      },
    )
  })

  it('falls back to unknown for a genuinely unrecognised raw status', () => {
    const result = getSubscriptionPresentation({ status: 'some_future_enum_value' })

    expect(result.kind).toBe('unknown')
    expect(result.isUnknown).toBe(true)
  })

  it('includes trial days remaining in the badge label when provided', () => {
    const result = getSubscriptionPresentation({ status: 'trial', trialDaysRemaining: 5 })

    expect(result.badgeLabel).toBe('Trial — 5 days remaining')
  })

  // These three cases are deliberately kept together, not "simplified" into one:
  //   - The case immediately below has isComped and compEndsAt already *agreeing* (both say "not
  //     comped"). It cannot tell "correctly trusts isComped" apart from "incorrectly re-derives
  //     comp status from compEndsAt against the client clock" — a regression that only kicks in
  //     for a *past* compEndsAt sails through it undetected.
  //   - The two below it use *conflicting* signals and are what actually give this file power to
  //     catch a clock-derivation regression (e.g.
  //     `isComped = signals.isComped || new Date(compEndsAt) > now`).
  it('does not treat a past compEndsAt as comped — the server is authoritative, never re-derived from the clock', () => {
    const result = getSubscriptionPresentation({
      status: 'inactive',
      isComped: false,
      compEndsAt: '2020-01-01T00:00:00Z',
    })

    expect(result.kind).not.toBe('comped')
    expect(result.isComped).toBe(false)
  })

  it('stays comped despite a past compEndsAt when isComped is true — a stale-looking end date must not override the server', () => {
    const result = getSubscriptionPresentation({
      status: 'inactive',
      isComped: true,
      compEndsAt: '2020-01-01T00:00:00Z',
    })

    expect(result.kind).toBe('comped')
    expect(result.isComped).toBe(true)
  })

  it('does not treat a future compEndsAt as comped when isComped is false — the discriminating case for a clock-derivation regression', () => {
    const future = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString()
    const result = getSubscriptionPresentation({
      status: 'inactive',
      isComped: false,
      compEndsAt: future,
    })

    expect(result.kind).not.toBe('comped')
    expect(result.isComped).toBe(false)
  })

  it('is pure: identical input yields deeply equal output', () => {
    const input = { status: 'trial', trialDaysRemaining: 3 }

    expect(getSubscriptionPresentation(input)).toEqual(getSubscriptionPresentation(input))
  })
})

describe('presentationFromSubscription', () => {
  // GetCurrentSubscriptionOutputBody's required fields, filled with representative values.
  const baseSubscription = {
    subscription_id: 'sub_123',
    status: 'active',
    tier: 'premium',
    billing_interval: 'month',
    cancel_at_period_end: false,
    amount: '0.00',
    currency: 'USD',
    is_comped: true,
  }

  it('maps comp_ends_at to compEndsAt', () => {
    const result = presentationFromSubscription({
      ...baseSubscription,
      comp_ends_at: '2026-08-10T00:00:00Z',
    })

    expect(result.compEndsAt).toBe('2026-08-10T00:00:00Z')
  })

  it('omits compEndsAt when the payload has no comp_ends_at', () => {
    const result = presentationFromSubscription({ ...baseSubscription })

    expect(result.compEndsAt).toBeUndefined()
  })
})
