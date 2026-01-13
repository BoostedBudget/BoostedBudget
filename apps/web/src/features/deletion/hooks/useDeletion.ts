/**
 * useDeletion Hook
 *
 * React hook for managing account deletion and cancellation.
 * Uses generated React Query hooks from OpenAPI spec.
 *
 * Issue #18: Account Deletion Flow (GDPR Article 17)
 */

import { useMutation } from '@tanstack/react-query'
import {
  useRequestDeletion,
  cancelDeletion,
} from '@boostedbudget/api-client/endpoints/user/user'
import type {
  RequestDeletionResponseBody,
  CancelDeletionResponseBody,
} from '@boostedbudget/api-client/models'

export interface UseDeletionResult {
  // Request deletion
  requestAccountDeletion: () => Promise<RequestDeletionResponseBody | null>
  isRequesting: boolean
  requestError: Error | null

  // Cancel deletion
  cancelAccountDeletion: (token: string) => Promise<CancelDeletionResponseBody | null>
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
  // Use generated mutation for requesting deletion
  const requestMutation = useRequestDeletion()

  // Create a mutation for canceling deletion (wrapping the generated GET function)
  const cancelMutation = useMutation({
    mutationFn: (token: string) => cancelDeletion(token),
  })

  async function requestAccountDeletion(): Promise<RequestDeletionResponseBody | null> {
    try {
      const response = await requestMutation.mutateAsync(undefined)
      return response
    } catch {
      return null
    }
  }

  async function cancelAccountDeletion(token: string): Promise<CancelDeletionResponseBody | null> {
    try {
      const response = await cancelMutation.mutateAsync(token)
      return response
    } catch {
      return null
    }
  }

  // Convert errors to Error instances
  const requestError = requestMutation.error
    ? (requestMutation.error instanceof Error
        ? requestMutation.error
        : new Error('Failed to request account deletion'))
    : null

  const cancelError = cancelMutation.error
    ? (cancelMutation.error instanceof Error
        ? cancelMutation.error
        : new Error('Failed to cancel account deletion'))
    : null

  return {
    requestAccountDeletion,
    isRequesting: requestMutation.isPending,
    requestError,
    cancelAccountDeletion,
    isCanceling: cancelMutation.isPending,
    cancelError,
  }
}
