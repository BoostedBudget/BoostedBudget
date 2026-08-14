package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Application-level Prometheus metrics.
// HTTP metrics are handled by middleware/metrics.go.
// These cover business-logic and infrastructure events.

// UnmeasuredGauge declares a gauge that DOES NOT EXIST IN THE SCRAPE UNTIL SOMETHING HAS MEASURED IT.
//
// ⚠ WHY THIS IS NOT A promauto.NewGauge, and it is the whole point. A registered plain Gauge is
// exported from the instant the process starts, carrying its zero value — so every gauge fed by a
// periodic collector publishes a confident 0 for the entire window between process start and the
// collector's first successful read, and publishes it again forever if that read never succeeds. For
// a backlog gauge that means "there is no backlog" asserted by something that has not looked. For
// observability.DeletionResidueRecords, whose prescribed rule is `increase(...) > 0`, it is worse
// than a wrong number: the later 0 → N step LOOKS LIKE AN INCREASE, so a process restart fires an
// alert about an incident that did not happen. A metric that exists but does not yet represent what
// it claims is the exact failure these gauges were added to detect elsewhere.
//
// A GaugeVec with NO label names collects nothing until WithLabelValues() creates its single child,
// and that child is an unlabelled sample carrying the same metric name — so the scrape shape is
// byte-identical to a plain Gauge once published, and the family is simply ABSENT before then.
// `absent(metric_name)` therefore distinguishes "nobody has measured this" from "measured, and it is
// zero", which no plain Gauge can express. Publish with `X.WithLabelValues().Set(v)`.
//
// ⚠ THE CORRESPONDING OBLIGATION: every alert on such a gauge needs an absent()/staleness companion
// rule, because silence is now a possible state. Each declaration below spells its rules out — this
// repo has no alert-rule file to put them in.
func UnmeasuredGauge(opts prometheus.GaugeOpts) *prometheus.GaugeVec {
	return promauto.NewGaugeVec(opts, nil)
}

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

	// InvalidWebhookAttemptsTotal counts webhook deliveries rejected for missing or
	// cryptographically invalid signatures, by provider/site. Issue #1514: this is the audit
	// signal that survives now that invalid attempts are no longer persisted into the full
	// paddle_webhook_events / webhook_events tables (only bounded metadata is recorded — see
	// repository.InvalidWebhookAttemptRepository).
	//
	// Expected label values:
	//   paddle              - HandlePaddleWebhook rejected an unsigned/invalid delivery
	//   plaid               - HandlePlaidWebhook rejected an unsigned/invalid delivery
	//   paddle_worker_guard - SubscriptionWebhookWorker refused to dispatch a signature_valid=false
	//                         row it was handed (defense-in-depth; should be near-zero in practice)
	//   paddle_replay_guard - ReplayPaddleWebhook refused to replay a signature_valid=false row
	//   plaid_worker_guard  - WebhookProcessingWorker refused to dispatch a signature_valid=false
	//                         Plaid row it was handed (defense-in-depth; should be near-zero)
	//   plaid_legacy_poller_guard - the legacy WebhookProcessor poller's refuseIfInvalidSignature
	//                         refused to dispatch a signature_valid=false row (workers/webhook_processor.go).
	//                         That poller is unwired dead code (production uses the River worker
	//                         above instead), so this label should be permanently zero in production.
	InvalidWebhookAttemptsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "invalid_webhook_attempts_total",
		Help: "Number of webhook deliveries rejected for missing or invalid signature, by provider/site.",
	}, []string{"provider"})

	// PaddleWebhookRejectionsTotal counts PaddleWebhookService.VerifyWebhook rejections by
	// reason (issue #1514 fix-round-1 item 2). InvalidWebhookAttemptsTotal above records the
	// SAME rejections but generically (label "paddle", no reason breakdown) at the handler
	// layer — that counter alone cannot tell a genuine forged signature apart from a stale/
	// future timestamp (checkTimestampTolerance) or a malformed header (parseSignature/
	// parseTimestamp), so a slow-onset host clock-skew incident — every delivery's ts= now
	// legitimately outside the tolerance window — would look identical to an attack, and
	// nobody would know to check the clock instead of waiting for an attacker to give up.
	//
	// Expected label values:
	//   invalid_format             - parseSignature/parseTimestamp rejected the header shape
	//                                or a non-numeric/negative/overflow ts=
	//   timestamp_out_of_tolerance - a well-formed ts= outside the configured tolerance window
	//                                (checkTimestampTolerance) — sustained non-zero here, with
	//                                signature_mismatch flat, is the clock-skew signal: real
	//                                Paddle deliveries being dropped, not forged ones
	//   signature_mismatch         - a well-formed, in-window ts= but no h1 candidate matched
	//                                the calculated HMAC — the "somebody sent a bad signature"
	//                                case
	PaddleWebhookRejectionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "paddle_webhook_rejections_total",
		Help: "Total PaddleWebhookService.VerifyWebhook rejections by reason (invalid_format, timestamp_out_of_tolerance, signature_mismatch). A sustained rate of timestamp_out_of_tolerance with signature_mismatch flat suggests host clock skew silently dropping genuine Paddle deliveries, not an attack.",
	}, []string{"reason"})

	// RateLimitIdentitySentinelFallbackTotal counts requests that reached the
	// rate limiter with GateStatusPassed (i.e. CloudflareOnly already verified
	// the immediate peer and the origin secret) but presented a missing,
	// duplicated, or malformed Cf-Connecting-Ip header. Issue #1514: rather than
	// trust an unverifiable value as identity, these requests are keyed to a
	// single fixed sentinel bucket instead — this metric is the corresponding
	// observability signal, since that bucket is deliberately coarse (shared,
	// globally throttled) and should be near-zero in practice. If it starts
	// climbing, Cloudflare has stopped sending Cf-Connecting-Ip as expected and
	// needs investigation.
	RateLimitIdentitySentinelFallbackTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rate_limit_identity_sentinel_fallback_total",
		Help: "Number of Cloudflare-gated requests that fell back to the shared rate-limit sentinel bucket due to a missing/duplicated/malformed Cf-Connecting-Ip header.",
	})

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
	//
	// ⚠ AN UnmeasuredGauge (see above): absent until startJobHealthMetricsCollector's first successful
	// read, rather than publishing a reassuring 0 from process start. Rules:
	//
	//	manual_tier_override_oldest_uncleared_expired_seconds > 86400
	//	  for: 1h
	//	absent(manual_tier_override_oldest_uncleared_expired_seconds)
	//	  for: 1h
	ManualTierOverrideOldestUnclearedExpiredSeconds = UnmeasuredGauge(prometheus.GaugeOpts{
		Name: "manual_tier_override_oldest_uncleared_expired_seconds",
		Help: "Age in seconds of the oldest expired-but-uncleared manual_tier_override; 0 when none exist. Absent until the collector has actually read the database, so alert on absence too.",
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
	//
	// ⚠ BOTH ARE UnmeasuredGauges (see above). Healthy is 0 for both — which is exactly why they must
	// not be exported as 0 before anything has looked: "no outstanding cleanup" is the reading an
	// operator is most likely to trust without checking when it was last refreshed. Rules:
	//
	//	plaid_cleanup_outstanding_users > 0
	//	  for: 24h
	//	absent(plaid_cleanup_outstanding_users)
	//	  for: 1h
	PlaidCleanupOutstandingUsers = UnmeasuredGauge(prometheus.GaugeOpts{
		Name: "plaid_cleanup_outstanding_users",
		Help: "Number of users with outstanding Plaid-disconnect cleanup (plaid_cleanup_pending_at set, with at least one live linked item); 0 when none exist. Absent until the collector has actually read the database, so alert on absence too.",
	})
	PlaidCleanupOldestOutstandingSeconds = UnmeasuredGauge(prometheus.GaugeOpts{
		Name: "plaid_cleanup_oldest_outstanding_seconds",
		Help: "Age in seconds of the oldest outstanding Plaid-disconnect cleanup (plaid_cleanup_pending_at with a live linked item); 0 when none exist. Absent until the collector has actually read the database, so alert on absence too.",
	})

	// DeletionResidueRecords and DeletionResidueNewestSeconds are Issue #1504's alerting half. One
	// residue record means a GDPR deletion ISSUED an irreversible external call against the user's
	// Paddle account — the subscription cancel, or (since round 15's finding A1) the customer
	// archive-and-anonymise, which is the one act this codebase describes as unrecoverable — and the
	// run then ended with the user still here: they keep their account and all their data, and
	// something was done to them that they did not ask for. Nothing automated can restore it —
	// re-subscribing somebody automatically would be a worse failure than telling a human, which is
	// why this is a metric and not a repair job.
	//
	// Both are refreshed together from repository.JobHealthRepository.GetDeletionResidueStats (see
	// cmd/server/main.go's startJobHealthMetricsCollector, for the same import-cycle reason the two
	// gauges above live there), and the same numbers appear on /admin/health via
	// domain.DeletionResidueMetrics — one query, one derivation, so the dashboard and the alert cannot
	// come to disagree.
	//
	// ⚠ ALERT ON ANY INCREASE, NOT ON ANY NON-ZERO VALUE. This gauge is monotonic by construction:
	// nothing resolves a residue record, nothing prunes audit_logs, and no column anywhere records that
	// the affected user was made whole (inventing one would be recording a claim as a proof). So a
	// permanently non-zero DeletionResidueRecords is the expected steady state after the first
	// occurrence, and `> 0` would be an alert that fires forever and gets muted. `increase(...) > 0` is
	// the actionable signal, and DeletionResidueNewestSeconds is its human-readable companion: a small
	// value means it just happened.
	//
	// ⚠ AND THAT IS WHY BOTH ARE UnmeasuredGauges (see above) RATHER THAN plain Gauges. An
	// increase()-based rule is uniquely poisoned by a startup zero: as plain Gauges these two were
	// exported as 0 from process start and only replaced on startJobHealthMetricsCollector's first
	// successful read, so on any process with pre-existing residue the 0 → N step LOOKED LIKE N NEW
	// INCIDENTS and the prescribed rule fired on a restart. Absent-until-measured removes the
	// fabricated sample entirely: increase() over a series with no prior sample produces nothing, so a
	// restart is silent and the first real change is the first alert. The collector now also performs
	// one read BEFORE its ticker starts, so the absent window is the length of a query rather than five
	// minutes — and if that read fails these stay absent rather than publishing a zero, because a zero
	// here means "no GDPR deletion has ever been left with a cancelled subscription", which a failed
	// read has established nothing about.
	//
	// ⚠ DeletionResidueNewestSeconds IS 0 BOTH WHEN THERE ARE NO RECORDS AND WHEN ONE WAS JUST WRITTEN.
	// Any rule using it must gate on DeletionResidueRecords > 0 first. Unlike
	// PlaidCleanupOldestOutstandingSeconds above, a zero here is not unambiguous good news.
	//
	// The intended rules, given this repo has no alert-rule file (see PlaidOrphanedItemsByAdminState
	// below for the same statement and the same reason):
	//
	//	# A NEW residue record: somebody's subscription was cancelled and their deletion then revoked.
	//	increase(deletion_residue_records[15m]) > 0
	//	# The collector has stopped reporting — unknown is not zero, and this rule is what makes the
	//	# absent-until-measured design safe rather than merely quiet.
	//	absent(deletion_residue_records)
	//	  for: 1h
	//
	// ⚠ NO `for:` ON THE FIRST RULE, deliberately. increase() over this gauge is a transient: it
	// returns to 0 once the step leaves the window, so a `for:` longer than the window would
	// guarantee the alert never fires at all.
	//
	// ⚠ ROUND 15: THIS GAUGE IS NO LONGER MONOTONIC, AND THAT IS A FEATURE. Residue records are now
	// written when each irreversible external call is ISSUED and retracted by DeleteUserCascade
	// inside the transaction that deletes the user, so a run that failed and was later retried
	// successfully makes the count go DOWN — the work item was resolved. No reader ever observes an
	// in-flight ordinary deletion's rows, because the write and the retraction are separated by a
	// commit boundary, not by a scrape interval. Practical consequence for anyone writing the rule:
	// `increase()` still catches every new incident, and a drop means "an operator's work item went
	// away by itself", which is the outcome the mechanism wants and not a signal to alert on.
	DeletionResidueRecords = UnmeasuredGauge(prometheus.GaugeOpts{
		Name: "deletion_residue_records",
		Help: "Number of OPEN records that a GDPR deletion issued an irreversible external Paddle call — a subscription cancel or a customer archive-and-anonymise — against a user who then survived the run (audit_logs action prefix user.deletion_residue.). Every one is a user who kept their account and had something done to their Paddle account that they did not ask for. NOT monotonic: a later run that completes the deletion retracts its own records. Alert on increase(), not on > 0. Absent until the collector has actually read the database (never a startup zero, which increase() would misread as new incidents), so alert on absence too.",
	})
	DeletionResidueNewestSeconds = UnmeasuredGauge(prometheus.GaugeOpts{
		Name: "deletion_residue_newest_seconds",
		Help: "Age in seconds of the MOST RECENT deletion-residue record; 0 both when none exist and when one was just written, so gate on deletion_residue_records > 0 before reading it. Absent until the collector has actually read the database.",
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

	// HouseholdOwnerDeletionBlockedTotal counts every time an erasure was refused because the
	// target is a household owner. It is incremented in TWO places, and that is not a double
	// count: DeletionService.ExecuteDeletion's pre-cleanup precheck (deletion_service.go) fires
	// it for the ordinary case, before any external effect is spent, and DeleteUserCascade
	// (profile_repository.go) fires it for the race the precheck cannot see — an invite accepted
	// between the precheck's read and the cascade's FOR UPDATE. A single occasion trips exactly
	// one of them, because a run that the precheck refuses never reaches the cascade at all.
	//
	// Either way the refusal is there because hard-deleting the owner would
	// CASCADE away another real user's data — households.owner_user_id CASCADEs the household row
	// itself (taking every other active member's household_members row with it), and
	// subscription_entitlements.payer_user_id CASCADEs away any entitlement this owner funds for
	// someone else, live membership or not. Issue #1440 Critical finding (Codex review): the #1367
	// owner guard (domain.ErrHouseholdHasActiveMembers) previously lived ONLY in
	// DeletionService.RequestDeletion, the user-initiated request path — ExecuteDeletion's other two
	// callers (the retention-triggered deleteExpiredAccounts, and the legacy
	// DeletionWorker/AccountDeletionWorker paths) reached DeleteUserCascade with no guard at all.
	//
	// This is now a genuine error (not a silent, expected (false, nil) skip like a lapsed comp or an
	// un-cleaned Plaid item) — ExecuteDeletion propagates it, so it is logged at Error by every
	// caller and, for the River-job paths, retried and eventually visible in the dead-letter queue
	// (the System Health Dashboard's DLQ metrics already surface this). A blocked deletion here
	// requires a human to resolve the household situation (transfer ownership, remove members, or
	// reconcile the stale entitlement) — it cannot self-heal, so alert on ANY sustained non-zero
	// rate: it means a GDPR deletion deadline is going unmet for a real, specific household.
	HouseholdOwnerDeletionBlockedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "household_owner_deletion_blocked_total",
		Help: "Total account erasures refused because the target is a household owner whose CASCADE would destroy another real user's household_members or subscription_entitlements data — counted at ExecuteDeletion's pre-cleanup precheck, or inside DeleteUserCascade for the race that precheck cannot see. Requires human resolution (ownership transfer, member removal, or entitlement reconciliation) — alert on any sustained non-zero rate.",
	})

	// OrphanedFKlessUserScopedRowsCount is Issue #1440 round-3 Finding 5's monitoring gauge: the
	// current count, per table, of rows in the three user-scoped tables with NO foreign key to users
	// at all (dead_letter_jobs, idempotency_keys, mcp_audit_log) whose user_id/actor_user_id no
	// longer names any real user — see profileRepository.GetOrphanedFKlessUserScopedRowCounts's own
	// doc comment for the full race this reports (a concurrent writer landing in the narrow window
	// between DeleteUserCascade's own erasure of these tables and its transaction's commit; none of
	// the three has any FK to users, so DeleteUserCascade's own row lock never blocks such a write).
	//
	// A GaugeVec labeled by table, mirroring MalformedManualTierOverrideCount's own gauge shape
	// (current outstanding count each pass observes, not a cumulative total of every orphan ever
	// seen). idempotency_keys is expected to self-heal to 0 within 24h regardless, via
	// IdempotencyCleanupWorker's independent TTL sweep — a sustained non-zero reading there points at
	// that sweep itself failing, not (only) this race. dead_letter_jobs and mcp_audit_log have no
	// such TTL backstop, so any non-zero reading for either is the sole visible signal that a row
	// needs manual cleanup.
	OrphanedFKlessUserScopedRowsCount = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "orphaned_fkless_user_scoped_rows_count",
		Help: "Current count, per table, of rows in dead_letter_jobs/idempotency_keys/mcp_audit_log whose user_id/actor_user_id no longer names a real user — residue of a concurrent write racing DeleteUserCascade's own erasure of these FK-less tables. idempotency_keys self-heals within 24h via its own TTL sweep; the other two do not and require manual cleanup.",
	}, []string{"table"})

	// PlaidOrphanedItemsByAdminState is Issue #1497 item 4's state gauge: how many
	// plaid_orphaned_items rows are currently in each domain.OrphanedItemAdminState, labeled by
	// state. Published by RetentionCleanupCheckWorker.monitorOrphanedPlaidItems (Step 0e) on every
	// pass, from the SAME derivation (domain.SummarisePlaidOrphanedItems) that backs
	// /admin/health's plaid_orphaned_items block — one derivation, so a dashboard and an alert
	// cannot disagree about how many Items need a human.
	//
	// ⚠ LABELED BY STATE, NOT ONE "UNRESOLVED" NUMBER, and the label is the whole point.
	// admin_state="manual_intervention_required" is a Plaid Item that NOTHING AUTOMATED WILL EVER
	// FINISH and that may still be BILLING an erased user's bank account; somebody has to remove it
	// in Plaid's own dashboard using plaid_item_id. admin_state="auto_retrying" is the opposite
	// instruction — a sweep holds a live credential and is working on it, so acting at Plaid now
	// duplicates work in flight. admin_state="resolved" is history. Collapsing these into one
	// unresolved total would make the gauge worse than absent, because an operator would believe
	// they had been told something.
	//
	// ⚠ admin_state="operator_attested" (Issue #1497 item 2) IS NOT admin_state="resolved", and a
	// dashboard that sums them is lying by arithmetic. "resolved" counts Items Plaid itself confirmed
	// gone; "operator_attested" counts Items a colleague SAID they removed by hand, verified by
	// nothing. The sweep keeps re-attempting attested rows precisely so that a true claim eventually
	// becomes a "resolved" one — so a row that STAYS attested for a long time is a claim no retry has
	// ever been able to confirm, and is worth an operator's eye even though it is not an alert.
	//
	// ⚠ EVERY STATE MUST BE Set() BY THE PUBLISHER. A GaugeVec label that is never set does not
	// appear in the scrape at all, so a state added to domain.OrphanedItemAdminState and not added to
	// monitorOrphanedPlaidItems' by-hand enumeration disappears silently rather than failing to
	// build. TestRetentionCleanupCheck_MonitorOrphanedPlaidItems_PublishesTheOperatorAttestedState
	// asserts the published states sum to the population, which is what notices.
	//
	// ⚠ A GAUGE, NOT A COUNTER, mirroring MalformedManualTierOverrideCount and
	// OrphanedFKlessUserScopedRowsCount above: this is how many rows are in each state RIGHT NOW,
	// each pass. A counter would only ever grow and would misrepresent one persistent stuck Item as
	// "many failures" the longer it went unnoticed — and, worse here, would never clear after an
	// operator actually fixed something.
	//
	// ⚠ THIS REPO HAS NO ALERT-RULE FILE (no Prometheus/Alertmanager rule group, no PrometheusRule
	// manifest, no scrape config is checked in), so the alerting intent lives here and in the Help
	// string, as it does for HouseholdOwnerDeletionBlockedTotal above. The intended rules:
	//
	//	# Somebody must act at Plaid; nothing automated will finish these.
	//	plaid_orphaned_items_by_admin_state{admin_state="manual_intervention_required"} > 0
	//	  for: 1h
	//	# The publishing pass itself has stopped reporting — unknown is not zero.
	//	absent(plaid_orphaned_items_by_admin_state{admin_state="manual_intervention_required"})
	//	  for: 48h
	//
	// The second rule is load-bearing: monitorOrphanedPlaidItems deliberately publishes NOTHING on a
	// failed or truncated read rather than publishing a reassuring 0, so staleness/absence is the
	// only signal that the check itself is broken.
	PlaidOrphanedItemsByAdminState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "plaid_orphaned_items_by_admin_state",
		Help: "Current count of plaid_orphaned_items rows per admin state. admin_state=\"manual_intervention_required\" means a Plaid Item survived a GDPR erasure, nothing automated can finish removing it, and it may still be billing — a human must remove it in Plaid's dashboard; alert on any sustained non-zero. auto_retrying means a sweep holds a live credential and is working on it (no action). resolved is history — Plaid confirmed the Item gone. operator_attested is an UNVERIFIED human claim that somebody removed the Item by hand; it is deliberately not the same number as resolved and must never be summed into it, and a row that stays attested rather than becoming resolved is a claim no retry has managed to confirm. Not published at all on a failed or over-cap read, so alert on absence too.",
	}, []string{"admin_state"})

	// PlaidOrphanedItemMaxRetryAttempts is Issue #1497 item 4's re-attempt gauge — THE METRIC THAT
	// ITEM EXISTS FOR, and the one two earlier passes over #1497 both missed.
	// plaid_orphaned_items.retry_attempt_count has been incremented by
	// plaidOrphanedItemRepository.RecordRetryFailure since #1497 landed and, until this gauge, was
	// read by no metric anywhere: jobs/plaid_orphan_retry.go emits only the generic JobsCompleted /
	// JobDuration every River worker emits, so an Item could be re-attempted every day forever with
	// nothing reporting it.
	//
	// ⚠ THE MAXIMUM OVER UNRESOLVED ROWS, and every word of that is deliberate.
	//   * MAXIMUM, not a sum or a mean: the question is "is something being retried into the
	//     ground?", which is a property of the WORST row. A sum grows with population size and a
	//     mean is diluted by healthy rows, so both can hide one Item that has failed 200 times.
	//   * UNRESOLVED, because a resolved row's attempt history is finished. Letting it drive this
	//     gauge would latch an alert forever on work that actually succeeded — the "mark outlives
	//     the work" defect this branch has closed five times, in alerting form.
	//
	// ⚠ A HIGH VALUE IS NOT A STATE. domain.PlaidOrphanedItem.AdminState deliberately ignores
	// RetryAttemptCount, because nothing in this system gives up on an attempt count: a row on
	// attempt 200 with a live credential really is still auto-retrying. So this gauge answers a
	// question the state cannot ("is the retrying futile?"), and the two must not be conflated.
	//
	// Intended rule, given this repo has no alert-rule file (see PlaidOrphanedItemsByAdminState):
	//
	//	# Re-attempts are piling up on an Item the sweep cannot finish.
	//	plaid_orphaned_item_max_retry_attempts > 20
	//	  for: 6h
	//
	// The threshold is a judgement, not a derivation: the retry sweep runs daily, and
	// domain.OrphanedItemTokenRetention is 30 days, so ~30 is the most attempts any single row can
	// ever accumulate before its credential expires and the row becomes
	// manual_intervention_required anyway. 20 therefore fires with roughly a third of the retention
	// window left — early enough for a human to act at Plaid before the automated path runs out.
	//
	// ⚠ AN UnmeasuredGauge, AND IT IS THE SIBLING OF PlaidOrphanedItemsByAdminState THAT WAS NOT
	// SAFE. That GaugeVec has always been absent until published, because a Vec collects nothing
	// until a label set is created — which is why monitorOrphanedPlaidItems' claim that it publishes
	// NOTHING on a failed or truncated read, and the `absent(...)` rule above that depends on the
	// claim, were true of it. As a plain Gauge, this one was the exception: it was exported as 0 from
	// process start and on every failed or over-cap read before the first successful pass, so it
	// asserted "no Item is being retried into the ground" with nothing having looked. And its window
	// is the WORST of any gauge here — monitorOrphanedPlaidItems runs on the retention worker's DAILY
	// cadence, not the five-minute job-health collector's, so a process that restarts more often than
	// once a day could report a permanent, entirely fabricated 0. Its `for: 6h` clock is reset by each
	// restart on top of that. Making it absent-until-measured is what makes the pass's documented
	// behaviour actually true of both its gauges, and the corresponding rule is:
	//
	//	absent(plaid_orphaned_item_max_retry_attempts)
	//	  for: 48h
	//
	// matching the cadence-aware window already prescribed for PlaidOrphanedItemsByAdminState. No
	// startup read is added for this one: unlike the job-health collector there is no process-start
	// hook here — the publisher is a scheduled River job — so the honest fix is silence until the
	// first pass, not a synthetic reading at boot.
	PlaidOrphanedItemMaxRetryAttempts = UnmeasuredGauge(prometheus.GaugeOpts{
		Name: "plaid_orphaned_item_max_retry_attempts",
		Help: "Highest retry_attempt_count among UNRESOLVED plaid_orphaned_items rows — the worst single Plaid Item whose automated removal keeps failing after its owner's GDPR erasure. The maximum (not a sum or mean) because the signal is one Item retried into the ground; unresolved-only so a succeeded row's history cannot latch the alert. The daily sweep plus the 30-day credential retention caps any row near 30, so a reading above ~20 means a human should act at Plaid before the automated path expires. Absent until the daily pass has actually read the table, so alert on absence too.",
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
