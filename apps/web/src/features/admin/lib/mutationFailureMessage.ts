import { getErrorDetail } from '@/features/household/hooks/errorStatus'

/**
 * mutationFailureMessage
 *
 * Issue #1496 (UI half): the DLQ retry action used to be invoked with `onSuccess` only, so a
 * rejected replay produced no operator-visible signal at all. Retry now reports failures
 * through a `toast.error`, which is the convention already established for admin mutations by
 * SubscriptionOverrideModal.
 *
 * The identical defect was live on the DLQ surface's other four mutations — per-row delete and
 * bulk delete in DeadLetterPage.tsx, and save-notes and delete in DeadLetterDetailModal.tsx —
 * which is why this helper was renamed from `retryFailureMessage`: it is now the shared
 * error-message extractor for every DLQ mutation, not just retry. All six call sites (2 in
 * DeadLetterPage.tsx, 4 across DeadLetterPage.tsx and DeadLetterDetailModal.tsx) import from
 * here.
 *
 * Issue #1497 item 2 added a SEVENTH call site outside the DLQ surface — the operator-attestation
 * write in PlaidOrphanedItemsPage.tsx, whose reachable rejections are a 409 (the record is already
 * attested, so the first attestation stands and this note was NOT stored) and a 404. The helper is
 * unchanged; it is simply no longer DLQ-only, which is worth saying here because the paragraph above
 * enumerates call sites and an enumeration that quietly goes stale is the exact rot this file's own
 * ticket was about.
 *
 * The reachable rejections all carry a Huma `ErrorModel` body whose `detail` is the only text
 * that tells the operator what actually happened (e.g. retry's 409 dedup / 422 unknown-kind;
 * delete's and save-notes' underlying repository failures). `error.message` for those is
 * axios's useless "Request failed with status code 409", so the server-supplied `detail` is
 * preferred and the transport message is only the fallback (network failures, timeouts — cases
 * with no response body at all).
 *
 * `getErrorDetail` is the existing shared extractor for that body shape; reused rather than
 * reimplemented.
 */
export function mutationFailureMessage(error: unknown): string {
  const detail = getErrorDetail(error)
  if (detail) return detail

  if (error instanceof Error && error.message) return error.message

  return 'unknown error'
}
