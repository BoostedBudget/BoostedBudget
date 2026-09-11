/**
 * useAccountCustomization Hook
 *
 * React hook for managing account customization operations.
 * Uses generated React Query hooks from OpenAPI spec.
 *
 * Issue #307: Account Customization
 * Issue #171: Updated to use UserAccount for stable customization storage
 */

import {
  useUpdateAccount as useUpdateAccountMutation,
  useReorderAccounts as useReorderAccountsMutation,
} from '@boostedbudget/api-client/endpoints/accounts/accounts'
import { useUpdateUserAccount as useUpdateUserAccountMutation } from '@boostedbudget/api-client/endpoints/user-accounts/user-accounts'
import type {
  Account,
  UpdateUserAccountRequest as GeneratedUpdateUserAccountRequest,
} from '@boostedbudget/api-client/models'

// Local type for partial updates (allows undefined values from forms)
interface PartialUpdateAccountRequest {
  nickname?: string | null
  custom_color?: string | null
  custom_icon?: string | null
  custom_emoji?: string | null
  display_order?: number | null
  is_hidden?: boolean | null
  // #1609 Item 5 — the per-chain opt-out for a brokerage account Plaid reports on BOTH
  // /transactions/sync and /investments/transactions/get, which otherwise shows every real
  // movement twice. #1618b added the toggle in AccountCustomizationModal, so this now reaches the
  // server from ordinary user action — it is no longer only set by explicit callers.
  // ⚠ NOT "read-layer only and fully reversible", which is what this comment said until #1631 M4.
  // The backend now also stops INGESTING investment transactions for the chain while the flag is
  // set. Rows stored beforehand are retained and reappear when it is cleared, and clearing it
  // triggers a backfill — but that backfill reaches only 2 years (Plaid's limit on
  // /investments/transactions/get), so a chain excluded for longer permanently loses its earliest
  // excluded period. That is why the toggle's copy in AccountCustomizationModal warns about it;
  // do not simplify that copy back to a plain de-duplication promise.
  // ⚠ It is carried ONLY on the user-accounts branch below; the legacy accounts branch strips it,
  // because the column lives on `user_accounts` and UpdateAccountRequest does not declare it.
  exclude_investment_sync?: boolean | null
}

interface AccountOrder {
  account_id: string
  display_order: number
}

export interface UseAccountCustomizationResult {
  // Update account - uses UserAccount if available for stable storage.
  //
  // ⚠ REJECTS on failure (#1609). It used to swallow the error and resolve with `null`, which
  // defeated AccountCustomizationModal's own failure detection: the modal decides whether to
  // close from `Promise.allSettled(...).status === 'rejected'`, so a swallowed failure closed
  // the modal exactly as a success would and the user was told their edit had saved.
  updateAccount: (account: Account, changes: PartialUpdateAccountRequest) => Promise<Account>
  isUpdating: boolean
  updateError: Error | null

  // Reorder accounts
  reorderAccounts: (accountOrders: AccountOrder[]) => Promise<boolean>
  isReordering: boolean
  reorderError: Error | null

  // Clear errors
  clearErrors: () => void
}

/**
 * Hook for account customization operations
 *
 * Issue #171: Customizations are now stored on UserAccount (stable reference)
 * when available. This ensures customizations survive Plaid re-links.
 * Falls back to Account update for backwards compatibility.
 *
 * @returns Object containing update/reorder functions and their states
 *
 * @example
 * ```tsx
 * function AccountSettings({ account }: { account: Account }) {
 *   const { updateAccount, isUpdating, updateError } = useAccountCustomization()
 *
 *   const handleNicknameChange = async (nickname: string) => {
 *     // Pass the full account object - hook will use user_account_id if available.
 *     // #1609: this REJECTS on failure. Let the rejection reach whoever decides what a failed
 *     // save means for the UI; do not swallow it here.
 *     const result = await updateAccount(account, { nickname })
 *     console.log('Account updated:', result.nickname)
 *   }
 *
 *   return (
 *     <form onSubmit={handleSubmit} disabled={isUpdating}>
 *       {updateError && <ErrorMessage error={updateError} />}
 *       ...
 *     </form>
 *   )
 * }
 * ```
 */
export function useAccountCustomization(): UseAccountCustomizationResult {
  // Use generated mutations
  const updateAccountMutation = useUpdateAccountMutation()
  const updateUserAccountMutation = useUpdateUserAccountMutation()
  const reorderMutation = useReorderAccountsMutation()

  /**
   * Update account customization fields
   *
   * Uses UserAccount endpoint if user_account_id is present (Issue #171),
   * otherwise falls back to Account endpoint for backwards compatibility.
   */
  async function updateAccount(
    account: Account,
    changes: PartialUpdateAccountRequest
  ): Promise<Account> {
    // ⚠ NO try/catch (#1609). A rejection must reach the caller: the modal closes only when
    // nothing failed, and turning the rejection into a resolved `null` here made every failed
    // save look like a successful one. The mutation hooks still surface the error through
    // `updateError`, so nothing is lost by letting it propagate.

    // Normalize request data (convert undefined to null)
    const requestData: GeneratedUpdateUserAccountRequest = {
      nickname: changes.nickname ?? null,
      custom_color: changes.custom_color ?? null,
      custom_icon: changes.custom_icon ?? null,
      custom_emoji: changes.custom_emoji ?? null,
      display_order: changes.display_order ?? null,
      is_hidden: changes.is_hidden ?? null,
      // null is the server's "no change" for every field on this request, so an update that says
      // nothing about the flag must send null rather than false — false would UN-exclude a chain
      // the user deliberately excluded, on every unrelated rename.
      exclude_investment_sync: changes.exclude_investment_sync ?? null,
    }

    // If account has a stable UserAccount reference, use that endpoint
    // This ensures customizations survive Plaid re-links (Issue #171)
    if (account.user_account_id) {
      await updateUserAccountMutation.mutateAsync({
        id: account.user_account_id,
        data: requestData,
      })
      // Return updated account with changed values (others preserved).
      //
      // ⚠ This is an OPTIMISTIC guess, not the server's answer, and it cannot be anything else:
      // PATCH /user-accounts/{id} returns a UserAccount, a different shape from Account. The
      // caller is responsible for reconciling it — AccountsPage invalidates the accounts query
      // after writing this into the cache (#1609). Do not treat it as authoritative.
      return {
        ...account,
        nickname: changes.nickname !== undefined ? (changes.nickname ?? undefined) : account.nickname,
        custom_color: changes.custom_color !== undefined ? (changes.custom_color ?? undefined) : account.custom_color,
        custom_icon: changes.custom_icon !== undefined ? (changes.custom_icon ?? undefined) : account.custom_icon,
        custom_emoji: changes.custom_emoji !== undefined ? (changes.custom_emoji ?? undefined) : account.custom_emoji,
        display_order: changes.display_order !== undefined ? (changes.display_order ?? undefined) : account.display_order,
        is_hidden: changes.is_hidden !== undefined ? (changes.is_hidden ?? false) : account.is_hidden,
      }
    }

    // Fallback: Use legacy Account endpoint (for accounts without user_account_id)
    //
    // ⚠ #1618b: exclude_investment_sync MUST be stripped here, and the reason is structural rather
    // than cosmetic. The flag is a column on `user_accounts`; an account with no user_account_id
    // has no chain row to store it in. It is also absent from UpdateAccountRequest — only
    // UpdateUserAccountRequest declares it — so sending it to PATCH /accounts/{id} is either a 422
    // or a silent drop depending on how strictly the decoder rejects unknown fields. Both outcomes
    // are wrong, and the silent one is worse: the modal would close as if it had saved.
    //
    // The modal disables the toggle for these accounts, so this should be unreachable in practice.
    // It is stripped here anyway because "unreachable via the UI" is not the same as unreachable,
    // and this hook is the last place that can tell the difference.
    // Pinned by "must NOT send exclude_investment_sync down the legacy Account endpoint".
    const { exclude_investment_sync: _unsupportedOnLegacyEndpoint, ...legacyRequestData } = requestData
    void _unsupportedOnLegacyEndpoint
    const response = await updateAccountMutation.mutateAsync({
      id: account.id,
      data: legacyRequestData,
    })
    return response.account
  }

  /**
   * Reorder multiple accounts by display_order
   */
  async function reorderAccounts(accountOrders: AccountOrder[]): Promise<boolean> {
    try {
      const response = await reorderMutation.mutateAsync({
        data: {
          account_orders: accountOrders,
        },
      })
      return response.success
    } catch {
      return false
    }
  }

  /**
   * Clear all errors
   */
  function clearErrors(): void {
    updateAccountMutation.reset()
    updateUserAccountMutation.reset()
    reorderMutation.reset()
  }

  // Helper to normalize error to Error instance
  const normalizeError = (err: unknown): Error | null => {
    if (!err) return null
    return err instanceof Error ? err : new Error('Operation failed')
  }

  const updateError = normalizeError(updateAccountMutation.error || updateUserAccountMutation.error)
  const reorderError = normalizeError(reorderMutation.error)

  return {
    updateAccount,
    isUpdating: updateAccountMutation.isPending || updateUserAccountMutation.isPending,
    updateError,
    reorderAccounts,
    isReordering: reorderMutation.isPending,
    reorderError,
    clearErrors,
  }
}
