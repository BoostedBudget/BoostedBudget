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
