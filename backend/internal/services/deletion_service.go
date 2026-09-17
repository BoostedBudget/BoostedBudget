package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"syscall"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// cachePurgeRetryJobArgs mirrors jobs.CachePurgeRetryArgs (internal/queue/jobs/cache_purge_retry.go)
// so this package can enqueue a durable retry without importing internal/queue/jobs, which would
// create an import cycle: jobs already imports services. Six other args types in this codebase
// document the identical mirroring pattern (see account_service.go's plaidSyncJobArgs) — the JSON
// shape and Kind() string here MUST match jobs.CachePurgeRetryArgs exactly.
type cachePurgeRetryJobArgs struct {
	UserID      string `json:"user_id"`
	ClerkUserID string `json:"clerk_user_id"`
	HouseholdID string `json:"household_id,omitempty"`
}

func (cachePurgeRetryJobArgs) Kind() string { return "cache_purge_retry" }

// ErrDeletionAuthorisationRevoked is returned by ExecuteDeletion's internal cleanup helpers when a
// use-time recheck finds that the authorisation this run was claimed under no longer holds — in
// practice, the user cancelled their own deletion request while the cleanup sequence was in flight.
//
// A SENTINEL RATHER THAN A BARE ERROR because the two dispositions are opposite: a Paddle or Plaid
// FAILURE must abort loudly and be retried by the next run, whereas a revocation must abort QUIETLY
// and never be retried — the user asked to keep their account. Without a distinguishable value the
// caller has to either treat a cancellation as a Paddle outage (alarming, and it would re-attempt the
// cleanup on every subsequent run against a user who is no longer eligible) or swallow real Paddle
// failures. It is deliberately not exported beyond this package's own use; callers of ExecuteDeletion
// see a clean nil, because a cancelled deletion is a completed decision, not an error.
var ErrDeletionAuthorisationRevoked = errors.New("deletion authorisation revoked since the claim was taken")

// DeletionService defines the interface for account deletion operations
type DeletionService interface {
	// RequestDeletion initiates account deletion with 30-day grace period
	RequestDeletion(ctx context.Context, userID, email string) (cancellationToken string, scheduledAt time.Time, err error)

	// CancelDeletion cancels a pending deletion using the cancellation token
	CancelDeletion(ctx context.Context, token string) error

	// ExecuteDeletion executes the full deletion process for a user under the authorisation
	// `kind` names — repository.DeletionKindUserRequested for a user's own elapsed GDPR deletion
	// request, repository.DeletionKindRetention for an expired retention clock on an account
	// nobody asked to delete.
	//
	// Issue #1440, T0: the kind is a required, caller-declared parameter rather than something
	// inferred from the user's row, because a caller can be arbitrarily stale — an
	// account_deletion job sits in River until a worker picks it up, during which the user may
	// cancel or re-request. Inferring the kind from current state would authorise exactly the
	// cases the split exists to refuse. It is threaded straight through to
	// repository.ProfileRepository.ClaimForDeletion; see that method for the predicates.
	ExecuteDeletion(ctx context.Context, userID string, kind repository.DeletionClaimKind) error

	// SetPlaidCleanupService sets the Plaid cleanup service for removing Plaid items
	// before user deletion. This setter allows wiring the service after DeletionService
	// is created, since Plaid services may be initialized after DeletionService.
	// Issue #668: Plaid Item Management for Cost Optimization
	SetPlaidCleanupService(svc PlaidItemCleanupService)

	// SetPaddleCleanupService sets the Paddle cleanup service for billing reconciliation
	// during deletion. Mirrors SetPlaidCleanupService pattern. Issue #1006.
	SetPaddleCleanupService(svc PaddleDeletionCleaner)
	SetRevenueCatCleanupService(svc RevenueCatDeletionCleaner)

	// SetAnalytics sets the analytics client for capturing deletion lifecycle events
	SetAnalytics(a phclient.Analytics)

	// SetHouseholdReader wires the household-read capability used to block owner
	// deletion while active non-owner members exist (#1367 — Phase 3). Mirrors the
	// SetPlaidCleanupService/SetPaddleCleanupService pattern. Should always be wired in
	// production (main.go); nil-safe (skips the guard) only to avoid breaking callers
	// that construct DeletionService directly in tests unrelated to households.
	SetHouseholdReader(r HouseholdReader)

	// SetHouseholdInviteRevoker wires the capability to revoke a household's pending
	// invites when an owner's deletion proceeds, so a grace-period accept cannot join a
	// doomed household. Nil-safe (skipped if unset).
	SetHouseholdInviteRevoker(r HouseholdInviteRevoker)

	// SetDB wires transaction capability so the household owner-guard (lock, re-check,
	// revoke invites, mark-for-deletion) can run atomically under the SAME household row
	// lock AcceptInvite uses — closing a race where an in-flight accept commits between
	// this guard's check and MarkForDeletion (Codex review finding). Nil-safe: when unset
	// (or HouseholdReader/HouseholdInviteRevoker are unset), falls back to the
	// non-transactional sequence.
	SetDB(db repository.DB)

	// SetWorkClaimsRepo wires the durable work-claims mechanism (Issue #1413 Critical
	// finding C2) that ExecuteDeletion uses to gain EXCLUSIVITY on ActionAccountDeletion
	// before any irreversible work — replacing ClaimForDeletion's self-assigning UPDATE,
	// which is an atomic guard (proves the predicate held at one instant) rather than a
	// claim (reserves nothing, so two retention workers could both proceed). Required for
	// ExecuteDeletion to run at all: unlike the other Set* methods above, this is not an
	// optional capability — a nil claims repo makes ExecuteDeletion fail loudly rather
	// than silently falling back to the old, race-prone guard-only behavior.
	SetWorkClaimsRepo(repo repository.WorkClaimsRepository)
	SetSubscriptionCleanupGuard(guard SubscriptionCleanupGuard)

	// SetCache wires Issue #1531 Task 10b's GDPR deletion purge: purger deletes the deleted
	// user's own u:<userID>/k:<hash(clerkID)> cache footprint immediately (see cache.Purger's doc
	// comment for why immediate deletion, not a generation bump, is used for THOSE two scopes);
	// gens bumps the generation of any household the user belonged to, orphaning (not deleting —
	// it legitimately contains other members' data) that household's cached aggregates; jq
	// enqueues a durable retry (internal/queue/jobs.CachePurgeRetryArgs) if the purge itself
	// fails, so a Redis outage never lets a GDPR deletion silently complete with an un-purged
	// cache. All three are nil-safe: nil purger/gens/jq (mirrors a disabled/noop cache, or a
	// caller not yet wired) skips the corresponding step entirely — ExecuteDeletion's own
	// cascade/erasure is unaffected either way.
	SetCache(purger cache.Purger, gens *cache.Generations, jq queue.JobQueue)
}

// HouseholdReader is the narrow household-read capability DeletionService needs.
// repository.HouseholdRepository satisfies this structurally.
type HouseholdReader interface {
	GetHouseholdForUser(ctx context.Context, userID string) (household *domain.Household, isOwner bool, found bool, err error)
	CountActiveMembers(ctx context.Context, householdID string) (int, error)
	// LockHouseholdForUpdate takes a row lock on the household, used to hold the
	// household lock through the mark-for-deletion write (see SetDB doc).
	LockHouseholdForUpdate(ctx context.Context, householdID string) (ownerUserID string, err error)
}

// HouseholdInviteRevoker is the narrow capability DeletionService needs to revoke a
// household's pending invites. repository.HouseholdInviteRepository satisfies this
// structurally.
type HouseholdInviteRevoker interface {
	RevokeAllPendingForHousehold(ctx context.Context, householdID string) error
}

// deletionService implements DeletionService
type deletionService struct {
	revenueCatCleanupSvc     RevenueCatDeletionCleaner
	subscriptionCleanupGuard SubscriptionCleanupGuard
	profileRepo              repository.ProfileRepository
	emailService             EmailService
	clerkService             ClerkService
	auditLogger              AuditLogger
	plaidCleanupSvc          PlaidItemCleanupService         // Issue #668: Remove Plaid items before DB deletion
	paddleCleanupSvc         PaddleDeletionCleaner           // Issue #1006: nil if Paddle not configured
	analytics                phclient.Analytics              // Optional PostHog analytics client
	householdReader          HouseholdReader                 // #1367 — Phase 3: nil skips the owner guard
	inviteRevoker            HouseholdInviteRevoker          // #1367 — Phase 3: nil skips invite revocation
	db                       repository.DB                   // #1367 — Phase 3: nil falls back to non-transactional guard
	claimsRepo               repository.WorkClaimsRepository // Issue #1413 C2: required for ExecuteDeletion
	cachePurger              cache.Purger                    // Issue #1531 Task 10b: nil skips the cache purge
	cacheGens                *cache.Generations              // Issue #1531 Task 10b: nil skips the household bump
	jobQueue                 queue.JobQueue                  // Issue #1531 Task 10b: nil skips the durable-retry enqueue
	gracePeriodDays          int
	tokenSecret              string
}

// accountDeletionClaimLease is the lease duration ExecuteDeletion requests for
// ActionAccountDeletion. 15 minutes is deliberately generous: deletion involves Paddle and
// Plaid calls, both of which can be slow, and the fencing token (see
// repository.WorkClaimsRepository) makes a lease over-run detectable rather than silently
// double-executing external work, so there is no correctness reason to size this tightly.
const accountDeletionClaimLease = 15 * time.Minute

// paddleCancelSideEffectLease is the lease requested for repository.ActionPaddleCancelSubscription
// — the OPERATION-keyed side-effect claim (Issue #1413 H1) wrapped around the actual
// CancelSubscriptionImmediately call, nested inside ExecuteDeletion's own ActionAccountDeletion
// flow claim. Matches plaidDisconnectSideEffectLease's reasoning (plaid_item_cleanup_service.go):
// shorter than the 15-minute flow-claim leases, since this only needs to outlive the single
// Paddle call, not the surrounding cascade/DB work.
const paddleCancelSideEffectLease = 10 * time.Minute

// AuditLogger defines the interface for audit logging. repository.AuditLogRepository is the
// production implementation and satisfies this structurally; see its doc comment for which
// audit_logs column receives which value and why. Until Issue #1505 replaced it, the ONLY
// production implementation was cmd/server/main.go's mockAuditLogger, which logged a line and
// persisted nothing — so every call below produced no durable record at all.
//
// NO ACTOR PARAMETER, DELIBERATELY, FOR NOW. audit_logs distinguishes the ACTING party
// (user_id) from the SUBJECT (resource_id), and this signature can only supply the latter. All
// three remaining production call sites are self-service — a user acting on their own account
// (RequestDeletion, CancelDeletion, RegisterUser) — so no caller has an actor to pass that is not
// either the subject or nothing. The implementation therefore writes user_id NULL rather than
// copying the subject into it, which would assert "this person performed this action" and be
// flatly false for a system-initiated action. If an admin-initiated deletion path is ever added,
// this interface MUST gain an explicit actor first: tracked in Issue #1506.
//
// ⚠ #1506 IS ALSO CONSTRAINED BY LOCKING, NOT ONLY BY HONESTY, AND THE CONSTRAINT IS NOT IN THIS
// PACKAGE. Since Issue #1507 the `user.deleted` entry no longer goes through this interface at all:
// it is written inside DeleteUserCascade's transaction via repository.LogInTx, whose doc comment
// explains why a non-NULL actor on THAT path reintroduces a reproduced SQLSTATE 40P01 deadlock.
// Read it before widening this signature.
//
// ERRORS ARE REPORTED, NEVER SWALLOWED BY THE IMPLEMENTATION — but the two deletion call sites
// below deliberately do NOT abort on one, because both describe a state change that has already
// committed. See the note at RequestDeletion's call for the reasoning, including why the
// in-transaction `user.deleted` write makes the opposite choice.
type AuditLogger interface {
	Log(ctx context.Context, action, resourceType, resourceID string) error
}

// logAuditFailure surfaces an audit-write failure at ERROR level and returns. It exists so the
// non-transactional audit call sites cannot drift apart on how they treat one, and so that "the
// audit trail silently recorded nothing" — the exact defect Issue #1505 closed — can never again be
// the quiet default. A nil error is the overwhelmingly common case and costs nothing.
//
// It is deliberately NOT used for `user.deleted` any more: since Issue #1507 that entry is written
// inside DeleteUserCascade's transaction, where a failure genuinely can and does roll the erasure
// back rather than needing to be tolerated.
func logAuditFailure(ctx context.Context, err error, action, subjectUserID string) {
	if err == nil {
		return
	}
	slog.Default().ErrorContext(ctx, "audit log write failed — the GDPR audit trail is missing an entry",
		"action", action, "resource_type", "user", "resource_id", subjectUserID, "error", err)
}

// PaddleDeletionCleaner is the narrowed Paddle interface used by DeletionService.
// paddleService satisfies this automatically via duck typing. Issue #1006.
type PaddleDeletionCleaner interface {
	CancelSubscriptionImmediately(ctx context.Context, subscriptionID string) error
	ArchiveAndAnonymizeCustomer(ctx context.Context, customerID, anonymizedEmail string) error
}

// NewDeletionService creates a new DeletionService instance
// plaidCleanupSvc can be nil for backwards compatibility, but should be provided
// to properly remove Plaid items and stop billing before user deletion.
func NewDeletionService(
	profileRepo repository.ProfileRepository,
	emailService EmailService,
	clerkService ClerkService,
	auditLogger AuditLogger,
	plaidCleanupSvc PlaidItemCleanupService, // Issue #668: Remove Plaid items before DB deletion
	gracePeriodDays int,
	tokenSecret string,
) DeletionService {
	return &deletionService{
		profileRepo:     profileRepo,
		emailService:    emailService,
		clerkService:    clerkService,
		auditLogger:     auditLogger,
		plaidCleanupSvc: plaidCleanupSvc,
		gracePeriodDays: gracePeriodDays,
		tokenSecret:     tokenSecret,
	}
}

// tokenClaims represents JWT claims for cancellation token
type tokenClaims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// RequestDeletion initiates account deletion with 30-day grace period
func (s *deletionService) RequestDeletion(ctx context.Context, userID, email string) (string, time.Time, error) {
	// Calculate scheduled deletion date (grace period from now)
	scheduledAt := time.Now().Add(time.Duration(s.gracePeriodDays) * 24 * time.Hour)

	// Generate cancellation token (JWT valid for grace period + 1 day buffer)
	cancellationToken, err := s.generateCancellationToken(userID, email, scheduledAt.Add(24*time.Hour))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to generate cancellation token: %w", err)
	}

	// #1367 — Phase 3: block owner deletion while active non-owner members exist, and
	// revoke the household's pending invites when deletion proceeds so a grace-period
	// accept cannot join a doomed household. When s.db is wired, the whole
	// lock/check/revoke/mark-for-deletion sequence runs in ONE transaction (Codex review,
	// High finding 4) — closing a race where an in-flight AcceptInvite commits a new
	// member between this guard's check and MarkForDeletion. Falls back to the
	// non-transactional sequence (still correct, just not atomic against a concurrent
	// accept) when db isn't wired. Full disband-or-transfer UX is Phase 7 (#1371); this
	// is the deletion-side invariant.
	if s.db != nil {
		if err := s.requestDeletionTransactional(ctx, userID, scheduledAt); err != nil {
			return "", time.Time{}, err
		}
	} else {
		if s.householdReader != nil {
			household, isOwner, found, err := s.householdReader.GetHouseholdForUser(ctx, userID)
			if err != nil {
				return "", time.Time{}, fmt.Errorf("failed to check household ownership: %w", err)
			}
			if found && isOwner {
				activeCount, err := s.householdReader.CountActiveMembers(ctx, household.ID)
				if err != nil {
					return "", time.Time{}, fmt.Errorf("failed to count household members: %w", err)
				}
				if activeCount > 1 {
					return "", time.Time{}, domain.ErrHouseholdHasActiveMembers
				}
				if s.inviteRevoker != nil {
					if err := s.inviteRevoker.RevokeAllPendingForHousehold(ctx, household.ID); err != nil {
						return "", time.Time{}, fmt.Errorf("failed to revoke pending household invites: %w", err)
					}
				}
			}
		}

		// Mark user for deletion in database
		if err := s.profileRepo.MarkForDeletion(ctx, userID, scheduledAt); err != nil {
			return "", time.Time{}, fmt.Errorf("failed to mark user for deletion: %w", err)
		}
	}

	// Send confirmation email with cancellation link
	// Email failure is not critical - user is still marked for deletion
	_ = s.emailService.SendDeletionConfirmation(ctx, email, cancellationToken, scheduledAt)

	// Create audit log.
	//
	// AN AUDIT-WRITE FAILURE DOES NOT ABORT THE DELETION, AND IS NOT DISCARDED EITHER. Both halves
	// are deliberate (Issue #1505):
	//
	//   - Not aborting: by the time this runs the state change (MarkForDeletion) has already
	//     committed. Returning an error would report a failure for work that succeeded. There is
	//     nothing to roll back and nothing to retry. A transient database blip must not block a
	//     GDPR request.
	//   - Not discarding: `_ =` was how the stub's permanent uselessness stayed invisible. A write
	//     the database refused is now logged at ERROR with the subject and action, so a missing
	//     trail entry is an alertable event rather than nothing at all.
	//
	// The alternative — make the audit write part of the same transaction as the state change — is
	// the stronger design and IS now taken wherever it is available. Issue #1507 took it for
	// `user.deleted`, which is written inside DeleteUserCascade's own transaction
	// (repository.AuditLogRepository.LogInTx) and DOES abort the deletion on failure, because there
	// the erasure has not committed yet and rolling back merely defers it to the next sweep. It is
	// still NOT available here: MarkForDeletion runs in a transaction this service does not hold
	// open (requestDeletionTransactional commits before returning), so this write has nothing left
	// to join. Same for CancelDeletion's entry.
	logAuditFailure(ctx, s.auditLogger.Log(ctx, "user.deletion_requested", "user", userID),
		"user.deletion_requested", userID)

	// Capture analytics event for deletion request
	if s.analytics != nil {
		s.analytics.Capture(userID, phclient.EventDeletionRequested, nil)
	}

	return cancellationToken, scheduledAt, nil
}

// CancelDeletion cancels a pending deletion using the cancellation token
func (s *deletionService) CancelDeletion(ctx context.Context, tokenString string) error {
	// Parse and validate token
	token, err := jwt.ParseWithClaims(tokenString, &tokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.tokenSecret), nil
	})

	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(*tokenClaims)
	if !ok || !token.Valid {
		return fmt.Errorf("invalid token claims")
	}

	// Check if token is expired
	if claims.ExpiresAt != nil && claims.ExpiresAt.Before(time.Now()) {
		return fmt.Errorf("token expired")
	}

	// Cancel deletion in database
	if err := s.profileRepo.CancelDeletion(ctx, claims.UserID); err != nil {
		return fmt.Errorf("failed to cancel deletion: %w", err)
	}

	// Create audit log. Non-fatal but never silent — see RequestDeletion's call site.
	logAuditFailure(ctx, s.auditLogger.Log(ctx, "user.deletion_cancelled", "user", claims.UserID),
		"user.deletion_cancelled", claims.UserID)

	// Capture analytics event for deletion cancellation
	if s.analytics != nil {
		s.analytics.Capture(claims.UserID, phclient.EventDeletionCancelled, nil)
	}

	return nil
}

// ExecuteDeletion executes the full deletion process for a user. See the DeletionService interface
// doc for why `kind` is a caller-declared parameter.
func (s *deletionService) ExecuteDeletion(ctx context.Context, userID string, kind repository.DeletionClaimKind) error {
	// Get user details before deletion
	user, err := s.profileRepo.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	// Check if already deleted
	if user.IsDeleted {
		// User already deleted - this is not an error, just skip
		return nil
	}

	// Issue #1413: close the grant-vs-delete race. ExecuteDeletion is shared by two callers:
	// the retention cleanup worker (auto-deletion of long-inactive accounts —
	// DeletionRequestedAt is always nil there; all three retention queries in
	// profile_repository.go filter on deletion_requested_at IS NULL) and the scheduled
	// deletion worker (a user-requested GDPR deletion — DeletionRequestedAt is always set
	// there). A ratified product decision: a user's own GDPR deletion request beats an admin
	// comp. So this guard is deliberately conditional on DeletionRequestedAt == nil — do NOT
	// "simplify" it into an unconditional comp check. A blanket guard would illegally strand
	// a comped user who has explicitly asked to be deleted, which is a GDPR compliance
	// problem, not just a bug.
	//
	// This read-only check is kept as a cheap belt-and-braces short-circuit against the
	// GetUserByID snapshot above (avoids an extra round-trip in the common already-comped
	// case), NOT the source of correctness — it cannot, by itself, protect the irreversible
	// work below from a comp granted in the window between this check and that work actually
	// running. The real enforcement is ClaimForDeletion just below: a conditional UPDATE
	// evaluated against committed state at the moment of the claim (Issue #1413 Critical
	// finding C2, Codex review round 2).
	// Deliberately HasActiveManualOverride (live-only), NOT HasAnyManualOverride. This path can
	// reach either authoritative claim predicate below — retentionDeletionClaimPredicate for a
	// genuine auto-deletion, or userRequestedDeletionClaimPredicate for a stale user-requested
	// job on a since-cancelled request (DeletionRequestedAt == nil at this point) — and only the
	// former uses the all-overrides rule via noManualOverrideAtAllPredicate; the latter
	// deliberately has no override check at all, for the same reason given above (a user's own
	// request beats an admin comp). That asymmetry is intentional: this cheap check only
	// short-circuits the case it can be certain
	// about (a currently-live comp). A LAPSED comp is left to fall through to the authoritative
	// claim, which refuses it. Both routes reach the same outcome — the user is not deleted —
	// but routing the lapsed case through the claim keeps the DB predicate the single source of
	// truth and keeps it exercised. Widening this check to all-overrides would short-circuit
	// before the authoritative predicate ran, masking any future divergence between them.
	// Pinned by TestExecuteDeletion_ExpiredComp_NoDeletionRequest_BeltAndBracesDoesNotShortCircuit.
	if user.HasActiveManualOverride() && user.DeletionRequestedAt == nil {
		slog.Default().Info("skipping auto-deletion for actively comped user",
			"user_id", userID)
		return nil
	}

	// Issue #1413 Critical finding C2 (closed for real, round 3): gain EXCLUSIVITY via a
	// durable work claim BEFORE re-verifying eligibility or doing any irreversible work.
	// ClaimForDeletion's self-assigning UPDATE below is an atomic GUARD — it proves the
	// predicate held at THIS instant but reserves nothing, so two retention workers (or a
	// worker and a user-initiated deletion) could both pass it concurrently. The work claim
	// closes that: exactly one caller can hold ActionAccountDeletion's lease for this user at
	// a time. This is not optional — a nil claimsRepo means the exclusivity mechanism this
	// whole branch exists to add is unavailable, so fail loudly rather than silently falling
	// back to guard-only behavior.
	if s.claimsRepo == nil {
		return fmt.Errorf("ExecuteDeletion: work claims repository not configured")
	}
	token, claimed, err := s.claimsRepo.Claim(ctx, repository.ResourceTypeUser, userID, repository.ActionAccountDeletion, accountDeletionClaimLease)
	if err != nil {
		return fmt.Errorf("failed to claim user for deletion: %w", err)
	}
	if !claimed {
		// Another worker already holds this lease, or the action was already completed.
		// This is a legitimate, expected outcome — not an error.
		slog.Default().Info("skipping auto-deletion — another worker holds the deletion claim, or it was already completed",
			"user_id", userID)
		return nil
	}

	// Issue #1413 finding H4: every path below that returns (or panics) without reaching
	// Complete must release the claim, or it leaks for the full 15-minute lease — blocking
	// any retry the whole time. Previously DeleteUserCascade failing did exactly that (no
	// Release call at all), and a panic anywhere in this section leaked it too (no deferred
	// release existed). `released`/`completed` make this defer a no-op on the paths that
	// already release/complete explicitly below, while still catching anything that doesn't
	// — including a bare `return` added carelessly in the future.
	//
	// Every Release call in this function — explicit ones below AND this deferred one — uses
	// a bounded, independent context (context.WithoutCancel + a short timeout), never ctx
	// directly: ctx may already be cancelled by the time a failure is being handled (e.g. the
	// same shutdown/deadline that caused the failure also cancelled the job's context), and
	// releasing with an already-cancelled context would silently fail to release at all.
	completed := false
	released := false
	releaseClaim := func(reason string) {
		if completed || released {
			return
		}
		released = true
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, releaseErr := s.claimsRepo.Release(rctx, repository.ResourceTypeUser, userID, repository.ActionAccountDeletion, token, reason); releaseErr != nil {
			slog.Default().Warn("failed to release deletion claim", "user_id", userID, "error", releaseErr, "release_reason", reason)
		}
	}
	defer func() {
		if r := recover(); r != nil {
			releaseClaim(fmt.Sprintf("panic during deletion: %v", r))
			panic(r) // do not swallow the panic — only ensure the claim is released first
		}
		releaseClaim("deletion returned without completing — releasing to allow retry")
	}()

	// The work claim gives EXCLUSIVITY; ClaimForDeletion's conditional UPDATE still
	// re-verifies CORRECTNESS ("is this user still eligible") against committed state at the
	// moment of the claim — e.g. an admin comp granted between the GetUserByID read above and
	// this call. The two are complementary, not redundant (see the #1413 design doc's required
	// call shape): the claim alone cannot know whether deletion is still authorised.
	eligible, err := s.profileRepo.ClaimForDeletion(ctx, userID, kind)
	if err != nil {
		releaseClaim(err.Error())
		return fmt.Errorf("failed to claim user for deletion: %w", err)
	}
	if !eligible {
		// The authorisation this caller was issued under no longer holds. For a retention caller:
		// an admin granted or extended a comp between the GetUserByID read above and this check.
		// For a user-requested caller (Issue #1440, T0): the user cancelled the request this job
		// was queued for, or re-requested with a fresh grace period that has not yet elapsed —
		// which is exactly the case that used to slip through and hard-delete a user who had asked
		// to keep their account. Release immediately so a later run doesn't have to wait out the
		// full lease. Legitimate outcome, not an error.
		releaseClaim("precondition lost — deletion no longer authorised under this claim kind")
		slog.Default().Info("skipping deletion — claim not granted (authorisation no longer holds)",
			"user_id", userID, "claim_kind", string(kind))
		return nil
	}

	// stillAuthorised re-asks, against committed state RIGHT NOW, the exact question
	// ClaimForDeletion answered when this run started: is a hard delete of this user still
	// authorised under `kind`, for the episode `user.DeletionRequestedAt` pins?
	//
	// WHY EVERY IRREVERSIBLE EXTERNAL CALL BELOW IS PRECEDED BY THIS. The claim above gives
	// EXCLUSIVITY — it stops two flows doing this work at once. It says nothing about whether THIS
	// flow's own authorisation still holds, and CancelDeletion (profile_repository.go) clears
	// deletion_requested_at and deletion_scheduled_at without coordinating with the claim at all.
	// Before this, a user who cancelled their deletion partway through the cleanup sequence still
	// had their subscription cancelled, their Paddle customer archived and anonymised, and their
	// bank connections severed — and only THEN did DeleteUserCascade's in-transaction guard
	// correctly refuse to delete them, leaving an account stripped of everything, irreversibly, for
	// someone who had explicitly asked to keep it.
	//
	// ⚠ THIS IS NOT ErrSideEffectClaimBusy AND DOES NOT DUPLICATE IT. That mechanism (#1413 H1)
	// coordinates BETWEEN flows so two flows cannot both disconnect a user; this asks whether this
	// flow may still act at all. Both are needed and neither implies the other.
	//
	// ⚠ IT DOES NOT CLOSE THE WINDOW, IT SHRINKS IT. A revocation landing between one of these
	// checks and the call two lines later is not caught — no database mechanism can make that
	// interval zero, and a worker already inside a Paddle call cannot be recalled. What changes is
	// the size: from "the whole cleanup sequence, including one network round-trip per linked item"
	// down to "one call". The same irreducible-TOCTOU argument cleanupItems makes for its own
	// per-item recheck applies here verbatim.
	stillAuthorised := func(c context.Context) (bool, error) {
		if kind == repository.DeletionKindRetention && s.subscriptionCleanupGuard != nil {
			allowed, err := s.subscriptionCleanupGuard.CanCleanup(c, userID)
			if err != nil || !allowed {
				return false, err
			}
		}
		return s.profileRepo.DeletionStillAuthorised(c, userID, kind, user.DeletionRequestedAt)
	}
	// abortIfUnauthorised returns a non-nil "handled" signal the caller returns directly. An
	// undetermined answer (err != nil) FAILS CLOSED with an error, because not knowing whether the
	// deletion is still authorised is not permission to perform an irreversible external call. A
	// determined "no" is a legitimate outcome and returns nil, exactly like the !eligible branch
	// above and the cascade's own (false, nil).
	abortIfUnauthorised := func(stage string) (bool, error) {
		authorised, err := stillAuthorised(ctx)
		if err != nil {
			releaseClaim(err.Error())
			return true, fmt.Errorf("aborting deletion before %s — could not establish whether deletion is still authorised: %w", stage, err)
		}
		if !authorised {
			releaseClaim("authorisation revoked since the claim — aborting before " + stage)
			slog.Default().Info("aborting deletion — the authorisation this run was claimed under no longer holds",
				"user_id", userID, "claim_kind", string(kind), "stage", stage)
			return true, nil
		}
		return false, nil
	}

	// ⚠ ROUND 15, FINDING D2 — THE HOUSEHOLD-OWNER PRECONDITION, ASKED BEFORE THE FIRST IRREVERSIBLE
	// EFFECT RATHER THAN AFTER THE LAST ONE.
	//
	// DeleteUserCascade already refuses to erase a household owner whose household still has another
	// active member or another beneficiary's live entitlement — households.owner_user_id CASCADEs, so
	// erasing them destroys a DIFFERENT real user's rows (Issue #1440). But that guard is evaluated
	// inside the cascade's own transaction, which is the LAST thing this function does. By then every
	// Plaid Item has been disconnected and the Paddle customer has been cancelled and archived — the
	// act this file itself calls "THE ONE ACTION NOTHING CAN UNDO".
	//
	// ⚠ AND THIS CONDITION CANNOT SELF-HEAL. Its own guard comment says so: nothing in the system will
	// transfer the ownership, remove the member, or reconcile the stale entitlement. So the retention
	// path — which reaches ExecuteDeletion directly (retention_cleanup_check.go calls it in process)
	// and therefore never passes through RequestDeletion's #1367 owner check — did the whole
	// irreversible sequence, was refused, and then DID IT AGAIN THE NEXT DAY, indefinitely.
	//
	// ⚠ THIS IS A REORDER, NOT A NEW RULE, AND DELIBERATELY SO. It asks the same SQL predicate the
	// cascade asks (householdOwnerBlocksDeletionClause, shared verbatim), and it is a pure local-state
	// question needing no external call, so asking it here costs one round-trip and removes the entire
	// class. It is placed BEFORE the first abortIfUnauthorised/Plaid leg for exactly that reason.
	//
	// ⚠ THE IN-CASCADE GUARD STAYS — defence in depth, and it is not redundant. This read runs outside
	// the cascade's transaction and its FOR UPDATE lock: an invite accepted in the interval is caught
	// only in there. This check shrinks the window in which the harm is irreversible; that guard is
	// what makes the outcome correct.
	//
	// Returns a genuine error, matching the cascade guard's own choice and for its reason: a silent
	// (false, nil) skip would let a GDPR deadline go unmet forever with no visible signal, and this is
	// the one deletion stop that requires a human to actually resolve the household.
	//
	// ⚠ AND IT FIRES THE COUNTER, WHICH THE REORDER INITIALLY DID NOT (EE re-review). The comment
	// here used to say the metric was "left to the cascade's guard so the counter keeps meaning
	// ... once per occasion rather than twice" — an argument that was true of the ORDER it
	// replaced and false the moment this check began returning before the cascade ran. The only
	// Inc() lives inside DeleteUserCascade, which this branch never reaches, so the one dedicated
	// alert for a non-self-healing GDPR block went permanently flat while the block itself got
	// MORE common (the retention path now stops here every day instead of inside the cascade).
	// It is still at most once per occasion: the cascade's Inc() now only fires for the race this
	// read cannot see — an invite accepted between this statement and the cascade's FOR UPDATE —
	// which is a different occasion, not this one counted twice.
	householdBlocks, err := s.profileRepo.HouseholdOwnerBlocksDeletion(ctx, userID)
	if err != nil {
		// Fail closed: not knowing whether erasing this user would destroy someone else's data is not
		// permission to start disconnecting banks and archiving Paddle customers.
		releaseClaim(err.Error())
		return fmt.Errorf("aborting deletion before any external cleanup — could not establish whether this user owns a household that blocks deletion: %w", err)
	}
	if householdBlocks {
		observability.HouseholdOwnerDeletionBlockedTotal.Inc()
		releaseClaim("household ownership blocks deletion — refusing before any irreversible external work")
		slog.Default().Error("aborting deletion before any external cleanup — this user owns a household with another active member or another beneficiary's live entitlement, so erasing them would destroy a different user's data; this needs a human to transfer or disband the household",
			"user_id", userID, "claim_kind", string(kind))
		return fmt.Errorf("aborting deletion — %w", domain.ErrHouseholdHasActiveMembers)
	}

	// ⚠ ORDERING IS DELIBERATE AND WAS CHANGED: PLAID FIRST, PADDLE SECOND. It used to be the other
	// way round. The two legs are not equally reversible — a Plaid disconnect costs the user a
	// re-link through Plaid Link, whereas ArchiveAndAnonymizeCustomer destroys PII at Paddle and
	// cannot be undone by anyone. Putting the least-reversible call LAST means the widest exposure
	// window carries the cheapest possible loss, and it means an abort or failure in the Plaid leg
	// (including the routine ErrSideEffectClaimBusy deferral) no longer leaves a still-subscribed
	// user's subscription cancelled with the deletion abandoned — which is what the old order did on
	// every Plaid failure, not merely on a revocation.
	if handled, err := abortIfUnauthorised("Plaid cleanup"); handled {
		return err
	}

	// Issue #668: Remove Plaid items BEFORE database deletion to stop billing. This is
	// critical for cost control — Plaid charges monthly per Item. A failure here now also
	// aborts (see the Paddle cleanup comment above for why) rather than proceeding to a
	// cascade that would strand an un-removed, still-billing Plaid item with no local record.
	//
	// Issue #1494: the nil branch is REACHABLE and no longer silent. main.go constructs this service
	// with nil for plaidCleanupSvc and calls SetPlaidCleanupService later only inside
	// `if cfg.PlaidClientID != "" && cfg.PlaidSecret != ""` (verified at the two call sites), so a
	// Plaid-less deployment runs with it nil for the process's whole life — which is legitimate and
	// must keep working, so the check belongs here rather than as a startup assertion. What it must not
	// do is skip silently: nothing in such a deployment can ever record a Plaid removal, so a user WITH
	// items whose removal is unproven hits DeleteUserCascade's C2 guard and gets (false, nil) — an
	// erasure that never completes, reported as "not deleted yet", with the actual cause (no Plaid
	// wiring) three layers away and stated nowhere.
	if s.plaidCleanupSvc == nil {
		// Counts the guard's EXACT population (uncleanedLinkedItemPredicate — neither removal proved
		// nor removal proved impossible), not "any linked item at all". A user whose items are all
		// already accounted for would sail through C2, and refusing THEM would be a self-inflicted
		// block on erasures that were fine — the failure mode this branch exists to expose, inverted.
		uncleanedCount, countErr := s.profileRepo.CountUncleanedLinkedItems(ctx, userID)
		if countErr != nil {
			// Fail closed: not knowing whether Plaid work is owed is not permission to proceed to an
			// irreversible erasure.
			releaseClaim(countErr.Error())
			return fmt.Errorf("aborting deletion — Plaid cleanup service not configured and the outstanding linked-item count could not be established: %w", countErr)
		}
		if uncleanedCount > 0 {
			releaseClaim(fmt.Sprintf("Plaid cleanup service not configured but user has %d linked item(s) with unproven Plaid removal", uncleanedCount))
			return fmt.Errorf("aborting deletion — Plaid cleanup service not configured but user has %d linked item(s) whose Plaid removal is unproven — refusing to delete", uncleanedCount)
		}
		// Zero outstanding items: there is no Plaid work to do, so a Plaid-less deployment erasing this
		// user is correct and safe. Proceed to the cascade, whose C2 guard re-checks this inside its
		// own transaction anyway (this count is diagnosis, not the authority — see its doc comment).
		slog.Default().Info("Plaid cleanup service not configured; user has no linked items with unproven Plaid removal, so deletion proceeds",
			"user_id", userID)
	} else {
		// stillAuthorised is the per-item eligibility callback, and passing it is the point: this call
		// site passed a literal nil until now, so cleanupItems' recheck — a parameter that exists for
		// exactly this and that DisconnectAllUserItemsIfEligible has always used — never fired for the
		// deletion flow. On an account with 200 linked items that meant 200 sequential Plaid
		// round-trips after a single authorisation read. Now a revocation landing partway through
		// stops the loop at the next item (cleanupItems returns ErrCleanupAborted; see the abort
		// branch below for how that is distinguished from a genuine Plaid failure).
		if err := s.plaidCleanupSvc.RemoveAllUserItems(ctx, userID, stillAuthorised); err != nil {
			// A revocation-triggered abort is not a Plaid failure and must not be reported as one. The
			// recheck is asked once more to decide which of ErrCleanupAborted's two causes this was:
			// authorisation genuinely gone (a legitimate, expected stop — release and return nil, and
			// notably WITHOUT proceeding to the Paddle leg) versus a new linked item having appeared
			// mid-pass (retryable; falls through to the error path below). An error from the recheck
			// itself falls through too, which is the fail-closed direction.
			if errors.Is(err, ErrCleanupAborted) {
				if authorised, checkErr := stillAuthorised(ctx); checkErr == nil && !authorised {
					releaseClaim("authorisation revoked mid Plaid cleanup — remaining items left connected")
					slog.Default().Info("aborting deletion mid Plaid cleanup — the authorisation this run was claimed under no longer holds; the user keeps their account and any items not yet disconnected",
						"user_id", userID, "claim_kind", string(kind))
					return nil
				}
			}
			// Issue #1413 H1: RemoveAllUserItems can return ErrSideEffectClaimBusy when another
			// flow (past_due grace-cancel, the subscription-cancellation webhook, or another
			// deletion attempt) currently holds ActionPlaidDisconnectUser for this user. This is a
			// normal, expected outcome — log it at Info distinctly from a genuine Plaid failure —
			// but it must still abort THIS deletion attempt exactly like any other cleanup
			// failure: release the flow claim and let the next run retry.
			if errors.Is(err, ErrSideEffectClaimBusy) {
				slog.Default().Info("deletion Plaid disconnect deferred — side-effect claim held by another flow",
					"user_id", userID)
			}
			// Issue #1494: ONE failure kind does not abort, and this is the only caller in the codebase
			// that treats it that way. ErrPlaidRemovalUnrecoverable means every item that failed did so
			// in a way no retry can fix, AND that verdict is durably recorded on each row (the sentinel
			// is not returned otherwise — see its doc comment). Aborting here is what made #1494
			// permanent: the next run fails identically, forever, and the user's GDPR erasure never
			// completes.
			//
			// Proceeding is safe only because two INDEPENDENT things stand between this line and an
			// erased Item: DeleteUserCascade's C2 guard re-reads each row and still blocks unless the
			// verdict really is recorded there, and the same transaction copies every
			// not-provably-removed row into plaid_orphaned_items before deleting anything. So a bug in
			// this branch alone cannot lose an Item — it can only fail to unblock. Logged at Error, not
			// Info: we are completing an erasure while a live Plaid Item may still be billing, which is
			// an operator's problem the moment it happens, not a routine event.
			if errors.Is(err, ErrPlaidRemovalUnrecoverable) {
				// ⚠ ROUND 15, FINDING D1 — PROCEEDING IS ONLY CORRECT ONCE C2 CAN ACTUALLY BE PASSED, AND
				// ON THE PASS THAT WRITES THE VERDICT IT PROVABLY CANNOT.
				//
				// The paragraph above is right that C2 makes proceeding SAFE. What it missed is that on
				// this pass C2 is not merely a backstop, it is a CERTAINTY: uncleanedLinkedItemPredicate
				// blocks while `plaid_removal_failed_at > now() - INTERVAL '7 days'`, and the verdict that
				// produced this sentinel was written by this very pass. So the old code went on to cancel
				// the subscription and archive-and-anonymise the Paddle customer — irreversibly — and then
				// hit a refusal that was guaranteed before the first of those calls was made. Daily, for a
				// week, each run now also emitting its own residue record with nothing to dedupe them.
				//
				// ⚠ THE 7-DAY WINDOW IS NOT THE BUG AND IS NOT TOUCHED. Its argument (see
				// uncleanedLinkedItemPredicate) is sound: Plaid returns INVALID_ACCESS_TOKEN both for a
				// genuinely dead token and for a token used against the wrong environment or client_id, so
				// the terminal verdict is an INFERENCE. The window exists to convert it into an
				// OBSERVATION, and the daily retries that happen during it are the mechanism — unblocking
				// on first sight would let one misconfigured deploy erase over the whole estate. Shortening
				// or conditioning the window would weaken exactly that. So the window keeps its full
				// length, and what changes is that this flow stops spending irreversible external effects
				// on a wait it is guaranteed to lose. The waiting was always the design; only the
				// spending-while-waiting was not.
				//
				// #1494 SURVIVES INTACT. This is a deferral bounded by the same window C2 already imposes,
				// not a return to the permanent abort #1494 fixed: once the verdict ages past seven days
				// the count falls to zero, this branch proceeds exactly as before, and the erasure
				// completes. The claim is released so the next daily sweep retries — and those retries are
				// precisely what makes the window an observation.
				//
				// FAIL CLOSED on an undeterminable count, like every other unknown in this function.
				deferred, deferErr := s.deferIfCascadeGuardWillRefuse(ctx, userID, kind, releaseClaim)
				if deferred {
					return deferErr
				}
				slog.Default().Error("proceeding with deletion despite Plaid item(s) that can never be removed — the obligation is recorded and will be preserved in plaid_orphaned_items; the Item(s) may still exist and still bill at Plaid",
					"user_id", userID, "error", err)
			} else {
				releaseClaim(err.Error())
				return fmt.Errorf("aborting deletion — Plaid cleanup failed: %w", err)
			}
		}
	}

	// Issue #1413 Critical finding C2: external cleanup failure now ABORTS the deletion
	// instead of being swallowed. DeleteUserCascade (below) clears paddle_subscription_id and
	// Plaid references, so proceeding to the cascade after a cleanup failure would destroy the
	// local record of the very external resources that failed to clean up — orphaning them
	// with nothing left to retry. Releasing the claim lets the NEXT run retry the whole
	// sequence from scratch instead.
	//
	// THIS LEG NOW RUNS AFTER THE PLAID LEG, not before it — see the ordering note above the first
	// abortIfUnauthorised call for why the least-reversible external work goes last.
	//
	// THERE IS DELIBERATELY NO abortIfUnauthorised CALL HERE. One was written and then removed as
	// provably dead: cleanupPaddleResources gates each of its two irreversible calls individually,
	// and those gates are strictly later and strictly finer than a check at this line would be. The
	// redundancy was established by mutation, not by reading — deleting a check here killed no test,
	// while deleting either inner gate kills exactly one. A check that cannot fail is not protection;
	// it is a second place for the rule to drift.
	//
	// ⚠ ROUND 15, TIER A — WHAT USED TO BE HERE AND WHY IT WAS WRONG AT THE ROOT. This block held a
	// single in-memory bool, `subscriptionCancelled`, set only when CancelSubscriptionImmediately
	// SUCCEEDED, and a closure that early-returned unless it was true. That bool was a PROXY for "an
	// irreversible external effect landed", and every path that lost the proxy lost the record:
	//
	//	A1  ArchiveAndAnonymizeCustomer set no flag and had no vocabulary, so the act this file itself
	//	    calls "THE ONE ACTION NOTHING CAN UNDO" could never be recorded at all.
	//	A2  A cancel that changed Paddle state but returned an error left the flag false.
	//	A3  The flag lived only in memory: an ordinary error after a landed cancel recorded nothing,
	//	    and process death or a panic took the fact with it.
	//
	// The fix is not five branches. Each irreversible external call now writes its OWN record, to the
	// database, immediately BEFORE it is issued — see recordIssuedExternalEffect and its two call
	// sites in cleanupPaddleResources. What survives here is only the ALARM and the one record that
	// describes an OUTCOME rather than an effect (the cascade-failed kind below).
	effects := paddleEffectsIssued{}
	// reportSurvivingResidue is the operator ALARM, not the record — the record for each effect was
	// already written when that effect was issued. It fires on the paths that end with the user still
	// present after irreversible external work had already gone out, so the several ways the same
	// harm happens cannot diverge in what an operator is told.
	reportSurvivingResidue := func(reason string) {
		if !effects.any() {
			return
		}
		slog.Default().Error("account deletion stopped after irreversible external work had already been issued — the user KEEPS their account and data; this needs a human",
			"user_id", userID, "claim_kind", string(kind),
			"subscription_cancel_issued", effects.subscriptionCancel,
			"customer_archive_issued", effects.customerArchive, "revenuecat_erasure_issued", effects.revenueCatErasure, "reason", reason)
	}
	if s.revenueCatCleanupSvc != nil {
		if err := s.revenueCatCleanupSvc.Prepare(ctx, userID, kind == repository.DeletionKindRetention, stillAuthorised); err != nil {
			releaseClaim(err.Error())
			if errors.Is(err, ErrDeletionAuthorisationRevoked) {
				return nil
			}
			return fmt.Errorf("prepare RevenueCat erasure: %w", err)
		}
	}
	if s.paddleCleanupSvc != nil || s.revenueCatCleanupSvc != nil {
		var issued paddleEffectsIssued
		var err error
		if s.revenueCatCleanupSvc != nil {
			issued, err = s.cleanupRevenueCatPaddleResources(ctx, userID, kind, user, stillAuthorised)
		} else {
			issued, err = s.cleanupPaddleResources(ctx, userID, kind, user, stillAuthorised)
		}
		effects = issued
		if err != nil {
			if errors.Is(err, ErrDeletionAuthorisationRevoked) {
				releaseClaim("authorisation revoked mid Paddle cleanup")
				slog.Default().Info("aborting deletion mid Paddle cleanup — the authorisation this run was claimed under no longer holds",
					"user_id", userID, "claim_kind", string(kind))
				reportSurvivingResidue("authorisation revoked between the subscription cancel and the customer archive")
				return nil
			}
			// A Paddle FAILURE (as opposed to a revocation) still releases the claim so the next run
			// retries the whole sequence. What CHANGED in round 15 (finding A2/A3): it no longer
			// implies "nothing to report". Whatever calls were already issued are already on record,
			// written before they went out, and a retry that completes removes them again — see
			// recordIssuedExternalEffect.
			releaseClaim(err.Error())
			reportSurvivingResidue("Paddle cleanup failed after external work had already been issued: " + err.Error())
			return fmt.Errorf("aborting deletion — Paddle cleanup failed: %w", err)
		}
	}
	if s.revenueCatCleanupSvc != nil {
		err := s.revenueCatCleanupSvc.Erase(ctx, userID, func(c context.Context) (bool, error) {
			allowed, e := stillAuthorised(c)
			if e != nil || !allowed {
				return allowed, e
			}
			if e = s.recordIssuedExternalEffect(c, userID, kind, repository.DeletionResidueRevenueCatCustomerErased, "about to erase RevenueCat customer", true); e != nil {
				return false, e
			}
			effects.revenueCatErasure = true
			return true, nil
		})
		if err != nil {
			releaseClaim(err.Error())
			reportSurvivingResidue("RevenueCat erasure stopped: " + err.Error())
			if errors.Is(err, ErrDeletionAuthorisationRevoked) {
				return nil
			}
			return fmt.Errorf("RevenueCat erasure: %w", err)
		}
	}

	// Execute cascade deletion in database. Issue #1413 finding H4: a failure here used to
	// return WITHOUT releasing the claim at all — the one path that leaked it every time this
	// step failed, since there was no explicit Release call and no deferred one. releaseClaim
	// (called via the defer above on any non-completing return) now covers this too.
	//
	// Issue #1413 round-4 Critical finding C1: DeleteUserCascade now re-verifies eligibility
	// INSIDE its own transaction, immediately before its destructive statements, using
	// user.DeletionRequestedAt — the value read at the very top of this function, BEFORE
	// ClaimForDeletion — as the expected occurrence. A comp granted, or (the serious case) the
	// user cancelling their own deletion request, during the external-cleanup window above (or
	// even between GetUserByID and ClaimForDeletion) flips deletion_requested_at or
	// manual_tier_override away from what was observed here, and the cascade's own guard catches
	// it. deleted == false is a NORMAL, EXPECTED outcome — NOT an error — covering that
	// precondition-lost case AND Critical finding C2 (an un-cleaned Plaid item still exists):
	// release the claim (never Complete) and let the next scheduled run re-evaluate.
	// Issue #1531 Task 10b: capture the user's household membership BEFORE the cascade runs — the
	// cascade's household_members row for this user is gone once DeleteUserCascade commits, so a
	// post-cascade lookup would always report "not found" and the household's cached aggregates
	// (which genuinely contain this user's transactions, joined across shared household accounts)
	// would never get orphaned. A lookup failure here is logged and treated as "no household" —
	// this must never abort a GDPR deletion that has already passed every eligibility check above.
	// Issue #1531 Blocker 3: this lookup used to be attempted exactly once, and a failure was
	// logged and silently treated as "no household" — with NO retry of any kind, not even the
	// durable (River) retry cache_purge_retry.go provides for a purge/bump failure. That durable
	// retry mechanism genuinely CANNOT cover a failure here, though: DeleteUserCascade (called
	// right below) deletes this user's household_members row as part of the same cascade, so a
	// retry job running even a few seconds later — after cascade commits — would look up "does
	// this user belong to a household" and correctly, uselessly, find nothing; not because the
	// bump failed, but because the fact it needs (the membership) has legitimately ceased to
	// exist. This is therefore the ONLY window in which the household ID can still be recovered
	// at all, which is why the retry lives HERE, bounded and synchronous, rather than as a River
	// job: it targets the realistic failure mode (a transient DB blip) inside the one window
	// where success is even possible, rather than manufacturing a "durable" retry that would
	// always find nothing. See lookupHouseholdWithRetry's own doc comment for the constants.
	var cachePurgeHouseholdID string
	if s.householdReader != nil {
		household, _, found, hhErr := s.lookupHouseholdWithRetry(ctx, userID)
		if hhErr != nil {
			slog.Default().Error("gdpr deletion: household lookup failed before cascade after retries; "+
				"the user's household (if any) will not have its cache orphaned", "user_id", userID, "error", hhErr)
		} else if found {
			cachePurgeHouseholdID = household.ID
		}
	}

	deleted, affectedOwnerIDs, err := s.profileRepo.DeleteUserCascade(ctx, userID, user.DeletionRequestedAt)
	if err != nil {
		releaseClaim(err.Error())
		// Round 12 finding AB-1: the THIRD way a user survives a run whose Paddle cancel already
		// landed, and until now the only one that recorded nothing at all. #1504 covered the gate
		// decline and the cascade's (false, nil) refusal; this branch was left out on the grounds
		// that an errored run still retries into success, so the user is not being LEFT in the state
		// the record describes.
		//
		// #1507 broke that premise without removing it. Making the terminal audit write abort the
		// cascade created a class of error — a revoked INSERT privilege on audit_logs, a new CHECK
		// or RLS policy, a trigger — that no retry clears. Every sweep re-runs the whole sequence,
		// re-cancels nothing (there is nothing left to cancel) and fails identically, while the user
		// keeps their account with no subscription and severed bank links, and BOTH of #1504's
		// metrics read zero. The harm the metrics were built for, occurring invisibly.
		//
		// ⚠ NOT "record on every cascade error" — that is what b76e1b8a's reasoning rules out and it
		// is still right. cascadeErrorIsRecognisedTransient names the classes for which the retry
		// premise genuinely holds and stays silent for those; everything else, including anything
		// unrecognised, records. Its doc comment argues the default direction.
		//
		// ⚠ NOT REMEDIATION. Nothing here re-subscribes anyone (see RecordDeletionResidue's own doc
		// comment); this makes an existing, already-occurring harm visible to a human.
		//
		// ⚠ ROUND 15, FINDING A4 — THE DECISION, AND HOW IT ENGAGES ROUND 14 RATHER THAN REVERSING IT
		// SILENTLY. Round 14's argument for silence on a recognised-transient error was: a permanent
		// operator artifact is wrong for an incident that resolves itself, BECAUSE THE RETRY
		// RECONCILES. That premise was sound and is still sound — but nothing implemented it. It was
		// an assumption about a future run, and it fails outright when no future run is authorised:
		// if the user cancels their deletion (or resubscribes) before the retry, neither sweep will
		// ever select them again and no work item exists anywhere.
		//
		// Round 15 makes the premise TRUE INSTEAD OF ASSUMED. Every irreversible external call now
		// records itself as it is issued, and DeleteUserCascade DELETES this user's residue rows
		// inside the same transaction that deletes the user — so the reconciliation round 14 was
		// relying on is now a real, atomic step rather than a hope. That inverts the cost: recording
		// on a transient failure is no longer a permanent artifact, it is a work item that the
		// successful retry retracts by itself. So the answer to A4 is RECORD ON TRANSIENT ERRORS TOO
		// — and it needs no branch here, because the landing records are already written.
		//
		// cascadeErrorIsRecognisedTransient is KEPT, doing the job it was actually designed for and
		// only that job: distinguishing "the deletion is OVER" from "the deletion is STUCK on a fault
		// a retry will not clear", which is a second, different task for the operator. That is what
		// the extra ..._cascade_failed row says, over and above the landing records.
		//
		// ⚠ ROUND 15, TIER E ITEM 8 — THE ARCHIVE-ONLY RUN NOW GETS ITS OWN STUCK ROW.
		//
		// This used to read `if effects.subscriptionCancel && ...` alone, justified as: the constant
		// NAMES the subscription, and an archive-only run "is still visible — its own landing record
		// is on file and reportSurvivingResidue fires — it simply does not get the second row". That
		// is right about the constant and wrong about the operator. The landing record says WHAT was
		// lost; neither it nor the log says whether the deletion is OVER or STUCK, and that
		// distinction is the entire reason a second kind exists. So an operator filtering for stuck
		// deletions saw every ex-subscriber's and none of the archive-only ones — and the archive-only
		// shape is not exotic: it is the deterministic retention-sweep case (an ex-subscriber whose
		// subscription is already cancelled, so there is nothing to cancel and only the customer to
		// archive).
		//
		// The remedy is a PARALLEL constant, not a widened one: recording
		// `..._subscription_cancelled_cascade_failed` for a run in which no subscription was cancelled
		// would put a false statement in an operator-facing action, which is what the original gate
		// was correctly avoiding. Each irreversible effect that was issued gets the stuck row that
		// names IT.
		// ⚠ THESE TWO ARE STILL BEST-EFFORT, AND THAT IS NOT AN INCONSISTENCY WITH THE FAIL-CLOSED
		// PRE-ISSUE WRITES. Fail-closed is meaningful only while the effect is still preventable;
		// here the external work has ALREADY landed and the cascade has already failed, so there is
		// nothing left to refuse. The error is discarded deliberately — recordIssuedExternalEffect
		// has already logged it at Error, and there is no better escalation available.
		if !cascadeErrorIsRecognisedTransient(err) {
			if effects.subscriptionCancel {
				_ = s.recordIssuedExternalEffect(ctx, userID, kind,
					repository.DeletionResiduePaddleSubscriptionCancelledCascadeFailed,
					"the cascade failed with a non-transient error after the subscription cancel had been issued: "+err.Error(), false)
			}
			if effects.customerArchive {
				_ = s.recordIssuedExternalEffect(ctx, userID, kind,
					repository.DeletionResiduePaddleCustomerArchivedCascadeFailed,
					"the cascade failed with a non-transient error after the customer archive had been issued: "+err.Error(), false)
			}
		}
		reportSurvivingResidue("the cascade failed after external work had already been issued: " + err.Error())
		return fmt.Errorf("failed to cascade delete user data: %w", err)
	}
	if !deleted {
		releaseClaim("cascade aborted — eligibility lost, or un-cleaned Plaid items remain, since the claim was taken")
		slog.Default().Info("skipping deletion cascade — re-verification inside the cascade transaction found the user no longer eligible",
			"user_id", userID)
		// Issue #1504: the SECOND way a user survives a run whose Paddle cancel already landed. The
		// harm is identical to the gate-decline case above — account kept, subscription gone — so the
		// alarm must be identical too; covering only the gate would silently miss this half. The
		// RECORD is not re-written here: since round 15 it was already written when the effect was
		// issued, which is the only version of this that survives a process that never gets here.
		reportSurvivingResidue("the cascade's own re-verification refused after external work had already been issued")
		return nil
	}

	// Issue #1531 Task 10b: GDPR deletion cache purge. `deleted == true` above is proof the erasure
	// already committed — this is purely about the cache's OWN, separate copies of that user's
	// data (and, for the household scope, orphaning this user's contribution to it). See
	// purgeCacheForDeletedUser's doc comment for the full design (why u:/k: are deleted outright
	// while the household scope is only bumped, and the durable-retry-on-failure behaviour).
	s.purgeCacheForDeletedUser(ctx, userID, user.ClerkUserID, cachePurgeHouseholdID)

	// GDPR round-6 finding P1: DeleteUserCascade's dangling cross-user custom-category-override
	// clear rewrites OTHER (surviving) users' transactions rows in place — purgeCacheForDeletedUser
	// above only ever bumps the DELETED user's own scopes, so without this, a surviving owner's warm
	// analytics cache keeps serving the just-erased user's custom category name until the cache
	// namespace's TTL elapses. §commit-then-bump: this runs only after DeleteUserCascade has
	// returned true (already committed above), and bumpCacheForAffectedOwners is void by design —
	// a bump failure here must never fail a deletion that has already committed.
	s.bumpCacheForAffectedOwners(ctx, affectedOwnerIDs)

	// Mark the claim durably complete. A false ok (with no error) means our lease was
	// reclaimed by another worker while we were doing the work above — i.e. the external
	// cleanup and cascade we just ran may have ALSO been run by whoever holds the claim now.
	// That is precisely the failure mode this whole mechanism exists to make visible instead
	// of silent, so it is logged at Error with enough context to investigate, not swallowed.
	//
	// `completed` is set to true regardless of the Complete outcome (ok true/false), as long
	// as the Complete call itself didn't error: by this point DeleteUserCascade has already
	// run, so there is nothing left for the deferred release to protect — and if our lease
	// was reclaimed (ok=false), the row now belongs to whoever holds the CURRENT token, and
	// our Release would rightly no-op against it anyway (fencing check), so there is no
	// reason to attempt it. Only a genuine error from Complete itself leaves `completed`
	// false, letting the deferred release fire so a stuck claim doesn't linger forever.
	completedOk, completeErr := s.claimsRepo.Complete(ctx, repository.ResourceTypeUser, userID, repository.ActionAccountDeletion, token)
	if completeErr != nil {
		slog.Default().Error("failed to mark deletion claim complete", "user_id", userID, "action", repository.ActionAccountDeletion, "error", completeErr)
	} else {
		completed = true
		if !completedOk {
			slog.Default().Error("deletion claim lease lost mid-flight — external cleanup and cascade may have run twice",
				"user_id", userID, "action", repository.ActionAccountDeletion)
		}
	}

	// Delete user from Clerk (non-critical)
	// Note: user.ClerkUserID stores the Clerk user ID (user_xxx format)
	// The database column name is legacy and will be renamed in a future migration
	_ = s.clerkService.DeleteUser(ctx, user.ClerkUserID)

	// NO `user.deleted` AUDIT WRITE HERE ANY MORE — Issue #1507. It used to be this line, issued
	// after DeleteUserCascade had already committed, with its error routed to logAuditFailure. That
	// made the single most important entry in the GDPR trail the one entry that could go missing
	// while the erasure it describes was permanent and had nothing left to retry from.
	//
	// It is now DeleteUserCascade's own last statement before COMMIT (repository.LogInTx), so the
	// record and the erasure land together or not at all. `deleted == true` above is therefore
	// already proof that the entry was written. Do NOT re-add a call here: it would produce a second
	// `user.deleted` row per deletion, which TestExecuteDeletion_Integration_RetentionCandidate_
	// IsDeleted refutes — that is the test that runs THIS function against a real database and
	// counts the rows. (Its repository-layer sibling,
	// TestDeleteUserCascade_Integration_AuditEntryIsWrittenExactlyOncePerDeletion, calls the cascade
	// directly and so cannot see this layer at all.)
	//
	// ⚠ AND NOTE WHAT A FAILURE OF THAT WRITE NOW COSTS. Because it aborts the cascade, an audit
	// INSERT that fails PERSISTENTLY (a revoked INSERT privilege on audit_logs, a new constraint or
	// RLS policy) gates this user's erasure for as long as it lasts — after the Plaid disconnect and
	// possibly the Paddle cancellation above have already run and cannot be undone. The trade is
	// argued at the write site in profile_repository.go; the part that lands HERE is that such a run
	// takes the error branch ABOVE. That branch used to record no Paddle-cancel residue at all, on
	// the grounds that the run was still expected to complete — a premise this very failure mode
	// breaks. It no longer does: the branch now records a
	// DeletionResiduePaddleSubscriptionCancelledCascadeFailed residue whenever the error is not one
	// cascadeErrorIsRecognisedTransient vouches for, which keeps b76e1b8a's transient case silent
	// and stops this one from being invisible. Round 12, finding AB-1.
	//
	// The service's OTHER audit writes are unchanged and stay non-fatal: RequestDeletion's
	// `user.deletion_requested` and CancelDeletion's `user.deletion_cancelled` both describe a state
	// change that has ALREADY committed in a transaction this service does not hold, so there is
	// nothing there to roll back and aborting would report failure for work that succeeded. Only
	// this one entry was inside reach of a transaction, which is why only this one moved.

	// Capture analytics event for account deletion
	if s.analytics != nil {
		s.analytics.Capture(userID, phclient.EventUserDeleted, nil)
	}

	return nil
}

// requestDeletionTransactional runs the household owner-guard and MarkForDeletion
// atomically under one transaction, using FRESH tx-scoped repositories (not
// s.householdReader/s.inviteRevoker, which are pool-bound) so the household row lock
// is actually held through the mark-for-deletion write. #1367 — Phase 3, Codex review
// High finding 4.
//
// repository.NewHouseholdInviteRepository and repository.NewProfileRepository normally
// require a *appcrypto.FieldEncryptor, but RevokeAllPendingForHousehold and
// MarkForDeletion never touch an encrypted column (both are pure status/timestamp
// writes) — passing nil for this single, locally-scoped use is safe.
func (s *deletionService) requestDeletionTransactional(ctx context.Context, userID string, scheduledAt time.Time) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	// Unconditional rollback-on-defer; see Gap Analysis Round 5, GT-7.
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	hhRepo := repository.NewHouseholdRepository(tx)

	household, isOwner, found, err := hhRepo.GetHouseholdForUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to check household ownership: %w", err)
	}
	if found && isOwner {
		if _, err := hhRepo.LockHouseholdForUpdate(ctx, household.ID); err != nil {
			return fmt.Errorf("failed to lock household: %w", err)
		}
		activeCount, err := hhRepo.CountActiveMembers(ctx, household.ID)
		if err != nil {
			return fmt.Errorf("failed to count household members: %w", err)
		}
		if activeCount > 1 {
			return domain.ErrHouseholdHasActiveMembers
		}
		if err := repository.NewHouseholdInviteRepository(tx, nil).RevokeAllPendingForHousehold(ctx, household.ID); err != nil {
			return fmt.Errorf("failed to revoke pending household invites: %w", err)
		}
	}

	if err := repository.NewProfileRepository(tx, nil).MarkForDeletion(ctx, userID, scheduledAt); err != nil {
		return fmt.Errorf("failed to mark user for deletion: %w", err)
	}

	return tx.Commit(ctx)
}

// generateCancellationToken generates a JWT token for cancellation link
func (s *deletionService) generateCancellationToken(userID, email string, expiresAt time.Time) (string, error) {
	claims := &tokenClaims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "boostedbudget-deletion-service",
			Subject:   userID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(s.tokenSecret))
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// SetPlaidCleanupService sets the Plaid cleanup service for removing Plaid items
// before user deletion. This setter allows wiring the service after DeletionService
// is created, since Plaid services may be initialized after DeletionService.
// Issue #668: Plaid Item Management for Cost Optimization
func (s *deletionService) SetPlaidCleanupService(svc PlaidItemCleanupService) {
	s.plaidCleanupSvc = svc
}

// SetPaddleCleanupService sets the Paddle cleanup service. Issue #1006.
func (s *deletionService) SetPaddleCleanupService(svc PaddleDeletionCleaner) {
	s.paddleCleanupSvc = svc
}

func (s *deletionService) SetRevenueCatCleanupService(svc RevenueCatDeletionCleaner) {
	s.revenueCatCleanupSvc = svc
}

// SetAnalytics sets the analytics client for capturing deletion lifecycle events
func (s *deletionService) SetAnalytics(a phclient.Analytics) {
	s.analytics = a
}

// SetHouseholdReader wires the household owner-deletion guard. #1367 — Phase 3.
func (s *deletionService) SetHouseholdReader(r HouseholdReader) {
	s.householdReader = r
}

// SetHouseholdInviteRevoker wires pending-invite revocation on owner deletion.
// #1367 — Phase 3.
func (s *deletionService) SetHouseholdInviteRevoker(r HouseholdInviteRevoker) {
	s.inviteRevoker = r
}

// householdLookupRetryAttempts / householdLookupRetryBackoff bound the immediate, synchronous
// retry ExecuteDeletion performs on the pre-cascade household lookup (Issue #1531 Blocker 3). See
// that call site's own comment for why a durable (River) retry cannot substitute for this: the
// membership row this lookup depends on is deleted by DeleteUserCascade moments later, so any
// retry attempted after that point — durable or not — would always, correctly, find nothing. A
// short bounded retry targets the realistic failure mode (a transient DB blip) while it can still
// succeed at all.
const (
	householdLookupRetryAttempts = 3
	householdLookupRetryBackoff  = 50 * time.Millisecond
)

// lookupHouseholdWithRetry retries s.householdReader.GetHouseholdForUser up to
// householdLookupRetryAttempts times with a short fixed backoff between attempts, returning the
// last error if every attempt fails. See the constants' doc comment above for why this exists.
func (s *deletionService) lookupHouseholdWithRetry(ctx context.Context, userID string) (household *domain.Household, isOwner bool, found bool, err error) {
	for attempt := 0; attempt < householdLookupRetryAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(householdLookupRetryBackoff)
		}
		household, isOwner, found, err = s.householdReader.GetHouseholdForUser(ctx, userID)
		if err == nil {
			return household, isOwner, found, nil
		}
	}
	return nil, false, false, err
}

// SetCache wires the GDPR deletion purge — see the DeletionService interface doc comment.
func (s *deletionService) SetCache(purger cache.Purger, gens *cache.Generations, jq queue.JobQueue) {
	s.cachePurger = purger
	s.cacheGens = gens
	s.jobQueue = jq
}

// purgeCacheForDeletedUser is called ONLY after DeleteUserCascade has committed (deleted == true):
// the erasure is already durable, so this is entirely about the cache's OWN copies of that data.
//
//   - u:<userID> and k:<hash(clerkID)> are purged OUTRIGHT (cache.Purger.PurgeUser) — see that
//     method's doc comment for why immediate deletion, not a generation bump + TTL wait, is used
//     for these two scopes specifically.
//   - householdID's generation is bumped (never deleted — it legitimately contains OTHER
//     members' data too), orphaning this user's contribution to any cached household aggregate.
//
// A PurgeUser failure enqueues a durable retry (River) rather than being logged and dropped: see
// cache.Purger's doc comment on why a Redis purge failure must never silently complete a deletion
// with an un-purged cache. The household bump is void by design (cache.Generations.Bump never
// returns an error, consistent with every other bump call site in this codebase) — the retry job
// re-attempts it too, for good measure, since it is idempotent.
//
// Issue #1531 Final fixes / M1: PurgeUser AND enqueueCachePurgeRetry both run on independently
// detached contexts, matching the household bump below. Both of this method's callers
// (account_deletion.go, retention_cleanup_check.go) are River jobs — River cancels a job's
// context on shutdown, retry, or timeout, and a cancellation landing here (after
// DeleteUserCascade has already committed) must not be allowed to take out the cache cleanup
// with it. This is NOT the household-bump case alone: a previous version of this comment argued
// PurgeUser could stay on the raw ctx because a failure there "already has its own durable
// remedy" (the retry enqueue) — but that remedy was ALSO on the same raw, cancellable ctx, so a
// single River cancellation failed BOTH the purge and its own fallback with context.Canceled,
// logged and dropped: the deletion completes while the user's cached PII survives, with no
// tombstone set (that Set used the same context too) and nothing scheduled — terminal, not
// TTL-bounded, because the tombstone never landed. Detaching both from the CALLER's cancellation
// closes that.
//
// Issue #1531 GA-1: detaching from cancellation was not enough — PurgeUser and the retry enqueue
// still shared ONE 5s (bumpCacheTimeout) budget when both ran on the same purgeCtx. Redis
// SCAN...MATCH iterates the entire keyspace (COUNT is only a hint), so on a populated cache
// PurgeUser can legitimately consume the whole budget; the retry enqueue then ran on an
// ALREADY-EXPIRED context and failed with context.DeadlineExceeded — the durable retry dropped
// via deadline exhaustion instead of cancellation, the same failure mode reappearing through a
// different door. Each of PurgeUser and enqueueCachePurgeRetry now gets its OWN fresh
// context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout), so a genuine Redis failure
// during the purge still triggers the durable retry exactly as designed, with its own full
// budget to enqueue against.
func (s *deletionService) purgeCacheForDeletedUser(ctx context.Context, userID, clerkUserID, householdID string) {
	if s.cachePurger != nil {
		purgeCtx, purgeCancel := context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
		defer purgeCancel()
		if err := s.cachePurger.PurgeUser(purgeCtx, userID, clerkUserID); err != nil {
			slog.Default().Error("gdpr deletion: cache purge failed; enqueuing durable retry",
				"user_id", userID, "error", err)
			enqueueCtx, enqueueCancel := context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
			defer enqueueCancel()
			s.enqueueCachePurgeRetry(enqueueCtx, userID, clerkUserID, householdID)
		}
	}
	if householdID != "" && s.cacheGens != nil {
		bumpCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
		defer cancel()
		s.cacheGens.Bump(bumpCtx, cache.ScopeHousehold, householdID, "gdpr_deletion")
	}
}

// bumpCacheForAffectedOwners bumps the cache generation for every OTHER (surviving) user whose
// transactions rows DeleteUserCascade's dangling cross-user custom-category-override clear
// rewrote in place — see that UPDATE's own comment and DeleteUserCascade's doc comment (GDPR
// round-6 finding P1) for the mechanism: the comment above the SQL argues (correctly) that the
// value it WRITES is not PII, but the value each affected owner's warm cache still HOLDS is the
// pre-rewrite one — the just-deleted user's own custom category name — until this bump lands.
//
// Called only from ExecuteDeletion, after DeleteUserCascade has returned true (i.e. already
// committed) — §commit-then-bump: a bump must never run before the erasure it is invalidating a
// cache for is itself durable. Like purgeCacheForDeletedUser, this is void by design and must
// never fail, retry, or delay a deletion that has already succeeded: a missed bump only widens the
// cache's own staleness window (bounded by the namespace TTL, or closed early by the owner's own
// next write), never leaves an erasure incomplete.
func (s *deletionService) bumpCacheForAffectedOwners(ctx context.Context, ownerIDs []string) {
	if s.cacheGens == nil || len(ownerIDs) == 0 {
		return
	}
	for _, ownerID := range ownerIDs {
		bumpCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
		s.cacheGens.Bump(bumpCtx, cache.ScopeUser, ownerID, "gdpr_deletion_cross_user_override_clear")
		cancel()

		// Best-effort: also bump the owner's household, if they belong to one, matching what
		// purgeCacheForDeletedUser already does for the DELETED user's own household — a
		// household-scoped analytics read (cache/consumer.go's Scope selection prefers household
		// over user whenever one exists) would otherwise keep serving the same stale value under a
		// DIFFERENT key than the one just bumped above. No retry here (unlike
		// lookupHouseholdWithRetry before the cascade): a failed lookup is logged and skipped, and
		// recovery is TTL-only — nothing re-attempts this bump. That is acceptable (unlike the
		// pre-cascade case) because this owner's own household_members row is untouched by this
		// deletion, so a later read of the SAME lookup would succeed; the risk is bounded purely by
		// the cache namespace's TTL, consistent with how the other void bumps in this file are
		// documented.
		if s.householdReader != nil {
			hhCtx, hhCancel := context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
			household, _, found, hhErr := s.householdReader.GetHouseholdForUser(hhCtx, ownerID)
			hhCancel()
			if hhErr != nil {
				slog.Default().Error("gdpr deletion: household lookup failed while bumping a cross-user "+
					"affected owner's cache; only their user-scoped cache was bumped",
					"owner_user_id", ownerID, "error", hhErr)
				continue
			}
			if found {
				hhBumpCtx, hhBumpCancel := context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
				s.cacheGens.Bump(hhBumpCtx, cache.ScopeHousehold, household.ID, "gdpr_deletion_cross_user_override_clear")
				hhBumpCancel()
			}
		}
	}
}

// enqueueCachePurgeRetry enqueues cachePurgeRetryJobArgs (mirrors jobs.CachePurgeRetryArgs) via
// the injected queue.JobQueue. A nil jobQueue (not yet wired) or an enqueue failure is logged at
// ERROR — there is nothing further to fall back to on this specific path — but never returned:
// this is called from a place that has already decided the overall deletion succeeded.
func (s *deletionService) enqueueCachePurgeRetry(ctx context.Context, userID, clerkUserID, householdID string) {
	if s.jobQueue == nil {
		slog.Default().Error("gdpr deletion: cache purge failed and no job queue is wired — "+
			"the cache purge retry job cannot be enqueued", "user_id", userID)
		return
	}
	args := cachePurgeRetryJobArgs{UserID: userID, ClerkUserID: clerkUserID, HouseholdID: householdID}
	if _, err := s.jobQueue.Enqueue(ctx, args); err != nil {
		slog.Default().Error("gdpr deletion: failed to enqueue cache purge retry job",
			"user_id", userID, "error", err)
	}
}

// deferIfCascadeGuardWillRefuse answers, before the Paddle leg spends anything irreversible, whether
// DeleteUserCascade's C2 guard is ALREADY certain to refuse this user — round 15, finding D1. See the
// ErrPlaidRemovalUnrecoverable call site for the full argument, including why the 7-day cooling-off
// window it defers to is left at its full length rather than shortened.
//
// Returns (handled, err) in ExecuteDeletion's established shape: handled==true means the caller must
// return err directly and do nothing else. A deferral is a legitimate, expected outcome and returns a
// nil error, exactly like the authorisation-revoked and claim-busy stops; only an UNDETERMINABLE count
// is an error, because not knowing whether the guard will refuse is not permission to start
// irreversible external work.
//
// It reads the guard's own population through CountUncleanedLinkedItems, which shares
// uncleanedLinkedItemPredicate with C2 verbatim, so this can only disagree with the guard if the rows
// changed in between. That staleness is harmless in both directions and cannot produce an erasure the
// guard would refuse — the guard still runs, unchanged, and is still the authority.
func (s *deletionService) deferIfCascadeGuardWillRefuse(ctx context.Context, userID string, kind repository.DeletionClaimKind, releaseClaim func(reason string)) (bool, error) {
	outstanding, err := s.profileRepo.CountUncleanedLinkedItems(ctx, userID)
	if err != nil {
		releaseClaim(err.Error())
		return true, fmt.Errorf("aborting deletion before irreversible Paddle cleanup — Plaid removal is unrecoverable and the outstanding linked-item count could not be established: %w", err)
	}
	if outstanding == 0 {
		return false, nil
	}
	releaseClaim(fmt.Sprintf("terminal Plaid removal verdict is still inside its cooling-off window — %d linked item(s) would make the cascade guard refuse", outstanding))
	// Info, not Error: this is a bounded, self-resolving wait that needs no operator, and it is the
	// alarm this finding was really about. Reporting it as "this needs a human" every day for a week —
	// which is what the old flow did, after doing the irreversible work first — is how a real signal
	// gets tuned out.
	slog.Default().Info("deferring deletion before any Paddle cleanup — every remaining Plaid removal failed terminally, but that verdict is still inside its cooling-off window, so the cascade guard would refuse; no irreversible external work is spent on a run that cannot complete",
		"user_id", userID, "claim_kind", string(kind), "outstanding_linked_items", outstanding)
	return true, nil
}

// SetDB wires transaction capability for the household owner-guard. #1367 — Phase 3.
func (s *deletionService) SetDB(db repository.DB) {
	s.db = db
}

// SetWorkClaimsRepo wires the durable work-claims mechanism ExecuteDeletion requires. See the
// DeletionService interface doc for why this is not treated as an optional capability like the
// other Set* methods. Issue #1413 Critical finding C2.
func (s *deletionService) SetWorkClaimsRepo(repo repository.WorkClaimsRepository) {
	s.claimsRepo = repo
}

func (s *deletionService) SetSubscriptionCleanupGuard(guard SubscriptionCleanupGuard) {
	s.subscriptionCleanupGuard = guard
}

// cleanupPaddleResources cancels subscriptions and archives the customer for GDPR compliance.
//
// Issue #1413 Critical finding C2 (closed for real, round 3): errors here now ABORT the
// deletion rather than being logged and swallowed. The previous graceful-degradation behavior
// let ExecuteDeletion proceed to DeleteUserCascade even after a failed Paddle call — which then
// clears paddle_subscription_id/paddle_customer_id, permanently destroying the only record of
// the resource that needs cleanup. Nothing would ever retry it. Returning the first failure lets
// the caller Release the claim so the whole sequence retries on the next run, while the IDs are
// still present in the database to retry against.
//
// ⚠ ROUND 15: THE RETURN VALUE IS NO LONGER THE RECORD, ONLY THE ALARM'S INPUT. It used to be
// `subscriptionCancelled bool`, set once CancelSubscriptionImmediately had SUCCEEDED, and the caller
// wrote the durable residue record off it at the END of ExecuteDeletion. That is finding A3's root:
// a fact that exists only in a local variable does not survive the process, and it never described
// the customer archive at all (A1). Each irreversible call now writes its own record BEFORE it goes
// out (recordIssuedExternalEffect). What is returned here is which calls were issued, so the caller
// can raise the right alarm and decide whether the extra ..._cascade_failed record applies. It is
// reported alongside the error on the abort paths precisely because those are the paths where it
// matters.
func (s *deletionService) cleanupPaddleResources(ctx context.Context, userID string, kind repository.DeletionClaimKind, user *domain.User, stillAuthorised func(ctx context.Context) (bool, error)) (issued paddleEffectsIssued, err error) {
	anonymizedEmail := fmt.Sprintf("deleted_%s@deleted.local", userID)

	// gate re-asks the authorisation question immediately before one specific irreversible call. It
	// is called before EACH of the two below rather than once for the pair, because they are
	// separately irreversible and the first is a network round-trip: a revocation landing during the
	// subscription cancellation must still stop the customer anonymisation, which is the one action
	// here that destroys data nobody can restore.
	gate := func(stage string) error {
		authorised, err := stillAuthorised(ctx)
		if err != nil {
			return fmt.Errorf("could not establish whether deletion is still authorised before %s: %w", stage, err)
		}
		if !authorised {
			return fmt.Errorf("%w before %s", ErrDeletionAuthorisationRevoked, stage)
		}
		return nil
	}

	// Cancel active subscriptions
	// #1413 PinnedRaw: deliberately reads the RAW user.SubscriptionStatus, not
	// ComputeEffectiveStatus(). Cancelling a Paddle subscription must be driven by what Paddle
	// itself reports the subscription's real state to be, not by a comp override that only
	// relabels access — a comped user's forced "active" effective status must not make this
	// gate fire against a subscription that (from Paddle's point of view) is already
	// cancelled/inactive. See
	// TestExecuteDeletion_CompedUser_RawCancelled_DoesNotCancelRealSubscription for the pin.
	if user.PaddleSubscriptionID != nil {
		status := domain.SubscriptionStatus(user.SubscriptionStatus)
		if status.IsActiveSubscription() || status == domain.SubscriptionStatusPastDue {
			if err := gate("cancelling the Paddle subscription"); err != nil {
				return issued, err
			}
			// Issue #1413 H1: claim the OPERATION-keyed ActionPaddleCancelSubscription side-effect
			// claim before calling Paddle, nested inside the ActionAccountDeletion flow claim
			// already held by ExecuteDeletion. Without this, a concurrent past_due grace-cancel
			// (the only other flow that cancels a Paddle subscription) could cancel the SAME
			// subscription at the same instant. ErrSideEffectClaimBusy propagates up through this
			// function's normal error path — the caller (ExecuteDeletion) already releases its own
			// flow claim and aborts on ANY error from this function, which is exactly the required
			// "another flow is mid-operation: release the flow claim, let the next run retry"
			// response (see claimSideEffectAndRun's doc comment, plaid_item_cleanup_service.go).
			err := claimSideEffectAndRun(ctx, s.claimsRepo, repository.ActionPaddleCancelSubscription, userID, paddleCancelSideEffectLease, func() error {
				// ⚠ THE RECORD GOES IN BEFORE THE CALL, AND IT IS INSIDE THIS CLOSURE ON PURPOSE.
				// Before: because nothing written after a network call can describe a process that
				// dies during it (A3), and because a call that returns an error may still have
				// changed Paddle's state (A2) — "issued" is the only claim about it we can honestly
				// make from this side. Inside: because claimSideEffectAndRun does NOT invoke this
				// closure when another flow holds the side-effect claim, and that case is a known
				// nothing-happened, not an ambiguity. Recording it would hand an operator a work
				// item for a subscription that is demonstrably untouched.
				// Fail-closed, and the ORDER matters: issued.subscriptionCancel is set only after
				// the record commits, because a refusal here means the call is never made and
				// there is nothing issued to report.
				if recErr := s.recordIssuedExternalEffect(ctx, userID, kind, repository.DeletionResiduePaddleSubscriptionCancelled,
					"about to cancel Paddle subscription "+*user.PaddleSubscriptionID, true); recErr != nil {
					return recErr
				}
				issued.subscriptionCancel = true
				return s.paddleCleanupSvc.CancelSubscriptionImmediately(ctx, *user.PaddleSubscriptionID)
			})
			if err != nil {
				if errors.Is(err, ErrSideEffectClaimBusy) {
					slog.Default().Info("deletion Paddle cancel deferred — side-effect claim held by another flow",
						"user_id", userID)
				}
				return issued, fmt.Errorf("cancel Paddle subscription %s: %w", *user.PaddleSubscriptionID, err)
			}
		}
	}

	// Archive customer and anonymize email (GDPR PII erasure)
	if user.PaddleCustomerID != nil {
		// THE LAST CHECK BEFORE THE ONE ACTION NOTHING CAN UNDO. A cancelled subscription can be
		// re-created; an anonymised Paddle customer cannot be recovered by us or by Paddle.
		if err := gate("archiving and anonymising the Paddle customer"); err != nil {
			return issued, err
		}
		// ⚠ FINDING A1, AT ITS ROOT. This call had no record of any kind: the old mechanism keyed
		// everything off the SUBSCRIPTION cancel, so the act two lines above is described as
		// unrecoverable could not be reported by any path. It is recorded here, before the call, on
		// exactly the same terms as the cancel — an anonymised Paddle customer cannot be restored,
		// so "issued" is again the strongest honest claim, and the strongest one worth making.
		// Fail-closed on the one act the comment above calls unrecoverable: if the record
		// cannot be written, the archive is not attempted at all.
		if recErr := s.recordIssuedExternalEffect(ctx, userID, kind, repository.DeletionResiduePaddleCustomerArchived,
			"about to archive and anonymise Paddle customer "+*user.PaddleCustomerID, true); recErr != nil {
			return issued, recErr
		}
		issued.customerArchive = true
		if err := s.paddleCleanupSvc.ArchiveAndAnonymizeCustomer(ctx, *user.PaddleCustomerID, anonymizedEmail); err != nil {
			return issued, fmt.Errorf("archive Paddle customer %s: %w", *user.PaddleCustomerID, err)
		}
	}

	return issued, nil
}

// paddleEffectsIssued names which of cleanupPaddleResources' two irreversible external calls this
// run actually ISSUED. Issued, not "succeeded": a call whose response never came back may have
// changed Paddle's state anyway (round 15 finding A2), and from this side of the network there is no
// way to tell the two apart.
type paddleEffectsIssued struct {
	revenueCatErasure  bool
	subscriptionCancel bool
	customerArchive    bool
}

// any reports whether this run did anything to the user's Paddle account that it cannot take back.
func (e paddleEffectsIssued) any() bool {
	return e.subscriptionCancel || e.customerArchive || e.revenueCatErasure
}

// deletionResidueWriteTimeout bounds the residue write's own independent context. Matches the 5s the
// claim Release calls in ExecuteDeletion use, for the same reason: this write runs on paths where
// something has already gone wrong, and it must neither hang the flow nor inherit its deadline.
const deletionResidueWriteTimeout = 5 * time.Second

// recordIssuedExternalEffect writes the durable, queryable record that this deletion run has ISSUED
// an irreversible external call against the user's Paddle account. Round 15, findings A1–A5.
//
// ⚠ IT IS WRITTEN WHEN THE EFFECT IS ISSUED, NOT WHEN THE RUN ENDS. That is the whole correction.
// The previous mechanism inferred the record at the end of ExecuteDeletion from an in-memory bool,
// which meant it could not describe a process that died mid-flight, a panic (the deferred handler
// releases the claim and re-panics, persisting nothing), a call whose acknowledgement was lost, or —
// because the bool only ever named the subscription — the customer archive at all.
//
// ⚠ WHY WRITING IT EAGERLY IS NOT NOISE. The record is not permanent. DeleteUserCascade removes this
// user's residue rows inside the SAME transaction that removes the user, so a deletion that
// COMPLETES leaves none behind and no observer ever sees an uncommitted intermediate. A record
// therefore means exactly what it has always meant — irreversible external work was done and the
// user is still here — and it now means it durably instead of hopefully.
//
// ⚠ THE CONTEXT IS DELIBERATELY NOT ctx (finding A5). Every Release in ExecuteDeletion already runs
// on context.WithoutCancel plus a short timeout, for a reason that applies here with more force: the
// shutdown or deadline that causes the failure has usually cancelled ctx as well, and a write issued
// on a cancelled context does not fail loudly — it does nothing and returns an error that reads like
// a database problem. That asymmetry existed only because this write was added later, next to the
// Release calls, without inheriting the reasoning printed above them.
//
// ⚠ `preventable` NAMES WHICH SITUATION THE CALLER IS IN, and it decides both the failure message
// and what the caller does with the returned error. true at the two PRE-ISSUE sites, where the
// external call has not happened yet and can still be refused; false at the two POST-CASCADE sites,
// where it has already landed and the record is only an operator work item. Passing it explicitly
// rather than inferring it keeps the asymmetry visible at every call site — the one-message version
// told operators an effect had been "refused" when it had in fact already landed.
//
// ⚠ IT IS FAIL-CLOSED WHERE IT CAN BE, AND IT USED NOT TO BE (EE re-review). A failure is logged at Error AND
// returned, and both call sites abort WITHOUT issuing their external call. The earlier best-effort
// shape carried an argument inherited from the old post-hoc mechanism — "the write cannot change the
// outcome of the deletion around it" — which stopped being true the moment the record moved BEFORE
// the call. When this write fails, the irreversible effect has not happened yet; proceeding anyway
// reproduced the exact defect the mechanism exists to close, on the one call the code itself calls
// unrecoverable. The trade is deliberate and asymmetric: a deletion deferred while audit_logs is
// unwritable is retryable and has the 30-day GDPR grace period to retry in, whereas an anonymised
// Paddle customer cannot be restored by us or by Paddle.
//
// ⚠ IT IS NOT REMEDIATION. Nothing here re-subscribes anyone or restores a Paddle customer; see
// ProfileRepository.RecordDeletionResidue's doc comment for why that is deliberate.
func (s *deletionService) recordIssuedExternalEffect(ctx context.Context, userID string, kind repository.DeletionClaimKind, effect repository.DeletionResidueEffect, reason string, preventable bool) error {
	// Branched on `preventable` for the same reason the failure message below is: the single
	// wording said "before issuing it" at the post-cascade sites too, where it contradicted the
	// structured `reason` printed on the very same line.
	msg := "recording an irreversible external effect that HAS ALREADY LANDED — this record is the operator's work item and is removed again by a deletion that completes"
	if preventable {
		msg = "recording an irreversible external effect before issuing it — this record is removed again by a deletion that completes"
	}
	slog.Default().Info(msg,
		"user_id", userID, "claim_kind", string(kind), "residue_effect", string(effect), "reason", reason)
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deletionResidueWriteTimeout)
	defer cancel()
	if recErr := s.profileRepo.RecordDeletionResidue(rctx, userID, effect); recErr != nil {
		// ⚠ THE MESSAGE HAS TO MATCH THE CALLER'S SITUATION, and it did not: the same line said
		// "refusing to issue" at the post-cascade sites, where the Paddle call had ALREADY
		// landed and nothing was being refused. An operator reading that during an incident
		// would conclude the opposite of the truth. Found by the EE re-review.
		if preventable {
			slog.Default().Error("refusing to issue an irreversible external effect: its durable record could not be written — the user KEEPS their account and this run will be retried",
				"user_id", userID, "residue_effect", string(effect), "error", recErr)
		} else {
			slog.Default().Error("an irreversible external effect HAS ALREADY LANDED and its stuck-state record could not be written — the effect itself is unaffected and the run's own error is the authority; this record was the operator's work item",
				"user_id", userID, "residue_effect", string(effect), "error", recErr)
		}
		return fmt.Errorf("record irreversible external effect %s: %w", string(effect), recErr)
	}
	return nil
}

// transientCascadeSQLSTATEClasses are the SQLSTATE classes for which "this run failed but the next
// one is still expected to complete" is KNOWN to be true, rather than merely hoped. Classified by
// two-character class, not by individual code, because the classes are defined by cause and a new
// member of one of them is transient for the same reason the existing members are:
//
//	08 — Connection Exception ................ the connection died; a new one will not be that one.
//	                                           ⚠ SQLSTATE 08 only covers a connection that EXISTED and
//	                                           broke. A connection that was never established carries
//	                                           no SQLSTATE at all and is matched separately, as a
//	                                           *pgconn.ConnectError, in the function below.
//	40 — Transaction Rollback ................ deadlock_detected / serialization_failure; retry is
//	                                           the DOCUMENTED response and this repo already relies
//	                                           on it (see the 40P01 analysis in profile_repository.go)
//	53 — Insufficient Resources .............. out of memory / connections / disk; relieves itself
//	55 — Object Not In Prerequisite State .... lock_not_available and friends; a lock is held NOW
//	57 — Operator Intervention ............... query_canceled, admin_shutdown; the operator did it
//
// Class 42 (insufficient_privilege, undefined_table), class 23 (constraint violations) and class P0
// (a trigger's raise_exception) are deliberately ABSENT: those are exactly the three persistent modes
// #1507's own commit message names as the cost of aborting the cascade on a failed audit write, and
// they do not clear on retry. Nothing else is enumerated either way — see the default direction below.
var transientCascadeSQLSTATEClasses = map[string]struct{}{
	"08": {}, "40": {}, "53": {}, "55": {}, "57": {},
}

// cascadeErrorIsRecognisedTransient answers the one question ExecuteDeletion's cascade-error branch
// needs: is this a failure we KNOW will be retried into success, so that recording a permanent
// operator artifact would be wrong?
//
// ⚠ WHY IT IS "RECOGNISED transient" AND NOT "IS persistent", AND WHY THE DEFAULT RECORDS. b76e1b8a
// argued — correctly, and the argument survives here intact — that recording residue on EVERY cascade
// error would break the transient case: a deadlock resolves in milliseconds, the claim is released,
// the next run completes the deletion, and an operator would have been handed a permanent artifact
// for a non-incident. That argument establishes what must NOT record. It says nothing about the case
// #1507 then created, where the cascade aborts on a database fault that no retry clears.
//
// So the predicate is an ALLOWLIST of known-transient classes and everything else records. The
// asymmetry is deliberate and matches this function's three siblings in ExecuteDeletion — the
// undetermined answer from abortIfUnauthorised, the failed CountUncleanedLinkedItems, the failed
// stillAuthorised recheck — all of which fail CLOSED, on the same reasoning: "we do not know" is not
// permission to stay silent about an irreversible external effect that has already landed. The two
// error directions are not symmetric in cost. A spurious residue row is a false alarm an operator
// closes in a minute; a missed one is a user who keeps their account, loses their subscription, and
// is never told, with both of #1504's metrics reading zero. The allowlist is what keeps b76e1b8a's
// case — which is the FREQUENT one — out of that default.
//
// A nil error is "transient" in the only sense that matters here: it is not a failure, so there is
// nothing to record. The call site guards on err != nil anyway; this makes the function total.
func cascadeErrorIsRecognisedTransient(err error) bool {
	if err == nil {
		return true
	}
	// ⚠ THE PERMANENT-SQLSTATE VETO RUNS BEFORE EVERY OTHER TRANSIENT LEG, AND THAT ORDERING IS THE
	// WHOLE POINT. Round 14's CE Codex review found the cancellation/deadline check sitting ABOVE this
	// walk, where `errors.Is` — which since Go 1.20 traverses `Unwrap() []error` as well — matched a
	// per-host dial timeout in a joined multi-host chain and returned true before any SQLSTATE was
	// examined. A permanent 28P01 later in the SAME chain therefore never got its veto, and the residue
	// was dropped for a fault that repeats on every retry, after the Plaid removal and the Paddle
	// cancel had already landed. That is the one direction this function's doc comment says must never
	// happen. Reachable in the exact shape CC-2 was written for: pgconn gives each candidate host its
	// own connect-timeout context and joins every host's error (pgx v5.9.2, pgconn.go:251 and :269), so
	// a timeout and an authentication failure end up in one tree — as COUSINS beneath sibling per-dial
	// wrappers, not as direct siblings, which is the correction the CE re-review made to this comment's
	// first draft. The walk is indifferent to the difference; the claim about the shape was not.
	//
	// Hoisting it costs nothing the transient legs needed: a cancelled or deadline-expired cascade with
	// no permanent SQLSTATE in its chain still falls through to the check below and still reads
	// transient. A cancellation mid-statement that DOES carry a SQLSTATE carries 57014 query_canceled,
	// which is class 57 and therefore allowlisted, so it does not trip this veto either.
	if anyErrorInTree(err, pgErrorWithTransience(false)) {
		return false
	}
	// Cancellation and deadline expiry are the non-SQLSTATE half: in practice the same shutdown or
	// job deadline that killed the cascade killed the run, and the next run redoes the sequence.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// A CONNECTION ATTEMPT THAT NEVER REACHED POSTGRES CARRIES NO SQLSTATE, so the class-08
	// ("connection exception") entry in the allowlist below cannot see it. DeleteUserCascade opens
	// its transaction only AFTER the external cleanup, and a pool acquisition that cannot reach the
	// server fails there as a *pgconn.ConnectError, which carries no SQLSTATE — so errors.As for a
	// *pgconn.PgError missed it and the syntactic default recorded a PERMANENT operator artifact for
	// a blip the very next run recovers from. Nothing ever clears such a row. Whoever put "08" in the
	// allowlist meant connection failures to be transient; recognising ConnectError applies that same
	// intent to the shape the failure actually arrives in. Round 13, Codex review of
	// 127dc9c2..62014b4d.
	//
	// ⚠ ORDER IS LOAD-BEARING, AND THE FIRST ATTEMPT AT THIS GOT IT WRONG. The first version returned
	// true for ANY ConnectError, ahead of the SQLSTATE check, on the argument that a permanent
	// connection failure could not be recorded anyway because RecordDeletionResidue writes to that
	// same Postgres. The round-13 Codex RE-REVIEW refuted both halves and it was right:
	//
	//   * pgconn wraps the server's STARTUP ErrorResponse inside a ConnectError (pgconn.go, the
	//     connect path), so 28P01 invalid_password and 3D000 invalid_catalog_name — both permanent —
	//     arrive as a ConnectError that DOES contain a *pgconn.PgError. Returning true for those
	//     silently drops the human-facing Error log AND the residue for a harm that never self-heals.
	//   * "same database, therefore unwritable" is not categorical: RecordDeletionResidue issues its
	//     own pool Exec, and a pool Exec can be served by an ALREADY-ESTABLISHED connection that
	//     predates the credential rotation which is failing new handshakes.
	//
	// So the SQLSTATE check runs FIRST and a ConnectError is only considered at all once no SQLSTATE
	// was found. A ConnectError wrapping 57P03 cannot_connect_now still reads transient, correctly,
	// via class 57.
	//
	// ⚠ THAT SETTLES THE ORDER OF THE TWO LEGS AND NOTHING MORE. Round 14 found this passage being
	// read as if it closed the ordering question outright, while the SQLSTATE leg below was still
	// picking whichever PgError a first-match traversal reached first. It is not the same question
	// and it was not settled; see the block at that leg.
	//
	// ⚠ AND "NO SQLSTATE" IS NOT ITSELF EVIDENCE OF TRANSIENCE. The round-13 Codex THIRD pass caught
	// the next layer: plenty of PERMANENT failures produce a ConnectError with no PgError at all —
	// an expired or untrusted TLS certificate, a hostname mismatch, an unsupported SASL/channel-binding
	// configuration, a protocol version the server will never speak, and NXDOMAIN. Verified by
	// execution, not by reading: pgconn.Connect to a non-existent host yields a ConnectError carrying
	// a *net.DNSError with IsNotFound=true and IsTemporary=false. Treating all of those as transient
	// would silently drop the residue for a harm that never self-heals — the same defect as the
	// version above, one level over. So the ConnectError leg is an ALLOWLIST too, in the same
	// fail-closed shape as transientCascadeSQLSTATEClasses: see connectErrorIsRecognisedTransient.
	//
	// ⚠ WALKED, NOT errors.As'd — AND UNTIL ROUND 14 THIS LEG WAS THE INCONSISTENT ONE. It used to be
	// `errors.As(err, &pgErr) && len(pgErr.Code) >= 2`, which binds the FIRST assignable node in the
	// tree and never examines another. That is the same first-match unsoundness the round-13 FIFTH
	// pass removed from connectErrorIsRecognisedTransient's net.Error check, left in place here — so
	// the two legs disagreed about a question they both face, and the comment above (which settles
	// only the order of the LEGS, not the order WITHIN one) read as if the whole question were
	// closed. It was not. Round 14, finding CC-2.
	//
	// It bit in two ways, both requiring a tree with an `Unwrap() []error` node — pgconn's multi-host
	// aggregation, which joins the errors of every attempted host:
	//
	//   * a transient SQLSTATE in slice position 0 masked a PERMANENT one later in the chain, and the
	//     residue for a fault no retry clears was silently suppressed;
	//   * a PgError with an EMPTY code in position 0 failed the `len >= 2` guard, dropping the whole
	//     leg — so a usable SQLSTATE further along was never consulted at all.
	//
	// ⚠ THE QUANTIFIER IS DELIBERATELY NOT THE SIBLING LEG'S, AND THE DIFFERENCE IS THE POINT.
	// connectErrorIsRecognisedTransient says "any transient node wins". This leg says "any
	// NON-transient SQLSTATE loses". Both are the fail-CLOSED reading of their own evidence; they
	// differ because the evidence differs. A socket errno describes one host's reachability and says
	// nothing about the others, so a recoverable host redeems the chain. A server-issued SQLSTATE
	// does not work that way: 28P01 invalid_password and 3D000 invalid_catalog_name are statements
	// about the DEPLOYMENT, and a second host being briefly refused does not make the password right.
	// Importing "any transient node wins" here would suppress the residue in precisely the 28P01 case
	// the round-13 re-review restored it for. So the rule can only ever turn a former `true` into a
	// `false` — at worst a spurious residue row an operator closes in a minute, never a missing one.
	//
	// A codeless PgError is evidence of nothing and votes neither way; the chain then falls through
	// to the ConnectError leg exactly as it did before.
	//
	// A type assertion inside the walk, not errors.As: anyErrorInTree already provides the traversal
	// errors.As was being used for, and the repository wraps every failure in fmt.Errorf, so no
	// production caller ever sees a bare *pgconn.PgError at the root.
	// ⚠ ONLY THE POSITIVE HALF OF THIS LEG IS HERE. Its negative half — the permanent-SQLSTATE veto —
	// was hoisted above the cancellation check by round 14's CE review; the block up there explains
	// why, and the two halves are still one rule read in two directions.
	if anyErrorInTree(err, pgErrorWithTransience(true)) {
		return true
	}
	// No SQLSTATE anywhere in the chain: transient only if this was a connection attempt that failed
	// for a RECOGNISED transient reason. Everything else — including anything unrecognised — records,
	// which is the default direction this function's doc comment argues for.
	var connErr *pgconn.ConnectError
	return errors.As(err, &connErr) && connectErrorIsRecognisedTransient(err)
}

// pgErrorWithTransience builds a predicate for anyErrorInTree that matches any *pgconn.PgError whose
// SQLSTATE class's membership of transientCascadeSQLSTATEClasses equals `transient`.
//
// A PgError whose Code is shorter than two characters matches NEITHER polarity: it carries no class
// and so is not evidence of transience or of permanence. That is what lets a codeless PgError sit in
// a joined chain without vetoing a sibling that does carry a class, and it preserves the pre-round-14
// behaviour for the single-error case (`wrapPgError("")` still falls through to the ConnectError leg
// and records) — see TestCascadeErrorIsRecognisedTransient_UnknownErrorsAreNotTransient.
func pgErrorWithTransience(transient bool) func(error) bool {
	return func(e error) bool {
		pgErr, ok := e.(*pgconn.PgError)
		if !ok || len(pgErr.Code) < 2 {
			return false
		}
		_, isTransient := transientCascadeSQLSTATEClasses[pgErr.Code[:2]]
		return isTransient == transient
	}
}

// transientConnectErrnos are the socket-level failures that mean "not right now" rather than
// "not ever". Each one is a condition a restarting server, a failing-over replica or a brief network
// partition produces and a retry a minute later clears.
var transientConnectErrnos = []syscall.Errno{
	syscall.ECONNREFUSED, // nothing listening yet — a server mid-restart
	syscall.ECONNRESET,   // the peer tore the connection down
	syscall.EHOSTUNREACH, // routing gap
	syscall.ENETUNREACH,  // routing gap
	syscall.ETIMEDOUT,    // SYN never answered
	syscall.EPIPE,        // wrote to a socket the peer had already closed
}

// connectErrorIsRecognisedTransient decides whether a *pgconn.ConnectError that carries no SQLSTATE
// is one a retry is EXPECTED to clear.
//
// ⚠ "EXPECTED", NOT "GUARANTEED", AND THE DIFFERENCE IS DELIBERATE. The round-13 Codex fourth pass
// pointed out — correctly — that no socket-level signal can promise self-healing: ECONNREFUSED is a
// restarting server AND a permanently wrong port; EHOSTUNREACH is a routing blip AND a static routing
// mistake; a timeout is a slow network AND a firewall that will drop the packet forever. An earlier
// draft of this comment claimed the predicate answers "we KNOW a retry clears", which no
// implementation of it could honour. It does not. It answers "this is the shape a recoverable
// connection failure takes", and it is a likelihood heuristic on a deliberately chosen error
// direction: a wrong TRUE costs a suppressed residue, a wrong FALSE costs a spurious row an operator
// closes in a minute. That asymmetry is why the set is kept small and why everything unrecognised
// still records — but it is not certainty and is no longer written as if it were.
//
// ⚠ IT IS AN ALLOWLIST FOR THE SAME REASON transientCascadeSQLSTATEClasses IS. The set of permanent
// ways a connection attempt can fail is open-ended and grows with pgconn, Go's TLS stack and the
// deployment's network configuration — an expired certificate, an untrusted CA, a hostname mismatch,
// an unsupported SASL mechanism, a protocol the server will never speak, a hostname that does not
// exist. None of those carry a SQLSTATE and none of them clear on retry. Enumerating THOSE would be
// fail-open by construction: the next one nobody thought of would be classified transient and would
// silently suppress a residue. Enumerating the transient ones instead costs, at worst, a spurious
// residue row an operator closes in a minute — the asymmetry the parent function's doc comment
// argues at length.
//
// ⚠ ORDER: DNS IS CHECKED LAST, NOT FIRST. An earlier draft returned on the DNS branch immediately,
// which the fourth pass showed to be wrong for pgx's MULTI-HOST configurations: pgconn joins the
// errors from every attempted host into ONE ConnectError, so a chain can hold an NXDOMAIN from a
// fallback host beside an ECONNREFUSED from the primary. Returning on the DNS answer classified that
// whole (recoverable) case permanent. Scanning the recognised-transient causes first and consulting
// DNS only when none of them are present gives NXDOMAIN-alone the permanent answer it deserves while
// letting a mixed chain be read by its recoverable half. `(*net.DNSError).Timeout()` is IsTimeout, so
// an IsNotFound error cannot sneak through the generic timeout check either.
func connectErrorIsRecognisedTransient(err error) bool {
	for _, errno := range transientConnectErrnos {
		if errors.Is(err, errno) {
			return true
		}
	}
	// A GRACEFUL disconnect during the startup handshake: the server, proxy or load balancer accepted
	// the TCP connection and then closed it while pgx was awaiting the startup response. pgproto3
	// turns that into io.ErrUnexpectedEOF (or io.EOF) inside the ConnectError. It is neither an errno
	// nor a net.Error timeout, so the checks above and below both miss it — and it is one of the most
	// ordinary shapes a rolling restart or a failover produces, which made it the allowlist's worst
	// omission: an everyday deploy would have written false permanent residue. Round 13, Codex fourth
	// pass.
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	// A timeout at any layer that models net.Error: the attempt ran out of time rather than being
	// refused an answer. A *net.DNSError is consulted on its own fields, because it is ALSO a
	// net.Error whose Timeout() is merely IsTimeout, and NXDOMAIN must not read as a blip.
	//
	// ⚠ WALKED, NOT errors.As'd, AND THAT IS THE WHOLE POINT. errors.As stops at the FIRST match.
	// pgconn joins the per-host errors of a multi-host configuration into one tree, so a chain can
	// begin with an NXDOMAIN from a fallback host — itself a net.Error with Timeout() == false — and
	// carry the primary host's genuine timeout further along. errors.As bound the NXDOMAIN and the
	// timeout was never examined: an everyday timeout, classified permanent, false residue. Found by
	// the round-13 Codex FIFTH pass, in the fix the fourth pass had asked for. Any node in the tree
	// answering "transient" is enough; only a tree with no transient node anywhere records.
	return anyErrorInTree(err, func(e error) bool {
		if dnsErr, ok := e.(*net.DNSError); ok {
			// IsNotFound is the name not existing — a rename or a bad config, not a blip.
			return dnsErr.IsTemporary || dnsErr.IsTimeout
		}
		if netErr, ok := e.(net.Error); ok {
			return netErr.Timeout()
		}
		return false
	})
}

// anyErrorInTree reports whether pred holds for ANY error in err's tree, following both wrapping
// shapes: `Unwrap() error` and, for errors.Join and pgconn's per-host aggregation, `Unwrap() []error`.
//
// It exists because errors.As and errors.Is answer different questions from the one asked here.
// errors.Is walks the whole tree but only compares against one target value; errors.As walks the
// whole tree but STOPS at the first assignable node, which is wrong when several nodes share an
// interface and only a later one carries the answer — exactly the NXDOMAIN-then-timeout case above.
func anyErrorInTree(err error, pred func(error) bool) bool {
	for err != nil {
		if pred(err) {
			return true
		}
		switch u := err.(type) {
		case interface{ Unwrap() error }:
			err = u.Unwrap()
		case interface{ Unwrap() []error }:
			for _, branch := range u.Unwrap() {
				if anyErrorInTree(branch, pred) {
					return true
				}
			}
			return false
		default:
			return false
		}
	}
	return false
}
