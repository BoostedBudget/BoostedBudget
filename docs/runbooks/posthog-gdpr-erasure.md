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
- [`GET /api/projects/:project_id/persons/?distinct_id=...`](https://posthog.com/docs/api/persons) resolves every historical distinct ID to a stable person UUID before deletion. The UUIDs are encrypted because the bulk response has no request/correlation ID.
- [`GET /api/projects/:project_id/persons/deletion_status/?person_uuid=...&status=all`](https://posthog.com/docs/api/persons#get-api-projects-project_id-persons-deletion_status) exposes asynchronous **event** deletion status and `delete_verified_at`; it needs `person:read`. The worker polls every resolved UUID. This endpoint does not verify recordings.

PostHog documents no public API for deleting an individual Support conversation/ticket/message and
no public API for deleting an individual survey response. Survey responses may be represented as
events, but BoostedBudget does not assume that event deletion proves every survey-owned view is
gone. Neither surface is assumed to cascade from person deletion. A row enters `manual_pending`
only after the documented person/event flow completes; it cannot complete until an operator has
verified recordings, survey-response views/exports, and
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
2. Use authenticated `GET /api/v1/admin/posthog-erasure?limit=50` to discover outstanding IDs and
   targets. Responses are `Cache-Control: no-store`. For a legacy `unconfigured` row, call
   `POST /api/v1/admin/posthog-erasure/{id}/retarget` with a project and `us`/`eu` region. Retarget
   is refused after provider acceptance and is audited with an operator hash and database clock.
3. Call `POST /api/v1/admin/posthog-erasure/{id}/reveal` for the distinct IDs and resolved person
   UUIDs needed for provider searches. This sensitive reveal is audited and returned `no-store`;
   never paste its response into logs or tickets.
   After correcting a provider outage, rate limit, or credential problem, requeue an ordinary
   `dead_letter` with `POST /api/v1/admin/posthog-erasure/{id}/replay`. Replay preserves the target,
   identities, correlations, and attempt count and writes an operator-hashed audit row.
4. After the worker reports async event deletion verified, query/search the authorized provider
   tools for every distinct ID and confirm zero person, event/survey-response, recording, survey
   response, Support ticket, conversation, and message results. Request provider-side Support or
   survey cleanup when any remains.
5. For each of `recordings`, `surveys`, and `support`, call
   `POST /api/v1/admin/posthog-erasure/{id}/evidence` with bounded evidence `provider_zero` or
   `provider_case_deleted`. Repeat each check after at least five minutes. Completion is automatic
   only after all three second confirmations plus person/event verification; that same atomic
   update nulls both identity ciphertext columns.

If resolution finds no person (including an event-only distinct ID), the worker still submits the
documented bulk delete by distinct ID. Because PostHog exposes no deletion-status correlation for
that case, the operator must also record twice-separated `events` evidence; no provider correlation
is invented. Provider request
acceptance, a single zero query, a deleted PostHog person, or an operator's
unsupported assertion is insufficient evidence. Preserve completed rows as identity-free audit
history; do not remove retained financial history outside the existing local GDPR cascade.
