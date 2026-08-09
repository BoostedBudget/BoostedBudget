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
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

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
	// Issue #1504: subscriptionCancelled is the one fact this leg reports back, and it exists because
	// a Paddle subscription-cancel that has LANDED cannot be recalled by any later gate. Every path
	// below that ends with the user still existing (a gate declining after the cancel, and the
	// cascade's own guard refusing) records the residue, so the two ways the same harm happens cannot
	// diverge — see recordPaddleCancelResidue.
	subscriptionCancelled := false
	// recordPaddleCancelResidue makes a post-gate external effect visible to an operator when the
	// deletion it belonged to did not complete. It is deliberately NOT remediation: nothing here
	// re-creates the subscription (see ProfileRepository.RecordDeletionResidue's doc comment). A
	// failed write cannot change the outcome of a deletion that is already over, so it is logged at
	// Error rather than propagated — but it is never swallowed silently, which was the whole finding.
	//
	// The `effect` parameter exists because there are now TWO kinds of this one harm and an operator
	// has a different job for each — see repository.DeletionResiduePaddleSubscriptionCancelledCascadeFailed.
	recordPaddleCancelResidue := func(effect repository.DeletionResidueEffect, reason string) {
		if !subscriptionCancelled {
			return
		}
		slog.Default().Error("account deletion stopped after the user's Paddle subscription had already been cancelled — the user KEEPS their account and data but has NO subscription; this needs a human",
			"user_id", userID, "claim_kind", string(kind), "residue_effect", string(effect), "reason", reason)
		if recErr := s.profileRepo.RecordDeletionResidue(ctx, userID, effect); recErr != nil {
			slog.Default().Error("failed to record the post-gate external-effect residue — this user's cancelled subscription is now visible ONLY in logs",
				"user_id", userID, "error", recErr)
		}
	}
	if s.paddleCleanupSvc != nil {
		cancelled, err := s.cleanupPaddleResources(ctx, userID, user, stillAuthorised)
		subscriptionCancelled = cancelled
		if err != nil {
			if errors.Is(err, ErrDeletionAuthorisationRevoked) {
				releaseClaim("authorisation revoked mid Paddle cleanup")
				slog.Default().Info("aborting deletion mid Paddle cleanup — the authorisation this run was claimed under no longer holds",
					"user_id", userID, "claim_kind", string(kind))
				// Only fires when the revocation was caught by the SECOND gate, after the cancel had
				// already landed; a revocation caught by the first gate cancels nothing and so leaves
				// nothing to report.
				recordPaddleCancelResidue(repository.DeletionResiduePaddleSubscriptionCancelled,
					"authorisation revoked between the subscription cancel and the customer archive")
				return nil
			}
			// A Paddle FAILURE (as opposed to a revocation) deliberately records nothing: the claim is
			// released and the next run retries the whole sequence, so the deletion is still expected
			// to complete and the user is not being left in the state this record describes. Only a
			// run that ends with the user surviving qualifies.
			releaseClaim(err.Error())
			return fmt.Errorf("aborting deletion — Paddle cleanup failed: %w", err)
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
		if !cascadeErrorIsRecognisedTransient(err) {
			recordPaddleCancelResidue(repository.DeletionResiduePaddleSubscriptionCancelledCascadeFailed,
				"the cascade failed with a non-transient error after the subscription had been cancelled: "+err.Error())
		}
		return fmt.Errorf("failed to cascade delete user data: %w", err)
	}
	if !deleted {
		releaseClaim("cascade aborted — eligibility lost, or un-cleaned Plaid items remain, since the claim was taken")
		slog.Default().Info("skipping deletion cascade — re-verification inside the cascade transaction found the user no longer eligible",
			"user_id", userID)
		// Issue #1504: the SECOND way a user survives a run whose Paddle cancel already landed. The
		// harm is identical to the gate-decline case above — account kept, subscription gone — so the
		// record must be identical too; covering only the gate would silently miss this half.
		recordPaddleCancelResidue(repository.DeletionResiduePaddleSubscriptionCancelled,
			"the cascade's own re-verification refused after the subscription had been cancelled")
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
//
// Returns subscriptionCancelled = true once CancelSubscriptionImmediately has SUCCEEDED, and that
// return value is load-bearing rather than informational (Issue #1504): it is the only thing that
// distinguishes an abort which cost the user nothing from one that left them with no subscription,
// and the caller records the residue on exactly that distinction. It is reported alongside the error
// on the abort paths precisely because those are the paths where it matters.
func (s *deletionService) cleanupPaddleResources(ctx context.Context, userID string, user *domain.User, stillAuthorised func(ctx context.Context) (bool, error)) (subscriptionCancelled bool, err error) {
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
				return false, err
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
				return false, fmt.Errorf("cancel Paddle subscription %s: %w", *user.PaddleSubscriptionID, err)
			}
			// From here on the cancellation has LANDED at Paddle. Nothing below can undo it, which is
			// what every `return subscriptionCancelled, ...` after this line is carrying to the caller.
			subscriptionCancelled = true
		}
	}

	// Archive customer and anonymize email (GDPR PII erasure)
	if user.PaddleCustomerID != nil {
		// THE LAST CHECK BEFORE THE ONE ACTION NOTHING CAN UNDO. A cancelled subscription can be
		// re-created; an anonymised Paddle customer cannot be recovered by us or by Paddle.
		if err := gate("archiving and anonymising the Paddle customer"); err != nil {
			return subscriptionCancelled, err
		}
		if err := s.paddleCleanupSvc.ArchiveAndAnonymizeCustomer(ctx, *user.PaddleCustomerID, anonymizedEmail); err != nil {
			if s.analytics != nil {
				s.analytics.Capture(userID, phclient.EventPaddleCleanupFailed, map[string]interface{}{"step": "archive_customer"})
			}
			return subscriptionCancelled, fmt.Errorf("archive Paddle customer %s: %w", *user.PaddleCustomerID, err)
		}
	}

	return subscriptionCancelled, nil
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
	if anyErrorInTree(err, pgErrorWithTransience(false)) {
		return false
	}
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
