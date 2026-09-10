import { describe, expect, it } from 'bun:test'
import { investmentSubcategoryLabel, titleCaseWords } from './investmentSubcategory'

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

describe('titleCaseWords', () => {
  it('humanises space- and underscore-separated tokens', () => {
    expect(titleCaseWords('long-term capital gain')).toBe('Long-term Capital Gain')
    expect(titleCaseWords('non_resident_tax')).toBe('Non Resident Tax')
    expect(titleCaseWords('TAX WITHHELD')).toBe('Tax Withheld')
  })

  it('collapses repeated separators rather than emitting empty words', () => {
    expect(titleCaseWords('  fund   fee  ')).toBe('Fund Fee')
  })
})
