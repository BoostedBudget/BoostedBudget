/**
 * useAccountCustomization Hook
 *
 * React hook for managing account customization operations.
 * Provides methods to update account fields and reorder accounts.
 *
 * Issue #307: Account Customization
 */

import { useState } from 'react'
import { updateAccount as updateAccountApi, reorderAccounts as reorderAccountsApi } from '../api/plaidApi'
import type { Account, UpdateAccountRequest, AccountOrder } from '../types'

export interface UseAccountCustomizationResult {
  // Update account
  updateAccount: (accountId: string, changes: UpdateAccountRequest) => Promise<Account | null>
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
  // Update account state
  const [isUpdating, setIsUpdating] = useState<boolean>(false)
  const [updateError, setUpdateError] = useState<Error | null>(null)

  // Reorder accounts state
  const [isReordering, setIsReordering] = useState<boolean>(false)
  const [reorderError, setReorderError] = useState<Error | null>(null)

  /**
   * Update a single account's customization fields
   */
  async function updateAccount(
    accountId: string,
    changes: UpdateAccountRequest
  ): Promise<Account | null> {
    try {
      setIsUpdating(true)
      setUpdateError(null)
      const response = await updateAccountApi(accountId, changes)
      return response.account
    } catch (err) {
      const error = err instanceof Error ? err : new Error('Failed to update account')
      setUpdateError(error)
      return null
    } finally {
      setIsUpdating(false)
    }
  }

  /**
   * Reorder multiple accounts by display_order
   */
  async function reorderAccounts(accountOrders: AccountOrder[]): Promise<boolean> {
    try {
      setIsReordering(true)
      setReorderError(null)
      const response = await reorderAccountsApi(accountOrders)
      return response.success
    } catch (err) {
      const error = err instanceof Error ? err : new Error('Failed to reorder accounts')
      setReorderError(error)
      return false
    } finally {
      setIsReordering(false)
    }
  }

  /**
   * Clear all errors
   */
  function clearErrors(): void {
    setUpdateError(null)
    setReorderError(null)
  }

  return {
    updateAccount,
    isUpdating,
    updateError,
    reorderAccounts,
    isReordering,
    reorderError,
    clearErrors,
  }
}
