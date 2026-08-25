/**
 * Shared account utility helpers
 *
 * Tests for getAccountTypeLabel and formatAccountBalance.
 * These helpers consolidate duplicated logic from AccountCard and AccountTable.
 */

import { describe, it, expect } from 'bun:test'
import {
  getAccountTypeLabel,
  formatAccountBalance,
  accountMatchesBalanceRange,
  compareAccountsByBalance,
} from './utils'
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
// accountMatchesBalanceRange / compareAccountsByBalance  (Issue #1580)
// ---------------------------------------------------------------------------

describe('accountMatchesBalanceRange', () => {
  // A closed account carries NO current balance (user ruling, #1569): every account-shaped read
  // renders current_balance through accountBalanceProjection, which emits NULL — not zero — for
  // any terminal status. The page used to coalesce that to 0, which made the bug run BOTH ways:
  // a min-$1 filter EXCLUDED closed accounts, a max-$100 filter INCLUDED them, and neither answer
  // meant "unknown". An absent balance matches no bounded range in either direction.

  const closed: Account = { ...baseAccount, status: 'removed' }
  const live = (current_balance: number): Account => ({ ...baseAccount, current_balance })

  it('keeps every account when the range carries no bound at all', () => {
    expect(accountMatchesBalanceRange(closed, {})).toBe(true)
    expect(accountMatchesBalanceRange(live(50), {})).toBe(true)
  })

  it('keeps every account when there is no range', () => {
    expect(accountMatchesBalanceRange(closed, null)).toBe(true)
    expect(accountMatchesBalanceRange(closed, undefined)).toBe(true)
  })

  it('excludes a balance-less account from a min-only range', () => {
    expect(accountMatchesBalanceRange(closed, { min: 1 })).toBe(false)
  })

  it('excludes a balance-less account from a max-only range — the direction the old ?? 0 got wrong', () => {
    // With `?? 0` this returned true: 0 <= 100. A closed account was silently counted as broke.
    expect(accountMatchesBalanceRange(closed, { max: 100 })).toBe(false)
  })

  it('excludes a balance-less account from a range that straddles zero', () => {
    expect(accountMatchesBalanceRange(closed, { min: -10, max: 10 })).toBe(false)
  })

  it('still applies both bounds to an account that has a balance', () => {
    expect(accountMatchesBalanceRange(live(50), { min: 1, max: 100 })).toBe(true)
    expect(accountMatchesBalanceRange(live(0), { min: 1, max: 100 })).toBe(false)
    expect(accountMatchesBalanceRange(live(500), { min: 1, max: 100 })).toBe(false)
    expect(accountMatchesBalanceRange(live(1), { min: 1, max: 100 })).toBe(true)
    expect(accountMatchesBalanceRange(live(100), { min: 1, max: 100 })).toBe(true)
  })
})

describe('compareAccountsByBalance', () => {
  // Sorting by balance groups the balance-less accounts together at the END, and that grouping is
  // DIRECTION-INDEPENDENT: "no balance" is not a quantity, so it can neither be the largest nor
  // the smallest. Reversing the sort must reverse the ordering of the accounts that HAVE a
  // balance without promoting the ones that do not to the top.

  const closedA: Account = { ...baseAccount, id: 'closed-a', status: 'removed' }
  const closedB: Account = { ...baseAccount, id: 'closed-b', status: 'expired' }
  const low: Account = { ...baseAccount, id: 'low', current_balance: -50 }
  const high: Account = { ...baseAccount, id: 'high', current_balance: 900 }

  const sorted = (accounts: Account[], direction: 'asc' | 'desc') =>
    [...accounts]
      .sort((a, b) => compareAccountsByBalance(a, b, direction))
      .map((a) => a.id)

  it('sorts accounts that have a balance ascending', () => {
    expect(sorted([high, low], 'asc')).toEqual(['low', 'high'])
  })

  it('sorts accounts that have a balance descending', () => {
    expect(sorted([low, high], 'desc')).toEqual(['high', 'low'])
  })

  it('puts balance-less accounts last when ascending', () => {
    expect(sorted([closedA, high, low], 'asc')).toEqual(['low', 'high', 'closed-a'])
  })

  it('puts balance-less accounts last when descending too — not first', () => {
    // The bug: with `?? 0` a closed account sorted as if it held exactly $0, so it landed
    // between the negative and positive balances, and a desc sort could put it above a real one.
    expect(sorted([closedA, low, high], 'desc')).toEqual(['high', 'low', 'closed-a'])
  })

  it('treats two balance-less accounts as tied, in both directions', () => {
    expect(compareAccountsByBalance(closedA, closedB, 'asc')).toBe(0)
    expect(compareAccountsByBalance(closedA, closedB, 'desc')).toBe(0)
  })

  it('does not let a real $0 balance outrank a positive one on a desc sort', () => {
    const zero: Account = { ...baseAccount, id: 'zero', current_balance: 0 }
    expect(sorted([zero, high, closedA], 'desc')).toEqual(['high', 'zero', 'closed-a'])
  })
})
