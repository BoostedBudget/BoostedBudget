package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

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
	profileRepo      repository.ProfileRepository
	emailService     EmailService
	clerkService     ClerkService
	auditLogger      AuditLogger
	plaidCleanupSvc  PlaidItemCleanupService         // Issue #668: Remove Plaid items before DB deletion
	paddleCleanupSvc PaddleDeletionCleaner           // Issue #1006: nil if Paddle not configured
	analytics        phclient.Analytics              // Optional PostHog analytics client
	householdReader  HouseholdReader                 // #1367 — Phase 3: nil skips the owner guard
	inviteRevoker    HouseholdInviteRevoker          // #1367 — Phase 3: nil skips invite revocation
	db               repository.DB                   // #1367 — Phase 3: nil falls back to non-transactional guard
	claimsRepo       repository.WorkClaimsRepository // Issue #1413 C2: required for ExecuteDeletion
	gracePeriodDays  int
	tokenSecret      string
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

// AuditLogger defines the interface for audit logging
type AuditLogger interface {
	Log(ctx context.Context, action, resourceType, resourceID string) error
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

	// Create audit log
	// Audit log failure is not critical - don't fail the request
	_ = s.auditLogger.Log(ctx, "user.deletion_requested", "user", userID)

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

	// Create audit log (non-critical, errors ignored)
	_ = s.auditLogger.Log(ctx, "user.deletion_cancelled", "user", claims.UserID)

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
	// Deliberately HasActiveManualOverride (live-only), NOT HasAnyManualOverride — even though
	// the authoritative claim predicate this auto-deletion path reaches below,
	// retentionDeletionClaimPredicate (profile_repository.go), uses the all-overrides rule via
	// noManualOverrideAtAllPredicate. That
	// asymmetry is intentional: this cheap check only short-circuits the case it can be certain
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

	// Issue #1413 Critical finding C2: external cleanup failure now ABORTS the deletion
	// instead of being swallowed. DeleteUserCascade (below) clears paddle_subscription_id and
	// Plaid references, so proceeding to the cascade after a cleanup failure would destroy the
	// local record of the very external resources that failed to clean up — orphaning them
	// with nothing left to retry. Releasing the claim lets the NEXT run retry the whole
	// sequence from scratch instead.
	if s.paddleCleanupSvc != nil {
		if err := s.cleanupPaddleResources(ctx, userID, user); err != nil {
			releaseClaim(err.Error())
			return fmt.Errorf("aborting deletion — Paddle cleanup failed: %w", err)
		}
	}

	// Issue #668: Remove Plaid items BEFORE database deletion to stop billing. This is
	// critical for cost control — Plaid charges monthly per Item. A failure here now also
	// aborts (see the Paddle cleanup comment above for why) rather than proceeding to a
	// cascade that would strand an un-removed, still-billing Plaid item with no local record.
	if s.plaidCleanupSvc != nil {
		if err := s.plaidCleanupSvc.RemoveAllUserItems(ctx, userID); err != nil {
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
			releaseClaim(err.Error())
			return fmt.Errorf("aborting deletion — Plaid cleanup failed: %w", err)
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
	deleted, err := s.profileRepo.DeleteUserCascade(ctx, userID, user.DeletionRequestedAt)
	if err != nil {
		releaseClaim(err.Error())
		return fmt.Errorf("failed to cascade delete user data: %w", err)
	}
	if !deleted {
		releaseClaim("cascade aborted — eligibility lost, or un-cleaned Plaid items remain, since the claim was taken")
		slog.Default().Info("skipping deletion cascade — re-verification inside the cascade transaction found the user no longer eligible",
			"user_id", userID)
		return nil
	}

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

	// Create audit log (non-critical)
	_ = s.auditLogger.Log(ctx, "user.deleted", "user", userID)

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

// cleanupPaddleResources cancels subscriptions and archives the customer for GDPR compliance.
//
// Issue #1413 Critical finding C2 (closed for real, round 3): errors here now ABORT the
// deletion rather than being logged and swallowed. The previous graceful-degradation behavior
// let ExecuteDeletion proceed to DeleteUserCascade even after a failed Paddle call — which then
// clears paddle_subscription_id/paddle_customer_id, permanently destroying the only record of
// the resource that needs cleanup. Nothing would ever retry it. Returning the first failure lets
// the caller Release the claim so the whole sequence retries on the next run, while the IDs are
// still present in the database to retry against.
func (s *deletionService) cleanupPaddleResources(ctx context.Context, userID string, user *domain.User) error {
	anonymizedEmail := fmt.Sprintf("deleted_%s@deleted.local", userID)

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
				return s.paddleCleanupSvc.CancelSubscriptionImmediately(ctx, *user.PaddleSubscriptionID)
			})
			if err != nil {
				if errors.Is(err, ErrSideEffectClaimBusy) {
					slog.Default().Info("deletion Paddle cancel deferred — side-effect claim held by another flow",
						"user_id", userID)
				}
				if s.analytics != nil {
					s.analytics.Capture(userID, phclient.EventPaddleCleanupFailed, map[string]interface{}{"step": "cancel_subscription"})
				}
				return fmt.Errorf("cancel Paddle subscription %s: %w", *user.PaddleSubscriptionID, err)
			}
		}
	}

	// Archive customer and anonymize email (GDPR PII erasure)
	if user.PaddleCustomerID != nil {
		if err := s.paddleCleanupSvc.ArchiveAndAnonymizeCustomer(ctx, *user.PaddleCustomerID, anonymizedEmail); err != nil {
			if s.analytics != nil {
				s.analytics.Capture(userID, phclient.EventPaddleCleanupFailed, map[string]interface{}{"step": "archive_customer"})
			}
			return fmt.Errorf("archive Paddle customer %s: %w", *user.PaddleCustomerID, err)
		}
	}

	return nil
}
