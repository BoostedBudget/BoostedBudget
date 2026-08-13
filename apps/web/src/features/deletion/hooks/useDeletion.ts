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
import { redactUrlPath } from '@/shared/lib/piiScrubbing'

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

  // Convert errors to Error instances.
  //
  // Issue #1514 (fix round 6): both of these are rendered verbatim by consumers
  // (DeleteAccountSection.tsx shows requestError/cancelError.message directly). The
  // account-deletion cancellation token is a bearer CAPABILITY (the backend's
  // CancelDeletion endpoint is public and validates the request by the token alone) —
  // some network/adapter failures embed the request URL in the error message, which would
  // put the token right back into DOM text even though the component never touches a URL
  // itself. `redactUrlPath` is applied here, unconditionally, to whatever message text
  // reaches either error — a general containment rule, not a special case for the cancel
  // path (requestError goes through the same treatment even though today's known capability
  // routes don't overlap with the request-deletion endpoint itself).
  const requestError = requestMutation.error
    ? new Error(
        redactUrlPath(
          requestMutation.error instanceof Error
            ? requestMutation.error.message
            : 'Failed to request account deletion'
        )
      )
    : null

  const cancelError = cancelMutation.error
    ? new Error(
        redactUrlPath(
          cancelMutation.error instanceof Error
            ? cancelMutation.error.message
            : 'Failed to cancel account deletion'
        )
      )
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
