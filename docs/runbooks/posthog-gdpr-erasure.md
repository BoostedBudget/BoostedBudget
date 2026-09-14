# PostHog GDPR Erasure

BoostedBudget's local account erasure and its external PostHog erasure are separate durable states.
The local database transaction creates `posthog_erasure_outbox` before deleting the user. If that
insert fails, the entire local erasure rolls back. If PostHog is unavailable or configuration is
missing, local erasure may commit, but the external obligation remains pending or dead-lettered.
Never tell a user that provider erasure is complete merely because PostHog returned HTTP 202.

The encrypted envelope captures every identity BoostedBudget legitimately records at deletion time:
the canonical `users.clerk_user_id`, all `user_identities.clerk_user_id` rows, and the legacy internal
UUID used by older backend analytics. The application has no production writer that replaces a
Clerk subject in either table; additional identities are inserted as separate rows. Pre-#1650
aliases created directly in PostHog but never recorded by BoostedBudget are inherently undiscoverable.
Do not invent aliases to compensate. If an operator discovers such an alias during verification,
include it in the provider case and record only the case reference outside this database.

## Provider capability and evidence

The worker uses only PostHog's documented private API on the configured regional host:

- [`POST /api/projects/:project_id/persons/bulk_delete/`](https://posthog.com/docs/api/persons#post-api-projects-project_id-persons-bulk_delete) with `distinct_ids`, `delete_events=true`, `delete_recordings=true`, and `keep_person=false`. The personal API key needs `person:write`. A 202 response means queued/accepted, not complete.
- [`GET /api/projects/:project_id/persons/deletion_status/`](https://posthog.com/docs/api/persons#get-api-projects-project_id-persons-deletion_status) exposes asynchronous person deletion status and `delete_verified_at`; it needs `person:read`. Operators use this status when gathering completion evidence.

PostHog documents no public API for deleting an individual Support conversation/ticket/message and
no public API for deleting an individual survey response. Survey responses may be represented as
events, but BoostedBudget does not assume that event deletion proves every survey-owned view is
gone. Neither surface is assumed to cascade from person deletion. Accordingly, every accepted row
enters `manual_pending`; it cannot complete until an operator has verified all five surfaces:
person, events (including survey response events), recordings, survey-response views/exports, and
Support tickets/conversations/messages. Escalate to PostHog Support/Data Privacy for provider-side
deletion when the product UI does not expose a deletion control, and retain the provider ticket ID
in the approved incident/case system—not in the outbox's identity-free error fields.

The official capability references are the [Persons API](https://posthog.com/docs/api/persons),
[Session recordings API](https://posthog.com/docs/api/session-recordings),
[Surveys API](https://posthog.com/docs/api/surveys),
[Support JavaScript API](https://posthog.com/docs/support/javascript-api), and
[PostHog GDPR guidance](https://posthog.com/docs/privacy/gdpr-compliance).

## Operator procedure

1. Alert from Prometheus, not PostHog. Investigate any sustained `manual_pending` or `dead_letter`,
   and the age metric `posthog_erasure_oldest_outstanding_seconds`. Attempt counters have only
   bounded `phase`, `outcome`, and `error_class` labels and contain no identity.
2. Confirm the row's `project_id` and `region`; never use a production key against staging or vice
   versa. Identities remain encrypted and may be decrypted only through an approved, audited
   maintenance session using the deployment encryption key. Never paste them into logs or tickets.
3. For `dead_letter`, correct configuration/provider availability and replay without touching the
   ciphertext: set `state='retrying'`, `next_attempt_at=clock_timestamp()`, clear the lease, and
   increment `version`. Do not reset `attempt_count` or delete the row.
4. After PostHog reports the async person deletion verified, query/search the authorized provider
   tools for every distinct ID and confirm zero person, event/survey-response, recording, survey
   response, Support ticket, conversation, and message results. Request provider-side Support or
   survey cleanup when any remains.
5. Record the first complete zero confirmation with
   `PostHogErasureOutboxRepository.RecordFirstZeroVerification`. Repeat the entire cross-surface
   check at least five minutes later. Only then call `CompleteWithOperatorEvidence`. The repository
   and database constraint reject completion without the separated first confirmation and all five
   evidence timestamps.

Provider request acceptance, a single zero query, a deleted PostHog person, or an operator's
unsupported assertion is insufficient evidence. Preserve completed rows as identity-free audit
history; do not remove retained financial history outside the existing local GDPR cascade.
