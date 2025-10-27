/**
 * useDeletion Hook
 *
 * React hook for managing account deletion and cancellation.
 * Provides methods to request and cancel account deletion.
 *
 * Issue #18: Account Deletion Flow (GDPR Article 17)
 */

import { useState } from 'react'
import { requestDeletion, cancelDeletion } from '../api/deletionApi'
import type { RequestDeletionResponse, CancelDeletionResponse } from '../types'

export interface UseDeletionResult {
  // Request deletion
  requestAccountDeletion: () => Promise<RequestDeletionResponse | null>
  isRequesting: boolean
  requestError: Error | null

  // Cancel deletion
  cancelAccountDeletion: (token: string) => Promise<CancelDeletionResponse | null>
  isCanceling: boolean
  cancelError: Error | null
}

/**
 * Hook for account deletion operations
 *
 * @returns Object containing deletion request/cancel functions and their states
 *
 * @example
 * ```tsx
 * function DeleteAccountSection() {
 *   const { requestAccountDeletion, isRequesting, requestError } = useDeletion()
 *
 *   const handleDelete = async () => {
 *     const result = await requestAccountDeletion()
 *     if (result) {
 *       console.log('Deletion scheduled:', result.scheduled_at)
 *       console.log('Cancellation token:', result.cancellation_token)
 *     }
 *   }
 *
 *   return (
 *     <button onClick={handleDelete} disabled={isRequesting}>
 *       {isRequesting ? 'Processing...' : 'Delete Account'}
 *     </button>
 *   )
 * }
 * ```
 */
export function useDeletion(): UseDeletionResult {
  // Request deletion state
  const [isRequesting, setIsRequesting] = useState<boolean>(false)
  const [requestError, setRequestError] = useState<Error | null>(null)

  // Cancel deletion state
  const [isCanceling, setIsCanceling] = useState<boolean>(false)
  const [cancelError, setCancelError] = useState<Error | null>(null)

  async function requestAccountDeletion(): Promise<RequestDeletionResponse | null> {
    try {
      setIsRequesting(true)
      setRequestError(null)
      const response = await requestDeletion()
      return response
    } catch (err) {
      const error = err instanceof Error ? err : new Error('Failed to request account deletion')
      setRequestError(error)
      return null
    } finally {
      setIsRequesting(false)
    }
  }

  async function cancelAccountDeletion(token: string): Promise<CancelDeletionResponse | null> {
    try {
      setIsCanceling(true)
      setCancelError(null)
      const response = await cancelDeletion(token)
      return response
    } catch (err) {
      const error =
        err instanceof Error ? err : new Error('Failed to cancel account deletion')
      setCancelError(error)
      return null
    } finally {
      setIsCanceling(false)
    }
  }

  return {
    requestAccountDeletion,
    isRequesting,
    requestError,
    cancelAccountDeletion,
    isCanceling,
    cancelError,
  }
}
