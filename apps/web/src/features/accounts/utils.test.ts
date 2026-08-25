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
  getAccountClosedDate,
  getAccountLastKnownBalance,
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

  // ── fix round 1, item 8: the predicate keys on "balance absent", NOT on "account closed" ──
  //
  // Those two are not the same set. An ACTIVE account can also arrive with no `current_balance`:
  // the column is `omitempty` and a freshly-linked account has none until the first sync lands.
  // Such an account is excluded from a bounded range too, and that is deliberate — "between $1
  // and $100" is a claim about a number and there is no number, whichever reason it is missing.
  // It is strictly better than `?? 0`, which asserted the account holds exactly zero, but it IS a
  // behaviour change beyond closed accounts, so it is pinned here rather than left implicit.
  it('excludes an ACTIVE account that has no balance yet, for the same reason', () => {
    const unsynced: Account = { ...baseAccount, status: 'active' }
    expect(accountMatchesBalanceRange(unsynced, { min: 1 })).toBe(false)
    expect(accountMatchesBalanceRange(unsynced, { max: 100 })).toBe(false)
  })

  it('treats an explicit null balance the same as an absent one', () => {
    // The generated model types `current_balance?: number`, but a hand-built payload or a
    // JSON `null` reaches the same code path, and `null < 1` is false while `null > 100` is also
    // false — so a bare comparison would silently ADMIT it to every range.
    const nulled = { ...baseAccount, current_balance: null } as unknown as Account
    expect(accountMatchesBalanceRange(nulled, { min: 1 })).toBe(false)
    expect(accountMatchesBalanceRange(nulled, { max: 100 })).toBe(false)
    expect(accountMatchesBalanceRange(nulled, {})).toBe(true)
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

  // fix round 1, item 8: same non-equivalence on the sort side.
  it('groups an ACTIVE account with no balance yet alongside the closed ones, last', () => {
    const unsynced: Account = { ...baseAccount, id: 'unsynced', status: 'active' }
    expect(sorted([unsynced, high, low], 'asc')).toEqual(['low', 'high', 'unsynced'])
    expect(sorted([unsynced, low, high], 'desc')).toEqual(['high', 'low', 'unsynced'])
  })

  it('treats an explicit null balance as no balance when sorting', () => {
    const nulled = { ...baseAccount, id: 'nulled', current_balance: null } as unknown as Account
    expect(sorted([nulled, high, low], 'asc')).toEqual(['low', 'high', 'nulled'])
  })

  it('does not let a real $0 balance outrank a positive one on a desc sort', () => {
    const zero: Account = { ...baseAccount, id: 'zero', current_balance: 0 }
    expect(sorted([zero, high, closedA], 'desc')).toEqual(['high', 'zero', 'closed-a'])
  })
})

// ---------------------------------------------------------------------------
// getAccountClosedDate  (fix round 1, item 1)
// ---------------------------------------------------------------------------

describe('getAccountClosedDate', () => {
  // T7 removed getAccountBalanceAsOfDate because a closed account's BALANCE is never sent, so
  // the caption it fed ("Last known balance: N/A … as of Jun 4, 2025") labelled nothing. But the
  // DATE in that caption was real and correct, and it was the only place in the app it appeared —
  // removing the caption removed the date with it. On a surface whose thesis is "your history is
  // retained", losing the WHEN is the wrong direction, so the date comes back on its own terms.
  //
  // The precedence mirrors which timestamp the backend actually stamps per status:
  //   'removed'  → removed_at   (MarkRemovedByLinkedItemID / UpdateStatus)
  //   'replaced' → replaced_at  (UpdateStatus)
  //   'expired'  → no dedicated column exists; ExpireAccountsByFingerprint touches only
  //                updated_at, so that is the best available proxy.
  // All three columns are selected by the account reads and carried over the wire — see
  // account_repository.go GetByIDAndUser, whose comment exists precisely to stop one being
  // dropped from a projection.

  it('returns undefined for an active account — it has not closed', () => {
    expect(getAccountClosedDate({ ...baseAccount, status: 'active' })).toBeUndefined()
  })

  it('prefers removed_at for a removed account', () => {
    const account: Account = {
      ...baseAccount,
      status: 'removed',
      removed_at: '2026-03-01T00:00:00Z',
      replaced_at: '2026-02-01T00:00:00Z',
      updated_at: '2026-04-01T00:00:00Z',
    }
    expect(getAccountClosedDate(account)).toBe('2026-03-01T00:00:00Z')
  })

  it('prefers replaced_at for a replaced account', () => {
    const account: Account = {
      ...baseAccount,
      status: 'replaced',
      replaced_at: '2026-02-01T00:00:00Z',
      updated_at: '2026-04-01T00:00:00Z',
    }
    expect(getAccountClosedDate(account)).toBe('2026-02-01T00:00:00Z')
  })

  it('falls back to updated_at for an expired account (no dedicated timestamp exists)', () => {
    const account: Account = {
      ...baseAccount,
      status: 'expired',
      updated_at: '2026-04-01T00:00:00Z',
    }
    expect(getAccountClosedDate(account)).toBe('2026-04-01T00:00:00Z')
  })
})

// ---------------------------------------------------------------------------
// getAccountLastKnownBalance  (Issue #1591)
// ---------------------------------------------------------------------------

describe('getAccountLastKnownBalance', () => {
  // The other half of the closed-account story. `current_balance` is NULL for a terminal account
  // and stays that way; `last_known_balance` + `last_known_balance_at` come from
  // balance_snapshots, carry their own instant, and are therefore a HISTORICAL claim the UI can
  // state truthfully.
  //
  // The helper's whole job is the "both or neither" rule. An amount with no date is the mirror
  // image of the caption T7 deleted (a date with no amount), and neither is renderable.

  it('returns the pair for a closed account that has one', () => {
    const account: Account = {
      ...baseAccount,
      status: 'removed',
      current_balance: undefined,
      last_known_balance: 4321,
      last_known_balance_at: '2026-06-04T00:00:00Z',
    }
    expect(getAccountLastKnownBalance(account)).toEqual({
      amount: 4321,
      at: '2026-06-04T00:00:00Z',
    })
  })

  it('preserves a genuine recorded ZERO — it is a fact, not an absence', () => {
    // An account emptied before it closed really did hold $0.00 on a real date. A truthy check
    // here would erase it, and the erasure would read as "we never recorded one".
    const account: Account = {
      ...baseAccount,
      status: 'removed',
      current_balance: undefined,
      last_known_balance: 0,
      last_known_balance_at: '2026-06-04T00:00:00Z',
    }
    expect(getAccountLastKnownBalance(account)).toEqual({
      amount: 0,
      at: '2026-06-04T00:00:00Z',
    })
  })

  it('returns undefined for a closed account with no snapshot on record', () => {
    const account: Account = {
      ...baseAccount,
      status: 'removed',
      current_balance: undefined,
    }
    expect(getAccountLastKnownBalance(account)).toBeUndefined()
  })

  it('returns undefined when the amount arrives without its instant', () => {
    const account: Account = {
      ...baseAccount,
      status: 'removed',
      current_balance: undefined,
      last_known_balance: 4321,
    }
    expect(getAccountLastKnownBalance(account)).toBeUndefined()
  })

  it('returns undefined when the instant arrives without its amount', () => {
    const account: Account = {
      ...baseAccount,
      status: 'removed',
      current_balance: undefined,
      last_known_balance_at: '2026-06-04T00:00:00Z',
    }
    expect(getAccountLastKnownBalance(account)).toBeUndefined()
  })

  it('returns undefined for an ACTIVE account even if the fields are populated', () => {
    // The backend only populates these for terminal accounts, but the renderer must not depend on
    // that: an active account HAS a current balance, and showing a second, older figure beside it
    // is the "two balances, one account" confusion the whole ruling exists to prevent.
    const account: Account = {
      ...baseAccount,
      status: 'active',
      current_balance: 100,
      last_known_balance: 4321,
      last_known_balance_at: '2026-06-04T00:00:00Z',
    }
    expect(getAccountLastKnownBalance(account)).toBeUndefined()
  })
})

// ---------------------------------------------------------------------------
// A last-known balance is NOT a current balance  (Issue #1591, regression pin)
// ---------------------------------------------------------------------------

describe('last-known balance stays out of the current-balance predicates', () => {
  // balanceMatchesRange and compareBalanceValues are shared between the Accounts list and the Net
  // Worth breakdown. They answer questions about what an account HOLDS. Letting a dated historical
  // figure satisfy them would make a closed account match "between $1 and $100" again — the exact
  // behaviour #1580 removed, reintroduced through a new door.

  const closedWithHistory: Account = {
    ...baseAccount,
    id: 'closed-with-history',
    status: 'removed',
    current_balance: undefined,
    last_known_balance: 50,
    last_known_balance_at: '2026-06-04T00:00:00Z',
  }

  it('does not match a bounded range that its last-known balance would satisfy', () => {
    expect(accountMatchesBalanceRange(closedWithHistory, { min: 1, max: 100 })).toBe(false)
  })

  it('still matches no range in the other direction either', () => {
    expect(accountMatchesBalanceRange(closedWithHistory, { max: 100 })).toBe(false)
    expect(accountMatchesBalanceRange(closedWithHistory, { min: 1 })).toBe(false)
  })

  it('still sorts into the no-balance group, last, in both directions', () => {
    const funded: Account = { ...baseAccount, id: 'funded', current_balance: 10 }
    expect(compareAccountsByBalance(closedWithHistory, funded, 'asc')).toBeGreaterThan(0)
    expect(compareAccountsByBalance(closedWithHistory, funded, 'desc')).toBeGreaterThan(0)
  })
})
