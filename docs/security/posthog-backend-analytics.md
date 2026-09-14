# Backend PostHog product analytics

Backend product analytics passes through `posthog.Gateway`. Service and worker code supplies an
internal user UUID; the gateway resolves that UUID to the current canonical Clerk `sub`. Callers
cannot select a PostHog identity, and the backend never aliases browser identities.

Before every emission the authority repository joins the current user and
`privacy_settings.gdpr_analytics_consent` rows in the primary database. Missing settings, lookup
errors, inactive/deleted accounts, deletion-in-progress, and a missing Clerk subject all fail
closed. No permissive consent cache is used. PostgreSQL share locks are held through the SDK enqueue
callback, so consent withdrawal and emission are serialized: a withdrawal that commits first blocks
the event, while a withdrawal racing an already-authorized enqueue waits for that boundary.
The authority transaction contains no writes and is rolled back only to release its locks after
enqueue; cleanup failure cannot misreport an already-enqueued event as an authority drop.

New producers use a generated `TypedEvent` payload from `config/posthog_events.yaml`. Its private
marker method seals the interface to the PostHog package, so another backend package cannot forge
an event name or property bag. The gateway
validates required properties, bounded enums and numeric bounds, rejects unknown or recursively
sensitive keys, and never accepts a caller-provided distinct ID. Legacy generated-name producers
remain behind a compatibility adapter while they are migrated; because their schemas are marked
unreviewed, all of their properties are stripped before delivery.

The grandfathered `Analytics.Capture` adapters are explicit production wiring for: data export,
transactions, registration, rule application, savings goals, investments, boost execution,
households, account deletion, subscription webhooks, past-due handling, email-change propagation,
transfer correlation, investment sync, liability sync, merchants, and transfer
suggestion HTTP handling. They remain only because their catalog rows are still marked `legacy`
and their property contracts are unreviewed; moving them mechanically to typed payloads would bless
unknown semantics. The server injects the gateway—not the SDK client—into every adapter, and a
guard prevents raw SDK imports or raw-client production wiring. A second exact path-and-call-count
allowlist pins the exact generated event symbol at every grandfathered `.Capture` and
`.CaptureIfConsented` site, so adding a call or substituting its event fails tests. The household
adapter's private wrapper is additionally pinned at all nine generated-symbol callers.
Each adapter must be converted to a generated payload when its event semantics are reviewed.

Operational health does not belong in PostHog. Contract entries classified `operational` are
rejected by the gateway and remain observable through bounded Prometheus metrics, Sentry,
structured logs, or the audit log. Logs and metrics never include the user subject or event
properties. Investment/liability sync failures remain in their existing retry state, Prometheus,
and structured logs; Paddle cleanup failures remain in the deletion residue/audit workflow and
structured logs; dead letters remain in their durable repository, Prometheus, and structured logs.
Their former PostHog contracts are deprecated and cannot pass the gateway.
`dead_letter_queue_size` is refreshed from the durable repository at process startup and every job
health interval. It counts unresolved rows using the same replay-resolution predicate as the admin
health repository, so existing startup backlog appears immediately and successful replay or
deletion lowers the gauge; a failed database read leaves the series absent or at its last measured
value rather than asserting a false zero.

Capture is best effort and at most once from the application's perspective. Consent/identity
lookups and PostHog backpressure never change a business-operation result. The SDK queue is flushed
with `Close` during graceful server shutdown; enqueue failures are logged without payloads.

`financial_data_sync_completed` is produced only at the end of
`transactionSyncService.SyncTransactions`, after transaction storage, full pagination, and an
atomic durable completion/cursor write succeed. Partial progress is retained but never reported as
completion. A retained per-linked-item first-completion timestamp makes classification independent
of cursor shape: the first full catch-up for a new or relinked connection is `initial`; subsequent
completions and invalid-cursor recoveries are `refresh`. Invalid-cursor recovery compare-and-resets
only the cursor value that Plaid rejected, so a stale recovery cannot overwrite newer progress.
The migration backfills connections with durable `last_synced_at` evidence already present at
cutover. If an older binary writes `last_synced_at` after that snapshot while the latch remains
empty, the first new-code completion atomically sets the latch but emits no event: the old write
cannot prove full versus partial pagination. The next completion is reported as `refresh`. This
bounded rollout undercounts one completion instead of inventing an `initial` or `refresh` event.
The latch can commit before a best-effort
PostHog enqueue and is therefore a semantic completion fact, not a delivery receipt. That shared
boundary covers scheduled jobs and transaction webhooks without duplicate success producers.
Legacy `sync_completed` and `account_synced` emissions from the unused provider are not a production
metric source.
