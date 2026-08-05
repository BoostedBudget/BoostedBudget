// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/riverqueue/river"
)

// AccountDeletionArgs contains the arguments for an account deletion job.
// This job permanently deletes a user's account and all associated data
// after the GDPR grace period has passed.
type AccountDeletionArgs struct {
	// UserID is the ID of the user to delete
	UserID string `json:"user_id"`

	// RequestID is a deterministic, per-USER audit identifier (see
	// scheduled_deletion_check.go's construction, "scheduled-check-%s" keyed on user ID
	// alone) — NOT a per-request-episode identifier despite the field name. It is stable
	// BY DESIGN across every separate periodic check run that finds the same still-eligible
	// user, because AccountDeletionUniqueOpts() dedups via River's ByArgs (no ByPeriod),
	// which only matches enqueues whose full Args are byte-identical; a value that varied
	// per enqueue (e.g. embedding the enqueuing job's own ID or a timestamp) would defeat
	// that dedup unconditionally. Consequence: this field cannot distinguish two separate
	// deletion-request episodes for the same user (e.g. cancel-then-re-request) — it will be
	// identical for both.
	//
	// RETRACTION (Issue #1440, T0). This comment previously ended by asserting that the
	// episode distinction, and the guarantee that a stale job cannot delete a user who has since
	// cancelled, "comes from ClaimForDeletion's observed-value predicate on
	// deletion_requested_at". That was WRONG, and it was wrong in a way that mattered: no such
	// predicate existed. ClaimForDeletion took only a userID — no expected-timestamp parameter of
	// any kind — and its predicate authorised any user with no manual tier override at all, which
	// is true of every ordinary uncomped user whether they cancelled or not. There was therefore
	// NO guarantee here to describe: a cancelled user genuinely was hard-deleted by an
	// already-queued job, and a cancel-then-re-request genuinely was deleted before its new grace
	// period elapsed. The claim survived three reviews because each verified that ClaimForDeletion
	// existed and was called, not what it took as parameters.
	//
	// What actually provides the guarantee now: ClaimForDeletion takes an explicit
	// repository.DeletionClaimKind, and this job's worker passes DeletionKindUserRequested, whose
	// predicate requires deletion_requested_at IS NOT NULL AND deletion_scheduled_at <= NOW().
	// Cancellation NULLs both columns, so a stale job fails the claim and no-ops; a re-request
	// installs a future deletion_scheduled_at, so a stale job also fails until that window
	// elapses. The protection is in the claim's PREDICATE and the caller-declared kind — still not
	// in this field, which remains purely an audit label.
	RequestID string `json:"request_id,omitempty"`
}

// Kind returns the unique identifier for this job type.
//
// Issue #1440, T0 — deliberately UNCHANGED at "account_deletion", and Args deliberately unchanged
// byte-for-byte. Two reasons:
//
//  1. In-flight rows. A deploy can find queued/retryable `account_deletion` rows in QA or staging.
//     Registering a NEW kind and retiring this one would leave those rows with no registered
//     worker (River fails them until they exhaust attempts and dead-letter) while ALSO leaving
//     them unfixed. Keeping this kind and hard-wiring its worker to the STRICTER
//     user-requested claim predicate means every already-queued job gets the fix automatically,
//     with no drain step and no migration. That is safe because the predicate only ever REFUSES
//     claims it previously allowed: an in-flight job for a user who has not cancelled and whose
//     schedule has elapsed behaves exactly as before, and one for a user who has cancelled now
//     no-ops instead of deleting them.
//  2. Dedup. AccountDeletionUniqueOpts() dedups via ByArgs with no ByPeriod, so uniqueness holds
//     only while two enqueues' Args are byte-identical. Encoding the authorisation kind into Args
//     would have re-opened the previously-fixed dedup defect this file's own doc comments describe.
//     The kind is therefore passed in code (see AccountDeletionWorker.Work) rather than in Args.
//     TestScheduledDeletionCheckWorker_Work_SameUserAcrossRuns_ProducesIdenticalArgs still passes
//     untouched, which is the proof.
//
// There is consequently no second River job kind for retention-triggered deletion, and there must
// not be one: the retention path is not a job at all — RetentionCleanupCheckWorker calls
// DeletionService.ExecuteDeletion in-process (retention_cleanup_check.go). The split that closes
// the defect is between the two CLAIM AUTHORISATIONS, not between two job kinds.
func (AccountDeletionArgs) Kind() string { return "account_deletion" }

// AccountDeletionWorker processes account deletion jobs.
// This worker handles the final deletion of user accounts after the grace period.
type AccountDeletionWorker struct {
	river.WorkerDefaults[AccountDeletionArgs]

	deletionService services.DeletionService
	logger          *slog.Logger
}

// AccountDeletionWorkerDeps contains the dependencies for AccountDeletionWorker.
type AccountDeletionWorkerDeps struct {
	DeletionService services.DeletionService
	Logger          *slog.Logger
}

// NewAccountDeletionWorker creates a new account deletion worker.
func NewAccountDeletionWorker(deps AccountDeletionWorkerDeps) *AccountDeletionWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &AccountDeletionWorker{
		deletionService: deps.DeletionService,
		logger:          logger,
	}
}

// Work processes an account deletion job.
//
// The worker:
//  1. Validates the user is still scheduled for deletion (not cancelled)
//  2. Deletes all user data (transactions, accounts, budgets, rules, etc.)
//  3. Deletes the user profile
//  4. Logs the deletion for audit compliance
//
// Step 1 is not performed here — it is performed atomically inside ExecuteDeletion's claim, which
// is why this worker passes repository.DeletionKindUserRequested rather than checking anything
// itself. This job is only ever enqueued by ScheduledDeletionCheckWorker from
// GetUsersScheduledForDeletionPage, i.e. always on the strength of a user's own deletion request
// whose grace period has elapsed. That is the ONLY authorisation this worker may claim under;
// passing the retention kind here would authorise deleting a user who never asked to be deleted
// and whose retention clock has not expired.
//
// This is a critical operation - failures will be retried with exponential backoff.
// After max attempts, the job moves to dead letter queue for manual review.
func (w *AccountDeletionWorker) Work(ctx context.Context, job *river.Job[AccountDeletionArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting account deletion job",
		"job_id", job.ID,
		"user_id", args.UserID,
		"request_id", args.RequestID,
		"attempt", job.Attempt,
	)

	start := time.Now()
	defer func() {
		status := "success"
		if err != nil {
			status = "failed"
		}
		observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
		observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	}()

	// Execute the deletion through the service layer
	// This handles all cascading deletions and audit logging
	err = w.deletionService.ExecuteDeletion(ctx, args.UserID, repository.DeletionKindUserRequested)
	if err != nil {
		w.logger.Error("account deletion failed",
			"job_id", job.ID,
			"user_id", args.UserID,
			"request_id", args.RequestID,
			"error", err,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		return fmt.Errorf("failed to delete account: %w", err)
	}

	w.logger.Info("account deletion completed",
		"job_id", job.ID,
		"user_id", args.UserID,
		"request_id", args.RequestID,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return nil
}

// AccountDeletionUniqueOpts returns the recommended uniqueness options: intended to keep only one
// deletion job per user queued/running at a time.
//
// Batch S2 Task 3 correction: before this task, the SOLE production caller
// (ScheduledDeletionCheckWorker.Work, scheduled_deletion_check.go) built AccountDeletionArgs with
// a RequestID embedding the enqueuing job's own river job.ID and time.Now().Unix() — both differ
// on every single enqueue. Since ByArgs (below) is true with NO ByPeriod, River can only dedup
// two enqueues whose full Args are byte-identical; a RequestID that changes every time defeats
// this uniqueness constraint UNCONDITIONALLY, for every user, not as an edge case. Fixed by
// making RequestID a deterministic function of the user alone
// (see TestScheduledDeletionCheckWorker_Work_SameUserAcrossRuns_ProducesIdenticalArgs).
//
// Severity of the pre-fix bug was Medium, not Critical: ExecuteDeletion (deletion_service.go)
// acquires an independent work-claims lease (ActionAccountDeletion) BEFORE any destructive or
// external work, and ClaimForDeletion (profile_repository.go) atomically re-verifies eligibility
// at claim time — so a genuinely concurrent duplicate job blocks on the lease and a
// sequential duplicate (running after the first released its lease) simply no-ops rather than
// double-deleting. The real cost of the pre-fix bug was duplicate queued work, repeated
// external-service calls (Paddle cancel, Plaid disconnect — both run BEFORE the lease is released,
// so a duplicate whose lease-acquisition window doesn't overlap the first attempt's can still
// re-run them) per the interface doc on DeleteUserCascade, and log/error noise — never a second
// hard delete.
func AccountDeletionUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: true,
		// No time period - each user can only have one pending deletion
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}
