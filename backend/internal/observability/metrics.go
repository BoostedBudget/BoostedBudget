package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Application-level Prometheus metrics.
// HTTP metrics are handled by middleware/metrics.go.
// These cover business-logic and infrastructure events.

var (
	// AppErrorsTotal counts 5xx errors by type and status code
	AppErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "app_errors_total",
		Help: "Total number of application errors by type and HTTP status code.",
	}, []string{"error_type", "status_code"})

	// AppPanicsTotal is defined in middleware/recovery.go to avoid import cycles

	// PlaidSyncDuration tracks how long Plaid sync operations take
	PlaidSyncDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "plaid_sync_duration_seconds",
		Help:    "Duration of Plaid transaction sync operations in seconds.",
		Buckets: prometheus.DefBuckets,
	})

	// PlaidSyncTxns counts transactions processed during sync by action
	PlaidSyncTxns = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "plaid_sync_transactions_total",
		Help: "Total number of Plaid transactions processed by action (added/modified/removed).",
	}, []string{"action"})

	// PlaidAPIErrors counts Plaid API errors by type and error code
	PlaidAPIErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "plaid_api_errors_total",
		Help: "Total number of Plaid API errors by error type and code.",
	}, []string{"error_type", "error_code"})

	// AuthAttempts counts authentication attempts by result
	AuthAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "auth_attempts_total",
		Help: "Total number of authentication attempts by result (success/invalid/expired/error).",
	}, []string{"result"})

	// AuthRegistrations counts new user registrations
	AuthRegistrations = promauto.NewCounter(prometheus.CounterOpts{
		Name: "auth_registrations_total",
		Help: "Total number of new user registrations.",
	})

	// JobsCompleted counts River jobs completed by kind, queue, and status
	JobsCompleted = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "jobs_completed_total",
		Help: "Total number of River jobs completed by kind, queue, and status.",
	}, []string{"kind", "queue", "status"})

	// JobDuration tracks River job execution duration by kind
	JobDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "job_duration_seconds",
		Help:    "Duration of River job execution in seconds by kind.",
		Buckets: prometheus.DefBuckets,
	}, []string{"kind"})

	// DeadLetterQueueSize tracks the current size of the dead letter queue
	DeadLetterQueueSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dead_letter_queue_size",
		Help: "Current number of jobs in the dead letter queue.",
	})

	// ManualTierOverrideOldestUnclearedExpiredSeconds tracks the age (in seconds) of the OLDEST
	// manual_tier_override that has expired (manual_tier_ends_at in the past) but has not yet been
	// cleared. Issue #1413 Task P4: the direct measure of the Critical finding C1 hazard —
	// noManualOverrideAtAllPredicate (profile_repository.go) excludes a user from retention notice/
	// hard-delete for as long as ANY override survives on their row, live or lapsed, and clearing a
	// LAPSED one is exclusively manual_tier_override_expiry_check's job. Healthy is ~0; it climbs
	// monotonically, in lockstep with wall-clock time, the moment that job stops clearing overrides
	// — independent of whether that job's own jobs_completed_total/JobLastSuccessTimestamp metrics
	// are even being scraped. Refreshed periodically from repository.JobHealthRepository (see
	// cmd/server/main.go's startJobHealthMetricsCollector — lives there, not here, because
	// internal/repository already imports internal/observability, so the reverse dependency this
	// collector needs would be an import cycle if placed in this package).
	ManualTierOverrideOldestUnclearedExpiredSeconds = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "manual_tier_override_oldest_uncleared_expired_seconds",
		Help: "Age in seconds of the oldest expired-but-uncleared manual_tier_override; 0 when none exist.",
	})

	// PlaidCleanupOutstandingUsers and PlaidCleanupOldestOutstandingSeconds are the Issue #1413
	// direct measure of the sibling hazard to the manual-tier-override one above:
	// RetentionCleanupCheckWorker.repairPlaidDisconnects (backend/internal/queue/jobs/retention_cleanup_check.go)
	// is the ONLY place a lapsed user's Plaid bank connections are ever disconnected, and its
	// per-user error isolation (`errorCount++; continue` — deliberate: one user's Plaid failure
	// must never block the GDPR pre-deletion notices/deletion sweep the same run performs for
	// everyone else) means Work() still takes its success path and RecordJobSuccess still fires
	// while that user is silently re-selected on every subsequent run, forever. Before these
	// gauges, repairedCount/skippedCount/errorCount were LOG FIELDS ONLY — no metric of any kind
	// told an operator how many users have outstanding cleanup or how old the oldest is.
	//
	// Both are refreshed together from repository.JobHealthRepository.GetOutstandingPlaidCleanupStats
	// (see cmd/server/main.go's startJobHealthMetricsCollector — lives there, not here, for the
	// same import-cycle reason ManualTierOverrideOldestUnclearedExpiredSeconds's collector does).
	//
	// Healthy is 0 for both. CRITICAL: "plaid_cleanup_pending_at IS NOT NULL" alone is NOT a valid
	// proxy for "cleanup outstanding" — migration 000119 stamped that marker on EVERY inactive/
	// cancelled user regardless of whether they had ever linked a Plaid item at all, and 000120
	// re-stamped those residue rows to its own NOW(). repairPlaidDisconnects's own candidate query
	// (GetUsersNeedingPlaidDisconnectRepair) never reaches those rows — its live-linked-item EXISTS
	// clause filters them out before the marker is ever cleared — so a query that skipped that same
	// join would report a large PERMANENT phantom backlog on day one and get ignored forever. See
	// GetOutstandingPlaidCleanupStats's doc comment for the query, which joins on the SAME liveness
	// definition (a NULL linked_items.status counts as live) the repair query itself uses.
	PlaidCleanupOutstandingUsers = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "plaid_cleanup_outstanding_users",
		Help: "Number of users with outstanding Plaid-disconnect cleanup (plaid_cleanup_pending_at set, with at least one live linked item); 0 when none exist.",
	})
	PlaidCleanupOldestOutstandingSeconds = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "plaid_cleanup_oldest_outstanding_seconds",
		Help: "Age in seconds of the oldest outstanding Plaid-disconnect cleanup (plaid_cleanup_pending_at with a live linked item); 0 when none exist.",
	})

	// PlaidDisconnectRepairUserFailuresTotal counts per-user failures inside
	// RetentionCleanupCheckWorker.repairPlaidDisconnects's per-user loop (retention_cleanup_check.go)
	// — Issue #1413. Incremented at THREE per-user failure sites within that worker (Finding 4, Codex
	// review PR #1443, added the third): the pre-disconnect isStillEligible re-read erroring;
	// DisconnectAllUserItemsIfEligible itself failing for any reason other than the two benign,
	// non-failure outcomes it can return (services.ErrSideEffectClaimBusy — another flow already
	// mid-disconnect for that user — and, as of round-14 (Issue #1413 H1), services.ErrCleanupAborted
	// — the per-item eligibility recheck correctly stopped mid-run because the user became ineligible
	// partway through, e.g. a comp granted between items; some items may already be disconnected,
	// others deliberately left live for a future run — neither represents a Plaid failure, so neither
	// counts here); and, as of Finding 4, ClearPlaidCleanupPendingIfMatches still failing after
	// plaidCleanupMarkerClearMaxAttempts retries — this THIRD site is arguably the highest-risk of
	// the three: a successful disconnect already removed the live linked item
	// GetUsersNeedingPlaidDisconnectRepair's own EXISTS clause requires, so a user stuck here can
	// never be reselected by a future run at all, unlike the other two sites (which the daily
	// schedule naturally retries).
	//
	// Issue #1413 #1453 (Codex review, PR #1443 follow-up): also incremented from TWO more call
	// sites outside this worker — services.PastDueService's grace-expiry disconnect
	// (past_due_service.go) and queue/jobs.SubscriptionWebhookWorker's cancellation handler
	// (subscription_webhook.go) — each brought into line with repairPlaidDisconnects's identical
	// bounded-retry-then-Error-log pattern for the SAME underlying write
	// (ClearPlaidCleanupPendingIfMatches after an already-successful synchronous disconnect).
	// Reused (not duplicated per call site) deliberately: all five sites represent the exact same
	// operational hazard — a marker left stuck non-NULL after the live item it described has
	// already been disconnected, so no future repairPlaidDisconnects run can ever reselect that user
	// to retry the clear either. Deliberately NOT labeled by user_id:
	// an unbounded, per-entity Prometheus label is a cardinality/ops hazard this codebase avoids
	// everywhere else (see JobsCompleted/PlaidAPIErrors above — every label here is a small, fixed
	// set of values).
	//
	// This counter alone cannot distinguish "one user failed once" from "the same user has failed
	// every run for weeks" — that distinction is made by reading it TOGETHER with
	// PlaidCleanupOldestOutstandingSeconds and the per-user structured log line this counter's
	// increment site already emits (`user_id`, `error`): a sustained nonzero rate on this counter
	// while the oldest-outstanding-age gauge keeps climbing (rather than dropping back down within
	// a run or two) means failures are NOT resolving — an operator can then grep the logs by
	// user_id to find exactly who is stuck. A transient failure shows up here as a brief blip while
	// the age gauge stays low, since that user's next run succeeds and clears their marker.
	PlaidDisconnectRepairUserFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "plaid_disconnect_repair_user_failures_total",
		Help: "Total number of per-user failures in RetentionCleanupCheckWorker's Plaid-disconnect repair pass.",
	})

	// PlaidCleanupBackstopFailuresTotal counts failures of ALL THREE self-healing backstops for
	// plaid_cleanup_pending_at (profile_repository.go): the round-16 Stamp half
	// (profileRepository.StampMissingPlaidCleanupMarkers, which stamps the marker for any ineligible
	// user who has a live linked item but a NULL marker); its Clear half, added by Issue #1413
	// Finding 1 (Codex review, PR #1443 follow-up) (profileRepository.ClearCompletedPlaidCleanupMarkers,
	// which nulls the marker for any ineligible user whose episode's cleanup has already completed —
	// at least one linked_items row, none of them live); and, as of Issue #1455, the branch-1 backstop
	// (profileRepository.ClearMarkersOnEligibleStatus, which nulls the marker for any user whose
	// status is NOT ineligible at all — the invariant's OTHER branch, previously unguarded by any
	// runtime backstop). All three share this one counter deliberately: they are the same operational
	// hazard viewed from different angles — the invariant (domain.User.PlaidCleanupPendingAt's doc
	// comment) drifting out of sync with reality — and a reader alerting on "is the backstop healthy"
	// should not need to watch three counters to answer that question. repairPlaidDisconnects's own
	// structured log lines (healed_count / cleared_count, each call site under its own message and
	// field name) are what distinguish WHICH backstop is active on any given pass; this counter
	// answers only whether any of them is currently failing.
	//
	// This is deliberately a SEPARATE counter from the per-user one above, and the distinction is the
	// point. A per-user failure is self-limiting: that user keeps a non-NULL marker, so the candidate
	// query still selects them and the next pass retries them — the backstop covers for it. Nothing
	// covers for the backstop. Stamp is the only thing that returns a NULL-marker/live-item user to
	// visibility, and a user invisible to the candidate query is a user whose bank connections are
	// never disconnected; Clear is the only thing that returns a completed episode's marker to NULL
	// before a later comp grant/removal can preserve it as a stale, grace-bypassing timestamp (Finding
	// 1's own scenario); the branch-1 backstop (Issue #1455) is the only thing that ever repairs a
	// non-ineligible-status row whose marker was wrongly left set, closing the identical
	// grace-bypassing scenario from the OTHER side (a status transition, not a comp grant/removal).
	// So a persistently failing backstop, in any of the three directions, is a silent data-retention
	// or grace-window failure, and it must not be inferable only from the absence of
	// "healed"/"cleared" log lines.
	//
	// Deliberately does NOT fail the job (see repairPlaidDisconnects's call sites): returning an error
	// there would skip the 30-day/7-day GDPR pre-deletion notices and deleteExpiredAccounts for EVERY
	// user, which is a worse outcome than a delayed heal. The isolation is intentional; this counter
	// plus an Error-level log is what keeps it from also being invisible.
	//
	// Alert on any sustained non-zero rate. A single increment is a transient DB blip and the next
	// pass heals for free; a rate that persists across passes means the backstop itself is broken.
	PlaidCleanupBackstopFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "plaid_cleanup_backstop_failures_total",
		Help: "Total failures of the plaid_cleanup_pending_at self-healing backstop, all three directions (StampMissingPlaidCleanupMarkers, ClearCompletedPlaidCleanupMarkers, and ClearMarkersOnEligibleStatus). Sustained non-zero means the marker may be drifting out of sync with the linked_items it is supposed to summarise, or with subscription_status, in any direction.",
	})

	// PlaidCleanupServiceNotConfiguredTotal counts passes of
	// RetentionCleanupCheckWorker.repairPlaidDisconnects that skipped the Plaid-disconnect candidate
	// query and per-user disconnect loop entirely because no Plaid cleanup service was configured
	// (w.plaidCleanup == nil) — a CI finding fixed alongside Issue #1413 Finding 1, not from the Codex
	// review itself.
	//
	// Deliberately NOT the same counter as PlaidCleanupBackstopFailuresTotal above, even though both
	// live in the same function: that counter means the backstop's own SQL (Stamp or Clear) failed —
	// a narrower, different fact from "the collaborator this pass needs was never wired up at all".
	// Conflating the two would make an operator unable to tell "the backstop is broken" from "Plaid
	// isn't configured here", which call for entirely different responses.
	//
	// This is EXPECTED to be persistently non-zero in QA/E2E, which legitimately never configures
	// Plaid — RetentionCleanupCheckPeriodicOpts's RunOnStart: true means this job runs at every
	// process boot there, and through round-10 Fix 1 that used to hard-fail the WHOLE job (retention-
	// clock repair and GDPR notices included) on every single boot; see repairPlaidDisconnects's own
	// doc comment for why that changed. In PRODUCTION, where Plaid must always be configured, any
	// non-zero rate here is exactly as significant as the old hard failure was — alert on it there,
	// but do not alert on it globally, since a QA/E2E environment incrementing this counter on every
	// boot is the expected, correct steady state, not a regression.
	PlaidCleanupServiceNotConfiguredTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "plaid_cleanup_service_not_configured_total",
		Help: "Total repairPlaidDisconnects passes that skipped the Plaid-disconnect repair pass because no Plaid cleanup service was configured. Expected non-zero in QA/E2E; any non-zero rate in production is a configuration defect.",
	})

	// MalformedManualTierOverrideCount is Issue #1453's monitoring gauge: the number of users, as of
	// the MOST RECENT RetentionCleanupCheckWorker pass, whose manual_tier_override is non-empty but
	// does not name a currently-configured tier (config/subscription_tiers.yaml). Migration 000127
	// backfilled every such row to NULL as a one-time cleanup; this gauge exists to catch a FUTURE
	// writer reintroducing the class — a legacy row, a direct SQL write, or a write made while
	// tierConfig was nil, all of which admin_user_service.go's write-time validation
	// (SetOverrideTierValidator / tierConfig.GetTier) cannot see, since none of them go through it.
	//
	// A gauge, not a counter: this reports the CURRENT outstanding count each pass observes (via
	// profileRepository.GetUsersWithMalformedTierOverride), not a cumulative total of every row ever
	// seen malformed across all passes — the natural fit for "how many are broken right now",
	// mirroring GetOutstandingPlaidCleanupStats' own gauge-shaped backlog metric
	// (job_health_repository.go) rather than the counter shape PlaidCleanupBackstopFailuresTotal
	// above uses for "how many failures have ever happened".
	//
	// Expected to read 0 in every healthy environment, always — unlike
	// PlaidCleanupServiceNotConfiguredTotal above, there is no legitimate QA/E2E exception: a
	// malformed override should never exist anywhere post-000127. Alert on ANY sustained non-zero
	// value.
	MalformedManualTierOverrideCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "malformed_manual_tier_override_count",
		Help: "Current number of users whose manual_tier_override does not name a configured tier (config/subscription_tiers.yaml). Should always be 0 post-migration-000127; any sustained non-zero value means a writer bypassed write-time validation.",
	})

	// InvestmentHoldingsSyncDuration tracks how long holdings sync operations take.
	// Issue #659: Plaid Investments
	InvestmentHoldingsSyncDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "investment_holdings_sync_duration_seconds",
		Help:    "Duration of Plaid investment holdings sync operations in seconds.",
		Buckets: prometheus.DefBuckets,
	})

	// InvestmentTransactionsSyncDuration tracks how long investment transaction sync operations take.
	// Issue #659: Plaid Investments
	InvestmentTransactionsSyncDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "investment_transactions_sync_duration_seconds",
		Help:    "Duration of Plaid investment transactions sync operations in seconds.",
		Buckets: prometheus.DefBuckets,
	})

	// InvestmentHoldingsSynced counts holdings replaced per sync by outcome.
	// Issue #659: Plaid Investments
	InvestmentHoldingsSynced = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "investment_holdings_synced_total",
		Help: "Total number of investment holdings synced by outcome (replaced/cleared).",
	}, []string{"outcome"})

	// InvestmentTransactionsSynced counts investment transactions processed by action.
	// Issue #659: Plaid Investments
	InvestmentTransactionsSynced = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "investment_transactions_synced_total",
		Help: "Total number of investment transactions processed by action (inserted/skipped).",
	}, []string{"action"})

	// AccountsReplacedFiltered counts query executions that exclude replaced accounts or
	// resolve a re-link chain (Issue #1109). The "family" label groups call sites by domain
	// so we can confirm every filter path is exercised after a staging deploy.
	//
	// Expected families:
	//   accounts_list      - GetAccountsWithInstitutionFiltered (active-only path)
	//   balance_summary    - GetBalanceSummary, GetAccountTypes
	//   dashboard_history  - GetLatestByUserID, GetAggregatedHistory
	//   analytics          - analytics_repository spending/cash-flow joins
	//   transaction_match  - MonitorForMatches chain resolution
	//   automation         - rule execution chain resolution, savings goal creation
	//   investments        - holdings/investment_transactions active filter
	//   liabilities        - liability repositories active filter
	AccountsReplacedFiltered = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "accounts_replaced_filtered_total",
		Help: "Number of query executions that excluded at least one replaced account or expanded a re-link chain",
	}, []string{"family"})
)
