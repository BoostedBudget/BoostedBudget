package domain

import "time"

// PaddleStatusMetrics represents Paddle payment system health metrics
type PaddleStatusMetrics struct {
	ActiveSubscriptions         int64      `json:"active_subscriptions"`
	WebhookSuccessRate24h       float64    `json:"webhook_success_rate_24h"`
	FailedWebhooks24h           int64      `json:"failed_webhooks_24h"`
	TotalWebhooks24h            int64      `json:"total_webhooks24h"`
	LastReconciliationAt        *time.Time `json:"last_reconciliation_at"`
	ReconciliationDiscrepancies int64      `json:"reconciliation_discrepancies"`
}

// SystemHealthResponse represents the overall system health status
type SystemHealthResponse struct {
	Status          string                  `json:"status"` // "healthy", "degraded", "unhealthy"
	Timestamp       time.Time               `json:"timestamp"`
	Services        ServiceHealthStatuses   `json:"services"`
	JobQueues       map[string]QueueMetrics `json:"job_queues"`
	SyncStatus      SyncStatusMetrics       `json:"sync_status"`
	DeadLetterQueue DeadLetterMetrics       `json:"dead_letter_queue"`
	PaddleStatus    PaddleStatusMetrics     `json:"paddle_status"`
	// JobBacklog surfaces the three DB-derived Plaid-cleanup/manual-tier-override backlog gauges
	// (Issue #1458) — point-in-time snapshots structurally identical to DeadLetterMetrics above,
	// backed by repository.JobHealthRepository (the same queries the retention-cleanup repair
	// job and the override-expiry worker themselves use — never duplicated SQL). The other four
	// metrics PR #1443 added (three lifetime monotonic counters plus the in-process
	// JobLastSuccessTimestamp heartbeat) deliberately stay Prometheus-only — see
	// JobBacklogMetrics's own doc comment for why.
	JobBacklog JobBacklogMetrics `json:"job_backlog"`
	// PlaidOrphanedItems is the aggregate operator picture of plaid_orphaned_items (Issue #1497
	// item 5) — Plaid Items that may still exist and may still BILL, whose local linked_items row
	// was destroyed by a GDPR erasure before removal could be proved. Derived in Go over a bounded
	// full read via domain.SummarisePlaidOrphanedItems; see that function for why one of
	// AdminState's four checks makes a SQL COUNT impossible.
	PlaidOrphanedItems PlaidOrphanedItemMetrics `json:"plaid_orphaned_items"`
	// DeletionResidue is the count of OPEN records that a GDPR deletion issued an irreversible
	// external Paddle call — a subscription cancel, or the customer archive-and-anonymise nobody can
	// undo — against a user who then survived the run (Issue #1504; widened in round 15, which also
	// made a completed deletion retract its own records). Derived by a single indexed COUNT over the
	// audit_logs rows
	// repository.ProfileRepository.RecordDeletionResidue writes; see
	// repository.JobHealthRepository.GetDeletionResidueStats.
	DeletionResidue DeletionResidueMetrics `json:"deletion_residue"`
	// RejectedWebhookAttempts24h is the operator-visibility signal Issue #1514 requirement #5
	// restores. See RejectedWebhookAttemptMetrics for what it counts and, critically, what it does
	// NOT count.
	RejectedWebhookAttempts24h RejectedWebhookAttemptMetrics `json:"rejected_webhook_attempts_24h"`
	// ForkedSecurityIdentities is the count of securities.institution_security_id groups spanning
	// more than one plaid_security_id — a Plaid corporate action that forked one real-world
	// security's identity into multiple `securities` rows, each with its own separate transaction/
	// holding history (#1631 M8b). Detection only: nothing merges these, and whether a non-zero
	// count should degrade overall health is a product decision this task never asked for — see
	// ForkedSecurityIdentityMetrics for the "unknown is not zero" convention it follows.
	ForkedSecurityIdentities ForkedSecurityIdentityMetrics `json:"forked_security_identities"`
	// CacheEnabled reports whether the analytics-namespace cache (cache.NamespaceAnalytics) is
	// currently active for this deployment — Issue #1531 Task 11e. It is false whenever
	// CACHE_ENABLED=false, no cache Redis client could be constructed at startup, the namespace is
	// disabled via CACHE_NAMESPACES_DISABLED, or a live Redis control-key override has disabled it
	// — it is the exact same cache.Cache.Enabled call CachedAnalyticsService's own reads use to
	// decide whether to consult Redis at all, so this can never claim "enabled" while real reads
	// are silently bypassing the cache.
	//
	// This field exists specifically so the E2E cache-parity/fail-safe suite
	// (apps/web/e2e/cache-*.auth.spec.ts) can gate on a live signal before running, rather than
	// trusting compiled config or the DB/queue metrics above — the public /health endpoint
	// deliberately no longer exposes any component breakdown (Task 6's security fix), so this is
	// the only place left for that guard to check.
	CacheEnabled bool `json:"cache_enabled"`
}

// RejectedWebhookAttemptMetrics is the read side of repository.InvalidWebhookAttemptRepository —
// the audit signal for webhook deliveries REJECTED before ever being written to the full webhook
// tables, because their signature did not verify (or was absent). Issue #1514, requirement #5
// (Phase 2 gap fix).
//
// BACKGROUND. Phase 1 stopped persisting unsigned/invalid-signature Paddle and Plaid webhooks
// into paddle_webhook_events / webhook_events (see migration 000146/000147) and recorded bounded
// metadata into invalid_webhook_attempts instead — they ARE still persisted, just into that
// separate, capped, metadata-only table rather than the full tables an attacker could use to
// poison idempotency. As a side effect, that removed the ONLY place an operator could see a
// forged-webhook attempt on /admin/webhooks — that page reads paddle_webhook_events, which by
// construction can never again contain one of these rows. During a live forgery flood
// /admin/webhooks renders "No webhook events found", which reads as "no attacks are happening"
// when it actually means "we stopped recording rejections where you're looking." This struct is
// what makes that silence visible again, on /admin/health instead.
//
// ⚠ THIS IS NOT domain.PaddleStatusMetrics.FailedWebhooks24h, AND THE TWO MUST NEVER BE WIRED TO
// THE SAME SOURCE. FailedWebhooks24h counts paddle_webhook_events rows with
// processing_error IS NOT NULL — webhooks that PASSED signature verification and then failed
// during processing (a bug in this app, or a transient dependency failure). This struct counts
// webhooks that FAILED signature verification and were never written to that full-fidelity table
// at all — someone (an attacker, or a misconfigured sender) sending a request that never proved
// it came from Paddle or Plaid. A processing bug and a forgery attempt are unrelated failure
// modes with unrelated causes and unrelated responses; if a future change ever makes these two
// numbers move together, that is the regression this comment exists to name in advance.
type RejectedWebhookAttemptMetrics struct {
	// Total24h is the sum of ByProvider's values — the number of rejected attempts, per provider,
	// CURRENTLY ON RECORD in the trailing 24 hours.
	//
	// ⚠ NOT A LIFETIME/UNCAPPED COUNTER (Codex adversarial review finding). Each provider's rows in
	// invalid_webhook_attempts are periodically trimmed back toward
	// repository.invalidWebhookAttemptCap (see that constant's doc comment) as a defense against
	// unbounded storage growth under attack. Under a sustained flood that outpaces the trim
	// schedule, this number can plateau at the cap well below the true number of rejected
	// attempts that occurred in the window — it answers "how many rejected attempts are currently
	// on record", not "how many happened". The Prometheus counter
	// recordInvalidWebhookAttempt increments (webhook_security.go) is the uncapped lifetime total
	// for whoever needs that instead.
	Total24h int64 `json:"total_24h"`
	// ByProvider is Total24h broken down by provider (e.g. "paddle", "plaid"). A provider with no
	// rejected attempts in the window is simply absent from this map, not present with a 0 —
	// callers that want an explicit per-provider zero should read Total24h == 0 as "nothing from
	// anyone" rather than probing this map for absence.
	ByProvider map[string]int64 `json:"by_provider"`
	// MostRecentAt is when the single most recent rejected attempt (across all providers)
	// happened, or nil if there were none in the trailing 24h window (which, unlike
	// DeletionResidueMetrics.NewestRecordSeconds, is unambiguous here: nil always means "none",
	// never "just now" — there is no zero-value collision to disambiguate).
	MostRecentAt *time.Time `json:"most_recent_at"`
	// Available distinguishes "queried invalid_webhook_attempts successfully and found a real
	// (possibly zero) count" from "the query failed, or the repository is unwired, so there is no
	// number to report" — same fail-closed convention as JobBacklogMetrics.Available,
	// PlaidOrphanedItemMetrics.Available, and DeletionResidueMetrics.Available above. Defaults to
	// the Go zero value (false): any path that forgets to explicitly claim success reads as
	// "unknown", never as a falsely-confident "0 attacks" during an actual flood.
	Available bool `json:"available"`
}

// DeletionResidueMetrics is the post-gate external-effect residue as /admin/health reports it, and as
// observability.DeletionResidueRecords / observability.DeletionResidueNewestSeconds publish it.
// Issue #1504.
//
// WHAT ONE RECORD MEANS. A GDPR deletion request was revoked while the Paddle subscription-cancel
// call was already in flight. The authorisation gate had passed a moment earlier and a network call
// cannot be recalled, so the cancel landed; the cascade then correctly refused to delete anything.
// The user keeps their account and every byte of their data, and has no subscription. Nothing in the
// product tells them, nothing automated restores it, and before this section existed the ONLY trace
// was one audit_logs row that no code anywhere read.
//
// ⚠ ITS OWN SECTION, NOT A FIELD ON JobBacklogMetrics, and the reason is the one Available bool that
// struct documents at length: JobBacklogMetrics covers all three of its numbers with a single
// availability flag, so folding this in would make a failure of EITHER Plaid-cleanup query blank out
// the residue count too, and vice versa. These measure unrelated things with unrelated queries. A
// separate struct with its own flag is the same choice PlaidOrphanedItemMetrics made above.
//
// ⚠ THIS IS A GAUGE THAT NEVER GOES BACK DOWN, ON PURPOSE. There is no resolution column for a
// residue record, nothing prunes audit_logs, and no automated process can restore a cancelled
// subscription. So once this has happened once, Records stays non-zero. That is not a defect to be
// smoothed over with a time window or a "handled" flag: no record anywhere proves the affected user
// was ever made whole, and a surface that implied one would be conflating a claim with a proof.
// NewestRecordSeconds is the field that carries the "is this happening NOW?" signal instead.
//
// ⚠ DELIBERATELY NOT FED INTO SystemHealthResponse.Status, matching the JobBacklog and
// PlaidOrphanedItems sections: whether outstanding residue should degrade overall health is a product
// decision #1504 never asked for, and only making it VISIBLE is in scope.
type DeletionResidueMetrics struct {
	// Records is the all-time number of residue records — one per OCCURRENCE, not per user: two
	// revocations against the same user are two subscriptions somebody has to restore. Every one of
	// these is a specific, identifiable person whose subscription was cancelled without their
	// consent. Healthy is 0 and, in a system that has never hit the race, stays 0 forever.
	Records int64 `json:"records"`
	// NewestRecordSeconds is how long ago the MOST RECENT residue record was written, in seconds.
	//
	// ⚠ MEANINGFUL ONLY ALONGSIDE Records, which must be read FIRST. This is 0 both when there are no
	// records at all and when one was written this very instant — the two readings that could not
	// matter more differently. Records is the disambiguator. (The sibling backlog gauges above have no
	// such hazard, because their zero age is unambiguous good news; this one is not, which is exactly
	// why it is called out here rather than left for a consumer to discover.)
	//
	// It is the NEWEST record's age and not the oldest's because nothing ever clears a residue record,
	// so the oldest one's age climbs with wall-clock time forever and carries no information. See
	// repository.JobHealthRepository.GetDeletionResidueStats.
	NewestRecordSeconds int64 `json:"newest_record_seconds"`
	// Available distinguishes "queried successfully and found a real (possibly zero) residue count"
	// from "the query failed or the repository is unwired, so there is no number to report". Same
	// reasoning as JobBacklogMetrics.Available and PlaidOrphanedItemMetrics.Available below, and it
	// bites harder here than on either: the thing a falsely-confident zero would be denying is
	// "somebody's subscription was cancelled by a bug and nobody has been told". Defaults to the Go
	// zero value (false), the fail-closed direction — any path that forgets to claim success reads as
	// unknown, never as a green zero.
	Available bool `json:"available"`
}

// PlaidOrphanedItemMetrics is the aggregate state of plaid_orphaned_items as /admin/health reports
// it, and as observability.PlaidOrphanedItemsByAdminState /
// observability.PlaidOrphanedItemMaxRetryAttempts publish it. Issue #1497 items 4 and 5.
//
// ⚠ THE TWO ACTIONABLE COUNTS ARE SEPARATE FIELDS AND MUST STAY SEPARATE. The whole reason
// OrphanedItemAdminState exists as four states rather than a resolved/unresolved boolean is that
// auto_retrying and manual_intervention_required demand OPPOSITE human responses: the first means
// "do nothing, a sweep is working on it", the second means "go remove this Item in Plaid's dashboard
// using plaid_item_id, or it may keep billing an erased user's account". A single "unresolved" total
// — the obvious and much shorter implementation — erases exactly that distinction and would make this
// gauge worse than absent, because an operator would believe they had been told something.
//
// Resolved is carried too, but only as history: it is the count of rows an automated retry PROVED
// gone at Plaid. It is never work.
type PlaidOrphanedItemMetrics struct {
	// AutoRetrying is the number of rows holding a live, decryptable credential with a retry
	// observed inside domain.OrphanedItemRetryStaleAfter. No operator action.
	AutoRetrying int64 `json:"auto_retrying"`
	// ManualInterventionRequired is the number of rows nothing automated will finish — a dead,
	// purged or undecryptable credential, or a live one nothing has retried for
	// domain.OrphanedItemRetryStaleAfter. Every one of these is a Plaid Item a human must remove in
	// Plaid's own dashboard. This is the number the whole feature exists to surface.
	ManualInterventionRequired int64 `json:"manual_intervention_required"`
	// Resolved is the number of rows an automated retry proved removed at Plaid. History, not work —
	// and it never leaves the table, because nothing prunes plaid_orphaned_items.
	Resolved int64 `json:"resolved"`
	// OperatorAttested is the number of rows an operator has CLAIMED to have removed by hand in
	// Plaid's dashboard, with no verification of any kind. Issue #1497 item 2.
	//
	// ⚠ IT IS ITS OWN NUMBER AND MUST NOT BE ADDED INTO Resolved, however similar they look on a
	// dashboard. Resolved is a count of things Plaid confirmed; this is a count of things colleagues
	// said. Summing them would produce a single "done" figure whose accuracy nobody could state, on
	// the one surface whose job is to say what is actually outstanding — and it is the shape of every
	// claim-as-proof defect this feature was built to avoid (see OrphanedItemOperatorAttested).
	//
	// A NON-ZERO VALUE IS NOT AN ALERT, but a PERSISTENTLY non-zero one is worth an operator's eye:
	// the automated sweep keeps re-attempting attested rows, so a row that stays attested rather than
	// becoming resolved means no retry has ever managed to confirm the claim.
	OperatorAttested int64 `json:"operator_attested"`
	// MaxUnresolvedRetryAttemptCount is the highest retry_attempt_count among UNRESOLVED rows —
	// Issue #1497 item 4, the item named in that issue's own title. retry_attempt_count has been
	// written by RecordRetryFailure since #1497 landed and, until this field, was read by no metric
	// at all: a row could be re-attempted indefinitely with nothing anywhere reporting it.
	//
	// The MAXIMUM rather than a sum or a mean, deliberately: the question an operator has is "is
	// something being retried into the ground?", which is a property of the WORST row. A sum grows
	// with population size and a mean is diluted by healthy rows, so both can hide a single Item
	// that has failed two hundred times.
	//
	// Nothing in this system gives up on an attempt count (see AdminState's own note on why
	// RetryAttemptCount is absent from that derivation), so this is information for a human and an
	// alerting signal, never a state.
	MaxUnresolvedRetryAttemptCount int64 `json:"max_unresolved_retry_attempt_count"`
	// Available distinguishes "read the table successfully and derived a real (possibly zero) state"
	// from "the read failed, or the table has outgrown the bounded in-Go derivation, so there is no
	// number to report". Same reasoning as JobBacklogMetrics.Available above, and the same
	// fail-closed direction: it defaults to the Go zero value (false), so any path that forgets to
	// explicitly claim success reports "unknown" rather than a falsely confident green zero.
	//
	// ⚠ THAT DIRECTION MATTERS MORE HERE THAN ANYWHERE ELSE ON THIS RESPONSE. A false
	// manual_intervention_required: 0 tells an operator that no erased user's Plaid Item is still
	// billing — which, if the read actually failed, is a claim nothing checked. Pinned against a
	// REAL repository error (not a struct literal) by
	// TestGetSystemHealth_PlaidOrphanedItems_ListError_IsUnavailableNotAConfidentZero.
	Available bool `json:"available"`
}

// JobBacklogMetrics represents point-in-time DB snapshots of two operational backlogs PR #1443
// introduced (Issue #1458): the Plaid-disconnect cleanup queue and the manual-tier-override
// expiry-clearing queue. Both measure "how much work is stuck", the same class of signal as
// DeadLetterMetrics — an ageing, non-zero backlog means a repair/expiry job is stalling.
//
// Deliberately excludes the OTHER four metrics PR #1443 added
// (PlaidDisconnectRepairUserFailuresTotal, PlaidCleanupBackstopFailuresTotal,
// PlaidCleanupServiceNotConfiguredTotal, JobLastSuccessTimestamp): the first three are lifetime
// monotonic counters meaningful only as a Prometheus rate() — a point-in-time total can't express
// "sustained non-zero rate" vs. "one blip", and PlaidCleanupServiceNotConfiguredTotal is expected
// to be persistently non-zero in QA/E2E, so a bare number would send an operator chasing a
// non-problem. JobLastSuccessTimestamp is an in-process promauto gauge with no DB row to
// re-derive it from — in a multi-instance deploy the replica serving /admin/health may not be the
// one that ran the job, so the value would be stale or zero. All four stay exclusively
// Prometheus/Alertmanager's job.
type JobBacklogMetrics struct {
	// PlaidCleanupOutstandingUsers is the count of users with outstanding Plaid-disconnect
	// cleanup (repository.JobHealthRepository.GetOutstandingPlaidCleanupStats).
	PlaidCleanupOutstandingUsers int64 `json:"plaid_cleanup_outstanding_users"`
	// PlaidCleanupOldestOutstandingSeconds is how long the OLDEST such marker has been sitting
	// uncleared, in seconds (0 when there are none).
	PlaidCleanupOldestOutstandingSeconds int64 `json:"plaid_cleanup_oldest_outstanding_seconds"`
	// ManualTierOverrideOldestUnclearedExpiredSeconds is how long the OLDEST currently
	// expired-but-uncleared manual_tier_override has been sitting uncleared, in seconds (0 when
	// there are none) — repository.JobHealthRepository.GetOldestUnclearedExpiredManualTierOverrideAge.
	ManualTierOverrideOldestUnclearedExpiredSeconds int64 `json:"manual_tier_override_oldest_uncleared_expired_seconds"`
	// Available distinguishes "queried successfully and found a real (possibly zero) backlog"
	// from "the underlying query failed/is unwired" (Issue #1458 follow-up G16). Before this
	// field existed, SystemHealthService.getJobBacklogMetrics collapsed a query ERROR into the
	// exact same zero-valued struct as a genuinely empty backlog, so /admin/health rendered a
	// failed query as a confident green "0" — indistinguishable from "nothing to worry about".
	// That inverts this gauge's whole purpose: an operator loses the signal exactly when the
	// system can no longer report it.
	//
	// This is precisely the "unknown != zero" principle this package's own doc comment above
	// (see the four EXCLUDED metrics) already invokes to justify leaving
	// PlaidCleanupServiceNotConfiguredTotal off this response entirely — it was applied to those
	// four metrics and not to these three. This field closes that gap for these three instead of
	// excluding them.
	//
	// Deliberately a bool flag rather than making the three numeric fields above `*int64`: same
	// wire-visible signal, far less disruptive to the generated TypeScript client and to every
	// existing consumer that reads these fields as plain numbers (HealthDashboardPage.tsx,
	// formatBacklogAge, etc.), all of which would otherwise need nil-checks added everywhere.
	// Defaults to the Go zero value (false) — the fail-closed direction: any code path that
	// forgets to explicitly report success (a nil/unwired repo, an early return, a future bug)
	// silently reads as "unavailable", never as a falsely-confident "available, zero".
	Available bool `json:"available"`
}

// ServiceHealthStatuses contains health status for all services
type ServiceHealthStatuses struct {
	Database DatabaseHealth `json:"database"`
	Clerk    ServiceHealth  `json:"clerk"`
	Plaid    PlaidHealth    `json:"plaid"`
	// Redis is the admin-only detail view of the shared Redis health check (Issue #1531 Task 7d).
	// It follows the same "unwired optional collaborator reports unavailable, never a confident
	// zero" convention as JobBacklogMetrics/PlaidOrphanedItemMetrics/DeletionResidueMetrics above
	// (RedisHealth.Status == "not_configured" rather than a bare zero-value "up").
	//
	// ⚠ Redis DOES feed calculateOverallStatus, but ONLY as far as "degraded", never
	// "unhealthy"/503 (Issue #1531 criterion 12: "Redis remains non-critical — Redis down yields
	// degraded + HTTP 200, never 503"). The public /health liveness probe (Fly's http_checks)
	// still never reads Redis at all (Issue #1531 Task 6) — that boundary is unchanged and is
	// what actually prevents "a Redis blip cycles every machine": this field existing on the
	// ADMIN-only response, and only ever downgrading to "degraded", cannot reintroduce that
	// hazard. See TestSystemHealth_RedisDownReportsDegradedOverallStatus and
	// calculateOverallStatus's own doc comment for the exact boundary.
	Redis RedisHealth `json:"redis"`
}

// RedisHealth is the admin-only detail view of the shared, memoized Redis health.Check (Issue
// #1531 Task 7d) — reusing the SAME Check instance the public /health handler holds (but never
// calls), so an admin page load costs zero extra Redis PINGs beyond whatever the liveness probe
// already paid for.
type RedisHealth struct {
	// Status is "up", "down", or "not_configured" (no Redis client wired for this deployment).
	Status string `json:"status"`
	// Reason is a SANITIZED CLASSIFICATION — one of "timeout", "connection_refused",
	// "auth_failed", "not_configured", "unknown", or "" (Status == "up") — and is NEVER the raw
	// error text. Admin screens get screenshotted into support tickets and pasted into Slack; the
	// full error (host:port, Upstash endpoint names) is already in slog/Sentry, findable by
	// request ID. This deliberately tightens what issue #1531 permits.
	Reason string `json:"reason,omitempty"`
	// LatencyMs is the PROBE's latency (health.Result.Latency), not the time it took to read a
	// cached Result.
	LatencyMs int64 `json:"latency_ms"`
	// ObservedAgoMs is how long ago (in ms) the memoized probe that produced this Result actually
	// ran (health.Result.ObservedAt) — an admin page load can be reading a Result that is up to
	// the check's TTL old, and this makes that staleness honest instead of implied.
	ObservedAgoMs int64 `json:"observed_ago_ms"`
	// PoolStats is the limiter Redis client's local connection-pool snapshot, or nil when no
	// Redis client is wired for this deployment. Reads local pool state only — no extra Redis
	// command is ever dispatched to populate this (see observability.RedisPoolStatsSource's doc
	// comment for the same discipline on the Prometheus side).
	PoolStats *RedisPoolStats `json:"pool_stats,omitempty"`
}

// RedisPoolStats mirrors redisclient.PoolStats / observability.RedisPoolStats for the
// /admin/health wire response. Its own copy, per this codebase's established mirroring
// convention (see observability.RedisPoolStats's doc comment) — this domain package must never
// import internal/redisclient just to describe a wire shape.
type RedisPoolStats struct {
	Hits       uint32 `json:"hits"`       // cumulative since client construction
	Misses     uint32 `json:"misses"`     // cumulative since client construction
	Timeouts   uint32 `json:"timeouts"`   // cumulative since client construction
	TotalConns uint32 `json:"total_conns"`
	IdleConns  uint32 `json:"idle_conns"`
	StaleConns uint32 `json:"stale_conns"`
	PoolSize   int    `json:"pool_size"` // configured
}

// DatabaseHealth represents database connection pool health
type DatabaseHealth struct {
	Status      string              `json:"status"` // "up", "down"
	Connections ConnectionPoolStats `json:"connections"`
	LatencyMs   int64               `json:"latency_ms"`
}

// ConnectionPoolStats represents pgxpool connection statistics
type ConnectionPoolStats struct {
	Active       int32 `json:"active"`       // AcquiredConns
	Idle         int32 `json:"idle"`         // IdleConns
	Max          int32 `json:"max"`          // MaxConns
	Constructing int32 `json:"constructing"` // ConstructingConns
	Total        int32 `json:"total"`        // TotalConns
}

// ServiceHealth represents a generic service health status
type ServiceHealth struct {
	Status    string `json:"status"` // "up", "down"
	LatencyMs int64  `json:"latency_ms"`
}

// PlaidHealth represents Plaid service health
type PlaidHealth struct {
	Status      string `json:"status"`      // "up", "down"
	Environment string `json:"environment"` // "sandbox", "development", "production"
}

// QueueMetrics represents River job queue metrics
type QueueMetrics struct {
	Pending           int64 `json:"pending"`
	Running           int64 `json:"running"`
	Failed            int64 `json:"failed"`
	CompletedLastHour int64 `json:"completed_last_hour"`
}

// SyncStatusMetrics represents Plaid sync status metrics
type SyncStatusMetrics struct {
	ActiveSyncs       int64 `json:"active_syncs"`
	FailedLastHour    int64 `json:"failed_last_hour"`
	AverageDurationMs int64 `json:"average_duration_ms"`
}

// DeadLetterMetrics represents dead letter queue metrics
type DeadLetterMetrics struct {
	Count             int64 `json:"count"`
	OldestJobAgeHours int64 `json:"oldest_job_age_hours"`
}
