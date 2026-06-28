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

	results := make([]domain.MerchantNameOverride, 0, len(overrides))
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
	if len(overrides) == 0 {
		return []domain.MerchantNameOverride{}, nil
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

// GetMergeTarget returns the merged_into_merchant_id for the given user+merchant as a string,
// or "" if no override exists or the override has no merge pointer set.
func (r *MerchantOverrideRepository) GetMergeTarget(ctx context.Context, userID, merchantID string) (string, error) {
	const q = `
		SELECT COALESCE(merged_into_merchant_id::text, '')
		FROM   merchant_name_overrides
		WHERE  user_id = $1 AND merchant_id = $2::uuid`
	var target string
	err := r.db.QueryRow(ctx, q, userID, merchantID).Scan(&target)
	if err != nil {
		// pgx returns pgx.ErrNoRows when there is no matching override; treat as no merge.
		if err.Error() == "no rows in result set" {
			return "", nil
		}
		return "", err
	}
	return target, nil
}

// ReassignMergeTarget updates any overrides for userID whose merged_into_merchant_id equals
// oldTargetID to point at newTargetID instead. This is used to flatten merge chains when a
// target merchant is itself re-merged into a new canonical.
func (r *MerchantOverrideRepository) ReassignMergeTarget(ctx context.Context, userID, oldTargetID, newTargetID string) error {
	const q = `
		UPDATE merchant_name_overrides
		SET    merged_into_merchant_id = $3::uuid,
		       updated_at = NOW()
		WHERE  user_id = $1
		  AND  merged_into_merchant_id = $2::uuid`
	_, err := r.db.Exec(ctx, q, userID, oldTargetID, newTargetID)
	return err
}

// Ensure MerchantOverrideRepository satisfies MerchantOverrideRepo at compile time.
var _ MerchantOverrideRepo = (*MerchantOverrideRepository)(nil)
