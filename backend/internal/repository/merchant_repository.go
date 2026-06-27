package repository

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// MerchantRepo defines the interface for merchant data access.
// A minimal interface enables mocking in service tests.
type MerchantRepo interface {
	GetOrCreateMerchants(ctx context.Context, names []string) (map[string]string, error)
	// GetOrCreateMerchantsWithNormalize is like GetOrCreateMerchants but also sets
	// normalized_name when creating new rows. The normalize func is provided by the
	// caller to avoid import cycles between the repository and merchant normalization packages.
	GetOrCreateMerchantsWithNormalize(ctx context.Context, names []string, normalize func(string) string) (map[string]string, error)
	// ListMerchants returns aggregated merchant summaries for a user, incorporating
	// per-user overrides (rename/merge) and transaction statistics.
	ListMerchants(ctx context.Context, userID string) ([]domain.MerchantSummary, error)
	// GetDuplicateSuggestions uses pg_trgm similarity on normalized_name to find
	// groups of merchants that are likely duplicates. threshold is typically 0.6.
	GetDuplicateSuggestions(ctx context.Context, userID string, threshold float64) ([][]domain.MerchantSummary, error)
}

// MerchantOverrideRepo defines the interface for merchant_name_overrides data access.
type MerchantOverrideRepo interface {
	Upsert(ctx context.Context, override domain.MerchantNameOverride) (*domain.MerchantNameOverride, error)
	BulkUpsert(ctx context.Context, overrides []domain.MerchantNameOverride) ([]domain.MerchantNameOverride, error)
	GetByUserID(ctx context.Context, userID string) ([]domain.MerchantNameOverride, error)
	Delete(ctx context.Context, userID, merchantID string) error
}

// MerchantRepository handles data access for merchants.
type MerchantRepository struct {
	db DB
}

// NewMerchantRepository creates a new MerchantRepository.
func NewMerchantRepository(db DB) *MerchantRepository {
	return &MerchantRepository{db: db}
}

// GetOrCreateMerchants bulk-upserts merchants by name and returns a map of name→id.
// Uses a single DB round-trip via UNNEST.
// ON CONFLICT DO UPDATE is required (not DO NOTHING) to always RETURNING the id.
func (r *MerchantRepository) GetOrCreateMerchants(ctx context.Context, names []string) (map[string]string, error) {
	if len(names) == 0 {
		return map[string]string{}, nil
	}
	const q = `
        INSERT INTO merchants (id, name, created_at)
        SELECT gen_random_uuid(), u.name, NOW()
        FROM   unnest($1::text[]) AS u(name)
        ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
        RETURNING name, id`
	rows, err := r.db.Query(ctx, q, names)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string, len(names))
	for rows.Next() {
		var name, id string
		if err := rows.Scan(&name, &id); err != nil {
			return nil, err
		}
		result[name] = id
	}
	return result, rows.Err()
}

// GetOrCreateMerchantsWithNormalize bulk-upserts merchants by name, setting normalized_name
// via the provided normalize func. Only updates normalized_name when it was previously NULL
// (backfill-safe).
func (r *MerchantRepository) GetOrCreateMerchantsWithNormalize(ctx context.Context, names []string, normalize func(string) string) (map[string]string, error) {
	if len(names) == 0 {
		return map[string]string{}, nil
	}
	normalized := make([]string, len(names))
	for i, n := range names {
		normalized[i] = normalize(n)
	}
	const q = `
        INSERT INTO merchants (id, name, normalized_name, created_at)
        SELECT gen_random_uuid(), u.name, u.normalized, NOW()
        FROM   unnest($1::text[], $2::text[]) AS u(name, normalized)
        ON CONFLICT (name) DO UPDATE
            SET normalized_name = COALESCE(merchants.normalized_name, EXCLUDED.normalized_name)
        RETURNING name, id`
	rows, err := r.db.Query(ctx, q, names, normalized)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string, len(names))
	for rows.Next() {
		var name, id string
		if err := rows.Scan(&name, &id); err != nil {
			return nil, err
		}
		result[name] = id
	}
	return result, rows.Err()
}

// ListMerchants returns aggregated merchant summaries for a user.
// Merged merchants collapse into a single row (the merge target).
// Display name follows the override chain:
//
//	COALESCE(override.display_name, canonical_merchant.normalized_name, merchant.name)
func (r *MerchantRepository) ListMerchants(ctx context.Context, userID string) ([]domain.MerchantSummary, error) {
	const q = `
        SELECT
            COALESCE(o.merged_into_merchant_id, t.merchant_id)::text        AS effective_merchant_id,
            COALESCE(m2.normalized_name, m2.name)                           AS normalized_name,
            MAX(COALESCE(o.display_name, m2.normalized_name, m2.name))      AS effective_display_name,
            array_agg(DISTINCT m.name)                                       AS raw_variants,
            COUNT(*)                                                         AS transaction_count,
            COALESCE(SUM(ABS(t.amount)), 0)                                 AS total_spend,
            MODE() WITHIN GROUP (ORDER BY t.pfc_primary)                    AS dominant_category,
            bool_or(o.id IS NOT NULL)                                       AS has_override,
            bool_or(o.merged_into_merchant_id IS NOT NULL)                  AS is_merged,
            MAX(o.merged_into_merchant_id)::text                            AS merged_into_merchant_id
        FROM   transactions t
        JOIN   merchants m  ON m.id = t.merchant_id
        LEFT   JOIN merchant_name_overrides o
               ON o.merchant_id = t.merchant_id AND o.user_id = $1
        LEFT   JOIN merchants m2
               ON m2.id = COALESCE(o.merged_into_merchant_id, t.merchant_id)
        WHERE  t.user_id = $1
          AND  t.merchant_id IS NOT NULL
          AND  t.pending = false
        GROUP  BY COALESCE(o.merged_into_merchant_id, t.merchant_id), m2.id
        ORDER  BY total_spend DESC`

	rows, err := r.db.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []domain.MerchantSummary
	for rows.Next() {
		var s domain.MerchantSummary
		var rawVariants []string
		if err := rows.Scan(
			&s.MerchantID,
			&s.NormalizedName,
			&s.EffectiveDisplayName,
			&rawVariants,
			&s.TransactionCount,
			&s.TotalSpend,
			&s.DominantCategory,
			&s.HasOverride,
			&s.IsMerged,
			&s.MergedIntoMerchantID,
		); err != nil {
			return nil, err
		}
		s.RawVariants = rawVariants
		summaries = append(summaries, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if summaries == nil {
		summaries = []domain.MerchantSummary{}
	}
	return summaries, nil
}

// GetDuplicateSuggestions uses pg_trgm similarity on normalized_name to find pairs of merchants
// that are likely duplicates (similarity > threshold). Returns groups of similar merchants.
// threshold is typically 0.6.
func (r *MerchantRepository) GetDuplicateSuggestions(ctx context.Context, userID string, threshold float64) ([][]domain.MerchantSummary, error) {
	const q = `
        WITH user_merchants AS (
            SELECT DISTINCT t.merchant_id, m.normalized_name, m.name
            FROM   transactions t
            JOIN   merchants m ON m.id = t.merchant_id
            WHERE  t.user_id = $1
              AND  t.merchant_id IS NOT NULL
              AND  t.pending = false
              AND  m.normalized_name IS NOT NULL
        ),
        pairs AS (
            SELECT a.merchant_id AS id_a,
                   b.merchant_id AS id_b,
                   a.normalized_name AS norm_a,
                   b.normalized_name AS norm_b
            FROM   user_merchants a
            JOIN   user_merchants b
                   ON a.merchant_id < b.merchant_id
                  AND similarity(a.normalized_name, b.normalized_name) > $2
        ),
        grouped AS (
            SELECT DISTINCT unnest(ARRAY[id_a, id_b]) AS merchant_id,
                   LEAST(id_a, id_b)                  AS group_key
            FROM   pairs
        )
        SELECT g.group_key::text,
               g.merchant_id::text,
               COALESCE(m.normalized_name, m.name) AS normalized_name,
               COALESCE(
                   (SELECT display_name FROM merchant_name_overrides
                    WHERE user_id = $1 AND merchant_id = g.merchant_id AND display_name IS NOT NULL),
                   m.normalized_name,
                   m.name
               )                                     AS effective_display_name,
               (SELECT COUNT(*) FROM transactions t2
                WHERE t2.merchant_id = g.merchant_id AND t2.user_id = $1 AND NOT t2.pending) AS transaction_count,
               COALESCE(
                   (SELECT SUM(ABS(t2.amount)) FROM transactions t2
                    WHERE t2.merchant_id = g.merchant_id AND t2.user_id = $1 AND NOT t2.pending),
                   0
               )                                     AS total_spend,
               EXISTS (SELECT 1 FROM merchant_name_overrides o
                       WHERE o.user_id = $1 AND o.merchant_id = g.merchant_id) AS has_override,
               EXISTS (SELECT 1 FROM merchant_name_overrides o
                       WHERE o.user_id = $1 AND o.merchant_id = g.merchant_id
                         AND o.merged_into_merchant_id IS NOT NULL)            AS is_merged,
               (SELECT merged_into_merchant_id::text FROM merchant_name_overrides
                WHERE user_id = $1 AND merchant_id = g.merchant_id)           AS merged_into_merchant_id
        FROM   grouped g
        JOIN   merchants m ON m.id = g.merchant_id
        ORDER  BY g.group_key, g.merchant_id`

	rows, err := r.db.Query(ctx, q, userID, threshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type rowResult struct {
		groupKey string
		summary  domain.MerchantSummary
	}
	var results []rowResult

	for rows.Next() {
		var rr rowResult
		var s domain.MerchantSummary
		if err := rows.Scan(
			&rr.groupKey,
			&s.MerchantID,
			&s.NormalizedName,
			&s.EffectiveDisplayName,
			&s.TransactionCount,
			&s.TotalSpend,
			&s.HasOverride,
			&s.IsMerged,
			&s.MergedIntoMerchantID,
		); err != nil {
			return nil, err
		}
		s.RawVariants = []string{}
		rr.summary = s
		results = append(results, rr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Group by groupKey preserving order.
	groupMap := make(map[string][]domain.MerchantSummary)
	var groupOrder []string
	for _, rr := range results {
		if _, ok := groupMap[rr.groupKey]; !ok {
			groupOrder = append(groupOrder, rr.groupKey)
		}
		groupMap[rr.groupKey] = append(groupMap[rr.groupKey], rr.summary)
	}

	groups := make([][]domain.MerchantSummary, 0, len(groupOrder))
	for _, key := range groupOrder {
		groups = append(groups, groupMap[key])
	}
	return groups, nil
}
