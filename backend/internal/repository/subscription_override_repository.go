package repository

import (
	"context"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// SubscriptionOverrideRepository defines the interface for managing subscription overrides
// Allows for custom limits per user (enterprise deals, add-ons, promotions)
type SubscriptionOverrideRepository interface {
	// CreateOverride creates a new subscription override for a user
	CreateOverride(ctx context.Context, override *domain.UserSubscriptionOverride) error

	// GetActiveOverrides returns all overrides for a user active AS OF asOf — expires_at IS NULL
	// (permanent) or expires_at > asOf. asOf is a bound parameter, not the database's own NOW()
	// (Batch S2 Task 2): callers thread through the same instant already captured for the rest of
	// a request's response (e.g. the admin subscription-detail handler's `asOf`), so this query
	// agrees with every other expiry/liveness predicate evaluated for that same request.
	GetActiveOverrides(ctx context.Context, userID string, asOf time.Time) ([]domain.UserSubscriptionOverride, error)

	// DeleteOverride removes an override for a specific resource type
	DeleteOverride(ctx context.Context, userID, resourceType string) error

	// GetOverrideByID gets a specific override by ID
	GetOverrideByID(ctx context.Context, id int64) (*domain.UserSubscriptionOverride, error)

	// UpdateOverride updates an existing override
	UpdateOverride(ctx context.Context, override *domain.UserSubscriptionOverride) error
}

// Note: Actual implementation will be created when we add database support for overrides
// For now, this interface allows the service to compile and run tests
