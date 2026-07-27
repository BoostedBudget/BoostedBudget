package services

import (
	"context"
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

	// ExecuteDeletion executes the full deletion process for a user
	ExecuteDeletion(ctx context.Context, userID string) error

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
	plaidCleanupSvc  PlaidItemCleanupService // Issue #668: Remove Plaid items before DB deletion
	paddleCleanupSvc PaddleDeletionCleaner   // Issue #1006: nil if Paddle not configured
	analytics        phclient.Analytics      // Optional PostHog analytics client
	householdReader  HouseholdReader         // #1367 — Phase 3: nil skips the owner guard
	inviteRevoker    HouseholdInviteRevoker  // #1367 — Phase 3: nil skips invite revocation
	db               repository.DB           // #1367 — Phase 3: nil falls back to non-transactional guard
	gracePeriodDays  int
	tokenSecret      string
}

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

// ExecuteDeletion executes the full deletion process for a user
func (s *deletionService) ExecuteDeletion(ctx context.Context, userID string) error {
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
	if user.HasActiveManualOverride() && user.DeletionRequestedAt == nil {
		slog.Default().Info("skipping auto-deletion for actively comped user",
			"user_id", userID)
		return nil
	}

	// Issue #1413 Critical finding C2 (Codex review round 2): claim atomically BEFORE any
	// irreversible work. Ordering matters — Paddle/Plaid cleanup below must still run before
	// DeleteUserCascade (their IDs must be readable), so the claim has to come first as its
	// own step, or an admin comp granted after Paddle/Plaid cleanup already ran would still be
	// overtaken by the time DeleteUserCascade's own guard could catch it.
	claimed, err := s.profileRepo.ClaimForDeletion(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to claim user for deletion: %w", err)
	}
	if !claimed {
		// An admin granted or extended a comp between the GetUserByID read above and this
		// claim, and the user has no outstanding GDPR deletion request of their own to
		// override it. This is a legitimate, expected outcome — not an error. The daily
		// retention job will simply not re-select this user while the comp remains live.
		slog.Default().Info("skipping auto-deletion — claim not granted (comped since last read)",
			"user_id", userID)
		return nil
	}

	// Issue #1006: Paddle cleanup before Plaid and DB deletion (Paddle IDs must be readable)
	if s.paddleCleanupSvc != nil {
		s.cleanupPaddleResources(ctx, userID, user)
	}

	// Issue #668: Remove Plaid items BEFORE database deletion to stop billing
	// This is critical for cost control - Plaid charges monthly per Item
	// Errors are logged but don't stop deletion (graceful degradation)
	if s.plaidCleanupSvc != nil {
		_ = s.plaidCleanupSvc.RemoveAllUserItems(ctx, userID)
	}

	// Execute cascade deletion in database
	if err := s.profileRepo.DeleteUserCascade(ctx, userID); err != nil {
		return fmt.Errorf("failed to cascade delete user data: %w", err)
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
	var cerr error
	// Unconditional rollback-on-defer; see Gap Analysis Round 5, GT-7.
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	hhRepo := repository.NewHouseholdRepository(tx)

	household, isOwner, found, cerr := hhRepo.GetHouseholdForUser(ctx, userID)
	if cerr != nil {
		cerr = fmt.Errorf("failed to check household ownership: %w", cerr)
		return cerr
	}
	if found && isOwner {
		if _, cerr = hhRepo.LockHouseholdForUpdate(ctx, household.ID); cerr != nil {
			cerr = fmt.Errorf("failed to lock household: %w", cerr)
			return cerr
		}
		activeCount, err := hhRepo.CountActiveMembers(ctx, household.ID)
		if err != nil {
			cerr = fmt.Errorf("failed to count household members: %w", err)
			return cerr
		}
		if activeCount > 1 {
			cerr = domain.ErrHouseholdHasActiveMembers
			return cerr
		}
		if cerr = repository.NewHouseholdInviteRepository(tx, nil).RevokeAllPendingForHousehold(ctx, household.ID); cerr != nil {
			cerr = fmt.Errorf("failed to revoke pending household invites: %w", cerr)
			return cerr
		}
	}

	if cerr = repository.NewProfileRepository(tx, nil).MarkForDeletion(ctx, userID, scheduledAt); cerr != nil {
		cerr = fmt.Errorf("failed to mark user for deletion: %w", cerr)
		return cerr
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

// cleanupPaddleResources cancels subscriptions and archives customer for GDPR compliance.
// Errors are logged but don't stop deletion (graceful degradation).
func (s *deletionService) cleanupPaddleResources(ctx context.Context, userID string, user *domain.User) {
	anonymizedEmail := fmt.Sprintf("deleted_%s@deleted.local", userID)

	// Cancel active subscriptions
	if user.PaddleSubscriptionID != nil {
		status := domain.SubscriptionStatus(user.SubscriptionStatus)
		if status.IsActiveSubscription() || status == domain.SubscriptionStatusPastDue {
			if err := s.paddleCleanupSvc.CancelSubscriptionImmediately(ctx, *user.PaddleSubscriptionID); err != nil {
				slog.Default().Warn("failed to cancel Paddle subscription during deletion",
					"user_id", userID,
					"subscription_id", *user.PaddleSubscriptionID,
					"error", err,
				)
				if s.analytics != nil {
					s.analytics.Capture(userID, phclient.EventPaddleCleanupFailed, map[string]interface{}{"step": "cancel_subscription"})
				}
			}
		}
	}

	// Archive customer and anonymize email (GDPR PII erasure)
	if user.PaddleCustomerID != nil {
		if err := s.paddleCleanupSvc.ArchiveAndAnonymizeCustomer(ctx, *user.PaddleCustomerID, anonymizedEmail); err != nil {
			slog.Default().Warn("failed to archive Paddle customer during deletion",
				"user_id", userID,
				"customer_id", *user.PaddleCustomerID,
				"error", err,
			)
			if s.analytics != nil {
				s.analytics.Capture(userID, phclient.EventPaddleCleanupFailed, map[string]interface{}{"step": "archive_customer"})
			}
		}
	}
}
