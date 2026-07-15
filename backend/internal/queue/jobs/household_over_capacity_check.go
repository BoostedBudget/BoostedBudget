// Package jobs contains River job definitions and workers for async processing.
// Issue #1371 — Phase 7: Household over-capacity daily backstop check. This worker is
// the AUTHORITATIVE reconciler for the over-capacity grace-period lifecycle — it scans
// every household with active members (not just ones already flagged), so it catches
// anything the subscription_webhook.go fast-path reconcile step (Task 6, Part B) missed
// or raced against.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/riverqueue/river"
)

// householdOverCapacityGraceDays is how long an owner has to remove members or upgrade
// once a household enters over-capacity grace, before this worker auto-offboards the
// most-recently-joined excess members. #1371 — Phase 7 (Task 6).
const householdOverCapacityGraceDays = 14

// householdOverCapacityWarnBeforeDays is how many days before the grace deadline the
// "3 days left" follow-up warning is sent.
const householdOverCapacityWarnBeforeDays = 3

// HouseholdOverCapacityCheckArgs contains the arguments for the household over-capacity
// check job. This job runs daily to authoritatively reconcile every household's
// over-capacity grace state: flag newly over-capacity households, clear resolved ones,
// send the owner-facing grace notices at the right times, and auto-offboard excess
// members once a household's grace period has fully expired.
type HouseholdOverCapacityCheckArgs struct {
	// TriggerSource indicates what triggered this check (e.g., "periodic", "manual")
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (HouseholdOverCapacityCheckArgs) Kind() string { return "household_over_capacity_check" }

// HouseholdOverCapacityCheckRepository defines the repository methods needed for the
// daily over-capacity backstop scan: read every household with active members, plus the
// pure over-capacity state-transition methods built in Task 1. Deliberately narrow —
// this worker contains no member-removal SQL of its own (that lives in
// HouseholdOverCapacityOffboardService).
type HouseholdOverCapacityCheckRepository interface {
	// ListHouseholdsWithMembers returns every household with at least one active
	// member, regardless of over-capacity state — the scan target that makes this
	// worker authoritative rather than relying on already-flagged households alone.
	ListHouseholdsWithMembers(ctx context.Context) ([]*domain.Household, error)
	// CountActiveMembers returns the number of active members for a household.
	CountActiveMembers(ctx context.Context, householdID string) (int, error)
	// SetOverCapacity idempotently starts the grace clock (Task 1).
	SetOverCapacity(ctx context.Context, householdID string, since time.Time) error
	// ClearOverCapacity ends the current grace period (Task 1).
	ClearOverCapacity(ctx context.Context, householdID string) error
	// MarkOverCapacityStartNoticeSent records the initial grace notice was sent (Task 1).
	MarkOverCapacityStartNoticeSent(ctx context.Context, householdID string, sentAt time.Time) error
	// MarkOverCapacityWarn3dSent records the "3 days left" warning was sent (Task 1).
	MarkOverCapacityWarn3dSent(ctx context.Context, householdID string, sentAt time.Time) error
}

// HouseholdOverCapacityUserRepository defines the owner-lookup method needed to
// recompute the owner's current effective-tier seat count on every scan pass — the
// same technique paddle_checkout.go's checkDowngradeSeatCapacity (Task 5) uses.
type HouseholdOverCapacityUserRepository interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
}

// HouseholdOverCapacityOffboardService defines the auto-offboard method needed once a
// household's grace period has fully expired. Delegates ALL LIFO removal logic to
// HouseholdService.OffboardExcessMembers (#1371 Task 0) — this worker never touches
// member-removal SQL directly, only the grace-window bookkeeping around it.
type HouseholdOverCapacityOffboardService interface {
	OffboardExcessMembers(ctx context.Context, householdID string) ([]services.QueuedRemovalEmail, error)
}

// HouseholdOverCapacityEmailService defines the email methods needed by this worker:
// only the two owner-facing grace notices (Task 6, Part A). The member-removed email
// after an auto-offboard is NOT sent from here — HouseholdService.OffboardExcessMembers
// unconditionally sends it internally (via its own sendRemovalEmails, Task 3) right
// after its own tx.Commit, every time it's called, regardless of caller. The
// []QueuedRemovalEmail it returns exists purely so a caller (this worker included) can
// inspect what happened — e.g. for logging len(queued) — never so a caller can re-send
// from it; doing so would double-email every auto-offboarded member (a real bug caught
// in review).
type HouseholdOverCapacityEmailService interface {
	SendHouseholdOverCapacityNotice(ctx context.Context, toEmail, householdName string, activeCount, seatLimit int, deadline time.Time) error
	SendHouseholdOverCapacityWarning(ctx context.Context, toEmail, householdName string, activeCount, seatLimit int, deadline time.Time) error
}

// HouseholdOverCapacityCheckWorker processes household over-capacity check jobs. It is
// the AUTHORITATIVE reconciler for the over-capacity grace lifecycle (see package doc).
type HouseholdOverCapacityCheckWorker struct {
	river.WorkerDefaults[HouseholdOverCapacityCheckArgs]

	repo         HouseholdOverCapacityCheckRepository
	userRepo     HouseholdOverCapacityUserRepository
	offboardSvc  HouseholdOverCapacityOffboardService
	emailService HouseholdOverCapacityEmailService
	tierConfig   *config.TierConfig
	logger       *slog.Logger
}

// HouseholdOverCapacityCheckWorkerDeps contains the dependencies for
// HouseholdOverCapacityCheckWorker.
type HouseholdOverCapacityCheckWorkerDeps struct {
	Repo         HouseholdOverCapacityCheckRepository
	UserRepo     HouseholdOverCapacityUserRepository
	OffboardSvc  HouseholdOverCapacityOffboardService
	EmailService HouseholdOverCapacityEmailService
	TierConfig   *config.TierConfig
	Logger       *slog.Logger
}

// NewHouseholdOverCapacityCheckWorker creates a new household over-capacity check worker.
func NewHouseholdOverCapacityCheckWorker(deps HouseholdOverCapacityCheckWorkerDeps) *HouseholdOverCapacityCheckWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &HouseholdOverCapacityCheckWorker{
		repo:         deps.Repo,
		userRepo:     deps.UserRepo,
		offboardSvc:  deps.OffboardSvc,
		emailService: deps.EmailService,
		tierConfig:   deps.TierConfig,
		logger:       logger,
	}
}

// Work processes a household over-capacity check job.
//
// The worker scans every household with active members and, per household:
//  1. Clears a lingering grace flag if the owner's own subscription is read-only.
//  2. Clears grace if the household is back within its seat count.
//  3. Flags newly over-capacity households not yet in grace (webhook-missed case).
//  4. Auto-offboards excess members once the grace deadline has passed.
//  5. Sends the initial grace notice once, when not yet sent.
//  6. Sends the "3 days left" warning once, when not yet sent and the deadline nears.
//
// Partial per-household failures are logged and do not stop the scan — but the overall
// job returns a combined error so River retries, mirroring Task 2's worker.
func (w *HouseholdOverCapacityCheckWorker) Work(ctx context.Context, job *river.Job[HouseholdOverCapacityCheckArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting household over-capacity check",
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

	if scanErr := w.scanHouseholds(ctx, job.ID); scanErr != nil {
		w.logger.Error("failed to process household over-capacity check",
			"job_id", job.ID,
			"error", scanErr,
		)
		err = scanErr
	}

	w.logger.Info("household over-capacity check completed",
		"job_id", job.ID,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return err
}

// scanHouseholds lists every household with active members and processes each
// independently — a failure on one household is logged and does not prevent
// processing others, but the aggregate error is still returned so River retries.
func (w *HouseholdOverCapacityCheckWorker) scanHouseholds(ctx context.Context, jobID int64) error {
	households, err := w.repo.ListHouseholdsWithMembers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list households with members: %w", err)
	}

	if len(households) == 0 {
		w.logger.Info("no households with active members", "job_id", jobID)
		return nil
	}

	w.logger.Info("scanning households for over-capacity state", "job_id", jobID, "household_count", len(households))

	var errorCount int
	for _, h := range households {
		if procErr := w.processHousehold(ctx, jobID, h); procErr != nil {
			w.logger.Error("failed to process household over-capacity check",
				"job_id", jobID, "household_id", h.ID, "error", procErr)
			errorCount++
			continue
		}
	}

	if errorCount > 0 {
		return fmt.Errorf("household over-capacity check: %d of %d households failed (see logs for details)", errorCount, len(households))
	}
	return nil
}

// processHousehold applies the authoritative over-capacity state machine to a single
// household. See Work's doc comment for the six cases.
func (w *HouseholdOverCapacityCheckWorker) processHousehold(ctx context.Context, jobID int64, h *domain.Household) error {
	owner, err := w.userRepo.GetUserByID(ctx, h.OwnerUserID)
	if err != nil {
		return fmt.Errorf("failed to get owner %s: %w", h.OwnerUserID, err)
	}

	// Case 1: owner's own subscription is read-only (cancelled/past_due/inactive) —
	// grace is moot, members already fall back to their own tier via the (unchanged)
	// entitlement resolver's owner-read-only branch.
	if domain.SubscriptionStatus(owner.SubscriptionStatus).IsReadOnlySubscription() {
		if h.OverCapacitySince != nil {
			if err := w.repo.ClearOverCapacity(ctx, h.ID); err != nil {
				return fmt.Errorf("failed to clear over-capacity for read-only-owner household %s: %w", h.ID, err)
			}
		}
		return nil
	}

	tier, err := w.tierConfig.GetTier(domain.TierName(owner.ComputeEffectiveTier()))
	if err != nil {
		return fmt.Errorf("failed to resolve tier for owner %s: %w", h.OwnerUserID, err)
	}
	seatLimit := tier.Pricing.UsersIncluded

	activeCount, err := w.repo.CountActiveMembers(ctx, h.ID)
	if err != nil {
		return fmt.Errorf("failed to count active members for household %s: %w", h.ID, err)
	}

	// Case 2: within capacity.
	if activeCount <= seatLimit {
		if h.OverCapacitySince != nil {
			if err := w.repo.ClearOverCapacity(ctx, h.ID); err != nil {
				return fmt.Errorf("failed to clear over-capacity for household %s: %w", h.ID, err)
			}
		}
		return nil
	}

	// Fresh re-check: the top-of-function owner read (Case 1) — and the tier/seatLimit
	// derived from it — can both be stale by the time we're about to WRITE over-capacity
	// state below. Two concurrent transitions can land in the gap between that read and
	// here: (a) the owner goes read-only (e.g. the subscription webhook's fast-path
	// clear) — grace is moot; or (b) the owner upgrades tier, gaining enough seats —
	// the household is no longer actually over capacity. Re-fetch the owner and
	// recompute both before any write, bailing out (clearing grace if flagged) in either
	// case rather than flagging/re-flagging/notifying a household that no longer
	// warrants it. Mirrors the identical re-check added to
	// HouseholdService.OffboardExcessMembers in Round 3. This narrows the race window
	// but does not eliminate it (no lock is taken) — an intentional trade-off, since any
	// residual gap here self-heals on the next daily run. #1371 — Round 5 Gap Analysis
	// fix, extended per Codex's stale-seat-limit follow-up finding.
	freshOwner, err := w.userRepo.GetUserByID(ctx, h.OwnerUserID)
	if err != nil {
		return fmt.Errorf("failed to re-fetch owner %s for fresh read-only check: %w", h.OwnerUserID, err)
	}
	if domain.SubscriptionStatus(freshOwner.SubscriptionStatus).IsReadOnlySubscription() {
		if h.OverCapacitySince != nil {
			if err := w.repo.ClearOverCapacity(ctx, h.ID); err != nil {
				return fmt.Errorf("failed to clear over-capacity for read-only-owner household %s (fresh re-check): %w", h.ID, err)
			}
		}
		return nil
	}
	freshTier, err := w.tierConfig.GetTier(domain.TierName(freshOwner.ComputeEffectiveTier()))
	if err != nil {
		return fmt.Errorf("failed to resolve fresh tier for owner %s: %w", h.OwnerUserID, err)
	}
	seatLimit = freshTier.Pricing.UsersIncluded
	if activeCount <= seatLimit {
		if h.OverCapacitySince != nil {
			if err := w.repo.ClearOverCapacity(ctx, h.ID); err != nil {
				return fmt.Errorf("failed to clear over-capacity for household %s (fresh re-check): %w", h.ID, err)
			}
		}
		return nil
	}

	// Case 3: over capacity, not yet flagged — the webhook fast path missed this one.
	if h.OverCapacitySince == nil {
		since := time.Now()
		if err := w.repo.SetOverCapacity(ctx, h.ID, since); err != nil {
			return fmt.Errorf("failed to set over-capacity for household %s: %w", h.ID, err)
		}
		// Reflect the just-set flag into the in-memory struct and fall through to the
		// grace-window branching below, instead of returning early — so the owner
		// receives the start notice in THIS SAME run rather than waiting up to ~24h
		// for tomorrow's daily worker run to notice OverCapacitySince is now set (this
		// in-memory struct was loaded at the top of scanHouseholds/processHousehold,
		// before SetOverCapacity ran, so it would otherwise still read nil here).
		// Codex review finding — #1371.
		h.OverCapacitySince = &since
	}

	// In grace — either already, or just entered via Case 3 above. Branch on where
	// `now` falls relative to the grace window. These three cases are mutually
	// exclusive: a household exactly AT the deadline auto-offboards (case ordering
	// below checks the deadline FIRST), never a redundant warning.
	deadline := h.OverCapacitySince.Add(householdOverCapacityGraceDays * 24 * time.Hour)
	warnAt := deadline.Add(-householdOverCapacityWarnBeforeDays * 24 * time.Hour)
	now := time.Now()

	switch {
	case !now.Before(deadline):
		// Case 4: deadline reached (or passed) — auto-offboard.
		return w.autoOffboard(ctx, jobID, h, activeCount, seatLimit)
	case h.OverCapacityStartNoticeSentAt == nil:
		// Case 5: still within the grace window, initial notice not yet sent.
		return w.sendStartNotice(ctx, jobID, owner, h, activeCount, seatLimit, deadline)
	case !now.Before(warnAt) && h.OverCapacityWarn3dSentAt == nil:
		// Case 6: within 3 days of the deadline, warning not yet sent.
		return w.send3dWarning(ctx, jobID, owner, h, activeCount, seatLimit, deadline)
	}
	return nil
}

// autoOffboard delegates the LIFO excess-member removal to HouseholdService (Task 0),
// then clears the over-capacity flag (which OffboardExcessMembers itself doesn't know
// about — it's a Task 0 method, kept unaware of over-capacity by design). It does NOT
// send the removal emails itself: OffboardExcessMembers already sends
// SendHouseholdMemberRemoved internally (via HouseholdService.sendRemovalEmails)
// immediately after its own tx.Commit succeeds — see household_service.go. Sending
// again here from the returned queue would double-email every auto-offboarded member
// (caught in review; ForceDisband, Task 7, has the identical internally-sends-AND-
// returns-a-queue shape, so any future caller there must not repeat this mistake).
func (w *HouseholdOverCapacityCheckWorker) autoOffboard(ctx context.Context, jobID int64, h *domain.Household, activeCount, seatLimit int) error {
	queued, err := w.offboardSvc.OffboardExcessMembers(ctx, h.ID)
	if err != nil {
		return fmt.Errorf("failed to auto-offboard excess members for household %s: %w", h.ID, err)
	}
	if err := w.repo.ClearOverCapacity(ctx, h.ID); err != nil {
		return fmt.Errorf("failed to clear over-capacity after auto-offboard for household %s: %w", h.ID, err)
	}
	w.logger.Info("household auto-offboarded excess members after grace deadline",
		"job_id", jobID, "household_id", h.ID, "removed_count", len(queued),
		"active_count", activeCount, "seat_limit", seatLimit)
	return nil
}

// sendStartNotice sends the initial "you're over capacity" email and stamps it as sent
// — but ONLY on a successful send. Mirrors RetentionCleanupCheckWorker.send30dNotices'
// send-then-stamp tradeoff exactly: a failed send skips the stamp so the notice is
// retried on the next daily run instead of being silently skipped forever.
func (w *HouseholdOverCapacityCheckWorker) sendStartNotice(ctx context.Context, jobID int64, owner *domain.User, h *domain.Household, activeCount, seatLimit int, deadline time.Time) error {
	if w.emailService == nil {
		return nil
	}
	if owner.Email == "" {
		w.logger.Warn("household owner has no email, skipping over-capacity start notice", "job_id", jobID, "household_id", h.ID)
		return nil
	}
	if err := w.emailService.SendHouseholdOverCapacityNotice(ctx, owner.Email, h.Name, activeCount, seatLimit, deadline); err != nil {
		w.logger.Warn("failed to send over-capacity start notice; will retry next run",
			"job_id", jobID, "household_id", h.ID, "error", err)
		return nil
	}
	if err := w.repo.MarkOverCapacityStartNoticeSent(ctx, h.ID, time.Now()); err != nil {
		return fmt.Errorf("failed to mark over-capacity start notice sent for household %s: %w", h.ID, err)
	}
	return nil
}

// send3dWarning sends the "3 days left" follow-up email and stamps it as sent — same
// send-then-stamp tradeoff as sendStartNotice.
func (w *HouseholdOverCapacityCheckWorker) send3dWarning(ctx context.Context, jobID int64, owner *domain.User, h *domain.Household, activeCount, seatLimit int, deadline time.Time) error {
	if w.emailService == nil {
		return nil
	}
	if owner.Email == "" {
		w.logger.Warn("household owner has no email, skipping over-capacity 3-day warning", "job_id", jobID, "household_id", h.ID)
		return nil
	}
	if err := w.emailService.SendHouseholdOverCapacityWarning(ctx, owner.Email, h.Name, activeCount, seatLimit, deadline); err != nil {
		w.logger.Warn("failed to send over-capacity 3-day warning; will retry next run",
			"job_id", jobID, "household_id", h.ID, "error", err)
		return nil
	}
	if err := w.repo.MarkOverCapacityWarn3dSent(ctx, h.ID, time.Now()); err != nil {
		return fmt.Errorf("failed to mark over-capacity 3-day warning sent for household %s: %w", h.ID, err)
	}
	return nil
}

// HouseholdOverCapacityCheckUniqueOpts returns the recommended uniqueness options.
// Only one check job should run at a time to prevent duplicate processing.
func HouseholdOverCapacityCheckUniqueOpts() *queue.UniqueOpts {
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
