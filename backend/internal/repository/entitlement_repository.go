package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ErrEntitlementNotFound indicates the user has no currently active (effective_until IS NULL)
// subscription_entitlements row — the normal case for solo users. Callers should treat this
// as "fall back to the user's own tier", not as a real error.
var ErrEntitlementNotFound = errors.New("active entitlement not found")

// EntitlementRepository reads subscription_entitlements rows. Phase 2 is read-only:
// no rows are written in production until Phase 3 (invite-accept / member-removal).
type EntitlementRepository interface {
	// GetActiveEntitlement returns the user's currently open-ended entitlement row
	// (effective_until IS NULL, effective_from <= now()). Returns ErrEntitlementNotFound
	// if none exists. The 000112 partial unique index guarantees at most one such row.
	GetActiveEntitlement(ctx context.Context, userID string) (*domain.SubscriptionEntitlement, error)
}

type entitlementRepository struct {
	db DB
}

// NewEntitlementRepository creates a new EntitlementRepository instance.
func NewEntitlementRepository(db DB) EntitlementRepository {
	return &entitlementRepository{db: db}
}

func (r *entitlementRepository) GetActiveEntitlement(ctx context.Context, userID string) (*domain.SubscriptionEntitlement, error) {
	var e domain.SubscriptionEntitlement
	err := r.db.QueryRow(ctx, `
		SELECT id, user_id, source, tier, status, payer_user_id, household_id,
		       effective_from, effective_until, created_at, updated_at
		FROM subscription_entitlements
		WHERE user_id = $1 AND effective_until IS NULL AND effective_from <= now()
		LIMIT 1
	`, userID).Scan(
		&e.ID, &e.UserID, &e.Source, &e.Tier, &e.Status, &e.PayerUserID, &e.HouseholdID,
		&e.EffectiveFrom, &e.EffectiveUntil, &e.CreatedAt, &e.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEntitlementNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}
