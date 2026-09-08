// Package repository provides data access for savings impact snapshots.
// Issue #972: Savings Transfer Automation — daily aggregate stats.
package repository

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// SavingsImpactRepository defines the data access methods for savings impact snapshots.
type SavingsImpactRepository interface {
	// WriteSnapshot inserts a daily snapshot (idempotent per UTC date).
	// See implementation for ON CONFLICT details.
	WriteSnapshot(ctx context.Context, snapshot *domain.SavingsImpactSnapshot) error

	// GetLatestSnapshot returns the most recent snapshot row, or nil if none exists.
	GetLatestSnapshot(ctx context.Context) (*domain.SavingsImpactSnapshot, error)

	// GetUserImpact returns the sum of actual_amount for all completed suggestions by the
	// requesting user, regardless of their include_in_aggregate_stats setting. Also returns
	// count and recent items. The privacy filter only applies to aggregate (community-wide) stats.
	GetUserImpact(ctx context.Context, userID string) (*domain.UserSavingsImpact, error)

	// GetAggregateSavingsStats returns cross-user aggregate totals for completed suggestions
	// where include_in_aggregate_stats = true. Used by the daily stats snapshot worker.
	GetAggregateSavingsStats(ctx context.Context) (totalAmount float64, userCount int, completedCount int, err error)
}

// savingsImpactRepository implements SavingsImpactRepository using PostgreSQL.
type savingsImpactRepository struct {
	db DB
	// enc resolves and decrypts destination account names for recent items.
	// Nil when the FieldEncryptor is not available (e.g., integration tests without
	// encryption config, or when ClerkSecretKey is not set in prod).
	// When nil, account_name_hint decryption is skipped and the plaintext nickname
	// path is still attempted.
	enc *appcrypto.FieldEncryptor
}

// NewSavingsImpactRepository creates a new SavingsImpactRepository without an encryptor.
// Destination names for recent items will use only plaintext fields (nickname, name);
// encrypted account_name_hint will not be decrypted.
func NewSavingsImpactRepository(db DB) SavingsImpactRepository {
	return &savingsImpactRepository{db: db}
}

// NewSavingsImpactRepositoryWithEncryptor creates a new SavingsImpactRepository with an
// encryptor for decrypting user_accounts.account_name_hint (encrypted at rest, migration 000053).
// This is the constructor used in production (main.go) and integration tests.
func NewSavingsImpactRepositoryWithEncryptor(db DB, enc *appcrypto.FieldEncryptor) SavingsImpactRepository {
	return &savingsImpactRepository{db: db, enc: enc}
}

// WriteSnapshot inserts or updates a daily savings impact snapshot.
// The ON CONFLICT clause targets the unique expression index on
// (DATE(computed_at AT TIME ZONE 'UTC')) — this must exactly match the
// index expression in migration 000092 for PostgreSQL to use it.
func (r *savingsImpactRepository) WriteSnapshot(ctx context.Context, snapshot *domain.SavingsImpactSnapshot) error {
	query := `
		INSERT INTO savings_impact_snapshots (id, computed_at, total_amount, user_count, completed_count)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT ((DATE(computed_at AT TIME ZONE 'UTC'))) DO UPDATE SET
			total_amount    = EXCLUDED.total_amount,
			user_count      = EXCLUDED.user_count,
			completed_count = EXCLUDED.completed_count,
			computed_at     = EXCLUDED.computed_at
	`
	id := uuid.New().String()
	_, err := r.db.Exec(ctx, query,
		id,
		snapshot.ComputedAt,
		snapshot.TotalAmount,
		snapshot.UserCount,
		snapshot.CompletedCount,
	)
	return err
}

// GetLatestSnapshot returns the most recently computed snapshot, or nil if none exist.
func (r *savingsImpactRepository) GetLatestSnapshot(ctx context.Context) (*domain.SavingsImpactSnapshot, error) {
	query := `
		SELECT id, computed_at, total_amount, user_count, completed_count
		FROM savings_impact_snapshots
		ORDER BY computed_at DESC
		LIMIT 1
	`
	var s domain.SavingsImpactSnapshot
	err := r.db.QueryRow(ctx, query).Scan(
		&s.ID,
		&s.ComputedAt,
		&s.TotalAmount,
		&s.UserCount,
		&s.CompletedCount,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetAggregateSavingsStats returns cross-user aggregate totals for completed suggestions
// where include_in_aggregate_stats = true. This is the aggregate query used by the
// daily SavingsStatsWorker to build the platform-wide snapshot.
func (r *savingsImpactRepository) GetAggregateSavingsStats(ctx context.Context) (float64, int, int, error) {
	query := `
		SELECT
			COALESCE(SUM(ts.actual_amount), 0) AS total_amount,
			COUNT(DISTINCT ts.user_id)          AS user_count,
			COUNT(*)                            AS completed_count
		FROM transfer_suggestions ts
		JOIN privacy_settings ps ON ps.user_id = ts.user_id
		WHERE ts.status = 'completed'
		  AND ts.actual_amount IS NOT NULL
		  AND ps.include_in_aggregate_stats = true
	`
	var totalAmount float64
	var userCount, completedCount int
	err := r.db.QueryRow(ctx, query).Scan(&totalAmount, &userCount, &completedCount)
	if err != nil {
		return 0, 0, 0, err
	}
	return totalAmount, userCount, completedCount, nil
}

// GetUserImpact returns per-user savings totals and recent completed suggestions.
// Returns ALL completed suggestions for the requesting user regardless of their
// privacy/aggregate-stats setting — users always see their own data in full.
// The include_in_aggregate_stats privacy filter is applied only in GetAggregateSavingsStats,
// not in per-user queries.
//
// Recent items include a resolved ToAccountName (GAP CA, #972):
//   - Plaid-linked accounts: COALESCE(<chain-overlaid nickname>, accounts.name) — both plaintext.
//     ⚠ #1611: the nickname leg is accountCustomizationExpr(...), NOT a raw accounts.nickname.
//     The raw column was UNREACHABLE for a Plaid account (accounts.name is always populated),
//     which is precisely the bug #1611 fixed. Do not 'simplify' this back to accounts.nickname.
//   - Manual accounts (user_accounts): COALESCE(ua.nickname, decrypted ua.account_name_hint).
//     account_name_hint is encrypted at rest (migration 000053); decryption happens in-Go
//     for the ≤5 bounded rows via FieldEncryptor.DecryptNullableString.
//   - Nil when no name can be resolved (manual account with neither nickname nor hint).
func (r *savingsImpactRepository) GetUserImpact(ctx context.Context, userID string) (*domain.UserSavingsImpact, error) {
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	prevMonthStart := monthStart.AddDate(0, -1, 0)

	totalsQuery := `
		SELECT
			COALESCE(SUM(ts.actual_amount), 0)                                           AS total_amount,
			COUNT(*)                                                                      AS completed_count,
			COALESCE(SUM(ts.actual_amount) FILTER (WHERE ts.completed_at >= $2), 0)      AS month_amount,
			COALESCE(SUM(ts.actual_amount) FILTER (WHERE ts.completed_at >= $3 AND ts.completed_at < $2), 0) AS prev_month_amount
		FROM transfer_suggestions ts
		WHERE ts.user_id = $1
		  AND ts.status = 'completed'
		  AND ts.actual_amount IS NOT NULL
	`

	var impact domain.UserSavingsImpact
	err := r.db.QueryRow(ctx, totalsQuery, userID, monthStart, prevMonthStart).Scan(
		&impact.TotalAmount,
		&impact.CompletedCount,
		&impact.MonthAmount,
		&impact.PrevMonthAmount,
	)
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}

	// LEFT JOIN accounts (Plaid) and user_accounts (manual) to fetch plaintext name fields.
	// account_name_hint is encrypted at rest — decryption happens in-Go below (≤5 rows, bounded).
	// COALESCE(NULLIF(<chain overlay>,''), a.name): prefer the CHAIN-authoritative customization
	// overlay (accountCustomizationExpr — see account_liveness_sql.go), fall back to Plaid name.
	// #1611: a bare a.nickname here made the ua_nickname leg below dead code for every Plaid-linked
	// account, because a.name is always populated for one — the chain nickname must win INSIDE
	// plaid_account_name, via the same CASE-not-COALESCE overlay every other account read uses, so
	// a cleared chain override falls back to a.name instead of resurrecting a stale a.nickname.
	// ua (the manual-account join below, on to_user_account_id) is unrelated to a_ua (the chain
	// overlay join on a.user_account_id) and serves manual accounts only.
	itemsQuery := `
		SELECT
			ts.id,
			ts.amount,
			ts.actual_amount,
			ts.to_account_id,
			ts.to_user_account_id,
			ts.completed_at,
			ts.completion_source,
			COALESCE(NULLIF(` + accountCustomizationExpr("a", "nickname") + `, ''), a.name) AS plaid_account_name,
			NULLIF(ua.nickname, '')                      AS ua_nickname,
			ua.account_name_hint                         AS ua_hint_encrypted
		FROM transfer_suggestions ts
		LEFT JOIN accounts a     ON a.id  = ts.to_account_id     AND a.user_id  = ts.user_id` +
		accountCustomizationJoin("a") + `
		LEFT JOIN user_accounts ua ON ua.id = ts.to_user_account_id AND ua.user_id = ts.user_id
		WHERE ts.user_id = $1
		  AND ts.status = 'completed'
		  AND ts.actual_amount IS NOT NULL
		ORDER BY ts.completed_at DESC
		LIMIT 5
	`

	rows, err := r.db.Query(ctx, itemsQuery, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var item domain.CompletedSuggestionSummary
		var plaidName, uaNickname, uaHintEncrypted *string

		if scanErr := rows.Scan(
			&item.ID,
			&item.Amount,
			&item.ActualAmount,
			&item.ToAccountID,
			&item.ToUserAccountID,
			&item.CompletedAt,
			&item.CompletionSource,
			&plaidName,
			&uaNickname,
			&uaHintEncrypted,
		); scanErr != nil {
			return nil, scanErr
		}

		item.ToAccountName = resolveDestinationName(plaidName, uaNickname, uaHintEncrypted, r.enc)
		impact.RecentItems = append(impact.RecentItems, &item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if impact.RecentItems == nil {
		impact.RecentItems = make([]*domain.CompletedSuggestionSummary, 0)
	}

	return &impact, nil
}

// resolveDestinationName returns the best available display name for a transfer destination.
// Resolution order (first non-empty wins):
//  1. plaidName  — COALESCE(<chain-overlaid nickname>, accounts.name); see the #1611 note above.
//  2. uaNickname — user_accounts.nickname (plaintext) for manual destinations.
//  3. Decrypted uaHintEncrypted — user_accounts.account_name_hint, encrypted at rest.
//
// Returns nil when no name can be resolved. enc may be nil (decryption step is skipped).
func resolveDestinationName(plaidName, uaNickname, uaHintEncrypted *string, enc *appcrypto.FieldEncryptor) *string {
	if plaidName != nil && *plaidName != "" {
		return plaidName
	}
	if uaNickname != nil && *uaNickname != "" {
		return uaNickname
	}
	if enc != nil && uaHintEncrypted != nil && *uaHintEncrypted != "" {
		// account_name_hint uses dual-read: values without "ENC$" prefix are returned as-is
		// (plaintext passthrough in FieldEncryptor.DecryptNullableString).
		if !strings.HasPrefix(*uaHintEncrypted, "ENC$") {
			// Plaintext passthrough — no decryption needed.
			return uaHintEncrypted
		}
		decrypted, decryptErr := enc.DecryptNullableString(uaHintEncrypted)
		if decryptErr == nil && decrypted != nil && *decrypted != "" {
			return decrypted
		}
	}
	return nil
}
