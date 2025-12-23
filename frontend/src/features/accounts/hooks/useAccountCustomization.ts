/**
 * useAccountCustomization Hook
 *
 * React hook for managing account customization operations.
 * Uses generated React Query hooks from OpenAPI spec.
 *
 * Issue #307: Account Customization
 */

import {
  useUpdateAccount as useUpdateAccountMutation,
  useReorderAccounts as useReorderAccountsMutation,
} from '@/api/generated/endpoints/accounts/accounts'
import type {
  Account,
  UpdateAccountRequest as GeneratedUpdateAccountRequest,
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
  // Update account
  updateAccount: (accountId: string, changes: PartialUpdateAccountRequest) => Promise<Account | null>
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
 * @returns Object containing update/reorder functions and their states
 *
 * @example
 * ```tsx
 * function AccountSettings() {
 *   const { updateAccount, isUpdating, updateError } = useAccountCustomization()
 *
 *   const handleNicknameChange = async (accountId: string, nickname: string) => {
 *     const result = await updateAccount(accountId, { nickname })
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
  const updateMutation = useUpdateAccountMutation()
  const reorderMutation = useReorderAccountsMutation()

  /**
   * Update a single account's customization fields
   */
  async function updateAccount(
    accountId: string,
    changes: PartialUpdateAccountRequest
  ): Promise<Account | null> {
    try {
      // Convert undefined to null for API compatibility
      const requestData: GeneratedUpdateAccountRequest = {
        nickname: changes.nickname ?? null,
        custom_color: changes.custom_color ?? null,
        custom_icon: changes.custom_icon ?? null,
        custom_emoji: changes.custom_emoji ?? null,
        display_order: changes.display_order ?? null,
        is_hidden: changes.is_hidden ?? null,
      }
      const response = await updateMutation.mutateAsync({
        id: accountId,
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
    updateMutation.reset()
    reorderMutation.reset()
  }

  // Convert errors to Error instances
  const updateError = updateMutation.error
    ? (updateMutation.error instanceof Error
        ? updateMutation.error
        : new Error('Failed to update account'))
    : null

  const reorderError = reorderMutation.error
    ? (reorderMutation.error instanceof Error
        ? reorderMutation.error
        : new Error('Failed to reorder accounts'))
    : null

  return {
    updateAccount,
    isUpdating: updateMutation.isPending,
    updateError,
    reorderAccounts,
    isReordering: reorderMutation.isPending,
    reorderError,
    clearErrors,
  }
}
