/**
 * Types tests for the discriminated FeedRow union
 * Issue #1299: Investment transactions in unified Transactions feed
 * TDD: written BEFORE implementation
 */

import { describe, it, expect } from 'bun:test'
import type { FeedRow, BankingTransaction, InvestmentActivity, TransactionFilters, ActivityType } from './types'
import { isInvestmentRow } from './types'

// ---- Fixtures ----

const bankingRow: BankingTransaction = {
  id: 'txn-banking-1',
  user_id: 'user-123',
  account_id: 'account-456',
  linked_item_id: 'item-789',
  source: 'banking',
  amount: -25.5,
  date: '2025-10-25',
  name: 'Coffee Shop',
  pending: false,
  created_at: '2025-10-25T10:00:00Z',
  updated_at: '2025-10-25T10:00:00Z',
}

const investmentRow: InvestmentActivity = {
  id: 'inv-1',
  user_id: 'user-123',
  account_id: 'account-inv',
  source: 'investment',
  amount: -1500.0,
  date: '2025-10-25',
  name: 'Buy AAPL',
  ticker_symbol: 'AAPL',
  security_name: 'Apple Inc.',
  activity_type: 'buy',
  investment_quantity: 10,
  investment_price: 150.0,
  investment_fees: 0.0,
  created_at: '2025-10-25T10:00:00Z',
  updated_at: '2025-10-25T10:00:00Z',
}

// ---- isInvestmentRow type guard ----

describe('isInvestmentRow', () => {
  it('returns true for a row with source: investment', () => {
    const row: FeedRow = investmentRow
    expect(isInvestmentRow(row)).toBe(true)
  })

  it('returns false for a row with source: banking', () => {
    const row: FeedRow = bankingRow
    expect(isInvestmentRow(row)).toBe(false)
  })

  it('narrows the type: after positive guard, investment-specific fields are accessible', () => {
    const row: FeedRow = investmentRow
    if (isInvestmentRow(row)) {
      // These access patterns must compile without type errors.
      // If the discriminated union is wrong, TypeScript would error here.
      expect(row.ticker_symbol).toBe('AAPL')
      expect(row.security_name).toBe('Apple Inc.')
      expect(row.activity_type).toBe('buy')
      expect(row.investment_quantity).toBe(10)
      expect(row.investment_price).toBe(150.0)
      expect(row.investment_fees).toBe(0.0)
    } else {
      // Should not reach here for investmentRow
      throw new Error('isInvestmentRow should have returned true')
    }
  })

  it('narrows to BankingTransaction after negative guard', () => {
    const row: FeedRow = bankingRow
    if (!isInvestmentRow(row)) {
      // After negative guard, row is narrowed to BankingTransaction
      expect(row.source).toBe('banking')
      expect(row.id).toBe('txn-banking-1')
    } else {
      throw new Error('isInvestmentRow should have returned false for banking row')
    }
  })
})

// ---- ActivityType union ----

describe('ActivityType', () => {
  it('accepts all valid activity type values', () => {
    const validTypes: ActivityType[] = ['buy', 'sell', 'dividend', 'reinvest', 'fee', 'cash', 'other']
    expect(validTypes).toHaveLength(7)
    const buyType: ActivityType = 'buy'
    const sellType: ActivityType = 'sell'
    expect(buyType).toBe('buy')
    expect(sellType).toBe('sell')
  })
})

// ---- InvestmentActivity optional fields ----

describe('InvestmentActivity', () => {
  it('accepts optional investment_subtype and security_id', () => {
    const rowWithOptionals: InvestmentActivity = {
      ...investmentRow,
      investment_subtype: 'margin',
      security_id: 'sec-abc-123',
    }
    expect(rowWithOptionals.investment_subtype).toBe('margin')
    expect(rowWithOptionals.security_id).toBe('sec-abc-123')
  })

  it('is valid without optional fields', () => {
    // investmentRow fixture above has no investment_subtype or security_id
    expect(investmentRow.investment_subtype).toBeUndefined()
    expect(investmentRow.security_id).toBeUndefined()
  })
})

// ---- TransactionFilters extensions ----

describe('TransactionFilters', () => {
  it('accepts include_investment_activity boolean field', () => {
    const filters: TransactionFilters = {
      include_investment_activity: true,
    }
    expect(filters.include_investment_activity).toBe(true)
  })

  it('accepts activity_types array field', () => {
    const filters: TransactionFilters = {
      activity_types: ['buy', 'sell', 'dividend'],
    }
    expect(filters.activity_types).toEqual(['buy', 'sell', 'dividend'])
  })

  it('accepts both new fields together with existing fields', () => {
    const filters: TransactionFilters = {
      search: 'AAPL',
      include_investment_activity: true,
      activity_types: ['buy', 'dividend'],
      start_date: '2025-01-01',
      end_date: '2025-12-31',
    }
    expect(filters.include_investment_activity).toBe(true)
    expect(filters.activity_types).toHaveLength(2)
  })

  it('is valid without the new investment fields (backward compat)', () => {
    const filters: TransactionFilters = {
      search: 'coffee',
      pending: false,
    }
    // No investment fields — still valid
    expect(filters.include_investment_activity).toBeUndefined()
    expect(filters.activity_types).toBeUndefined()
  })
})

// ---- FeedRow union membership ----

describe('FeedRow', () => {
  it('accepts a BankingTransaction as a FeedRow', () => {
    const row: FeedRow = bankingRow
    expect(row.source).toBe('banking')
  })

  it('accepts an InvestmentActivity as a FeedRow', () => {
    const row: FeedRow = investmentRow
    expect(row.source).toBe('investment')
  })
})
