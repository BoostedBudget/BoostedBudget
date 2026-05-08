/**
 * useActiveAccounts Hook
 *
 * Provides filtered views of accounts that exclude replaced entries.
 * When a Plaid bank account is re-linked, the old `accounts` row gets
 * `status='replaced'`. This hook and its helpers centralise that filter
 * so every consumer stays consistent (Issue #1109).
 */

import {
  useGetAccounts,
} from '@boostedbudget/api-client/endpoints/accounts/accounts'
import type { GetAccountsParams } from '@boostedbudget/api-client/models'
import type { Account } from '@boostedbudget/api-client/models'

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
  refetch: () => void
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

  const rawAccounts = (query.data as { accounts?: Account[] } | undefined)?.accounts

  return {
    accounts: rawAccounts !== undefined ? filterActiveAccounts(rawAccounts) : undefined,
    isLoading: query.isLoading,
    isError: query.isError,
    error: query.error as Error | null,
    refetch: query.refetch as () => void,
  }
}
