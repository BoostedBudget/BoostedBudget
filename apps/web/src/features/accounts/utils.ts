/**
 * Shared account utility helpers
 *
 * Consolidates duplicated formatting logic from AccountCard and AccountTable.
 * Each helper accepts a surface/options parameter to reproduce the exact output
 * each consumer previously produced inline.
 */

import { isInactive } from './hooks/useActiveAccounts'
import type { Account } from './types'

/**
 * The instant a retained (non-active) account stopped being live, or `undefined` for an active
 * one (fix round 1 of Issue #1580, item 1).
 *
 * ⚠ This is NOT the old `getAccountBalanceAsOfDate`. That helper existed to date a frozen
 * BALANCE, and it was deleted because `accountBalanceProjection`
 * (backend/internal/repository/account_liveness_sql.go:160-166) is a bare
 * `CASE WHEN status = 'active'` on both balance columns — so there was never a figure to date and
 * the caption rendered "Last known balance: N/A … as of Jun 4, 2025". The DATE in that caption,
 * though, was real, correct, and the only place in the app it appeared: removing the caption took
 * the closure date with it, on the one surface whose entire thesis is that history is retained.
 * This helper brings the date back on its own terms, unattached to any balance claim.
 *
 * Precedence mirrors which timestamp the backend actually stamps for each status:
 * - 'removed':  `removed_at`  (accountRepository.MarkRemovedByLinkedItemID / UpdateStatus)
 * - 'replaced': `replaced_at` (accountRepository.UpdateStatus)
 * - 'expired':  no dedicated column exists — ExpireAccountsByFingerprint touches only
 *   `updated_at`, so that is the best available proxy for when the account stopped being live.
 *
 * All three columns are carried over the wire; `account_repository.go`'s `GetByIDAndUser` comment
 * exists specifically to stop one being dropped from a projection.
 */
export function getAccountClosedDate(account: Account): string | undefined {
  if (!isInactive(account)) return undefined
  return account.removed_at ?? account.replaced_at ?? account.updated_at
}

/**
 * True when `account` satisfies a balance-range predicate (Issue #1580).
 *
 * ⚠ An account with NO current balance matches no bounded range, in either direction.
 *
 * A closed account carries no current balance (user ruling, Issue #1569): every account-shaped
 * read renders `current_balance` through `accountBalanceProjection`
 * (backend/internal/repository/account_liveness_sql.go), which emits NULL — not zero — for any
 * terminal status (`removed` / `replaced` / `expired`). The page previously coalesced that to 0
 * before comparing, which made the defect run BOTH ways: a `min: 1` filter EXCLUDED closed
 * accounts and a `max: 100` filter INCLUDED them, and neither of those answers meant "unknown".
 * Excluding is the honest reading — "between $1 and $100" is a claim about a number, and there
 * is no number here.
 *
 * A range with neither bound is not a predicate, so it filters nothing out.
 */
export function accountMatchesBalanceRange(
  account: Account,
  range: { min?: number; max?: number } | null | undefined
): boolean {
  if (!range) return true
  const { min, max } = range
  if (min === undefined && max === undefined) return true

  // ⚠ This keys on "no balance", not on "account closed", and the two are not the same set: an
  // ACTIVE account has no `current_balance` until its first sync lands. Such an account is
  // excluded too, deliberately — the question "is it between $1 and $100" has no true answer
  // either way. Strictly better than the old `?? 0`, which answered it by asserting the account
  // holds exactly zero, but it is a behaviour change beyond closed accounts, so it is pinned by
  // its own tests rather than left to be discovered.
  const balance = account.current_balance
  if (balance === undefined || balance === null) return false

  if (min !== undefined && balance < min) return false
  if (max !== undefined && balance > max) return false
  return true
}

/**
 * Comparator for the accounts list's "Balance" sort (Issue #1580).
 *
 * Accounts with no current balance (see `accountMatchesBalanceRange` for why that happens) form
 * their own group, always LAST, and that grouping is **direction-independent**: "no balance" is
 * not a quantity, so it can be neither the largest nor the smallest value. The previous `?? 0`
 * placed a closed account at exactly $0 — between the overdrawn accounts and the funded ones —
 * and a descending sort could lift it above an account that really holds money.
 *
 * `direction` is applied INSIDE the comparator rather than by negating its result at the call
 * site, because negating would flip the grouping too.
 */
export function compareAccountsByBalance(
  a: Account,
  b: Account,
  direction: 'asc' | 'desc'
): number {
  const balA = a.current_balance
  const balB = b.current_balance
  // Same non-equivalence as accountMatchesBalanceRange: the group is "has no balance", which
  // includes an active account that has not synced yet, not only a closed one.
  const hasA = balA !== undefined && balA !== null
  const hasB = balB !== undefined && balB !== null

  if (!hasA && !hasB) return 0
  if (!hasA) return 1
  if (!hasB) return -1

  const comparison = balA - balB
  return direction === 'asc' ? comparison : -comparison
}

/**
 * Returns a human-readable account type label.
 *
 * - surface:'card' (default) — full format used by AccountCard:
 *   e.g. "Bank Account - Checking", "Investment - 401k", "Credit Card - Credit card"
 * - surface:'table' — compact format used by AccountTable:
 *   e.g. "Checking", "401(k)", "Credit Card"
 */
export function getAccountTypeLabel(
  type: string,
  subtype?: string | null,
  options: { surface: 'card' | 'table' } = { surface: 'card' }
): string {
  return options.surface === 'table'
    ? _getAccountTypeLabelTable(type, subtype)
    : _getAccountTypeLabelCard(type, subtype)
}

function _getAccountTypeLabelCard(type: string, subtype?: string | null): string {
  const typeMap: Record<string, string> = {
    depository: 'Bank Account',
    credit: 'Credit Card',
    loan: 'Loan',
    investment: 'Investment',
    other: 'Other',
  }

  const baseLabel = typeMap[type] ?? type

  if (!subtype) return baseLabel

  const subtypeCapitalized = subtype.charAt(0).toUpperCase() + subtype.slice(1)
  return `${baseLabel} - ${subtypeCapitalized}`
}

function _getAccountTypeLabelTable(type: string, subtype?: string | null): string {
  if (subtype) {
    switch (subtype.toLowerCase()) {
      case 'checking': return 'Checking'
      case 'savings': return 'Savings'
      case 'credit card': return 'Credit Card'
      case 'money market': return 'Money Market'
      case 'cd': return 'CD'
      case 'paypal': return 'PayPal'
      case 'brokerage': return 'Brokerage'
      case '401k': return '401(k)'
      case 'ira': return 'IRA'
      case 'roth': return 'Roth IRA'
      case 'mortgage': return 'Mortgage'
      case 'auto': return 'Auto Loan'
      case 'student': return 'Student Loan'
      default: return subtype.charAt(0).toUpperCase() + subtype.slice(1)
    }
  }
  switch (type.toLowerCase()) {
    case 'depository': return 'Bank Account'
    case 'credit': return 'Credit'
    case 'investment': return 'Investment'
    case 'loan': return 'Loan'
    case 'other': return 'Other'
    default: return type.charAt(0).toUpperCase() + type.slice(1)
  }
}

/**
 * Formats an account balance as a currency string.
 *
 * - Honors currencyCode (not hardcoded USD)
 * - Returns options.empty ('N/A' or '—') for undefined/null amount
 * - Handles negative amounts correctly
 * - Falls back gracefully on invalid currency codes
 *
 * AccountCard migrates with { empty: 'N/A' } (default)
 * AccountTable migrates with { empty: '—' }
 */
export function formatAccountBalance(
  amount: number | undefined | null,
  currencyCode: string | undefined | null,
  options: { empty: 'N/A' | '—' } = { empty: 'N/A' }
): string {
  if (amount === undefined || amount === null) return options.empty

  // Fall back to USD when currencyCode is absent (account not yet currency-resolved).
  const currency = currencyCode ?? 'USD'

  try {
    const absAmount = Math.abs(amount)
    const formatted = new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency,
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(absAmount)
    // Use Math.abs + manual '-' prefix to match the AccountTable formatting convention;
    // output is identical for en-US (Intl.NumberFormat with Math.abs vs. negative input).
    return amount < 0 ? `-${formatted}` : formatted
  } catch {
    // Fallback if currency code is invalid
    return `${currency} ${amount.toFixed(2)}`
  }
}
