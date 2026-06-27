package repository

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// MerchantOverrideRepository handles data access for merchant_name_overrides.
type MerchantOverrideRepository struct {
	db DB
}

// NewMerchantOverrideRepository creates a new MerchantOverrideRepository.
func NewMerchantOverrideRepository(db DB) *MerchantOverrideRepository {
	return &MerchantOverrideRepository{db: db}
}

// Upsert inserts or updates a merchant name override for a user.
// The unique constraint (user_id, merchant_id) governs conflict resolution.
// At least one of DisplayName or MergedIntoMerchantID must be non-nil (DB CHECK).
func (r *MerchantOverrideRepository) Upsert(ctx context.Context, override domain.MerchantNameOverride) (*domain.MerchantNameOverride, error) {
	const q = `
        INSERT INTO merchant_name_overrides
            (id, user_id, merchant_id, display_name, merged_into_merchant_id, created_at, updated_at)
        VALUES
            (gen_random_uuid(), $1, $2, $3, $4, NOW(), NOW())
        ON CONFLICT (user_id, merchant_id) DO UPDATE
            SET display_name            = EXCLUDED.display_name,
                merged_into_merchant_id = EXCLUDED.merged_into_merchant_id,
                updated_at              = NOW()
        RETURNING id, user_id, merchant_id, display_name, merged_into_merchant_id, created_at, updated_at`

	row := r.db.QueryRow(ctx, q,
		override.UserID,
		override.MerchantID,
		override.DisplayName,
		override.MergedIntoMerchantID,
	)

	var out domain.MerchantNameOverride
	if err := row.Scan(
		&out.ID,
		&out.UserID,
		&out.MerchantID,
		&out.DisplayName,
		&out.MergedIntoMerchantID,
		&out.CreatedAt,
		&out.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &out, nil
}

// BulkUpsert inserts or updates multiple merchant name overrides in a single query
// using UNNEST to pass parallel arrays. Empty input returns an empty slice immediately.
func (r *MerchantOverrideRepository) BulkUpsert(ctx context.Context, overrides []domain.MerchantNameOverride) ([]domain.MerchantNameOverride, error) {
	if len(overrides) == 0 {
		return []domain.MerchantNameOverride{}, nil
	}

	userIDs := make([]string, len(overrides))
	merchantIDs := make([]string, len(overrides))
	displayNames := make([]*string, len(overrides))
	mergedIntoIDs := make([]*string, len(overrides))

	for i, o := range overrides {
		userIDs[i] = o.UserID
		merchantIDs[i] = o.MerchantID
		displayNames[i] = o.DisplayName
		mergedIntoIDs[i] = o.MergedIntoMerchantID
	}

	const q = `
        INSERT INTO merchant_name_overrides
            (id, user_id, merchant_id, display_name, merged_into_merchant_id, created_at, updated_at)
        SELECT gen_random_uuid(),
               u.user_id,
               u.merchant_id::uuid,
               u.display_name,
               u.merged_into_merchant_id::uuid,
               NOW(),
               NOW()
        FROM   unnest($1::text[], $2::text[], $3::text[], $4::text[])
               AS u(user_id, merchant_id, display_name, merged_into_merchant_id)
        ON CONFLICT (user_id, merchant_id) DO UPDATE
            SET display_name            = EXCLUDED.display_name,
                merged_into_merchant_id = EXCLUDED.merged_into_merchant_id,
                updated_at              = NOW()
        RETURNING id, user_id, merchant_id, display_name, merged_into_merchant_id, created_at, updated_at`

	rows, err := r.db.Query(ctx, q, userIDs, merchantIDs, displayNames, mergedIntoIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []domain.MerchantNameOverride
	for rows.Next() {
		var o domain.MerchantNameOverride
		if err := rows.Scan(
			&o.ID,
			&o.UserID,
			&o.MerchantID,
			&o.DisplayName,
			&o.MergedIntoMerchantID,
			&o.CreatedAt,
			&o.UpdatedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if results == nil {
		results = []domain.MerchantNameOverride{}
	}
	return results, nil
}

// GetByUserID returns all merchant name overrides for a user, ordered by created_at DESC.
func (r *MerchantOverrideRepository) GetByUserID(ctx context.Context, userID string) ([]domain.MerchantNameOverride, error) {
	const q = `
        SELECT id, user_id, merchant_id, display_name, merged_into_merchant_id, created_at, updated_at
        FROM   merchant_name_overrides
        WHERE  user_id = $1
        ORDER  BY created_at DESC`

	rows, err := r.db.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var overrides []domain.MerchantNameOverride
	for rows.Next() {
		var o domain.MerchantNameOverride
		if err := rows.Scan(
			&o.ID,
			&o.UserID,
			&o.MerchantID,
			&o.DisplayName,
			&o.MergedIntoMerchantID,
			&o.CreatedAt,
			&o.UpdatedAt,
		); err != nil {
			return nil, err
		}
		overrides = append(overrides, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if overrides == nil {
		overrides = []domain.MerchantNameOverride{}
	}
	return overrides, nil
}

// Delete removes a merchant name override for a specific user+merchant pair.
// Returns nil if no row matched (DELETE is idempotent).
func (r *MerchantOverrideRepository) Delete(ctx context.Context, userID, merchantID string) error {
	const q = `DELETE FROM merchant_name_overrides WHERE user_id = $1 AND merchant_id = $2`
	_, err := r.db.Exec(ctx, q, userID, merchantID)
	return err
}

// Ensure MerchantOverrideRepository satisfies MerchantOverrideRepo at compile time.
var _ MerchantOverrideRepo = (*MerchantOverrideRepository)(nil)
