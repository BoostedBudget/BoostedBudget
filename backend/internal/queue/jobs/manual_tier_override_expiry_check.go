// Package jobs contains River job definitions and workers for async processing.
// Issue #1413 Task 9: Manual tier override expiry check — the time-triggered twin of the admin
// "remove override" endpoint (Issue #1414 / Task 7). HasActiveManualOverride() derives comp
// state at READ time only: once manual_tier_ends_at passes, reads simply start reporting the
// base tier/status again, but nothing fires to (1) re-arm a retention clock a comp had paused,
// (2) re-sync Clerk, or (3) audit the lapse. This job closes that gap by running the SAME
// idempotent transition RemoveUserTierOverride performs for admin-initiated removal, once a
// day, for every override that has lapsed on its own.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// expireManualTierOverrideReason is the audit-log "reason" recorded for every system-initiated
// expiry, distinguishing it (alongside the NULL actor) from an admin-supplied reason string.
const expireManualTierOverrideReason = "manual tier override expired"

// ManualTierOverrideExpiryCheckArgs contains the arguments for the manual tier override expiry
// check job. This job runs daily to find users whose manual_tier_override has lapsed
// (manual_tier_ends_at has passed) and perform the same clear/re-arm-retention/audit transition
// the admin "remove override" endpoint performs for a manual removal.
type ManualTierOverrideExpiryCheckArgs struct {
	// TriggerSource indicates what triggered this check (e.g., "periodic", "manual")
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (ManualTierOverrideExpiryCheckArgs) Kind() string { return "manual_tier_override_expiry_check" }

// ManualTierOverrideProfileRepository defines the repository method needed to find users whose
// manual tier override has expired.
type ManualTierOverrideProfileRepository interface {
	// GetUsersWithExpiredManualTierOverride returns users whose manual_tier_override is set and
	// whose manual_tier_ends_at has strictly passed.
	GetUsersWithExpiredManualTierOverride(ctx context.Context) ([]*domain.User, error)
}

// ManualTierOverrideRemover is the single admin-repository method this job reuses to perform the
// exact same idempotent clear-override/re-arm-retention/audit transition
// AdminUserRepository.RemoveUserTierOverrideIfMatches implements for the CAS-guarded removal path
// (Issue #1414 / Task 7; Issue #1413 High finding H4, Codex review round 3) — this job is the
// time-triggered twin, not a new policy, so it calls the same method rather than duplicating its
// CTE. It uses the CAS-guarded variant (not the unconditional RemoveUserTierOverride the admin
// "Remove Override" button calls) because this worker's candidate list can go stale between
// GetUsersWithExpiredManualTierOverride's select and this call: an admin who extends or replaces
// the override in that window must not have their fresh action clobbered by this worker's
// now-stale decision.
//
// Known, deliberate limitation (documented rather than worked around by duplicating SQL): the
// underlying query hardcodes the audit action as 'remove_manual_tier'. An expiry-specific action
// (e.g. 'expire_manual_tier') would require adding an action parameter to
// AdminUserRepository.RemoveUserTierOverrideIfMatches, which lives in admin_user_repository.go —
// a file owned by a different, concurrently-in-flight task in this plan round, and out of this
// job's scope fence. Every automatic expiry therefore audits as 'remove_manual_tier',
// distinguishable from an admin-initiated removal only by its NULL actor (see
// expireManualTierOverrideReason).
type ManualTierOverrideRemover interface {
	RemoveUserTierOverrideIfMatches(ctx context.Context, actorUserID, targetUserID, reason string, retentionMonths int, expectedOverride *string, expectedEndsAt *time.Time) (repository.RemoveTierOverrideRow, bool, error)
}

// ManualTierOverrideExpiryCheckWorker processes manual tier override expiry check jobs.
type ManualTierOverrideExpiryCheckWorker struct {
	river.WorkerDefaults[ManualTierOverrideExpiryCheckArgs]

	profileRepo     ManualTierOverrideProfileRepository
	remover         ManualTierOverrideRemover
	clerkService    ClerkService
	retentionMonths int
	logger          *slog.Logger
}

// ManualTierOverrideExpiryCheckWorkerDeps contains the dependencies for
// ManualTierOverrideExpiryCheckWorker.
type ManualTierOverrideExpiryCheckWorkerDeps struct {
	ProfileRepo     ManualTierOverrideProfileRepository
	Remover         ManualTierOverrideRemover
	ClerkService    ClerkService // optional — nil-safe, matches SubscriptionWebhookWorker's convention
	RetentionMonths int
	Logger          *slog.Logger
}

// NewManualTierOverrideExpiryCheckWorker creates a new manual tier override expiry check worker.
func NewManualTierOverrideExpiryCheckWorker(deps ManualTierOverrideExpiryCheckWorkerDeps) *ManualTierOverrideExpiryCheckWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	retentionMonths := deps.RetentionMonths
	if retentionMonths <= 0 {
		retentionMonths = 6 // Default: 6-month retention period
	}
	return &ManualTierOverrideExpiryCheckWorker{
		profileRepo:     deps.ProfileRepo,
		remover:         deps.Remover,
		clerkService:    deps.ClerkService,
		retentionMonths: retentionMonths,
		logger:          logger,
	}
}

// Work processes a manual tier override expiry check job.
//
// For each candidate returned by the repository query:
//  1. Belt-and-braces guard: re-check domain.User.HasActiveManualOverride() (mirrors
//     retention_cleanup_check.go's comped-user guard) — protects against a stale candidate list
//     when an admin extends/re-applies the override between the query and this loop, and keeps
//     boundary agreement with the read path (an override ending exactly now is still active).
//  2. Call RemoveUserTierOverrideIfMatches with a system (empty) actor (coerced to SQL NULL) and
//     the override/expiry values observed on THIS candidate at select time — the CAS-guarded
//     sibling of RemoveUserTierOverride (Issue #1413 High finding H4, Codex review round 3). The
//     guard in step 1 above is a cheap, in-memory pre-check; it cannot by itself close the race,
//     since the query, the guard, and this call are not atomic with each other — an admin can
//     still extend/replace the override in the gap between step 1 and this call landing. The CAS
//     is what actually closes it: claimed=false means the row no longer matches what step 1 saw,
//     so this iteration must skip rather than clobber a fresher admin action.
//  3. Best-effort re-sync Clerk to the reverted tier/status — only when claimed.
//
// One user's failure is logged and does not stop the batch; a non-empty error count causes the
// job to return an error so River retries (failed users are naturally re-selected next run).
// claimed=false is NOT an error — it's a normal, expected skip per the claim-then-act pattern; the
// user is left untouched and, if still genuinely expired, is re-selected and retried next run.
// A second pass over an already-cleared user is a no-op: the repository query no longer returns
// them (manual_tier_override is NULL).
func (w *ManualTierOverrideExpiryCheckWorker) Work(ctx context.Context, job *river.Job[ManualTierOverrideExpiryCheckArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting manual tier override expiry check",
		"job_id", job.ID,
		"trigger_source", args.TriggerSource,
		"attempt", job.Attempt,
	)

	start := time.Now()
	defer func() {
		status := "success"
		if err != nil {
			status = "failed"
		} else {
			// Issue #1413 Task P4: generic last-success heartbeat — lets an alert fire on
			// "this job has not succeeded in > 48h", which jobs_completed_total{status="failed"}
			// alone cannot: that counter never increments at all if the job stops running
			// entirely (crashes before Work() runs, or sits dead-lettered with no replay).
			observability.RecordJobSuccess(job.Kind)
		}
		observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
		observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	}()

	users, fetchErr := w.profileRepo.GetUsersWithExpiredManualTierOverride(ctx)
	if fetchErr != nil {
		err = fmt.Errorf("failed to fetch users with expired manual tier override: %w", fetchErr)
		w.logger.Error("failed to fetch users with expired manual tier override",
			"job_id", job.ID, "error", fetchErr)
		return err
	}

	if len(users) == 0 {
		w.logger.Info("no users with expired manual tier override", "job_id", job.ID)
		return nil
	}

	w.logger.Info("found users with expired manual tier override", "job_id", job.ID, "user_count", len(users))

	var expiredCount, errorCount int
	for _, user := range users {
		if user.HasActiveManualOverride() {
			w.logger.Info("skipping manual tier override expiry — override is still active",
				"job_id", job.ID, "user_id", user.ID)
			continue
		}

		row, claimed, removeErr := w.remover.RemoveUserTierOverrideIfMatches(ctx, "", user.ID,
			expireManualTierOverrideReason, w.retentionMonths, user.ManualTierOverride, user.ManualTierEndsAt)
		if removeErr != nil {
			w.logger.Error("failed to expire manual tier override",
				"job_id", job.ID, "user_id", user.ID, "error", removeErr)
			errorCount++
			continue
		}
		if !claimed {
			// An admin extended or replaced the override between the select and this call
			// (Issue #1413 High finding H4) — a legitimate skip, not a failure. The user is
			// re-selected and re-evaluated next run if still genuinely expired then.
			w.logger.Info("manual tier override expiry claim lost — admin changed override concurrently, skipping",
				"job_id", job.ID, "user_id", user.ID)
			continue
		}

		w.syncClerkMetadata(ctx, job.ID, user.ID, row)
		expiredCount++
	}

	w.logger.Info("manual tier override expiry processed",
		"job_id", job.ID, "expired", expiredCount, "errors", errorCount)
	if errorCount > 0 {
		err = fmt.Errorf("manual tier override expiry: %d of %d expirations failed (see logs for details)", errorCount, len(users))
		// Issue #1413 C1 follow-up ("unbounded exclusion"): this job clearing manual_tier_override
		// is the ONLY thing that bounds how long noManualOverrideAtAllPredicate can delay a
		// retention notice/deletion. This job runs with PeriodicJobOpts{RunOnStart: false} and a
		// failed run's retries eventually land in the dead-letter queue with no automatic replay —
		// so if this job silently stops working, that "delay" becomes indefinite ("never deleted",
		// not "delayed by a day"). An explicit ERROR-level log here, on top of the per-user Error
		// logs already emitted above and the job_kind-labeled jobs_completed_total{status="failed"}
		// metric the deferred func at the top of Work() already increments (the repo's established
		// River-job observability mechanism — see the observability skill), is what keeps a FAILING
		// run observable. Neither of those, by itself, catches the job simply not running at all
		// (crashed before Work() started, dropped from the schedule, or dead-lettered with no
		// replay) — that silent-stop case is what Task P4's two additions cover instead:
		// observability.JobLastSuccessTimestamp (the deferred func's success branch calls
		// RecordJobSuccess) lets an alert fire on "hasn't succeeded in > 48h", and
		// repository.JobHealthRepository.GetOldestUnclearedExpiredManualTierOverrideAge (wired to
		// a gauge in main.go) is the direct measure of the resulting hazard, independent of
		// whether this job's OWN metrics are even being scraped.
		w.logger.Error("manual tier override expiry check completed with failures",
			"job_id", job.ID, "errors", errorCount, "total", len(users))
	}
	return err
}

// syncClerkMetadata best-effort re-syncs Clerk to the reverted tier/status returned by
// RemoveUserTierOverride. Logs and swallows errors — matches
// AdminUserService.RemoveUserTierOverride's own sync semantics (Issue #1413 Task 7), and this
// package's existing nil-safe ClerkService convention (SubscriptionWebhookWorker).
func (w *ManualTierOverrideExpiryCheckWorker) syncClerkMetadata(ctx context.Context, jobID int64, userID string, row repository.RemoveTierOverrideRow) {
	if w.clerkService == nil || row.ClerkUserID == "" {
		return
	}
	scratch := &domain.User{
		SubscriptionTier:   row.RevertedTier,
		SubscriptionStatus: row.RevertedStatus,
	}
	if syncErr := w.clerkService.UpdateUserMetadata(ctx, row.ClerkUserID, scratch.ClerkPublicMetadata()); syncErr != nil {
		w.logger.Warn("failed to sync reverted tier to clerk metadata",
			"job_id", jobID, "user_id", userID, "clerk_user_id", row.ClerkUserID, "error", syncErr)
	}
}

// ManualTierOverrideExpiryCheckUniqueOpts returns the recommended uniqueness options.
// Only one check job should run at a time to prevent duplicate expiry processing.
func ManualTierOverrideExpiryCheckUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: false, // All check jobs are equivalent
		// 23 hours period — avoids overlap on daily schedule
		ByPeriod: 23 * time.Hour,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}

// ManualTierOverrideExpiryCheckInsertOpts returns the *river.InsertOpts this job's periodic
// registration in main.go must use — Queue: "low" plus the uniqueness options from
// ManualTierOverrideExpiryCheckUniqueOpts(), converted from this package's queue-runner-agnostic
// queue.UniqueOpts to the river.UniqueOpts type river.InsertOpts.UniqueOpts actually requires
// (the same conversion internal/queue/river/client.go's toRiverUniqueOpts performs for the
// general-purpose enqueue path).
//
// Issue #1413 G3: main.go's periodic-job registration previously passed only
// &river.InsertOpts{Queue: "low"} — a hand-written literal that never called
// ManualTierOverrideExpiryCheckUniqueOpts() at all, so the dedup that function documents (and
// TestManualTierOverrideExpiryCheckUniqueOpts asserts) was never actually enforced in
// production. main.go now calls THIS function directly instead of hand-writing the InsertOpts
// literal, so main.go is no longer a second, independently-maintained expression of the same
// shape (unlike this package's other periodic jobs' *UniqueOpts() helpers, which main.go still
// hand-duplicates inline) — a future change to ManualTierOverrideExpiryCheckUniqueOpts()
// automatically propagates to what actually ships.
func ManualTierOverrideExpiryCheckInsertOpts() *river.InsertOpts {
	unique := ManualTierOverrideExpiryCheckUniqueOpts()

	states := make([]rivertype.JobState, len(unique.ByState))
	for i, s := range unique.ByState {
		states[i] = rivertype.JobState(s)
	}

	return &river.InsertOpts{
		Queue: "low",
		UniqueOpts: river.UniqueOpts{
			ByArgs:   unique.ByArgs,
			ByPeriod: unique.ByPeriod,
			ByQueue:  unique.ByQueue,
			ByState:  states,
		},
	}
}
