/**
 * Shared account utility helpers
 *
 * Consolidates duplicated formatting logic from AccountCard and AccountTable.
 * Each helper accepts a surface/options parameter to reproduce the exact output
 * each consumer previously produced inline.
 */

import type { Account } from './types'

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
