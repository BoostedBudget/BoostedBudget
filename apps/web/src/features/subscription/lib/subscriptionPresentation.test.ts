import { describe, it, expect } from 'bun:test'
import { getSubscriptionPresentation } from './subscriptionPresentation'

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

    it('suppresses billing nags', () => {
      const result = getSubscriptionPresentation({ status: 'inactive', isComped: true })

      expect(result.suppressBillingNags).toBe(true)
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
      'denies write access for "%s" without suppressing billing nags',
      (status) => {
        const result = getSubscriptionPresentation({ status })

        expect(result.hasWriteAccess).toBe(false)
        expect(result.suppressBillingNags).toBe(false)
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

  it('does not treat a past compEndsAt as comped — the server is authoritative, never re-derived from the clock', () => {
    const result = getSubscriptionPresentation({
      status: 'inactive',
      isComped: false,
      compEndsAt: '2020-01-01T00:00:00Z',
    })

    expect(result.kind).not.toBe('comped')
    expect(result.isComped).toBe(false)
  })

  it('is pure: identical input yields deeply equal output', () => {
    const input = { status: 'trial', trialDaysRemaining: 3 }

    expect(getSubscriptionPresentation(input)).toEqual(getSubscriptionPresentation(input))
  })
})
