import { getErrorDetail } from '@/features/household/hooks/errorStatus'

/**
 * retryFailureMessage
 *
 * Issue #1496 (UI half): the DLQ retry action used to be invoked with `onSuccess` only, so a
 * rejected replay produced no operator-visible signal at all. Both retry surfaces
 * (DeadLetterPage's per-row button and DeadLetterDetailModal's footer button) now report
 * failures through a `toast.error`, which is the convention already established for admin
 * mutations by SubscriptionOverrideModal.
 *
 * The two reachable rejections both carry a Huma `ErrorModel` body whose `detail` is the only
 * text that tells the operator what actually happened:
 *   - 409 — the replay was deduplicated by the kind's uniqueness options, so nothing was enqueued
 *   - 422 — the job kind has no registered uniqueness policy, so the replay was refused
 * `error.message` for those is axios's useless "Request failed with status code 409", so the
 * server-supplied `detail` is preferred and the transport message is only the fallback (network
 * failures, timeouts — cases with no response body at all).
 *
 * `getErrorDetail` is the existing shared extractor for that body shape; reused rather than
 * reimplemented.
 */
export function retryFailureMessage(error: unknown): string {
  const detail = getErrorDetail(error)
  if (detail) return detail

  if (error instanceof Error && error.message) return error.message

  return 'unknown error'
}
