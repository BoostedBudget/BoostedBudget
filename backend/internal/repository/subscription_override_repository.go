package repository

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// SubscriptionOverrideRepository defines the interface for managing subscription overrides
// Allows for custom limits per user (enterprise deals, add-ons, promotions)
type SubscriptionOverrideRepository interface {
	// CreateOverride creates a new subscription override for a user
	CreateOverride(ctx context.Context, override *domain.UserSubscriptionOverride) error

	// GetActiveOverrides returns all active (non-expired) overrides for a user
	GetActiveOverrides(ctx context.Context, userID string) ([]domain.UserSubscriptionOverride, error)

	// DeleteOverride removes an override for a specific resource type
	DeleteOverride(ctx context.Context, userID, resourceType string) error

	// GetOverrideByID gets a specific override by ID
	GetOverrideByID(ctx context.Context, id int64) (*domain.UserSubscriptionOverride, error)

	// UpdateOverride updates an existing override
	UpdateOverride(ctx context.Context, override *domain.UserSubscriptionOverride) error
}

// Note: Actual implementation will be created when we add database support for overrides
// For now, this interface allows the service to compile and run tests
