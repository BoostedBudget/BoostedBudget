/**
 * Shared account utility helpers
 *
 * Tests for getAccountTypeLabel and formatAccountBalance.
 * These helpers consolidate duplicated logic from AccountCard and AccountTable.
 */

import { describe, it, expect } from 'bun:test'
import { getAccountTypeLabel, formatAccountBalance, getAccountBalanceAsOfDate } from './utils'
import type { Account } from './types'

const baseAccount: Account = {
  id: 'acc-1',
  linked_item_id: 'item-1',
  user_id: 'user-1',
  plaid_account_id: 'plaid-1',
  name: 'Test Account',
  type: 'depository',
  currency_code: 'USD',
  is_hidden: false,
  status: 'active',
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

describe('getAccountTypeLabel', () => {
  describe('surface: card (default)', () => {
    // AccountCard outputs: "<TypeLabel> - <Capitalize(subtype)>" when subtype present,
    // or just "<TypeLabel>" when no subtype.

    it('depository/checking → "Bank Account - Checking"', () => {
      expect(getAccountTypeLabel('depository', 'checking')).toBe('Bank Account - Checking')
    })

    it('depository/savings → "Bank Account - Savings"', () => {
      expect(getAccountTypeLabel('depository', 'savings')).toBe('Bank Account - Savings')
    })

    it('credit/credit card → "Credit Card - Credit card"', () => {
      // AccountCard capitalizes only the first char of subtype: "credit card" → "Credit card"
      expect(getAccountTypeLabel('credit', 'credit card')).toBe('Credit Card - Credit card')
    })

    it('loan/mortgage → "Loan - Mortgage"', () => {
      expect(getAccountTypeLabel('loan', 'mortgage')).toBe('Loan - Mortgage')
    })

    it('investment/401k → "Investment - 401k"', () => {
      expect(getAccountTypeLabel('investment', '401k')).toBe('Investment - 401k')
    })

    it('depository (no subtype) → "Bank Account"', () => {
      expect(getAccountTypeLabel('depository')).toBe('Bank Account')
    })

    it('depository with null subtype → "Bank Account"', () => {
      expect(getAccountTypeLabel('depository', null)).toBe('Bank Account')
    })

    it('credit (no subtype) → "Credit Card"', () => {
      expect(getAccountTypeLabel('credit')).toBe('Credit Card')
    })

    it('loan (no subtype) → "Loan"', () => {
      expect(getAccountTypeLabel('loan')).toBe('Loan')
    })

    it('investment (no subtype) → "Investment"', () => {
      expect(getAccountTypeLabel('investment')).toBe('Investment')
    })

    it('other (no subtype) → "Other"', () => {
      expect(getAccountTypeLabel('other')).toBe('Other')
    })

    it('unknown type with subtype → "<type> - <Subtype>"', () => {
      expect(getAccountTypeLabel('crypto', 'bitcoin')).toBe('crypto - Bitcoin')
    })

    it('explicit surface card same as default', () => {
      expect(getAccountTypeLabel('depository', 'checking', { surface: 'card' })).toBe('Bank Account - Checking')
    })
  })

  describe('surface: table', () => {
    // AccountTable outputs compact labels with special subtype mappings.

    it('depository/checking → "Checking"', () => {
      expect(getAccountTypeLabel('depository', 'checking', { surface: 'table' })).toBe('Checking')
    })

    it('depository/savings → "Savings"', () => {
      expect(getAccountTypeLabel('depository', 'savings', { surface: 'table' })).toBe('Savings')
    })

    it('credit/credit card → "Credit Card"', () => {
      expect(getAccountTypeLabel('credit', 'credit card', { surface: 'table' })).toBe('Credit Card')
    })

    it('depository/money market → "Money Market"', () => {
      expect(getAccountTypeLabel('depository', 'money market', { surface: 'table' })).toBe('Money Market')
    })

    it('depository/cd → "CD"', () => {
      expect(getAccountTypeLabel('depository', 'cd', { surface: 'table' })).toBe('CD')
    })

    it('depository/paypal → "PayPal"', () => {
      expect(getAccountTypeLabel('depository', 'paypal', { surface: 'table' })).toBe('PayPal')
    })

    it('investment/brokerage → "Brokerage"', () => {
      expect(getAccountTypeLabel('investment', 'brokerage', { surface: 'table' })).toBe('Brokerage')
    })

    it('investment/401k → "401(k)"', () => {
      expect(getAccountTypeLabel('investment', '401k', { surface: 'table' })).toBe('401(k)')
    })

    it('investment/ira → "IRA"', () => {
      expect(getAccountTypeLabel('investment', 'ira', { surface: 'table' })).toBe('IRA')
    })

    it('investment/roth → "Roth IRA"', () => {
      expect(getAccountTypeLabel('investment', 'roth', { surface: 'table' })).toBe('Roth IRA')
    })

    it('loan/mortgage → "Mortgage"', () => {
      expect(getAccountTypeLabel('loan', 'mortgage', { surface: 'table' })).toBe('Mortgage')
    })

    it('loan/auto → "Auto Loan"', () => {
      expect(getAccountTypeLabel('loan', 'auto', { surface: 'table' })).toBe('Auto Loan')
    })

    it('loan/student → "Student Loan"', () => {
      expect(getAccountTypeLabel('loan', 'student', { surface: 'table' })).toBe('Student Loan')
    })

    it('unknown subtype → capitalize first char', () => {
      expect(getAccountTypeLabel('investment', 'sep ira', { surface: 'table' })).toBe('Sep ira')
    })

    it('depository (no subtype) → "Bank Account"', () => {
      expect(getAccountTypeLabel('depository', undefined, { surface: 'table' })).toBe('Bank Account')
    })

    it('depository with null subtype → "Bank Account"', () => {
      expect(getAccountTypeLabel('depository', null, { surface: 'table' })).toBe('Bank Account')
    })

    it('credit (no subtype) → "Credit"', () => {
      expect(getAccountTypeLabel('credit', undefined, { surface: 'table' })).toBe('Credit')
    })

    it('investment (no subtype) → "Investment"', () => {
      expect(getAccountTypeLabel('investment', undefined, { surface: 'table' })).toBe('Investment')
    })

    it('loan (no subtype) → "Loan"', () => {
      expect(getAccountTypeLabel('loan', undefined, { surface: 'table' })).toBe('Loan')
    })

    it('other (no subtype) → "Other"', () => {
      expect(getAccountTypeLabel('other', undefined, { surface: 'table' })).toBe('Other')
    })

    it('unknown type (no subtype) → capitalize first char', () => {
      expect(getAccountTypeLabel('crypto', undefined, { surface: 'table' })).toBe('Crypto')
    })
  })
})

describe('formatAccountBalance', () => {
  describe('empty: N/A (default, card surface)', () => {
    it('undefined amount → "N/A"', () => {
      expect(formatAccountBalance(undefined, 'USD')).toBe('N/A')
    })

    it('null amount → "N/A"', () => {
      expect(formatAccountBalance(null, 'USD')).toBe('N/A')
    })

    it('USD positive amount → "$2,500.50"', () => {
      expect(formatAccountBalance(2500.5, 'USD')).toBe('$2,500.50')
    })

    it('EUR amount → uses EUR symbol', () => {
      // Intl.NumberFormat en-US with EUR produces "€1,500.75"
      expect(formatAccountBalance(1500.75, 'EUR')).toMatch(/€1,500.75/)
    })

    it('explicit empty: N/A', () => {
      expect(formatAccountBalance(undefined, 'USD', { empty: 'N/A' })).toBe('N/A')
    })

    it('invalid currency code → Intl formats with currency code prefix', () => {
      // Bun's Intl.NumberFormat formats unknown codes as "XYZ 100.00" (non-breaking space)
      // rather than throwing. The fallback path ("XYZ 100.00" with regular space) is only
      // reached in environments where Intl throws on invalid currency codes.
      const result = formatAccountBalance(100, 'XYZ')
      expect(result).toMatch(/XYZ/)
      expect(result).toMatch(/100\.00/)
    })

    it('negative amount formats as negative', () => {
      expect(formatAccountBalance(-500, 'USD')).toBe('-$500.00')
    })
  })

  describe('empty: — (table surface)', () => {
    it('undefined amount → "—"', () => {
      expect(formatAccountBalance(undefined, 'USD', { empty: '—' })).toBe('—')
    })

    it('null amount → "—"', () => {
      expect(formatAccountBalance(null, 'USD', { empty: '—' })).toBe('—')
    })

    it('USD positive amount → "$2,500.50"', () => {
      expect(formatAccountBalance(2500.5, 'USD', { empty: '—' })).toBe('$2,500.50')
    })

    it('negative amount → "-$1,250.75" (table style)', () => {
      // AccountTable uses Math.abs then prepends "-" for negatives
      expect(formatAccountBalance(-1250.75, 'USD', { empty: '—' })).toBe('-$1,250.75')
    })

    it('zero balance → "$0.00"', () => {
      expect(formatAccountBalance(0, 'USD', { empty: '—' })).toBe('$0.00')
    })
  })
})

// ---------------------------------------------------------------------------
// getAccountBalanceAsOfDate
// ---------------------------------------------------------------------------

describe('getAccountBalanceAsOfDate', () => {
  // Issue #1569: a retained (non-active) account's current_balance is frozen at the moment it
  // stopped being active. Callers must render that as "last known balance, as of <date>", never
  // as a live figure. The instant differs by status — 'removed' and 'replaced' each have a
  // dedicated timestamp; 'expired' has none, so updated_at is the best available proxy (see
  // ExpireAccountsByFingerprint in account_repository.go, which sets only updated_at).

  it('returns undefined for an active account (no freeze to report)', () => {
    expect(getAccountBalanceAsOfDate({ ...baseAccount, status: 'active' })).toBeUndefined()
  })

  it('prefers removed_at for a removed account', () => {
    const account: Account = {
      ...baseAccount,
      status: 'removed',
      removed_at: '2026-03-01T00:00:00Z',
      replaced_at: '2026-02-01T00:00:00Z',
      updated_at: '2026-04-01T00:00:00Z',
    }
    expect(getAccountBalanceAsOfDate(account)).toBe('2026-03-01T00:00:00Z')
  })

  it('prefers replaced_at for a replaced account', () => {
    const account: Account = {
      ...baseAccount,
      status: 'replaced',
      replaced_at: '2026-02-01T00:00:00Z',
      updated_at: '2026-04-01T00:00:00Z',
    }
    expect(getAccountBalanceAsOfDate(account)).toBe('2026-02-01T00:00:00Z')
  })

  it('falls back to updated_at for an expired account (no dedicated timestamp)', () => {
    const account: Account = {
      ...baseAccount,
      status: 'expired',
      updated_at: '2026-04-01T00:00:00Z',
    }
    expect(getAccountBalanceAsOfDate(account)).toBe('2026-04-01T00:00:00Z')
  })
})
