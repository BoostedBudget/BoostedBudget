/**
 * #1631 CX1. The investment surfaces rendered every figure through a dollar-defaulting formatter,
 * so a GBP total read "$52,340.50" and a MIXED total read "$52,340.50" too -- the UI asserting a
 * currency the backend had explicitly REFUSED to assert by serving currency_code: null alongside
 * is_mixed_currency: true.
 */

import { describe, it, expect } from 'bun:test'
import { formatPortfolioAmount, isCurrencyUnnameable } from './portfolioCurrency'

describe('formatPortfolioAmount', () => {
  it('formats a named currency in that currency', () => {
    expect(formatPortfolioAmount(52340.5, { currency_code: 'GBP' })).toContain('£')
    expect(formatPortfolioAmount(52340.5, { currency_code: 'GBP' })).not.toContain('$')
  })

  it('formats a dollar portfolio in dollars', () => {
    expect(formatPortfolioAmount(10, { currency_code: 'USD' })).toContain('$')
  })

  // The behaviour that must NOT change for the existing user base. Every row written before
  // migration 000197 states no currency, and that is UNKNOWN, not a conflict -- the same rule the
  // backend applies everywhere in this task. Dropping the symbol for all of them would be a
  // regression dressed up as a fix.
  it('keeps the dollar rendering when no currency is stated and nothing is mixed', () => {
    expect(formatPortfolioAmount(10, {})).toContain('$')
    expect(formatPortfolioAmount(10, { is_mixed_currency: false })).toContain('$')
  })

  // The case the whole finding turns on: a currency-blind sum is not a quantity of any currency,
  // so no symbol may be attached to it.
  it('renders a MIXED total with no currency symbol at all', () => {
    const out = formatPortfolioAmount(52340.5, { is_mixed_currency: true })
    expect(out).not.toContain('$')
    expect(out).not.toContain('£')
    expect(out).toContain('52,340.50')
  })

  // A mixed flag beats a stated code. It should never happen -- the backend serves a null code
  // with the mixed flag precisely so nothing labels the sum -- but if it ever did, labelling would
  // be the confidently-wrong outcome this whole task exists to remove.
  it('refuses to label a mixed total even if a code arrives alongside the flag', () => {
    expect(formatPortfolioAmount(10, { currency_code: 'USD', is_mixed_currency: true })).not.toContain('$')
  })

  it('falls back rather than throwing on a malformed code', () => {
    expect(() => formatPortfolioAmount(10, { currency_code: 'not-a-code' })).not.toThrow()
  })

  // #1631 I3 — a malformed code is a fourth situation, distinct from "no code stated". Falling
  // back to USD re-commits the original defect in miniature: it labels a figure with a currency
  // nobody established. It gets the SAME unlabelled treatment as the mixed-currency case.
  it.each(['US', 'DOLLARS', ''])(
    'renders an unusable code (%s) unlabelled, not as dollars, and does not throw',
    (code) => {
      let out = ''
      expect(() => {
        out = formatPortfolioAmount(52340.5, { currency_code: code })
      }).not.toThrow()
      expect(out).not.toContain('$')
      expect(out).not.toContain('£')
      expect(out).toContain('52,340.50')
    },
  )

  // The undefined/absent-code path is UNCHANGED: it means "legacy row, before migration 000197",
  // and rendering it as dollars is the documented, deliberate choice for the existing user base.
  it('still renders as dollars when the code is absent entirely (legacy row)', () => {
    expect(formatPortfolioAmount(10, { currency_code: undefined })).toContain('$')
    expect(formatPortfolioAmount(10, {})).toContain('$')
  })

  it('still renders a named currency (GBP) in pounds', () => {
    expect(formatPortfolioAmount(52340.5, { currency_code: 'GBP' })).toContain('£')
  })
})

describe('isCurrencyUnnameable', () => {
  it('is true only for a mixed total', () => {
    expect(isCurrencyUnnameable({ is_mixed_currency: true })).toBe(true)
    expect(isCurrencyUnnameable({ currency_code: 'GBP' })).toBe(false)
    expect(isCurrencyUnnameable({})).toBe(false)
  })
})
