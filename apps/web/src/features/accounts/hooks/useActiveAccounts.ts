/**
 * useActiveAccounts Hook
 *
 * Provides filtered views of accounts that exclude non-active entries.
 * An account's `status` becomes `replaced` (superseded by a re-link),
 * `expired` (90-day fingerprint TTL released the hold), or `removed` (the
 * user deliberately disconnected it) — only `active` accounts are live. This
 * hook and its helpers centralise that filter so every consumer stays
 * consistent (Issue #1109, widened to cover all three non-active statuses in
 * Issue #1569).
 */

import { useEffect } from 'react'
import { useUser } from '@clerk/react-router'
import {
  useGetAccounts,
} from '@boostedbudget/api-client/endpoints/accounts/accounts'
import type { GetAccountsOutputBody, GetAccountsParams } from '@boostedbudget/api-client/models'
import type { Account } from '@boostedbudget/api-client/models'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'

// ---------------------------------------------------------------------------
// Pure helpers (usable inside other hooks that already hold an accounts array)
// ---------------------------------------------------------------------------

/**
 * Returns true when the account has been superseded by a re-link.
 *
 * Accounts with `status === 'replaced'` are soft-deleted after Plaid re-link.
 * Accounts without a `status` field (legacy, created before the field was added)
 * are treated as active for backward compatibility.
 *
 * Distinct from `isInactive`: `isReplaced` carries chain semantics (it means
 * specifically "superseded by a re-link", used when walking `replaced_by_id`
 * chains) and must not be broadened. Use `isInactive` for "is this account
 * live right now" — the question every list/picker/count actually asks.
 */
export function isReplaced(account: Account): boolean {
  return account.status === 'replaced'
}

/**
 * Returns true when the account is NOT live: `replaced` (superseded by a
 * re-link), `expired` (90-day fingerprint TTL released the hold), or
 * `removed`. Only `active` accounts are live — see
 * backend/internal/domain/account.go's AccountStatus enum (Issue #1569).
 *
 * ⚠ `removed` has THREE causes, not one, and this comment used to name only the first
 * ("the user deliberately disconnected it"). Issue #1569 task 20 added the third and the backend
 * enum was updated; this mirror was not, which is how a doc comment becomes a false statement
 * about behaviour. The causes are distinguished by the backend's `accounts.removed_reason`, never
 * by this status:
 *   - `user_disconnect`  the user asked (DisconnectAccount / DisconnectInstitution);
 *   - `provider_absent`  a confirmed-complete Plaid roster stopped returning it (the bank closed it);
 *   - `link_removed`     one of the product's own flows removed the Plaid Item — the orphaned-item
 *                        cleanup sweep, a subscription-cancellation disconnect, or erasure.
 *
 * ⚠ THE THIRD CAUSE MEANS THIS PREDICATE CAN FLIP WITH NO USER ACTION. A background sweep can move
 * an account to `removed`, and everything keyed on isInactive() moves with it: the account leaves
 * the default Accounts list for the Closed toggle, and it drops out of every "now" total built
 * from active accounts (current net worth, dashboard balances). Historical series are unaffected —
 * they are built from balance_snapshots, which are retained. This is the intended behaviour, and
 * the alternative is worse: an account that has stopped syncing would otherwise keep showing a
 * frozen balance as a current one, undated, which is the defect task 20 (A2) exists to fix.
 *
 * Accounts without a `status` field (legacy, created before the field was
 * added) are treated as active for backward compatibility, same as
 * `isReplaced`.
 */
export function isInactive(account: Account): boolean {
  return account.status === 'replaced' || account.status === 'expired' || account.status === 'removed'
}

/**
 * Pure filter — removes non-active accounts (replaced, expired, removed) from
 * an array.
 *
 * Safe to call with an empty array or an array of legacy accounts that
 * predate the `status` field (they pass through unchanged).
 */
export function filterActiveAccounts(accounts: Account[]): Account[] {
  return accounts.filter((acc) => !isInactive(acc))
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

export interface UseActiveAccountsResult {
  /** Active (non-replaced) accounts, or undefined while loading */
  accounts: Account[] | undefined
  isLoading: boolean
  isError: boolean
  error: Error | null
  refetch: () => Promise<void>
}

/**
 * React Query hook that wraps `useGetAccounts` and returns only accounts
 * whose `status` is not `'replaced'`.
 *
 * Preserves the full query result shape (loading, error, refetch) so callers
 * can handle all states uniformly.
 *
 * @param opts - Optional query params forwarded to `useGetAccounts`
 *
 * @example
 * ```tsx
 * function AccountPicker() {
 *   const { accounts, isLoading } = useActiveAccounts()
 *   if (isLoading) return <Spinner />
 *   return <Select options={accounts} />
 * }
 * ```
 */
export interface UseAllAccountsResult {
  /** All accounts regardless of status (active, replaced, expired, removed), or undefined while loading */
  accounts: Account[] | undefined
  isLoading: boolean
  isError: boolean
  error: Error | null
  refetch: () => Promise<void>
}

/**
 * React Query hook that wraps `useGetAccounts` with `status: 'all_statuses'`,
 * returning the full universe of accounts (active, replaced, expired,
 * removed) unfiltered.
 *
 * Use this — not `useActiveAccounts` — for a **display/lookup** path that
 * must resolve a retained (non-active) account's name, e.g. an id→name map
 * for transaction or investment-activity history (Issue #1569, Task 3). The
 * backend deliberately returns a disconnected account's transactions, and a
 * lookup built from `useActiveAccounts` alone renders them as "Unknown
 * account" and offers no way to filter to them.
 *
 * Do NOT use this for a **selection** path — a picker where the user chooses
 * an account to act on (e.g. "transfer to", "contribute to goal from"). A
 * closed account must never be offered there; use `useActiveAccounts` for
 * those.
 *
 * @param opts - Optional query params forwarded to `useGetAccounts`. Callers
 * may override `status` explicitly; it defaults to `'all_statuses'` here.
 */
export function useAllAccounts(opts?: GetAccountsParams): UseAllAccountsResult {
  const query = useGetAccounts({ status: 'all_statuses', ...opts }, undefined)
  const data = query.data as GetAccountsOutputBody | undefined

  return {
    accounts: data?.accounts ?? undefined,
    isLoading: query.isLoading,
    isError: query.isError,
    error: query.error as Error | null,
    refetch: async () => { await query.refetch() },
  }
}

export function useActiveAccounts(opts?: GetAccountsParams): UseActiveAccountsResult {
  const { user, isLoaded: clerkLoaded } = useUser()
  const query = useGetAccounts(opts, undefined)
  const trackEvent = useTrackEvent()

  const data = query.data as GetAccountsOutputBody | undefined
  const rawAccounts = data?.accounts
  const filteredAccounts = rawAccounts != null ? filterActiveAccounts(rawAccounts) : undefined

  // Fire PostHog event when replaced accounts are detected.
  // Gate on clerkLoaded so the event is not emitted under the ':anon' key during
  // Clerk bootstrap before the real user identity is resolved (Issue #1109 W4-D).
  // Key is per-user so that two users sharing a browser each get independent dedup.
  const excludedCount = (rawAccounts?.length ?? 0) - (filteredAccounts?.length ?? 0)
  useEffect(() => {
    if (!clerkLoaded) return
    const sentinelKey = `ph_acct_replaced_filtered:${user?.id ?? 'anon'}`
    if (excludedCount > 0 && !sessionStorage.getItem(sentinelKey)) {
      sessionStorage.setItem(sentinelKey, '1')
      trackEvent(ANALYTICS_EVENTS.ACCOUNT_REPLACED_FILTERED, { excluded_count: excludedCount })
    }
  }, [excludedCount, trackEvent, user?.id, clerkLoaded])

  return {
    accounts: filteredAccounts,
    isLoading: query.isLoading,
    isError: query.isError,
    error: query.error as Error | null,
    refetch: async () => { await query.refetch() },
  }
}
