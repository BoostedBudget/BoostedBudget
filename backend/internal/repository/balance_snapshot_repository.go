package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

var (
	// ErrSnapshotNotFound is returned when a balance snapshot is not found in the database
	ErrSnapshotNotFound = errors.New("balance snapshot not found")
)

// BalanceSnapshotRepository defines the interface for balance snapshot data access
type BalanceSnapshotRepository interface {
	// Create inserts a new balance snapshot.
	// Does NOT perform upsert - allows multiple snapshots per account per day for high-frequency syncs.
	// Deduplication (5-minute window) should be handled by the service layer using GetLatestByAccountID.
	Create(ctx context.Context, snapshot *domain.BalanceSnapshot) error

	// GetByUserIDAndDateRange retrieves all balance snapshots for a user within a date range.
	// Results are ordered by snapshot_at ascending (oldest first).
	GetByUserIDAndDateRange(ctx context.Context, userID string, startDate, endDate time.Time) ([]domain.BalanceSnapshot, error)

	// GetAggregatedHistory retrieves aggregated balance history for dashboard charts.
	// Returns daily total balance sums (grouped by DATE(snapshot_at)), optionally filtered by account types.
	// If accountTypes is nil or empty, all account types are included.
	GetAggregatedHistory(ctx context.Context, userID string, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error)

	// GetLatestByUserID retrieves the most recent balance snapshot for each account for a user.
	// Uses DISTINCT ON (account_id) to get only the latest snapshot per account.
	GetLatestByUserID(ctx context.Context, userID string) ([]domain.BalanceSnapshot, error)

	// GetLatestByAccountID retrieves the most recent balance snapshot for a specific account.
	// Used for deduplication checking (5-minute window) before creating new snapshots.
	GetLatestByAccountID(ctx context.Context, accountID string) (*domain.BalanceSnapshot, error)

	// DeleteByAccountID removes all balance snapshots for a specific account.
	// Used when an account is deleted (cascade delete should handle this, but kept for explicit cleanup).
	DeleteByAccountID(ctx context.Context, accountID string) error
}

// balanceSnapshotRepository implements BalanceSnapshotRepository using PostgreSQL
type balanceSnapshotRepository struct {
	db DB
}

// NewBalanceSnapshotRepository creates a new BalanceSnapshotRepository instance
func NewBalanceSnapshotRepository(db DB) BalanceSnapshotRepository {
	return &balanceSnapshotRepository{db: db}
}

// Create inserts a new balance snapshot
// Issue #171: Includes user_account_id for stable reference across Plaid re-links
func (r *balanceSnapshotRepository) Create(ctx context.Context, snapshot *domain.BalanceSnapshot) error {
	// Generate UUID if not already set
	if snapshot.ID == "" {
		snapshot.ID = uuid.New().String()
	}

	// Set created timestamp
	now := time.Now()
	snapshot.CreatedAt = now

	query := `
		INSERT INTO balance_snapshots
		(id, account_id, user_account_id, user_id, current_balance, available_balance, currency_code, snapshot_at, source, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, err := r.db.Exec(ctx, query,
		snapshot.ID,
		snapshot.AccountID,
		snapshot.UserAccountID,
		snapshot.UserID,
		snapshot.CurrentBalance,
		snapshot.AvailableBalance,
		snapshot.CurrencyCode,
		snapshot.SnapshotAt,
		snapshot.Source,
		snapshot.CreatedAt,
	)

	return err
}

// GetByUserIDAndDateRange retrieves all balance snapshots for a user within a date range
func (r *balanceSnapshotRepository) GetByUserIDAndDateRange(ctx context.Context, userID string, startDate, endDate time.Time) ([]domain.BalanceSnapshot, error) {
	query := `
		SELECT id, account_id, user_account_id, user_id, current_balance, available_balance,
		       currency_code, snapshot_at, source, created_at
		FROM balance_snapshots
		WHERE user_id = $1
		  AND snapshot_at >= $2
		  AND snapshot_at <= $3
		ORDER BY snapshot_at ASC
	`
	return r.scanSnapshots(ctx, query, userID, startDate, endDate)
}

// GetAggregatedHistory retrieves aggregated balance history for dashboard charts.
// The query LEFT JOINs accounts unconditionally so that per-type breakdowns are always
// available for net-worth computation. Orphaned snapshots (deleted account rows) are
// included in TotalBalance but omitted from ByType.
func (r *balanceSnapshotRepository) GetAggregatedHistory(ctx context.Context, userID string, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error) {
	// Canary counter: every call applies the re-link chain CTE dedup (Issue #1109).
	observability.AccountsReplacedFiltered.WithLabelValues("dashboard_history").Inc()

	// Base query with parameterized account-type filter in CTE.
	// If accountTypes is provided, add AND a.type = ANY($4) to the WHERE clause.
	const queryBase = `
		WITH latest_per_account_per_day AS (
		    SELECT
		        (snapshot_at AT TIME ZONE 'UTC')::date AS snapshot_date,
		        COALESCE(bs.user_account_id::text, bs.account_id::text) AS chain_key,
		        MAX(snapshot_at) AS latest_at,
		        MAX(bs.id::text) AS latest_id
		    FROM balance_snapshots bs
		    LEFT JOIN accounts a ON a.id = bs.account_id
		    WHERE bs.user_id = $1
		      AND bs.snapshot_at >= $2
		      AND bs.snapshot_at <= $3
		      %s
		    GROUP BY snapshot_date, chain_key
		),
		deduped AS (
		    SELECT (bs.snapshot_at AT TIME ZONE 'UTC')::date AS snapshot_date,
		           bs.current_balance, a.type AS account_type
		    FROM balance_snapshots bs
		    JOIN latest_per_account_per_day l
		      ON COALESCE(bs.user_account_id::text, bs.account_id::text) = l.chain_key
		      AND (bs.snapshot_at AT TIME ZONE 'UTC')::date = l.snapshot_date
		      AND bs.snapshot_at = l.latest_at
		      AND bs.id::text = l.latest_id
		    LEFT JOIN accounts a ON a.id = bs.account_id
		    WHERE bs.user_id = $1
		)
		SELECT snapshot_date,
		       COALESCE(account_type, '') AS account_type,
		       SUM(current_balance) AS type_balance
		FROM deduped
		GROUP BY snapshot_date, account_type
		ORDER BY snapshot_date ASC, account_type ASC
	`

	var query string
	var args []interface{}

	if len(accountTypes) > 0 {
		query = fmt.Sprintf(queryBase, "AND a.type = ANY($4)")
		args = []interface{}{userID, startDate, endDate, accountTypes}
	} else {
		query = fmt.Sprintf(queryBase, "")
		args = []interface{}{userID, startDate, endDate}
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []domain.BalanceHistoryPoint
	var current *domain.BalanceHistoryPoint
	for rows.Next() {
		var d time.Time
		var accountType string
		var bal float64
		if err := rows.Scan(&d, &accountType, &bal); err != nil {
			return nil, err
		}
		if current == nil || !current.Date.Equal(d) {
			history = append(history, domain.BalanceHistoryPoint{Date: d, ByType: map[string]float64{}})
			current = &history[len(history)-1]
		}
		current.TotalBalance += bal
		if accountType != "" {
			current.ByType[accountType] += bal
		}
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return history, nil
}

// GetLatestByUserID retrieves the most recent balance snapshot for each account for a user
func (r *balanceSnapshotRepository) GetLatestByUserID(ctx context.Context, userID string) ([]domain.BalanceSnapshot, error) {
	// Canary counter: every call uses the re-link chain dedup key (Issue #1109).
	observability.AccountsReplacedFiltered.WithLabelValues("dashboard_history").Inc()
	// Use COALESCE(user_account_id::text, account_id::text) as the dedup key so that
	// all physical accounts in a re-link chain share one key (user_account_id) and only
	// the most recent snapshot (the active account) is returned. Legacy rows where
	// user_account_id IS NULL fall back to account_id, preserving previous behaviour.
	query := `
		SELECT DISTINCT ON (COALESCE(user_account_id::text, account_id::text))
		       id, account_id, user_account_id, user_id, current_balance, available_balance,
		       currency_code, snapshot_at, source, created_at
		FROM balance_snapshots
		WHERE user_id = $1
		ORDER BY COALESCE(user_account_id::text, account_id::text), snapshot_at DESC
	`
	return r.scanSnapshots(ctx, query, userID)
}

// GetLatestByAccountID retrieves the most recent balance snapshot for a specific account
func (r *balanceSnapshotRepository) GetLatestByAccountID(ctx context.Context, accountID string) (*domain.BalanceSnapshot, error) {
	query := `
		SELECT id, account_id, user_account_id, user_id, current_balance, available_balance,
		       currency_code, snapshot_at, source, created_at
		FROM balance_snapshots
		WHERE account_id = $1
		ORDER BY snapshot_at DESC
		LIMIT 1
	`

	var s domain.BalanceSnapshot
	err := r.db.QueryRow(ctx, query, accountID).Scan(
		&s.ID,
		&s.AccountID,
		&s.UserAccountID,
		&s.UserID,
		&s.CurrentBalance,
		&s.AvailableBalance,
		&s.CurrencyCode,
		&s.SnapshotAt,
		&s.Source,
		&s.CreatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // No snapshot found - return nil, nil (not an error)
	}
	if err != nil {
		return nil, err
	}

	return &s, nil
}

// DeleteByAccountID removes all balance snapshots for a specific account
func (r *balanceSnapshotRepository) DeleteByAccountID(ctx context.Context, accountID string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM balance_snapshots WHERE account_id = $1`, accountID)
	// It's not an error if no rows were deleted (account might not have any snapshots)
	return err
}

// scanSnapshots is a helper to reduce duplication in snapshot scanning logic
func (r *balanceSnapshotRepository) scanSnapshots(ctx context.Context, query string, args ...interface{}) ([]domain.BalanceSnapshot, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []domain.BalanceSnapshot
	for rows.Next() {
		var s domain.BalanceSnapshot
		err := rows.Scan(
			&s.ID,
			&s.AccountID,
			&s.UserAccountID,
			&s.UserID,
			&s.CurrentBalance,
			&s.AvailableBalance,
			&s.CurrencyCode,
			&s.SnapshotAt,
			&s.Source,
			&s.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, s)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return snapshots, nil
}
