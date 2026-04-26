import { describe, expect, it } from 'bun:test'
import { formatPFCEnum, formatPFCPrimary, formatPFCDetailed } from './pfcFormat'

describe('formatPFCPrimary', () => {
  it('formats_primary_enum_to_display', () => {
    expect(formatPFCPrimary('FOOD_AND_DRINK')).toBe('Food And Drink')
    expect(formatPFCPrimary('TRANSPORTATION')).toBe('Transportation')
    expect(formatPFCPrimary('RENT_AND_UTILITIES')).toBe('Rent And Utilities')
    expect(formatPFCPrimary('GOVERNMENT_AND_NON_PROFIT')).toBe('Government And Non Profit')
    expect(formatPFCPrimary('LOAN_DISBURSEMENTS')).toBe('Loan Disbursements')
    expect(formatPFCPrimary('OTHER')).toBe('Other')
  })
})

describe('formatPFCDetailed', () => {
  it('formats_detailed_enum_to_breadcrumb_with_known_primary', () => {
    expect(formatPFCDetailed('FOOD_AND_DRINK_GROCERIES')).toBe('Food And Drink › Groceries')
    expect(formatPFCDetailed('FOOD_AND_DRINK_FAST_FOOD')).toBe('Food And Drink › Fast Food')
    expect(formatPFCDetailed('TRANSPORTATION_TAXIS_AND_RIDE_SHARES')).toBe('Transportation › Taxis And Ride Shares')
    expect(formatPFCDetailed('RENT_AND_UTILITIES_INTERNET_AND_CABLE')).toBe('Rent And Utilities › Internet And Cable')
  })

  it('formats_underscore_heavy_detailed_with_curated_primary_match', () => {
    // LOAN_DISBURSEMENTS_AUTO: must match LOAN_DISBURSEMENTS + '_', not LOAN_PAYMENTS partial match
    expect(formatPFCDetailed('LOAN_DISBURSEMENTS_AUTO')).toBe('Loan Disbursements › Auto')
    expect(formatPFCDetailed('LOAN_DISBURSEMENTS_EQUITY_LOAN')).toBe('Loan Disbursements › Equity Loan')
    // LOAN_PAYMENTS should also work correctly
    expect(formatPFCDetailed('LOAN_PAYMENTS_CAR_PAYMENT')).toBe('Loan Payments › Car Payment')
  })

  it('falls_back_for_unknown_enum', () => {
    // Unknown enum: no curated primary matches as P + '_' prefix
    // Falls back to title-case all tokens and join with › between first and rest
    expect(formatPFCDetailed('FOO_BAR_BAZ')).toBe('Foo › Bar Baz')
    expect(formatPFCDetailed('UNKNOWN_CATEGORY_DETAIL')).toBe('Unknown › Category Detail')
  })
})

describe('formatPFCEnum', () => {
  it('formats_primary_when_no_underscore_suffix_matches', () => {
    // Bare primary enums (no detailed suffix) — title-case only, no breadcrumb
    expect(formatPFCEnum('FOOD_AND_DRINK')).toBe('Food And Drink')
    expect(formatPFCEnum('INCOME')).toBe('Income')
    expect(formatPFCEnum('OTHER')).toBe('Other')
  })

  it('formats_detailed_enum_with_breadcrumb', () => {
    expect(formatPFCEnum('FOOD_AND_DRINK_GROCERIES')).toBe('Food And Drink › Groceries')
    expect(formatPFCEnum('LOAN_DISBURSEMENTS_AUTO')).toBe('Loan Disbursements › Auto')
  })
})
