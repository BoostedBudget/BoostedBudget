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

/**
 * Both `requestError`/`cancelError` are rendered verbatim by consumers
 * (DeleteAccountSection.tsx shows `.message` directly). The account-deletion cancellation
 * token is a bearer CAPABILITY (the backend's CancelDeletion endpoint is public and
 * validates the request by the token alone) — some network/adapter failures embed the
 * request URL in the error message, which would put the token right back into DOM text
 * even though the component never touches a URL itself. `redactUrlPath` is applied here,
 * unconditionally, to whatever message text reaches either error — a general containment
 * rule, not a special case for the cancel path.
 *
 * Issue #1514 (fix round 8): this used to construct a bare `new Error(redactedMessage)`,
 * which discarded the original error's `name`, `stack`, and any custom fields (an
 * AxiosError's `.config`/`.response`, for instance) — real diagnostic information an
 * on-call engineer needs, thrown away for the sake of one field. This preserves `name` and
 * `stack` (itself run through `redactUrlPath` too — a raw V8 stack's first line is
 * `${name}: ${message}`, so the unredacted token would otherwise survive there even with
 * `.message` fixed) and attaches the original error as `.cause`, so nothing is lost for
 * anyone who inspects the error object directly — only the rendered `.message` string (and
 * the copied `.stack`'s message line) are sanitized.
 *
 * RESOLVED (fix round 10): attaching the raw, unredacted original error as `.cause` was
 * flagged as a possible bypass, since Sentry's `linkedErrorsIntegration` (a default
 * integration — see `sentry.ts`) walks `.cause` and creates a SEPARATE exception entry
 * for it if this error ever reaches `Sentry.captureException`. Probe-confirmed there is no
 * bypass: `beforeSend` (`sentry.ts`) recursively redacts the ENTIRE `event.exception`
 * object via `redactPii`, which includes every linked-error entry `linkedErrorsIntegration`
 * adds, not just the top-level one — so the `.cause`-carried original message gets the same
 * `redactUrlPath` treatment before it ever leaves the browser. Do not re-litigate this
 * without a fresh probe; this was checked, not assumed.
 */
function sanitizeError(error: unknown, fallbackMessage: string): Error {
  if (!(error instanceof Error)) {
    return new Error(fallbackMessage)
  }
  const sanitized = new Error(redactUrlPath(error.message), { cause: error })
  sanitized.name = error.name
  if (error.stack) {
    sanitized.stack = redactUrlPath(error.stack)
  }
  return sanitized
}

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

  const requestError = requestMutation.error
    ? sanitizeError(requestMutation.error, 'Failed to request account deletion')
    : null

  const cancelError = cancelMutation.error
    ? sanitizeError(cancelMutation.error, 'Failed to cancel account deletion')
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
