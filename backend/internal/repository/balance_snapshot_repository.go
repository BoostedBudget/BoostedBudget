package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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
	db *sql.DB
}

// NewBalanceSnapshotRepository creates a new BalanceSnapshotRepository instance
func NewBalanceSnapshotRepository(db *sql.DB) BalanceSnapshotRepository {
	return &balanceSnapshotRepository{db: db}
}

// Create inserts a new balance snapshot
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
		(id, account_id, user_id, current_balance, available_balance, currency_code, snapshot_at, source, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err := r.db.ExecContext(ctx, query,
		snapshot.ID,
		snapshot.AccountID,
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
		SELECT id, account_id, user_id, current_balance, available_balance,
		       currency_code, snapshot_at, source, created_at
		FROM balance_snapshots
		WHERE user_id = $1
		  AND snapshot_at >= $2
		  AND snapshot_at <= $3
		ORDER BY snapshot_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, userID, startDate, endDate)
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

// GetAggregatedHistory retrieves aggregated balance history for dashboard charts
func (r *balanceSnapshotRepository) GetAggregatedHistory(ctx context.Context, userID string, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error) {
	var query string
	var args []interface{}

	// Build query based on whether account types filter is provided
	// Uses timezone-fixed date cast to aggregate multiple intraday snapshots into daily totals
	if len(accountTypes) > 0 {
		query = `
			SELECT (bs.snapshot_at AT TIME ZONE 'UTC')::date as snapshot_date, SUM(bs.current_balance) as total_balance
			FROM balance_snapshots bs
			JOIN accounts a ON a.id = bs.account_id
			WHERE bs.user_id = $1
			  AND bs.snapshot_at >= $2
			  AND bs.snapshot_at <= $3
			  AND a.type = ANY($4)
			GROUP BY (bs.snapshot_at AT TIME ZONE 'UTC')::date
			ORDER BY (bs.snapshot_at AT TIME ZONE 'UTC')::date ASC
		`
		args = []interface{}{userID, startDate, endDate, pq.Array(accountTypes)}
	} else {
		query = `
			SELECT (bs.snapshot_at AT TIME ZONE 'UTC')::date as snapshot_date, SUM(bs.current_balance) as total_balance
			FROM balance_snapshots bs
			WHERE bs.user_id = $1
			  AND bs.snapshot_at >= $2
			  AND bs.snapshot_at <= $3
			GROUP BY (bs.snapshot_at AT TIME ZONE 'UTC')::date
			ORDER BY (bs.snapshot_at AT TIME ZONE 'UTC')::date ASC
		`
		args = []interface{}{userID, startDate, endDate}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []domain.BalanceHistoryPoint
	for rows.Next() {
		var point domain.BalanceHistoryPoint
		err := rows.Scan(
			&point.Date,
			&point.TotalBalance,
		)
		if err != nil {
			return nil, err
		}
		history = append(history, point)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return history, nil
}

// GetLatestByUserID retrieves the most recent balance snapshot for each account for a user
func (r *balanceSnapshotRepository) GetLatestByUserID(ctx context.Context, userID string) ([]domain.BalanceSnapshot, error) {
	query := `
		SELECT DISTINCT ON (account_id)
		       id, account_id, user_id, current_balance, available_balance,
		       currency_code, snapshot_at, source, created_at
		FROM balance_snapshots
		WHERE user_id = $1
		ORDER BY account_id, snapshot_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
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

// GetLatestByAccountID retrieves the most recent balance snapshot for a specific account
func (r *balanceSnapshotRepository) GetLatestByAccountID(ctx context.Context, accountID string) (*domain.BalanceSnapshot, error) {
	query := `
		SELECT id, account_id, user_id, current_balance, available_balance,
		       currency_code, snapshot_at, source, created_at
		FROM balance_snapshots
		WHERE account_id = $1
		ORDER BY snapshot_at DESC
		LIMIT 1
	`

	var s domain.BalanceSnapshot
	err := r.db.QueryRowContext(ctx, query, accountID).Scan(
		&s.ID,
		&s.AccountID,
		&s.UserID,
		&s.CurrentBalance,
		&s.AvailableBalance,
		&s.CurrencyCode,
		&s.SnapshotAt,
		&s.Source,
		&s.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No snapshot found - return nil, nil (not an error)
	}
	if err != nil {
		return nil, err
	}

	return &s, nil
}

// DeleteByAccountID removes all balance snapshots for a specific account
func (r *balanceSnapshotRepository) DeleteByAccountID(ctx context.Context, accountID string) error {
	query := `
		DELETE FROM balance_snapshots WHERE account_id = $1
	`

	result, err := r.db.ExecContext(ctx, query, accountID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	// It's not an error if no rows were deleted (account might not have any snapshots)
	if rowsAffected == 0 {
		return nil
	}

	return nil
}
