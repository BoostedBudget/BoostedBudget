package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// querier is the minimal interface satisfied by both DB (*pgxpool.Pool) and pgx.Tx.
// Private tx-scoped SQL helpers accept a querier so the same SQL body can run either
// inside an open transaction (for BulkApply) or directly against the pool (for the
// existing single-op public methods).
type querier interface {
	Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
}

// BulkMergeGroup is a pre-resolved merge operation. The service layer resolves canonicals
// and toFlatten slices before calling BulkApply; the repository is a pure write.
// Self-merges (src == Canonical) must be excluded by the caller.
type BulkMergeGroup struct {
	Sources      []string   // merchant IDs to merge into Canonical (self-merges excluded)
	Canonical    string     // resolved canonical target merchant ID
	ToFlatten    []string   // IDs whose inbound merge chains must be re-pointed to Canonical
	DisplayNames []*string  // parallel slice to Sources; nil entries preserve existing names
}

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
//
// ON CONFLICT COALESCE invariant: merged_into_merchant_id is preserved when incoming is NULL
// (pure-rename path). This prevents nil Upsert from wiping a merge pointer set by MergeAndFlatten (G31, #957).
func (r *MerchantOverrideRepository) Upsert(ctx context.Context, override domain.MerchantNameOverride) (*domain.MerchantNameOverride, error) {
	const q = `
        INSERT INTO merchant_name_overrides
            (id, user_id, merchant_id, display_name, merged_into_merchant_id, created_at, updated_at)
        VALUES
            (gen_random_uuid(), $1, $2, $3, $4, NOW(), NOW())
        ON CONFLICT (user_id, merchant_id) DO UPDATE
            SET display_name            = EXCLUDED.display_name,
                merged_into_merchant_id = COALESCE(EXCLUDED.merged_into_merchant_id, merchant_name_overrides.merged_into_merchant_id),
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

// BulkUpsert inserts or updates multiple merchant name overrides using UNNEST.
// Empty input returns an empty slice immediately.
// Applies the same ON CONFLICT COALESCE invariant as Upsert (G31, #957).
//
// Delegates to bulkUpsertTx so the same SQL body can be reused inside BulkApply's tx.
func (r *MerchantOverrideRepository) BulkUpsert(ctx context.Context, overrides []domain.MerchantNameOverride) ([]domain.MerchantNameOverride, error) {
	if len(overrides) == 0 {
		return []domain.MerchantNameOverride{}, nil
	}
	return bulkUpsertTx(ctx, r.db, overrides)
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
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return target, nil
}

// MergeAndFlatten atomically executes two steps inside a single transaction:
//  1. Re-points every override whose merged_into_merchant_id references any ID in toFlatten
//     to canonical (fixes both the source-side depth-2 gap G20 and the pre-merged-target case G17).
//  2. Bulk-upserts each source → canonical override (one row per source).
//
// displayNames is an optional parallel slice aligned with sources. When displayNames[i] is
// non-nil it is written as that source's display_name (COALESCE: new wins, existing preserved
// if nil). When displayNames is nil all source display_names are preserved unchanged (G27).
//
// Running both steps in one transaction ensures callers never observe a half-flattened state.
// sources must not include canonical (callers are responsible for filtering it out).
//
// Delegates to mergeAndFlattenTx so the same SQL body can be reused inside BulkApply's tx.
func (r *MerchantOverrideRepository) MergeAndFlatten(ctx context.Context, userID string, sources []string, canonical string, toFlatten []string, displayNames []*string) ([]domain.MerchantNameOverride, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("MergeAndFlatten: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var results []domain.MerchantNameOverride
	results, err = mergeAndFlattenTx(ctx, tx, userID, sources, canonical, toFlatten, displayNames)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("MergeAndFlatten: commit: %w", err)
	}
	return results, nil
}

// BulkApply atomically applies all display-only overrides and all pre-resolved merge groups
// inside a single transaction. A failure at any point rolls back every write — including
// display-only upserts that preceded a failing merge group.
//
// Callers (service layer) are responsible for pre-resolving canonicals and toFlatten slices
// (via GetMergeTarget / resolveCanonical) and for filtering out self-merges from Sources
// before building BulkMergeGroup entries. Self-merge renames must be passed in displayOnly.
func (r *MerchantOverrideRepository) BulkApply(ctx context.Context, userID string, displayOnly []domain.MerchantNameOverride, groups []BulkMergeGroup) ([]domain.MerchantNameOverride, error) {
	if len(displayOnly) == 0 && len(groups) == 0 {
		return []domain.MerchantNameOverride{}, nil
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("BulkApply: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	results := make([]domain.MerchantNameOverride, 0, len(displayOnly))

	if len(displayOnly) > 0 {
		var bulkResults []domain.MerchantNameOverride
		bulkResults, err = bulkUpsertTx(ctx, tx, displayOnly)
		if err != nil {
			return nil, fmt.Errorf("BulkApply: display-only upsert: %w", err)
		}
		results = append(results, bulkResults...)
	}

	for _, g := range groups {
		var merged []domain.MerchantNameOverride
		merged, err = mergeAndFlattenTx(ctx, tx, userID, g.Sources, g.Canonical, g.ToFlatten, g.DisplayNames)
		if err != nil {
			return nil, fmt.Errorf("BulkApply: merge group (canonical=%s): %w", g.Canonical, err)
		}
		results = append(results, merged...)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("BulkApply: commit: %w", err)
	}
	return results, nil
}

var _ MerchantOverrideRepo = (*MerchantOverrideRepository)(nil)

// bulkUpsertTx executes the UNNEST bulk-upsert SQL on q (DB or tx).
// Applies ON CONFLICT COALESCE (G31): nil merged_into_merchant_id preserves existing merge pointer.
func bulkUpsertTx(ctx context.Context, q querier, overrides []domain.MerchantNameOverride) ([]domain.MerchantNameOverride, error) {
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

	const sql = `
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
                merged_into_merchant_id = COALESCE(EXCLUDED.merged_into_merchant_id, merchant_name_overrides.merged_into_merchant_id),
                updated_at              = NOW()
        RETURNING id, user_id, merchant_id, display_name, merged_into_merchant_id, created_at, updated_at`

	rows, err := q.Query(ctx, sql, userIDs, merchantIDs, displayNames, mergedIntoIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]domain.MerchantNameOverride, 0, len(overrides))
	for rows.Next() {
		var o domain.MerchantNameOverride
		if err := rows.Scan(
			&o.ID, &o.UserID, &o.MerchantID, &o.DisplayName,
			&o.MergedIntoMerchantID, &o.CreatedAt, &o.UpdatedAt,
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

// mergeAndFlattenTx executes chain-flatten UPDATE + source upsert INSERT on q.
// Does NOT open or commit a transaction; caller is responsible for wrapping in a tx.
func mergeAndFlattenTx(ctx context.Context, q querier, userID string, sources []string, canonical string, toFlatten []string, displayNames []*string) ([]domain.MerchantNameOverride, error) {
	// Step 1: flatten inbound chains — re-point anything currently pointing at any toFlatten
	// ID to the canonical. The extra guard (merchant_id != canonical) prevents self-pointing.
	if len(toFlatten) > 0 {
		const flattenQ = `
			UPDATE merchant_name_overrides
			SET    merged_into_merchant_id = $2::uuid,
			       updated_at = NOW()
			WHERE  user_id = $1
			  AND  merged_into_merchant_id = ANY($3::uuid[])
			  AND  merchant_id != $2::uuid`
		if _, err := q.Exec(ctx, flattenQ, userID, canonical, toFlatten); err != nil {
			return nil, fmt.Errorf("mergeAndFlattenTx: flatten chains: %w", err)
		}
	}

	// Step 2: bulk-upsert each source → canonical override.
	// displayNames is an optional parallel array; COALESCE prefers the new name over the
	// existing value (nil entry = keep existing). This threads a rename atomically through
	// the same operation as the merge (G27).
	dnSlice := make([]*string, len(sources))
	if len(displayNames) > 0 {
		copy(dnSlice, displayNames)
	}

	const upsertQ = `
		INSERT INTO merchant_name_overrides
		    (id, user_id, merchant_id, display_name, merged_into_merchant_id, created_at, updated_at)
		SELECT gen_random_uuid(), $1, u.merchant_id::uuid, u.display_name, $2::uuid, NOW(), NOW()
		FROM   unnest($3::text[], $4::text[]) AS u(merchant_id, display_name)
		ON CONFLICT (user_id, merchant_id) DO UPDATE
		    SET merged_into_merchant_id = EXCLUDED.merged_into_merchant_id,
		        display_name            = COALESCE(EXCLUDED.display_name, merchant_name_overrides.display_name),
		        updated_at              = NOW()
		RETURNING id, user_id, merchant_id, display_name, merged_into_merchant_id, created_at, updated_at`

	rows, err := q.Query(ctx, upsertQ, userID, canonical, sources, dnSlice)
	if err != nil {
		return nil, fmt.Errorf("mergeAndFlattenTx: upsert sources: %w", err)
	}

	results := make([]domain.MerchantNameOverride, 0, len(sources))
	for rows.Next() {
		var o domain.MerchantNameOverride
		if err = rows.Scan(&o.ID, &o.UserID, &o.MerchantID, &o.DisplayName, &o.MergedIntoMerchantID, &o.CreatedAt, &o.UpdatedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("mergeAndFlattenTx: scan: %w", err)
		}
		results = append(results, o)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("mergeAndFlattenTx: rows: %w", err)
	}
	return results, nil
}
