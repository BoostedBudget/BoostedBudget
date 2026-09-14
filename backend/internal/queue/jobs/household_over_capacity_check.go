// Package jobs contains River job definitions and workers for async processing.
// Issue #1371 — Phase 7: Household over-capacity daily backstop check. This worker is
// the AUTHORITATIVE reconciler for the over-capacity grace-period lifecycle — it scans
// every household with active members (not just ones already flagged), so it catches
// anything the subscription_webhook.go fast-path reconcile step (Task 6, Part B) missed
// or raced against.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
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
	// SetOverCapacity idempotently starts the grace clock (Task 1). Always returns
	// the true effective over_capacity_since — which may be EARLIER than the
	// `since` passed in, if a concurrent writer (e.g. the subscription-webhook
	// fast path) already set it first — so callers must use the returned value,
	// never their own local `since`, to compute anything owner-facing.
	SetOverCapacity(ctx context.Context, householdID string, since time.Time) (time.Time, error)
	// ClearOverCapacity ends the current grace period (Task 1). Used by Cases 1/2/3's
	// own fresh-re-check clears, which have no natural "expected anchor" to verify
	// against — NOT by autoOffboard, which uses ClearOverCapacityIfAnchorMatches instead.
	ClearOverCapacity(ctx context.Context, householdID string) error
	// ClearOverCapacityIfAnchorMatches atomically clears the over-capacity grace state
	// ONLY if the household's CURRENT over_capacity_since still equals
	// expectedOverCapacitySince — used EXCLUSIVELY by autoOffboard, after a successful
	// OffboardExcessMembers call. Between that call's commit (releasing its lock) and
	// this clear, an entirely new, independent over-capacity event could have re-flagged
	// the household (e.g. another member joining, unrelated to the removal that just
	// happened) — an unconditional ClearOverCapacity would erase that brand-new,
	// legitimate grace period. A mismatch (0 rows affected) is not an error; it just
	// means autoOffboard must leave the flag alone and let the next daily scan handle
	// whatever the current state now is. #1371 — Round 9 Gap Analysis fix (fourth Codex
	// adversarial pass, Bug 4).
	ClearOverCapacityIfAnchorMatches(ctx context.Context, householdID string, expectedOverCapacitySince time.Time) error
	// MarkOverCapacityStartNoticeSent records the initial grace notice was sent (Task 1).
	// expectedOverCapacitySince must be the SAME anchor (h.OverCapacitySince) the notice
	// email's content was actually generated from — a mismatch means a concurrent
	// clear-then-re-flag cycle superseded that grace period between the send and this
	// stamp call, and the mismatched stamp attempt correctly no-ops rather than marking
	// the NEW, unrelated grace period as already-notified. #1371 — Round 9 Gap Analysis
	// fix (fourth Codex adversarial pass, Bug 5).
	MarkOverCapacityStartNoticeSent(ctx context.Context, householdID string, sentAt time.Time, expectedOverCapacitySince time.Time) error
	// MarkOverCapacityWarn3dSent records the "3 days left" warning was sent (Task 1). See
	// MarkOverCapacityStartNoticeSent's doc comment for why expectedOverCapacitySince is
	// required. #1371 — Round 9 Gap Analysis fix (fourth Codex adversarial pass, Bug 5).
	MarkOverCapacityWarn3dSent(ctx context.Context, householdID string, sentAt time.Time, expectedOverCapacitySince time.Time) error
	// GetHouseholdByID re-reads the full household record, keyed by ID, with no lock. Needed
	// because the owner/tier/activeCount fresh re-checks added in Rounds 5-8 never re-read
	// h.OverCapacitySince or its two notice-sent markers themselves — this worker's scan-time
	// snapshot of a household can go stale sitting unprocessed in the sequential scan loop,
	// and ClearOverCapacity nulls all three of those columns together, so a real
	// clear-then-re-flag cycle produces a genuinely fresh anchor+markers triple in the
	// database that the stale in-memory struct has no way to see without this re-read.
	// #1371 — Round 9 Gap Analysis fix.
	GetHouseholdByID(ctx context.Context, householdID string) (*domain.Household, error)
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
//
// expectedOverCapacitySince must be the SAME over_capacity_since this worker's own fresh
// re-check (processHousehold's GetHouseholdByID re-read) based its deadline-passed
// decision on — never a locally-recomputed or stale value. OffboardExcessMembers
// re-verifies this anchor still matches the row it actually locks, and no-ops (rather
// than removing anyone) if a concurrent clear-then-re-flag cycle landed in the race
// window between this worker's fresh re-check and OffboardExcessMembers's own lock
// acquisition — see that method's doc comment for the full rationale. #1371 — Round 9
// Gap Analysis fix (Codex adversarial review).
//
// On that anchor-mismatch no-op, OffboardExcessMembers returns
// domain.ErrOverCapacityAnchorMismatch rather than a nil-error empty slice —
// autoOffboard MUST check for this sentinel via errors.Is and, when it matches, skip its
// own ClearOverCapacity call entirely (rather than clearing unconditionally, as it does
// for every other nil-error outcome). The household is still over capacity at that point,
// just under a different, legitimately fresh grace period that this worker's stale
// decision must not erase. #1371 — Round 9 Gap Analysis fix (third Codex adversarial pass).
type HouseholdOverCapacityOffboardService interface {
	OffboardExcessMembers(ctx context.Context, householdID string, expectedOverCapacitySince time.Time) ([]services.QueuedRemovalEmail, error)
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
	ownAccessResolver services.OwnAccessResolver
	river.WorkerDefaults[HouseholdOverCapacityCheckArgs]

	repo         HouseholdOverCapacityCheckRepository
	userRepo     HouseholdOverCapacityUserRepository
	offboardSvc  HouseholdOverCapacityOffboardService
	emailService HouseholdOverCapacityEmailService
	tierConfig   *config.TierConfig
	logger       *slog.Logger
	// cacheGens is optional (nil = cache invalidation disabled/not wired); see
	// SetCacheInvalidation. Issue #1531 Task 10b-2a-iii.
	cacheGens *cache.Generations
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

// SetCacheInvalidation wires cache generation bumping (setter injection, matching this
// codebase's established convention). Issue #1531 Task 10b-2a-iii: autoOffboard's call to
// OffboardExcessMembers removes household members (via HouseholdService.removeMemberTx —
// the SAME helper the HTTP-covered removeHouseholdMember operation and ForceDisband use), but
// OffboardExcessMembers has exactly one caller — this job — so nothing bumped the household's
// cache generation after an auto-offboard actually removed members. This worker's own
// SetOverCapacity/ClearOverCapacity/notice-marker writes carry no cache impact of their own
// (that report was never committed; see docs/architecture/cache-invalidation-dispositions.md); only the member-removal path
// below needs this. Optional: nil
// is a safe no-op.
func (w *HouseholdOverCapacityCheckWorker) SetCacheInvalidation(gens *cache.Generations) {
	w.cacheGens = gens
}

func (w *HouseholdOverCapacityCheckWorker) SetOwnAccessResolver(resolver services.OwnAccessResolver) {
	w.ownAccessResolver = resolver
}
func (w *HouseholdOverCapacityCheckWorker) ownerAccess(ctx context.Context, owner *domain.User) (domain.RevenueCatAccess, error) {
	if w.ownAccessResolver != nil {
		return w.ownAccessResolver.ResolveOwnAccessAt(ctx, owner.ID, time.Now())
	}
	return domain.RevenueCatAccess{Tier: w.effectiveTierName(owner), Status: owner.ComputeEffectiveStatus()}, nil
}

// bumpCacheForHousehold invalidates householdID's cached household-scoped analytics after a
// successful auto-offboard has actually removed at least one member. Void by design, matching
// every other generation bump in this codebase.
//
// Issue #1531 Final fixes: runs on context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
// rather than the raw job ctx. River cancels a job's context on shutdown, retry, or timeout — if
// that cancellation lands after OffboardExcessMembers's removal transaction has already committed
// but before this bump runs, the raw ctx would carry the cancellation into cacheGens.Bump, which
// swallows the resulting error silently: the membership removal commits, the bump never happens,
// and household-scoped cached analytics serve stale values until TTL. WithoutCancel alone would
// leave the bump unbounded (it strips the parent's deadline too), hence the explicit WithTimeout —
// matching household_service.go's bumpCacheForHousehold and the sync services' bumpCacheForSync.
func (w *HouseholdOverCapacityCheckWorker) bumpCacheForHousehold(ctx context.Context, householdID string) {
	if w.cacheGens == nil {
		return
	}
	bumpCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
	defer cancel()
	w.cacheGens.Bump(bumpCtx, cache.ScopeHousehold, householdID, "household_over_capacity_check")
}

// effectiveTierName returns owner's effective tier name, falling back to the raw SubscriptionTier
// when an active manual_tier_override names no tier configured in w.tierConfig (Issue #1413 —
// the tier axis of the malformed-override rule, this task). manual_tier_override is an
// unconstrained VARCHAR(50) with no CHECK constraint, so its value can be garbage; both tier
// lookups in processHousehold (the top-of-function read and the fresh re-check) previously
// passed owner.ComputeEffectiveTier() straight into w.tierConfig.GetTier(), which would fail the
// ENTIRE daily run for a household whose owner carries such a value.
func (w *HouseholdOverCapacityCheckWorker) effectiveTierName(owner *domain.User) string {
	tier, overrideValue, hasOverride := owner.EffectiveTierWithActiveOverride()
	if hasOverride && w.tierConfig != nil {
		if _, err := w.tierConfig.GetTier(domain.TierName(overrideValue)); err != nil {
			w.logger.Warn("rejecting manual tier override on tier axis: value does not name a configured tier",
				"user_id", owner.ID, "manual_tier_override", overrideValue)
			return owner.SubscriptionTier
		}
	}
	return tier
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
		} else {
			observability.RecordJobSuccess(job.Kind) // Issue #1413 P4: last-success heartbeat
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

	// Case 1: owner's own EFFECTIVE subscription status is read-only
	// (cancelled/past_due/inactive) AND the owner isn't currently comped via an active
	// manual_tier_override (#1413) — grace is moot, members already fall back to their own
	// tier via the entitlement resolver's owner-read-only branch, which reads the same
	// effective status so the two can never disagree.
	access, err := w.ownerAccess(ctx, owner)
	if err != nil {
		return err
	}
	if domain.SubscriptionStatus(access.Status).IsReadOnlySubscription() {
		if h.OverCapacitySince != nil {
			if err := w.repo.ClearOverCapacity(ctx, h.ID); err != nil {
				return fmt.Errorf("failed to clear over-capacity for read-only-owner household %s: %w", h.ID, err)
			}
		}
		return nil
	}

	tier, err := w.tierConfig.GetTier(domain.TierName(access.Tier))
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
	freshAccess, err := w.ownerAccess(ctx, freshOwner)
	if err != nil {
		return err
	}
	if domain.SubscriptionStatus(freshAccess.Status).IsReadOnlySubscription() {
		if h.OverCapacitySince != nil {
			if err := w.repo.ClearOverCapacity(ctx, h.ID); err != nil {
				return fmt.Errorf("failed to clear over-capacity for read-only-owner household %s (fresh re-check): %w", h.ID, err)
			}
		}
		return nil
	}
	freshTier, err := w.tierConfig.GetTier(domain.TierName(freshAccess.Tier))
	if err != nil {
		return fmt.Errorf("failed to resolve fresh tier for owner %s: %w", h.OwnerUserID, err)
	}
	seatLimit = freshTier.Pricing.UsersIncluded

	// Also re-count active members: the top-of-function activeCount read is just as
	// susceptible to staleness as the owner/tier reads above it — e.g. the owner removes
	// members in the gap between that read and this write path. Reassign activeCount in
	// place (mirroring how seatLimit itself is reassigned above) so every downstream use —
	// this re-check's own capacity comparison, the eventual SetOverCapacity call, and the
	// notice email content passed to sendStartNotice/send3dWarning/autoOffboard further
	// down — reflects the fresh count, not the stale one.
	activeCount, err = w.repo.CountActiveMembers(ctx, h.ID)
	if err != nil {
		return fmt.Errorf("failed to re-count active members for household %s (fresh re-check): %w", h.ID, err)
	}
	if activeCount <= seatLimit {
		if h.OverCapacitySince != nil {
			if err := w.repo.ClearOverCapacity(ctx, h.ID); err != nil {
				return fmt.Errorf("failed to clear over-capacity for household %s (fresh re-check): %w", h.ID, err)
			}
		}
		return nil
	}

	// Fresh over-capacity-state re-read: the owner/tier/activeCount fresh re-checks above
	// (Rounds 5-8) all guard against THIS worker flagging/notifying/offboarding a household
	// that no longer warrants it — but none of them re-read h.OverCapacitySince itself, nor
	// the two notice-sent markers (h.OverCapacityStartNoticeSentAt/h.OverCapacityWarn3dSentAt).
	// Those are still the values scanHouseholds's single ListHouseholdsWithMembers call read
	// at the START of this scan pass, which can be stale by the time this household's turn
	// comes up in the sequential scan loop. ClearOverCapacity nulls all three of those columns
	// TOGETHER, so a real "clear, then re-flag" sequence (e.g. the owner briefly comes back
	// within capacity via the webhook fast path, then goes over again shortly after) produces
	// a genuinely FRESH anchor+markers triple in the database — new over_capacity_since,
	// both notice-sent markers back to nil. Without this re-read, Case 3's `h.OverCapacitySince
	// == nil` check is skipped (the stale h already has a non-nil value), and the deadline
	// below gets computed from the STALE, possibly already-past-deadline anchor — triggering
	// an immediate, incorrect auto-offboard (real member removal) even though the database's
	// TRUE grace period may have started only moments ago. Re-reading owner/tier/activeCount
	// alone cannot catch this: none of those three values change across a clear-then-re-flag
	// cycle, only the over-capacity columns themselves do. #1371 — Round 9 Gap Analysis fix
	// (Codex adversarial review).
	freshHousehold, err := w.repo.GetHouseholdByID(ctx, h.ID)
	if errors.Is(err, domain.ErrHouseholdNotFound) {
		// Household was disbanded concurrently (e.g. ForceDisband) — nothing left to do.
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to re-read over-capacity state for household %s (fresh re-check): %w", h.ID, err)
	}
	h.OverCapacitySince = freshHousehold.OverCapacitySince
	h.OverCapacityStartNoticeSentAt = freshHousehold.OverCapacityStartNoticeSentAt
	h.OverCapacityWarn3dSentAt = freshHousehold.OverCapacityWarn3dSentAt
	// Also refresh h.Name: it's shown to the owner in the notice emails below, and — like
	// the over-capacity fields above — is otherwise still the scan-time snapshot, stale if
	// the owner renamed the household while it waited its turn in the scan loop. Free to
	// correct here since GetHouseholdByID already fetched the current row. #1371 — Round 9
	// follow-up (Codex adversarial review, same staleness class as Round 6's stale-owner-
	// email fix, applied to the household's own display name instead of the owner's).
	h.Name = freshHousehold.Name

	// Case 3: over capacity, not yet flagged — the webhook fast path missed this one.
	if h.OverCapacitySince == nil {
		since := time.Now()
		effectiveSince, err := w.repo.SetOverCapacity(ctx, h.ID, since)
		if err != nil {
			return fmt.Errorf("failed to set over-capacity for household %s: %w", h.ID, err)
		}
		// Reflect the just-set flag into the in-memory struct and fall through to the
		// grace-window branching below, instead of returning early — so the owner
		// receives the start notice in THIS SAME run rather than waiting up to ~24h
		// for tomorrow's daily worker run to notice OverCapacitySince is now set (this
		// in-memory struct was loaded at the top of scanHouseholds/processHousehold,
		// before SetOverCapacity ran, so it would otherwise still read nil here).
		// Codex review finding — #1371.
		//
		// Use effectiveSince — SetOverCapacity's true effective return value — rather
		// than the locally-computed `since`: this worker can lose a race against a
		// concurrent writer (e.g. the subscription-webhook fast path, which holds a
		// household row lock via LockHouseholdByID) that already set an EARLIER
		// over_capacity_since between this worker's read of h and this write. If that
		// happens, `since` is NOT the true grace anchor — trusting it here would
		// compute a grace deadline (below) LATER than what the database will actually
		// enforce, and email the owner a fabricated, incorrect deadline. effectiveSince
		// is always the true value, whichever writer got there first.
		h.OverCapacitySince = &effectiveSince
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
		return w.sendStartNotice(ctx, jobID, freshOwner, h, activeCount, seatLimit, deadline)
	case !now.Before(warnAt) && h.OverCapacityWarn3dSentAt == nil:
		// Case 6: within 3 days of the deadline, warning not yet sent.
		return w.send3dWarning(ctx, jobID, freshOwner, h, activeCount, seatLimit, deadline)
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
//
// h.OverCapacitySince is guaranteed non-nil here: this is only ever called from Case 4 in
// processHousehold, which only fires once the grace deadline (itself computed FROM
// h.OverCapacitySince, after this worker's own fresh GetHouseholdByID re-read) has passed.
// That dereferenced value is passed through as OffboardExcessMembers's
// expectedOverCapacitySince — the anchor this worker's OWN deadline decision was based
// on — so OffboardExcessMembers can detect (and no-op on) a concurrent clear-then-re-flag
// race landing between this worker's re-read and OffboardExcessMembers's own lock
// acquisition. #1371 — Round 9 Gap Analysis fix (Codex adversarial review).
//
// The post-success clear below uses ClearOverCapacityIfAnchorMatches, NOT the
// unconditional ClearOverCapacity: OffboardExcessMembers just succeeded USING the anchor
// this worker expected, but between its commit (releasing its lock) and this clear call,
// an entirely new, independent over-capacity event could have landed (e.g. the
// subscription webhook re-flagging the household for an unrelated reason — another
// member joining, another tier downgrade). An unconditional clear would erase that
// brand-new, legitimate grace period; the anchor-guarded clear leaves it untouched on a
// mismatch instead. #1371 — Round 9 Gap Analysis fix (fourth Codex adversarial pass,
// Bug 4).
func (w *HouseholdOverCapacityCheckWorker) autoOffboard(ctx context.Context, jobID int64, h *domain.Household, activeCount, seatLimit int) error {
	queued, err := w.offboardSvc.OffboardExcessMembers(ctx, h.ID, *h.OverCapacitySince)
	if errors.Is(err, domain.ErrOverCapacityAnchorMismatch) {
		// A concurrent clear-then-re-flag cycle landed between this worker's fresh
		// re-read and OffboardExcessMembers's own lock acquisition — the household is
		// now under a DIFFERENT (or cleared) grace period than the one this worker
		// decided to act on. Do NOT clear over-capacity here: doing so would erase a
		// legitimately fresh grace period that deserves its own full 14 days. Leave the
		// flag exactly as OffboardExcessMembers found it — the next daily scan will
		// correctly re-evaluate against whatever the CURRENT anchor now is. #1371 —
		// Round 9 Gap Analysis fix (third Codex adversarial pass).
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to auto-offboard excess members for household %s: %w", h.ID, err)
	}
	// Issue #1531 Task 10b-2a-iii: bump ONLY here — reached exclusively when
	// OffboardExcessMembers returned (queue, nil), meaning its own removeMemberTx calls
	// already committed inside that method's transaction. len(queued) > 0 gates a bump on an
	// actual removal happening — a zero-excess call (queued empty, e.g. the seat count
	// resolved itself before this transaction) commits nothing membership-shaped, so there is
	// nothing to invalidate. This bump does not depend on the clear below succeeding: the
	// membership change already committed regardless of whether the bookkeeping clear that
	// follows does.
	if len(queued) > 0 {
		w.bumpCacheForHousehold(ctx, h.ID)
	}
	if err := w.repo.ClearOverCapacityIfAnchorMatches(ctx, h.ID, *h.OverCapacitySince); err != nil {
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
//
// The stamp call passes *h.OverCapacitySince as the expected anchor: h.OverCapacitySince
// at this point is the SAME fresh value processHousehold used to compute deadline (this
// is the caller's already-current struct field, not re-read again) — the exact anchor
// the notice email's content was generated for. Without threading it through, a
// clear-then-re-flag cycle landing between the send above and this stamp call would mark
// the NEW, unrelated grace period as already-notified, silently skipping ITS legitimate
// notice. #1371 — Round 9 Gap Analysis fix (fourth Codex adversarial pass, Bug 5).
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
	if err := w.repo.MarkOverCapacityStartNoticeSent(ctx, h.ID, time.Now(), *h.OverCapacitySince); err != nil {
		return fmt.Errorf("failed to mark over-capacity start notice sent for household %s: %w", h.ID, err)
	}
	return nil
}

// send3dWarning sends the "3 days left" follow-up email and stamps it as sent — same
// send-then-stamp tradeoff as sendStartNotice, including the anchor-guarded stamp (see
// that method's doc comment for the full mis-stamping race this closes). #1371 — Round 9
// Gap Analysis fix (fourth Codex adversarial pass, Bug 5).
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
	if err := w.repo.MarkOverCapacityWarn3dSent(ctx, h.ID, time.Now(), *h.OverCapacitySince); err != nil {
		return fmt.Errorf("failed to mark over-capacity 3-day warning sent for household %s: %w", h.ID, err)
	}
	return nil
}

// HouseholdOverCapacityCheckUniqueOpts returns the recommended uniqueness options. Only
// one check job may exist in any of the listed non-terminal states AT ALL, regardless of
// how much time has elapsed since it was inserted — deliberately NOT time-windowed via
// ByPeriod. Round 8 Codex review: a ByPeriod-bucketed window (the original design) only
// dedupes inserts within the SAME bucket, so a still-running instance that bridges into
// the next bucket would NOT block a new insert — reopening the duplicate-notice-email
// risk this UniqueOpts wiring exists to close. The trade-off: a job stuck in a
// non-terminal state (e.g. perpetually retryable) blocks all future daily runs of this
// kind until it reaches a terminal state (completed/discarded/cancelled) via River's own
// max-attempts/backoff exhaustion or manual intervention — accepted as preferable to a
// silent duplicate-run window, since these are lightweight daily backstop jobs never
// expected to run anywhere near long enough to matter in practice.
//
// ⚠️ DO NOT ADD queue.JobStateCompleted HERE — same reasoning as
// HouseholdMemberCleanupCheckUniqueOpts (see its doc comment). Issue #1448's final
// branch-wide review correctly flagged the eight RunOnStart:true jobs that DO carry a
// ByPeriod for omitting Completed, but this job and its sibling deliberately leave ByPeriod
// unset, and Completed without a time bucket means "never enqueue again, ever" — the job
// would run once and then stop permanently.
func HouseholdOverCapacityCheckUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: false, // All check jobs are equivalent
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}

// HouseholdOverCapacityCheckInsertOpts returns the *river.InsertOpts this job's periodic
// registration in main.go must use — Queue: "low" plus the uniqueness options from
// HouseholdOverCapacityCheckUniqueOpts(), converted to river's UniqueOpts type.
//
// Issue #1448 (following on from issue1371_wiring_drift_test.go's Round 8 finding):
// main.go previously hand-duplicated this UniqueOpts shape as an inline river.UniqueOpts{}
// literal instead of calling this package's own helper, so nothing enforced the two stay in
// sync. main.go now calls this function directly.
func HouseholdOverCapacityCheckInsertOpts() *river.InsertOpts {
	return riverInsertOptsFromUnique(queue.QueueLow, HouseholdOverCapacityCheckUniqueOpts())
}

// HouseholdOverCapacityCheckPeriodicOpts returns the *river.PeriodicJobOpts this job's
// periodic registration in main.go must use. RunOnStart: true is only safe because
// HouseholdOverCapacityCheckInsertOpts() above always carries
// HouseholdOverCapacityCheckUniqueOpts()'s dedup — previously this job used
// RunOnStart: false, meaning it could be enqueued zero times ever on a deploy cadence
// faster than its interval (Issue #1448). ID is set so River restores nextRunAt across
// restarts/leader elections instead of recomputing now+interval every time.
func HouseholdOverCapacityCheckPeriodicOpts() *river.PeriodicJobOpts {
	return &river.PeriodicJobOpts{RunOnStart: true, ID: HouseholdOverCapacityCheckArgs{}.Kind()}
}
