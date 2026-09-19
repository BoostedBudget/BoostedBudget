package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

type postgresSubscriptionOverrideRepository struct {
	db DB
}

// NewSubscriptionOverrideRepository creates a postgres-backed repo
func NewSubscriptionOverrideRepository(db DB) SubscriptionOverrideRepository {
	return &postgresSubscriptionOverrideRepository{
		db: db,
	}
}

func (r *postgresSubscriptionOverrideRepository) CreateOverride(ctx context.Context, override *domain.UserSubscriptionOverride) error {
	query := `
		INSERT INTO user_subscription_overrides (user_id, resource_type, limit_value, reason, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at
	`
	err := r.db.QueryRow(ctx, query, override.UserID, override.ResourceType, override.LimitValue, override.Reason, override.ExpiresAt).
		Scan(&override.ID, &override.CreatedAt, &override.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create override: %w", err)
	}

	return nil
}

func (r *postgresSubscriptionOverrideRepository) GetActiveOverrides(ctx context.Context, userID string, asOf time.Time) ([]domain.UserSubscriptionOverride, error) {
	query := `
		SELECT id, user_id, resource_type, limit_value, reason, expires_at, created_at, updated_at
		FROM user_subscription_overrides
		WHERE user_id = $1 AND (expires_at IS NULL OR expires_at > $2)
	`
	rows, err := r.db.Query(ctx, query, userID, asOf)
	if err != nil {
		return nil, fmt.Errorf("failed to query active overrides: %w", err)
	}
	defer rows.Close()

	var overrides []domain.UserSubscriptionOverride
	for rows.Next() {
		var o domain.UserSubscriptionOverride
		if err := rows.Scan(&o.ID, &o.UserID, &o.ResourceType, &o.LimitValue, &o.Reason, &o.ExpiresAt, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan override: %w", err)
		}
		overrides = append(overrides, o)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	// Always return an empty slice instead of nil for JSON serialization consistency
	if overrides == nil {
		overrides = []domain.UserSubscriptionOverride{}
	}

	return overrides, nil
}

func (r *postgresSubscriptionOverrideRepository) DeleteOverride(ctx context.Context, userID, resourceType string) error {
	query := `DELETE FROM user_subscription_overrides WHERE user_id = $1 AND resource_type = $2`
	cmd, err := r.db.Exec(ctx, query, userID, resourceType)
	if err != nil {
		return fmt.Errorf("failed to delete override: %w", err)
	}

	if cmd.RowsAffected() == 0 {
		return errors.New("override not found")
	}

	return nil
}

func (r *postgresSubscriptionOverrideRepository) GetOverrideByID(ctx context.Context, id int64) (*domain.UserSubscriptionOverride, error) {
	query := `
		SELECT id, user_id, resource_type, limit_value, reason, expires_at, created_at, updated_at 
		FROM user_subscription_overrides 
		WHERE id = $1
	`
	var o domain.UserSubscriptionOverride
	err := r.db.QueryRow(ctx, query, id).
		Scan(&o.ID, &o.UserID, &o.ResourceType, &o.LimitValue, &o.Reason, &o.ExpiresAt, &o.CreatedAt, &o.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("override not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get override by ID: %w", err)
	}

	return &o, nil
}

// UpdateOverride's updated_at deliberately stays NOW() (the database's own transaction-time
// clock), NOT a threaded asOf parameter (Batch S2 Task 2 decision, made explicit rather than left
// implicit as the task brief required): updated_at here is a WRITE audit timestamp — "when this
// row was actually persisted" — not a READ-side filter predicate that needs to agree with a
// caller's already-captured response snapshot. No current caller needs this write to line up with
// any request's `asOf` (ApplyOverride/RemoveOverride have no asOf to thread, and the admin
// subscription-detail handler never calls UpdateOverride at all). This also matches this
// codebase's established convention for every OTHER write-audit timestamp on this branch
// (UpdateSubscriptionFromPaddle, ClaimForDeletion, etc.) — all use the
// database's own NOW()/clock_timestamp() for `updated_at`, reserving Go-computed instant
// parameters for read-side WHERE-clause predicates that must match a caller's own snapshot.
// Threading asOf through here would require plumbing it through ApplyOverride/RemoveOverride's
// call chains for no caller-visible benefit, purely to look "consistent" with the read-side fixes
// above — not worth the churn.
func (r *postgresSubscriptionOverrideRepository) UpdateOverride(ctx context.Context, override *domain.UserSubscriptionOverride) error {
	query := `
		UPDATE user_subscription_overrides
		SET limit_value = $1, reason = $2, expires_at = $3, updated_at = NOW()
		WHERE id = $4
		RETURNING updated_at
	`

	var updatedAt time.Time
	err := r.db.QueryRow(ctx, query, override.LimitValue, override.Reason, override.ExpiresAt, override.ID).Scan(&updatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("override not found")
	}
	if err != nil {
		return fmt.Errorf("failed to update override: %w", err)
	}

	override.UpdatedAt = updatedAt
	return nil
}
