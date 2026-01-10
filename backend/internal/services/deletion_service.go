package services

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
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
}

// deletionService implements DeletionService
type deletionService struct {
	profileRepo      repository.ProfileRepository
	emailService     EmailService
	clerkService     ClerkService
	auditLogger      AuditLogger
	plaidCleanupSvc  PlaidItemCleanupService // Issue #668: Remove Plaid items before DB deletion
	gracePeriodDays  int
	tokenSecret      string
}

// AuditLogger defines the interface for audit logging
type AuditLogger interface {
	Log(ctx context.Context, action, resourceType, resourceID string) error
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
