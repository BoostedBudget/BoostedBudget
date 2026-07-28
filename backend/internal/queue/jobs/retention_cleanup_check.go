// Package jobs contains River job definitions and workers for async processing.
// Issue #930: Retention cleanup check — 6-month auto-deletion of inactive/cancelled accounts.
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
)

// retentionNoticeClaimLease is the lease duration requested for both ActionRetentionNotice30d and
// ActionRetentionNotice7d work claims (Issue #1413 Medium finding M9, Codex review round 3).
// Shorter than the 15-minute lease used by claims guarding a Plaid/Paddle call (ExecuteDeletion,
// PastDueService, SubscriptionWebhookWorker): sending a notice email has no slow external cleanup
// step behind it, so 5 minutes is generous while still keeping a crashed worker's claim quickly
// reclaimable.
const retentionNoticeClaimLease = 5 * time.Minute

// RetentionCleanupCheckArgs contains the arguments for the retention cleanup check job.
// This job runs daily to:
//  1. Send 30-day pre-deletion warning emails
//  2. Send 7-day pre-deletion warning emails
//  3. Auto-delete users whose retention period has expired
type RetentionCleanupCheckArgs struct {
	// TriggerSource indicates what triggered this check (e.g., "periodic", "manual")
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (RetentionCleanupCheckArgs) Kind() string { return "retention_cleanup_check" }

// RetentionProfileRepository defines the repository methods needed for retention cleanup.
type RetentionProfileRepository interface {
	// GetUsersNeedingRetention30dNotice returns users whose retention expires within 30 days and 30d notice not sent.
	GetUsersNeedingRetention30dNotice(ctx context.Context) ([]*domain.User, error)
	// GetUsersNeedingRetention7dNotice returns users whose retention expires within 7 days and 7d notice not sent.
	GetUsersNeedingRetention7dNotice(ctx context.Context) ([]*domain.User, error)
	// GetUsersWithExpiredRetention returns users whose retention_expires_at has passed and are not deleted/pending.
	GetUsersWithExpiredRetention(ctx context.Context) ([]*domain.User, error)

	// IsRetentionNotice30dEligible re-verifies, at the moment the caller already holds an
	// exclusive work_claims claim on repository.ActionRetentionNotice30d, that a user is still
	// eligible for the 30-day notice: still in the notice window, still unsent, and carrying no
	// manual_tier_override at all (live or lapsed) — the same rule
	// GetUsersNeedingRetention30dNotice's own query enforces. Returns (true, nil) when safe to
	// send the notice email now, (false, nil) when any of those conditions changed since the
	// candidate query ran — a legitimate skip, never an error. Performs NO write. Issue #1413
	// Medium finding M9 (Codex review round 3): replaces the old ClaimRetentionNotice30dSent,
	// which stamped the *_sent_at marker BEFORE the send was even attempted, so a failed send
	// left the marker set and the user was silently never warned again.
	IsRetentionNotice30dEligible(ctx context.Context, userID string) (bool, error)
	// IsRetentionNotice7dEligible is IsRetentionNotice30dEligible's 7-day sibling.
	IsRetentionNotice7dEligible(ctx context.Context, userID string) (bool, error)

	// SetRetentionNotice30dSent marks the 30-day pre-deletion notice as sent. Callers MUST only
	// invoke this AFTER the notice email has been sent successfully (Issue #1413 M9) — never
	// before, and never on a skipped/failed send.
	SetRetentionNotice30dSent(ctx context.Context, userID string) error
	// SetRetentionNotice7dSent is SetRetentionNotice30dSent's 7-day sibling.
	SetRetentionNotice7dSent(ctx context.Context, userID string) error
}

// RetentionDeletionService defines the deletion methods needed for retention cleanup.
type RetentionDeletionService interface {
	// ExecuteDeletion executes the full deletion process for a user.
	ExecuteDeletion(ctx context.Context, userID string) error
}

// RetentionEmailService defines the email methods needed for retention notices.
type RetentionEmailService interface {
	// SendRetentionNoticeEmail sends a retention notice email to a user.
	SendRetentionNoticeEmail(ctx context.Context, toEmail, subject, body string) error
}

// RetentionCleanupCheckWorker processes retention cleanup check jobs.
// It handles the daily lifecycle of inactive/cancelled accounts:
//   - 30d before expiry: warn user via email
//   - 7d before expiry: final warning via email
//   - After expiry: auto-delete account data
type RetentionCleanupCheckWorker struct {
	river.WorkerDefaults[RetentionCleanupCheckArgs]

	profileRepo     RetentionProfileRepository
	deletionService RetentionDeletionService
	emailService    RetentionEmailService
	// claimsRepo backs the durable work-claims mechanism send30dNotices/send7dNotices require for
	// EXCLUSIVITY on ActionRetentionNotice30d/7d before a notice email is sent (Issue #1413 Medium
	// finding M9, Codex review round 3). See SetWorkClaimsRepo — like
	// DeletionService/PastDueService/SubscriptionWebhookWorker's claims repos, this is required,
	// not optional.
	claimsRepo      repository.WorkClaimsRepository
	retentionMonths int
	logger          *slog.Logger
}

// RetentionCleanupCheckWorkerDeps contains the dependencies for RetentionCleanupCheckWorker.
type RetentionCleanupCheckWorkerDeps struct {
	ProfileRepo     RetentionProfileRepository
	DeletionService RetentionDeletionService
	EmailService    RetentionEmailService
	RetentionMonths int
	Logger          *slog.Logger
}

// NewRetentionCleanupCheckWorker creates a new retention cleanup check worker.
func NewRetentionCleanupCheckWorker(deps RetentionCleanupCheckWorkerDeps) *RetentionCleanupCheckWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	retentionMonths := deps.RetentionMonths
	if retentionMonths <= 0 {
		retentionMonths = 6 // Default: 6-month retention period
	}
	return &RetentionCleanupCheckWorker{
		profileRepo:     deps.ProfileRepo,
		deletionService: deps.DeletionService,
		emailService:    deps.EmailService,
		retentionMonths: retentionMonths,
		logger:          logger,
	}
}

// SetWorkClaimsRepo wires the durable work-claims mechanism send30dNotices/send7dNotices require
// for EXCLUSIVITY on ActionRetentionNotice30d/7d before a notice email is sent (Issue #1413 Medium
// finding M9, Codex review round 3). Mirrors DeletionService/PastDueService/
// SubscriptionWebhookWorker's SetWorkClaimsRepo — like theirs, this is NOT an optional capability:
// a nil claims repo makes the notice-sending path fail loudly (logged, per-user, as an error) for
// every candidate rather than silently reverting to the old conflated claim-marker behavior.
func (w *RetentionCleanupCheckWorker) SetWorkClaimsRepo(repo repository.WorkClaimsRepository) {
	w.claimsRepo = repo
}

// releaseClaimOnFailure releases a work claim and logs any release failure. Mirrors
// PastDueService/SubscriptionWebhookWorker's helper of the same name.
func (w *RetentionCleanupCheckWorker) releaseClaimOnFailure(ctx context.Context, userID, action, token, reason string) {
	if _, releaseErr := w.claimsRepo.Release(ctx, repository.ResourceTypeUser, userID, action, token, reason); releaseErr != nil {
		w.logger.Warn("failed to release work claim", "user_id", userID, "action", action, "reason", reason, "error", releaseErr)
	}
}

// Work processes a retention cleanup check job.
//
// The worker:
//  1. Finds users needing 30-day notice → sends email → marks notice sent
//  2. Finds users needing 7-day notice → sends email → marks notice sent
//  3. Finds users with expired retention → calls ExecuteDeletion for each
//
// Partial deletion failures cause the job to return an error so River retries the run.
func (w *RetentionCleanupCheckWorker) Work(ctx context.Context, job *river.Job[RetentionCleanupCheckArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting retention cleanup check",
		"job_id", job.ID,
		"trigger_source", args.TriggerSource,
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

	// Step 1: Send 30-day pre-deletion warnings
	if notice30Err := w.send30dNotices(ctx, job.ID); notice30Err != nil {
		w.logger.Error("failed to process 30d retention notices",
			"job_id", job.ID,
			"error", notice30Err,
		)
		// Don't fail job — continue with other steps
	}

	// Step 2: Send 7-day pre-deletion warnings
	if notice7Err := w.send7dNotices(ctx, job.ID); notice7Err != nil {
		w.logger.Error("failed to process 7d retention notices",
			"job_id", job.ID,
			"error", notice7Err,
		)
		// Don't fail job — continue with other steps
	}

	// Step 3: Auto-delete users with expired retention
	// Propagate partial failure so River retries — failed users will be picked up again
	if deleteErr := w.deleteExpiredAccounts(ctx, job.ID); deleteErr != nil {
		w.logger.Error("failed to process expired retention deletions",
			"job_id", job.ID,
			"error", deleteErr,
		)
		err = deleteErr
	}

	w.logger.Info("retention cleanup check completed",
		"job_id", job.ID,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return err
}

// retentionNoticeSpec parameterizes processRetentionNotices over the 30-day/7-day notice
// variants, which differ only in: the work_claims action, the log/email copy, the eligibility
// re-check, and which *_sent_at column gets stamped after a successful send.
type retentionNoticeSpec struct {
	action     string
	noticeName string // for log messages, e.g. "30d"
	subject    string
	body       string
	isEligible func(ctx context.Context, userID string) (bool, error)
	markSent   func(ctx context.Context, userID string) error
}

// send30dNotices finds users needing 30-day warning and sends emails.
func (w *RetentionCleanupCheckWorker) send30dNotices(ctx context.Context, jobID int64) error {
	users, err := w.profileRepo.GetUsersNeedingRetention30dNotice(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch users needing 30d notice: %w", err)
	}
	if len(users) == 0 {
		w.logger.Info("no users needing 30d retention notice", "job_id", jobID)
		return nil
	}
	w.logger.Info("found users needing 30d retention notice", "job_id", jobID, "user_count", len(users))

	sentCount, errorCount, skippedCount := w.processRetentionNotices(ctx, jobID, users, retentionNoticeSpec{
		action:     repository.ActionRetentionNotice30d,
		noticeName: "30d",
		subject:    "Your BoostedBudget data will be deleted in 30 days",
		body:       "Your account data is preserved in read-only mode. Your data will be permanently deleted in 30 days unless you resubscribe. Visit boostedbudget.com/pricing to restore full access.",
		isEligible: w.profileRepo.IsRetentionNotice30dEligible,
		markSent:   w.profileRepo.SetRetentionNotice30dSent,
	})

	w.logger.Info("30d retention notices processed",
		"job_id", jobID, "sent", sentCount, "errors", errorCount, "skipped", skippedCount)
	return nil
}

// send7dNotices finds users needing 7-day warning and sends emails.
func (w *RetentionCleanupCheckWorker) send7dNotices(ctx context.Context, jobID int64) error {
	users, err := w.profileRepo.GetUsersNeedingRetention7dNotice(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch users needing 7d notice: %w", err)
	}
	if len(users) == 0 {
		w.logger.Info("no users needing 7d retention notice", "job_id", jobID)
		return nil
	}
	w.logger.Info("found users needing 7d retention notice", "job_id", jobID, "user_count", len(users))

	sentCount, errorCount, skippedCount := w.processRetentionNotices(ctx, jobID, users, retentionNoticeSpec{
		action:     repository.ActionRetentionNotice7d,
		noticeName: "7d",
		subject:    "Final notice: BoostedBudget data deletion in 7 days",
		body:       "This is your final warning. Your BoostedBudget account data will be permanently deleted in 7 days. Resubscribe at boostedbudget.com/pricing to keep your data.",
		isEligible: w.profileRepo.IsRetentionNotice7dEligible,
		markSent:   w.profileRepo.SetRetentionNotice7dSent,
	})

	w.logger.Info("7d retention notices processed",
		"job_id", jobID, "sent", sentCount, "errors", errorCount, "skipped", skippedCount)
	return nil
}

// processRetentionNotices implements the M9-corrected claim -> re-verify -> send -> stamp ->
// complete sequence shared by the 30-day and 7-day retention notices (Issue #1413 Medium finding
// M9, Codex review round 3):
//
//  1. Belt-and-braces HasActiveManualOverride() short-circuit against the candidate snapshot —
//     cheap, but NOT the source of correctness (see the claim/re-verify below for that).
//  2. Skip WITHOUT claiming when there is demonstrably nothing to send (no email service
//     configured, or the user has no address on file) — claiming and immediately releasing would
//     just churn the work_claims table for no benefit.
//  3. Claim spec.action for EXCLUSIVITY. Nothing user-visible has happened yet.
//  4. Re-verify eligibility via spec.isEligible (still in window, still unsent, still uncomped)
//     against the freshest possible read — the claim gives exclusivity, this gives correctness.
//  5. Send the notice email.
//  6. ONLY on send success, stamp *_sent_at via spec.markSent, then mark the claim Complete.
//
// On any failure (claim error, ineligible, send error, or a failure to stamp *_sent_at after a
// successful send), the claim is Released with the failure reason instead of Completed — the
// *_sent_at column stays NULL, so the next daily run retries. This is the fix for the bug this
// task exists to close: the old code stamped *_sent_at BEFORE sending, so a failed send left the
// marker set and the user was silently never warned again before their data was deleted.
//
// Residual, by design: if the email PROVIDER accepts the message but SendRetentionNoticeEmail
// still returns an error (or spec.markSent itself fails right after a genuinely successful send),
// the claim is Released and the next run retries — which re-sends the email. At-least-once is the
// deliberate choice over never-warn: a duplicate warning email is recoverable, a silently-skipped
// one is not.
func (w *RetentionCleanupCheckWorker) processRetentionNotices(ctx context.Context, jobID int64, users []*domain.User, spec retentionNoticeSpec) (sentCount, errorCount, skippedCount int) {
	for _, user := range users {
		// #1413: belt-and-braces — a comped user grants access and must not be warned about, or
		// slated for, deletion. Cheap short-circuit against the candidate list snapshot; the claim
		// + re-verify below is the real enforcement.
		if user.HasActiveManualOverride() {
			w.logger.Info("skipping "+spec.noticeName+" retention notice for actively comped user",
				"job_id", jobID, "user_id", user.ID)
			continue
		}

		// Issue #1413 M9: prefer not to claim at all when there is demonstrably nothing to send —
		// claiming and immediately releasing would just churn the work_claims table for no
		// benefit.
		if !w.canSendRetentionNotice(jobID, spec.noticeName, user) {
			skippedCount++
			continue
		}

		if w.claimsRepo == nil {
			w.logger.Error("processRetentionNotices: work claims repository not configured",
				"job_id", jobID, "user_id", user.ID, "notice", spec.noticeName)
			errorCount++
			continue
		}
		token, claimed, err := w.claimsRepo.Claim(ctx, repository.ResourceTypeUser, user.ID, spec.action, retentionNoticeClaimLease)
		if err != nil {
			w.logger.Warn("failed to claim "+spec.noticeName+" retention notice",
				"job_id", jobID, "user_id", user.ID, "error", err)
			errorCount++
			continue
		}
		if !claimed {
			// Another worker holds this lease, or the action was already completed. Legitimate
			// outcome, not an error.
			w.logger.Info("skipping "+spec.noticeName+" retention notice — claim not granted (comped since query, already sent, or a concurrent worker holds it)",
				"job_id", jobID, "user_id", user.ID)
			skippedCount++
			continue
		}

		// Re-verify eligibility against the freshest possible read, immediately after winning the
		// claim and before the email actually goes out.
		eligible, err := spec.isEligible(ctx, user.ID)
		if err != nil {
			w.releaseClaimOnFailure(ctx, user.ID, spec.action, token, err.Error())
			w.logger.Warn("failed to re-verify "+spec.noticeName+" retention notice eligibility",
				"job_id", jobID, "user_id", user.ID, "error", err)
			errorCount++
			continue
		}
		if !eligible {
			w.releaseClaimOnFailure(ctx, user.ID, spec.action, token, "precondition lost since claim")
			w.logger.Info("skipping "+spec.noticeName+" retention notice — precondition lost since claim (comped, already sent, or window closed)",
				"job_id", jobID, "user_id", user.ID)
			skippedCount++
			continue
		}

		if sendErr := w.emailService.SendRetentionNoticeEmail(ctx, user.Email, spec.subject, spec.body); sendErr != nil {
			w.releaseClaimOnFailure(ctx, user.ID, spec.action, token, sendErr.Error())
			w.logger.Warn("failed to send "+spec.noticeName+" retention notice",
				"job_id", jobID, "user_id", user.ID, "error", sendErr)
			errorCount++
			continue
		}

		// Only stamp *_sent_at AFTER the send actually succeeded — this ordering is the entire
		// M9 fix.
		if markErr := spec.markSent(ctx, user.ID); markErr != nil {
			// The email went out but we failed to record it. Release (not Complete) so the next
			// run retries — see the residual note in the doc comment above: this risks a
			// duplicate send, the deliberate at-least-once trade-off.
			w.releaseClaimOnFailure(ctx, user.ID, spec.action, token, markErr.Error())
			w.logger.Error("sent "+spec.noticeName+" retention notice but failed to record it — next run will retry and may re-send",
				"job_id", jobID, "user_id", user.ID, "error", markErr)
			errorCount++
			continue
		}

		completed, err := w.claimsRepo.Complete(ctx, repository.ResourceTypeUser, user.ID, spec.action, token)
		if err != nil {
			w.logger.Error("failed to mark "+spec.noticeName+" retention notice claim complete",
				"job_id", jobID, "user_id", user.ID, "action", spec.action, "error", err)
		} else if !completed {
			// The lease was reclaimed by another worker while we were mid-flight — the fencing-
			// token scenario. The notice may have been sent twice; this must be visible, not
			// silent (per the #1413 ambiguity resolution for Complete returning ok=false).
			w.logger.Error(spec.noticeName+" retention notice claim lease lost mid-flight — notice may have been sent twice",
				"job_id", jobID, "user_id", user.ID, "action", spec.action)
		}

		sentCount++
	}
	return sentCount, errorCount, skippedCount
}

// canSendRetentionNotice reports whether a notice email can actually be sent for user right now,
// logging why not otherwise. Issue #1413 M9: called BEFORE claiming so a demonstrably-unsendable
// notice (no email service configured, no address on file) never touches work_claims at all —
// claiming and immediately releasing would just churn the table for no benefit. Prior to this fix,
// both cases still stamped the *_sent_at marker despite nothing being sent.
func (w *RetentionCleanupCheckWorker) canSendRetentionNotice(jobID int64, noticeName string, user *domain.User) bool {
	if w.emailService == nil {
		w.logger.Info("skipping "+noticeName+" retention notice: no email service configured",
			"job_id", jobID, "user_id", user.ID)
		return false
	}
	if user.Email == "" {
		w.logger.Warn("user has no email, skipping "+noticeName+" retention notice",
			"job_id", jobID, "user_id", user.ID)
		return false
	}
	return true
}

// deleteExpiredAccounts finds users with expired retention and executes deletion.
func (w *RetentionCleanupCheckWorker) deleteExpiredAccounts(ctx context.Context, jobID int64) error {
	users, err := w.profileRepo.GetUsersWithExpiredRetention(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch users with expired retention: %w", err)
	}

	if len(users) == 0 {
		w.logger.Info("no users with expired retention", "job_id", jobID)
		return nil
	}

	w.logger.Info("found users with expired retention", "job_id", jobID, "user_count", len(users))

	var deletedCount, errorCount int
	for _, user := range users {
		// #1413 Critical finding C1 (third round of remediation): belt-and-braces hard-delete
		// guard — a user with ANY manual tier override, live or lapsed-but-uncleared, must never
		// be auto-deleted. This MUST use HasAnyManualOverride (not HasActiveManualOverride) to
		// agree with the retention queries (noManualOverrideAtAllPredicate) and with
		// claimForDeletionPredicate, which ExecuteDeletion's own claim now enforces — using the
		// live-only check here was exactly the bug this fix closes: a comp that lapsed moments
		// before the independently-scheduled expiry job clears the column would pass this guard
		// and reach ExecuteDeletion. The real enforcement is still ExecuteDeletion's claim; this
		// worker-level guard protects against a stale candidate list when a comp lands (or
		// lapses) between the query and this loop.
		if user.HasAnyManualOverride() {
			w.logger.Info("skipping retention deletion for user with a manual tier override (live or lapsed)",
				"job_id", jobID, "user_id", user.ID)
			continue
		}

		if deleteErr := w.deletionService.ExecuteDeletion(ctx, user.ID); deleteErr != nil {
			w.logger.Error("failed to execute retention deletion",
				"job_id", jobID, "user_id", user.ID, "error", deleteErr)
			errorCount++
			continue
		}

		w.logger.Info("retention deletion executed", "job_id", jobID, "user_id", user.ID)
		deletedCount++
	}

	w.logger.Info("retention deletions processed",
		"job_id", jobID, "deleted", deletedCount, "errors", errorCount)
	if errorCount > 0 {
		return fmt.Errorf("retention deletion: %d of %d deletions failed (see logs for details)", errorCount, len(users))
	}
	return nil
}

// RetentionCleanupCheckUniqueOpts returns the recommended uniqueness options.
// Only one check job should run at a time to prevent duplicate deletion processing.
func RetentionCleanupCheckUniqueOpts() *queue.UniqueOpts {
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
