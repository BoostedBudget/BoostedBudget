import { describe, expect, it } from 'bun:test'
import {
  investmentSubcategoryLabel,
  securityTypeLabel,
  SECURITY_TYPE_ACRONYM_LABELS,
} from './investmentSubcategory'

describe('investmentSubcategoryLabel', () => {
  it('names the structural reinvestment, whose raw subtype is only "withdrawal"', () => {
    // 155 production rows. Rendering the raw value would read "Reinvest / Withdrawal".
    expect(investmentSubcategoryLabel('reinvest', 'withdrawal')).toBe('Dividend Reinvestment')
  })

  it('names the structural dividend, whose raw subtype is only "deposit"', () => {
    // 211 production rows.
    expect(investmentSubcategoryLabel('dividend', 'deposit')).toBe('Dividend')
  })

  it('keeps the provider subtype wherever it already says something', () => {
    expect(investmentSubcategoryLabel('dividend', 'qualified dividend')).toBe('Qualified Dividend')
    expect(investmentSubcategoryLabel('fee', 'management fee')).toBe('Management Fee')
    expect(investmentSubcategoryLabel('interest', 'interest receivable')).toBe('Interest Receivable')
    expect(investmentSubcategoryLabel('reinvest', 'dividend reinvestment')).toBe('Dividend Reinvestment')
  })

  it('does NOT rewrite a deposit or withdrawal the structural rule did not claim', () => {
    // A genuine cash movement on a brokerage cash-management account: the backend labelled it
    // "cash", so the raw subtype is the honest answer.
    expect(investmentSubcategoryLabel('cash', 'withdrawal')).toBe('Withdrawal')
    expect(investmentSubcategoryLabel('cash', 'deposit')).toBe('Deposit')
  })

  it('returns null when there is no subtype, so the caller can render its own placeholder', () => {
    expect(investmentSubcategoryLabel('cash', null)).toBeNull()
    expect(investmentSubcategoryLabel('cash', undefined)).toBeNull()
    expect(investmentSubcategoryLabel('cash', '   ')).toBeNull()
  })

  it('tolerates a missing activity type', () => {
    expect(investmentSubcategoryLabel(undefined, 'dividend')).toBe('Dividend')
    expect(investmentSubcategoryLabel(null, 'withdrawal')).toBe('Withdrawal')
  })
})

// #1631 C3: securityTypeLabel and SECURITY_TYPE_ACRONYM_LABELS are the shared helper C2's
// HoldingsTable/AllocationDonutChart agreement test depends on, and had no direct coverage.
describe('SECURITY_TYPE_ACRONYM_LABELS', () => {
  it('maps every acronym to its canonical uppercase form, not a naive title-case', () => {
    expect(SECURITY_TYPE_ACRONYM_LABELS.etf).toBe('ETF')
    expect(SECURITY_TYPE_ACRONYM_LABELS.reit).toBe('REIT')
    expect(SECURITY_TYPE_ACRONYM_LABELS.mbs).toBe('MBS')
    expect(SECURITY_TYPE_ACRONYM_LABELS.abs).toBe('ABS')
  })
})

describe('securityTypeLabel', () => {
  it('uppercases a known acronym rather than title-casing it', () => {
    expect(securityTypeLabel('etf', 'Unknown')).toBe('ETF')
    expect(securityTypeLabel('ETF', 'Unknown')).toBe('ETF')
  })

  it('title-cases a multi-word value that is not an acronym', () => {
    expect(securityTypeLabel('mutual fund', 'Unknown')).toBe('Mutual Fund')
  })

  it('title-cases a single unrecognised value rather than dropping it', () => {
    expect(securityTypeLabel('stock', 'Unknown')).toBe('Stock')
    expect(securityTypeLabel('some_new_provider_token', 'Unknown')).toBe('Some New Provider Token')
  })

  it('takes the caller-supplied fallback for null, empty, whitespace, and the literal "unknown"', () => {
    expect(securityTypeLabel(null, 'Unknown')).toBe('Unknown')
    expect(securityTypeLabel(undefined, 'Unknown')).toBe('Unknown')
    expect(securityTypeLabel('', 'Unknown')).toBe('Unknown')
    expect(securityTypeLabel('   ', 'Unknown')).toBe('Unknown')
    expect(securityTypeLabel('unknown', 'Unknown')).toBe('Unknown')
    expect(securityTypeLabel('UNKNOWN', 'Unknown')).toBe('Unknown')
    // Same empty inputs, a different caller-chosen fallback (the donut's aggregation bucket name).
    expect(securityTypeLabel(null, 'Other')).toBe('Other')
    expect(securityTypeLabel('unknown', 'Other')).toBe('Other')
  })
})
