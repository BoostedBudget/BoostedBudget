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
} from '@/api/generated/endpoints/accounts/accounts'
import { useUpdateUserAccount as useUpdateUserAccountMutation } from '@/api/generated/endpoints/user-accounts/user-accounts'
import type {
  Account,
  UpdateUserAccountRequest as GeneratedUpdateUserAccountRequest,
} from '@/api/generated/models'

// Local type for partial updates (allows undefined values from forms)
interface PartialUpdateAccountRequest {
  nickname?: string | null
  custom_color?: string | null
  custom_icon?: string | null
  custom_emoji?: string | null
  display_order?: number | null
  is_hidden?: boolean | null
}

interface AccountOrder {
  account_id: string
  display_order: number
}

export interface UseAccountCustomizationResult {
  // Update account - uses UserAccount if available for stable storage
  updateAccount: (account: Account, changes: PartialUpdateAccountRequest) => Promise<Account | null>
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
 *     // Pass the full account object - hook will use user_account_id if available
 *     const result = await updateAccount(account, { nickname })
 *     if (result) {
 *       console.log('Account updated:', result.nickname)
 *     }
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
  ): Promise<Account | null> {
    try {
      // Normalize request data (convert undefined to null)
      const requestData: GeneratedUpdateUserAccountRequest = {
        nickname: changes.nickname ?? null,
        custom_color: changes.custom_color ?? null,
        custom_icon: changes.custom_icon ?? null,
        custom_emoji: changes.custom_emoji ?? null,
        display_order: changes.display_order ?? null,
        is_hidden: changes.is_hidden ?? null,
      }

      // If account has a stable UserAccount reference, use that endpoint
      // This ensures customizations survive Plaid re-links (Issue #171)
      if (account.user_account_id) {
        await updateUserAccountMutation.mutateAsync({
          id: account.user_account_id,
          data: requestData,
        })
        // Return updated account with changed values (others preserved)
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
      const response = await updateAccountMutation.mutateAsync({
        id: account.id,
        data: requestData,
      })
      return response.account
    } catch {
      return null
    }
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
