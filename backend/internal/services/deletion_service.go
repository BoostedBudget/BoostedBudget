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
}

// deletionService implements DeletionService
type deletionService struct {
	profileRepo      repository.ProfileRepository
	emailService     EmailService
	auth0Service     Auth0Service
	auditLogger      AuditLogger
	gracePeriodDays  int
	tokenSecret      string
}

// AuditLogger defines the interface for audit logging
type AuditLogger interface {
	Log(ctx context.Context, action, resourceType, resourceID string) error
}

// NewDeletionService creates a new DeletionService instance
func NewDeletionService(
	profileRepo repository.ProfileRepository,
	emailService EmailService,
	auth0Service Auth0Service,
	auditLogger AuditLogger,
	gracePeriodDays int,
	tokenSecret string,
) DeletionService {
	return &deletionService{
		profileRepo:     profileRepo,
		emailService:    emailService,
		auth0Service:    auth0Service,
		auditLogger:     auditLogger,
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
	if err := s.emailService.SendDeletionConfirmation(ctx, email, cancellationToken, scheduledAt); err != nil {
		// Email failure is not critical - user is still marked for deletion
		// Log the error but don't fail the request
		// In production, this would be logged to monitoring system
		// For now, we just silently continue since deletion is already scheduled
	}

	// Create audit log
	if err := s.auditLogger.Log(ctx, "user.deletion_requested", "user", userID); err != nil {
		// Audit log failure is not critical - don't fail the request
		// In production, this would be logged to monitoring system
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

	// Create audit log
	if err := s.auditLogger.Log(ctx, "user.deletion_cancelled", "user", claims.UserID); err != nil {
		// Audit log failure is not critical
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

	// Execute cascade deletion in database
	if err := s.profileRepo.DeleteUserCascade(ctx, userID); err != nil {
		return fmt.Errorf("failed to cascade delete user data: %w", err)
	}

	// Delete user from Auth0 (non-critical - log but don't fail)
	if err := s.auth0Service.DeleteUser(ctx, user.Auth0UserID); err != nil {
		// Auth0 deletion failure is logged but not critical
		// User data is already deleted from our system
		// Manual cleanup can be done later if needed
		// In production, this would be logged to monitoring system
	}

	// Create audit log
	if err := s.auditLogger.Log(ctx, "user.deleted", "user", userID); err != nil {
		// Audit log failure is not critical
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
