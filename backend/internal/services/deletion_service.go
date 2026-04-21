package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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

	// Mark user for deletion in database
	if err := s.profileRepo.MarkForDeletion(ctx, userID, scheduledAt); err != nil {
		return "", time.Time{}, fmt.Errorf("failed to mark user for deletion: %w", err)
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

	// Issue #1006: Paddle cleanup before Plaid and DB deletion (Paddle IDs must be readable)
	if s.paddleCleanupSvc != nil {
		anonymizedEmail := fmt.Sprintf("deleted_%s@deleted.local", userID)

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
