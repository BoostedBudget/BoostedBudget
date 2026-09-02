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
 * - 'expired':  `expired_at`  (accountRepository.ExpireAccountsByFingerprint)
 *
 * ⚠ THE `expired_at` LEG IS A BUGFIX, not tidiness (#1569 round 6, G-F). This helper used to say
 * "'expired': no dedicated column exists" and fall through to `updated_at`. That comment was true
 * when written and became false with migration 000155, which added `accounts.expired_at` on this
 * same branch — and the fallthrough it justified was user-visible: `updated_at` is bumped with NO
 * status predicate by `accountRepository.Update` (nickname / emoji / colour / hide) and
 * `UpdateDisplayOrders` (drag-reorder), so RENAMING OR REORDERING a closed account moved its
 * rendered "Closed on {date}" forward to today, on the one surface whose stated thesis is that
 * history is retained. Migration 000155's `accounts_expired_at_consistency` CHECK makes the column
 * non-NULL for every 'expired' row, backfill included, so the leg is total rather than
 * best-effort.
 *
 * `updated_at` survives only as a last resort, for a row from a projection that does not carry the
 * dedicated column. All four columns are carried by the two reads whose rows reach this helper
 * (`GetByIDAndUser`, `GetAccountsWithInstitutionFiltered`); `account_repository.go`'s
 * `GetByIDAndUser` comment exists specifically to stop one being dropped from a projection, and
 * `TestAccountRepository_ExpiredAtSurvivesACosmeticWrite_Integration` pins the pair end to end.
 */
export function getAccountClosedDate(account: Account): string | undefined {
  if (!isInactive(account)) return undefined
  return account.removed_at ?? account.replaced_at ?? account.expired_at ?? account.updated_at
}

/**
 * The last balance this app ever recorded for a CLOSED account, and the instant it was true
 * (Issue #1591) — or `undefined` when there is no such pair.
 *
 * ⚠ THIS IS NOT A CURRENT BALANCE, and nothing that answers "what does this account hold" may
 * read it. `balanceMatchesRange` and `compareBalanceValues` read `current_balance` and nothing
 * else, deliberately: a closed account matches no bounded range and sorts into its own group, and
 * routing a dated historical figure into either would undo #1580 through a new door. A test in
 * utils.test.ts pins that separation.
 *
 * WHERE THE NUMBER COMES FROM. `accounts.current_balance` is frozen at the moment an account
 * stopped syncing and carries no as-of instant, so `accountBalanceProjection`
 * (backend/internal/repository/account_liveness_sql.go) sends NULL for it on every terminal
 * account, and that stays. These two fields come from `balance_snapshots` instead — the newest row
 * for the account — which carries `snapshot_at`. That is the whole difference: a figure that can
 * say WHEN it was true is a historical claim, not an orphan.
 *
 * BOTH OR NEITHER, which is the only rule this helper enforces. #1569 T7 deleted a caption that
 * rendered "Last known balance: N/A … as of Jun 4, 2025" — a date with nothing to label. An
 * amount with no date is the same defect pointed the other way, and would be worse: an undated
 * figure beside a closed account reads exactly like a current one. The backend populates the pair
 * from a single snapshot row so a half-pair should be unreachable; this refuses to render one
 * anyway, because "unreachable" is a claim about today's query.
 *
 * ⚠ `0` IS A VALUE, NOT AN ABSENCE. An account emptied before it was closed has a genuine 0.00
 * snapshot on a real date. A truthiness check here would erase it and the erasure would render
 * identically to "we never recorded one", which is a different and false statement. The explicit
 * `== null` comparisons below are the point.
 *
 * Gated on `isInactive` rather than trusting the server's own gating: an ACTIVE account has a
 * current balance, and a second, older figure beside it is the "two balances, one account"
 * confusion the whole ruling exists to prevent.
 */
export function getAccountLastKnownBalance(
  account: Account
): { amount: number; at: string } | undefined {
  if (!isInactive(account)) return undefined
  const { last_known_balance: amount, last_known_balance_at: at } = account
  if (amount == null || at == null) return undefined
  return { amount, at }
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
  return balanceMatchesRange(account.current_balance, range)
}

/**
 * The value-level form of `accountMatchesBalanceRange` (Issue #1569 whole-branch review, I1).
 *
 * Extracted so the net-worth breakdown can reuse the SAME semantics instead of re-deriving them
 * — the two surfaces disagreeing about a balance-less account is the defect this closes. The
 * breakdown ranges on magnitude (a liability's balance is negative there), so it passes the
 * absolute value; the accounts list passes the signed balance. Only the sign convention differs,
 * and it stays at the call site.
 *
 * ⚠ The whole point is the `null` branch. `null < min` and `null > max` are BOTH false, so a
 * bare comparison admits a balance-less row to every range while reading as correct.
 */
export function balanceMatchesRange(
  balance: number | null | undefined,
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
  return compareBalanceValues(a.current_balance, b.current_balance, direction)
}

/**
 * The value-level form of `compareAccountsByBalance` (Issue #1569 whole-branch review, I1).
 *
 * Shared with the net-worth breakdown for the same reason as `balanceMatchesRange`: the
 * "no balance sorts into its own group, last, in both directions" rule must have exactly one
 * definition. Callers decide what the number MEANS (signed on the accounts list, magnitude in
 * the breakdown); this decides what happens when there isn't one.
 */
export function compareBalanceValues(
  balA: number | null | undefined,
  balB: number | null | undefined,
  direction: 'asc' | 'desc'
): number {
  // Same non-equivalence as balanceMatchesRange: the group is "has no balance", which includes
  // an active account that has not synced yet, not only a closed one.
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
