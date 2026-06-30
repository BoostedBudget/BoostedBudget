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
	// GetMergeTarget returns the merged_into_merchant_id for the given user+merchant, or
	// "" if no override exists or the override has no merge pointer. Used by MergeMerchants
	// to resolve canonical targets and maintain depth-1 chain invariant.
	GetMergeTarget(ctx context.Context, userID, merchantID string) (string, error)
	// MergeAndFlatten atomically re-points every override whose merged_into_merchant_id
	// references any ID in toFlatten to canonical (flattening depth-≥2 chains, both the
	// source-side G20 fix and the pre-merged-target G17 fix), then bulk-upserts each
	// source → canonical override. Both steps run inside a single transaction so callers
	// never observe a half-flattened state. Maintains the depth-1 invariant so single-hop
	// read resolvers in the transaction and merchant-list queries stay correct.
	//
	// displayNames is an optional parallel slice aligned with sources. When displayNames[i]
	// is non-nil it is written as that source's display_name via COALESCE (new value wins,
	// existing preserved if nil). Pass nil for displayNames when no display_name change is
	// needed. This allows the caller to thread a rename atomically through the same
	// transaction as the merge, eliminating the pre-Upsert non-atomicity (G27).
	MergeAndFlatten(ctx context.Context, userID string, sources []string, canonical string, toFlatten []string, displayNames []*string) ([]domain.MerchantNameOverride, error)
	// BulkApply atomically applies all display-only overrides and all pre-resolved merge
	// groups inside a single transaction. A failure at any point rolls back every write,
	// including display-only upserts that preceded a failing merge group. This eliminates
	// the partial-persist race where BulkUpsert + per-group MergeAndFlatten used separate
	// transactions (promoted out of #1348, Issue #957 G — BulkSetOverrides atomicity).
	//
	// Callers must pre-resolve canonicals and toFlatten slices and exclude self-merges from
	// BulkMergeGroup.Sources (self-merge renames go into displayOnly instead).
	BulkApply(ctx context.Context, userID string, displayOnly []domain.MerchantNameOverride, groups []BulkMergeGroup) ([]domain.MerchantNameOverride, error)
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
// Display name follows the per-user override chain (G6b + G32):
//
//	COALESCE(
//	  source_override.display_name,    -- user renamed the source merchant directly
//	  target_override.display_name,    -- user renamed the canonical (merged-into) merchant
//	  NULLIF(canonical.normalized_name, ''),
//	  canonical.name
//	)
//
// The target-override join (mno_t) mirrors ResolveEffectiveMerchantNames so the Merchants
// tab and the transaction list always resolve display names consistently (G32, issue #957).
func (r *MerchantRepository) ListMerchants(ctx context.Context, userID string) ([]domain.MerchantSummary, error) {
	const q = `
        SELECT
            COALESCE(o.merged_into_merchant_id, t.merchant_id)::text        AS effective_merchant_id,
            COALESCE(NULLIF(m2.normalized_name, ''), m2.name)               AS normalized_name,
            MAX(COALESCE(o.display_name, mno_t.display_name, NULLIF(m2.normalized_name, ''), m2.name)) AS effective_display_name,
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
               ON o.merchant_id = t.merchant_id AND o.user_id = $1::uuid
        LEFT   JOIN merchants m2
               ON m2.id = COALESCE(o.merged_into_merchant_id, t.merchant_id)
        LEFT   JOIN merchant_name_overrides mno_t
               ON mno_t.merchant_id = COALESCE(o.merged_into_merchant_id, t.merchant_id) AND mno_t.user_id = $1::uuid
        WHERE  t.user_id = $1::uuid
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
            WHERE  t.user_id = $1::uuid
              AND  t.merchant_id IS NOT NULL
              AND  t.pending = false
              AND  m.normalized_name IS NOT NULL
              AND  NOT EXISTS (
                       SELECT 1 FROM merchant_name_overrides o
                       WHERE  o.user_id = $1::uuid
                         AND  o.merchant_id = t.merchant_id
                         AND  o.merged_into_merchant_id IS NOT NULL
                   )
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
        ),
        override_stats AS (
            SELECT merchant_id,
                   COALESCE(display_name, '') AS display_name,
                   merged_into_merchant_id::text,
                   (merged_into_merchant_id IS NOT NULL) AS is_merged
            FROM   merchant_name_overrides
            WHERE  user_id = $1::uuid
        )
        SELECT g.group_key::text,
               g.merchant_id::text,
               COALESCE(m.normalized_name, m.name) AS normalized_name,
               COALESCE(NULLIF(os.display_name, ''), m.normalized_name, m.name) AS effective_display_name,
               (SELECT COUNT(*) FROM transactions t2
                WHERE t2.merchant_id = g.merchant_id AND t2.user_id = $1::uuid AND NOT t2.pending) AS transaction_count,
               COALESCE(
                   (SELECT SUM(ABS(t2.amount)) FROM transactions t2
                    WHERE t2.merchant_id = g.merchant_id AND t2.user_id = $1::uuid AND NOT t2.pending),
                   0
               )                                     AS total_spend,
               os.merchant_id IS NOT NULL          AS has_override,
               os.is_merged                        AS is_merged,
               os.merged_into_merchant_id          AS merged_into_merchant_id
        FROM   grouped g
        JOIN   merchants m ON m.id = g.merchant_id
        LEFT   JOIN override_stats os ON os.merchant_id = g.merchant_id
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
