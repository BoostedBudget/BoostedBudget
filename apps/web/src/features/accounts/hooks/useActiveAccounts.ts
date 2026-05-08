/**
 * useActiveAccounts Hook
 *
 * Provides filtered views of accounts that exclude replaced entries.
 * When a Plaid bank account is re-linked, the old `accounts` row gets
 * `status='replaced'`. This hook and its helpers centralise that filter
 * so every consumer stays consistent (Issue #1109).
 */

import { useEffect } from 'react'
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
 */
export function isReplaced(account: Account): boolean {
  return account.status === 'replaced'
}

/**
 * Pure filter — removes replaced accounts from an array.
 *
 * Safe to call with an empty array or an array of legacy accounts that
 * predate the `status` field (they pass through unchanged).
 */
export function filterActiveAccounts(accounts: Account[]): Account[] {
  return accounts.filter((acc) => !isReplaced(acc))
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
export function useActiveAccounts(opts?: GetAccountsParams): UseActiveAccountsResult {
  const query = useGetAccounts(opts, undefined)
  const trackEvent = useTrackEvent()

  const rawAccounts = (query.data as GetAccountsOutputBody | undefined)?.accounts ?? undefined
  const filteredAccounts = rawAccounts !== undefined ? filterActiveAccounts(rawAccounts) : undefined

  // Fire a PostHog event when replaced accounts are detected so we can confirm
  // the filter is working in production after the Issue #1109 deploy.
  const excludedCount = (rawAccounts?.length ?? 0) - (filteredAccounts?.length ?? 0)
  useEffect(() => {
    if (excludedCount > 0) {
      trackEvent(ANALYTICS_EVENTS.ACCOUNT_REPLACED_FILTERED, { excluded_count: excludedCount })
    }
  }, [excludedCount, trackEvent])

  return {
    accounts: filteredAccounts,
    isLoading: query.isLoading,
    isError: query.isError,
    error: query.error as Error | null,
    refetch: () => query.refetch().then(() => void 0),
  }
}
