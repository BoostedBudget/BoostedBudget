package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupSnapshotMockDB creates a mock database and sqlmock instance for balance snapshot repository testing
func setupSnapshotMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock, BalanceSnapshotRepository) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	repo := NewBalanceSnapshotRepository(db)
	return db, mock, repo
}

// TestBalanceSnapshotRepository_CreateOrUpdate_NewSnapshot tests inserting a new snapshot
func TestBalanceSnapshotRepository_CreateOrUpdate_NewSnapshot(t *testing.T) {
	db, mock, repo := setupSnapshotMockDB(t)
	defer db.Close()

	ctx := context.Background()
	now := time.Now()
	snapshotDate := time.Date(2024, 11, 15, 0, 0, 0, 0, time.UTC)
	balance := 1500.50
	available := 1450.25

	snapshot := &domain.BalanceSnapshot{
		ID:               uuid.New().String(),
		AccountID:        uuid.New().String(),
		UserID:           uuid.New().String(),
		CurrentBalance:   &balance,
		AvailableBalance: &available,
		CurrencyCode:     "USD",
		SnapshotDate:     snapshotDate,
		Source:           domain.SnapshotSourcePlaidWebhook,
		CreatedAt:        now,
	}

	// Expect UPSERT query (INSERT ... ON CONFLICT DO UPDATE)
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO balance_snapshots
		(id, account_id, user_id, current_balance, available_balance, currency_code, snapshot_date, source, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (account_id, snapshot_date)
		DO UPDATE SET
			current_balance = EXCLUDED.current_balance,
			available_balance = EXCLUDED.available_balance,
			created_at = EXCLUDED.created_at
	`)).WithArgs(
		snapshot.ID,
		snapshot.AccountID,
		snapshot.UserID,
		snapshot.CurrentBalance,
		snapshot.AvailableBalance,
		snapshot.CurrencyCode,
		sqlmock.AnyArg(), // snapshot_date (DATE type)
		snapshot.Source,
		sqlmock.AnyArg(), // created_at
	).WillReturnResult(sqlmock.NewResult(1, 1))

	// Execute
	err := repo.CreateOrUpdate(ctx, snapshot)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_CreateOrUpdate_ConflictUpdate tests updating existing snapshot for same day
func TestBalanceSnapshotRepository_CreateOrUpdate_ConflictUpdate(t *testing.T) {
	db, mock, repo := setupSnapshotMockDB(t)
	defer db.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()
	snapshotDate := time.Date(2024, 11, 15, 0, 0, 0, 0, time.UTC)

	// First snapshot (will be updated)
	balance1 := 1000.00
	snapshot := &domain.BalanceSnapshot{
		ID:             uuid.New().String(),
		AccountID:      accountID,
		UserID:         userID,
		CurrentBalance: &balance1,
		CurrencyCode:   "USD",
		SnapshotDate:   snapshotDate,
		Source:         domain.SnapshotSourcePlaidWebhook,
		CreatedAt:      time.Now(),
	}

	// Expect UPSERT query - should update existing row
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO balance_snapshots
		(id, account_id, user_id, current_balance, available_balance, currency_code, snapshot_date, source, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (account_id, snapshot_date)
		DO UPDATE SET
			current_balance = EXCLUDED.current_balance,
			available_balance = EXCLUDED.available_balance,
			created_at = EXCLUDED.created_at
	`)).WithArgs(
		sqlmock.AnyArg(),
		snapshot.AccountID,
		snapshot.UserID,
		snapshot.CurrentBalance,
		snapshot.AvailableBalance,
		snapshot.CurrencyCode,
		sqlmock.AnyArg(),
		snapshot.Source,
		sqlmock.AnyArg(),
	).WillReturnResult(sqlmock.NewResult(0, 1)) // 0 rows inserted, 1 row updated

	// Execute
	err := repo.CreateOrUpdate(ctx, snapshot)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_GetByUserIDAndDateRange tests fetching snapshots for a user within date range
func TestBalanceSnapshotRepository_GetByUserIDAndDateRange(t *testing.T) {
	db, mock, repo := setupSnapshotMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	startDate := time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 11, 15, 0, 0, 0, 0, time.UTC)

	// Mock data
	balance1 := 1000.00
	balance2 := 1100.00
	rows := sqlmock.NewRows([]string{
		"id", "account_id", "user_id", "current_balance", "available_balance",
		"currency_code", "snapshot_date", "source", "created_at",
	}).
		AddRow(
			uuid.New().String(), accountID, userID, 1000.00, 950.00,
			"USD", time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC), "plaid_webhook", time.Now(),
		).
		AddRow(
			uuid.New().String(), accountID, userID, 1100.00, 1050.00,
			"USD", time.Date(2024, 11, 15, 0, 0, 0, 0, time.UTC), "plaid_webhook", time.Now(),
		)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, account_id, user_id, current_balance, available_balance,
		       currency_code, snapshot_date, source, created_at
		FROM balance_snapshots
		WHERE user_id = $1
		  AND snapshot_date >= $2
		  AND snapshot_date <= $3
		ORDER BY snapshot_date ASC
	`)).WithArgs(userID, startDate, endDate).WillReturnRows(rows)

	// Execute
	snapshots, err := repo.GetByUserIDAndDateRange(ctx, userID, startDate, endDate)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, snapshots, 2)
	assert.Equal(t, balance1, *snapshots[0].CurrentBalance)
	assert.Equal(t, balance2, *snapshots[1].CurrentBalance)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_GetByUserIDAndDateRange_NoResults tests query with no matching snapshots
func TestBalanceSnapshotRepository_GetByUserIDAndDateRange_NoResults(t *testing.T) {
	db, mock, repo := setupSnapshotMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	// Mock empty result
	rows := sqlmock.NewRows([]string{
		"id", "account_id", "user_id", "current_balance", "available_balance",
		"currency_code", "snapshot_date", "source", "created_at",
	})

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, account_id, user_id, current_balance, available_balance,
		       currency_code, snapshot_date, source, created_at
		FROM balance_snapshots
		WHERE user_id = $1
		  AND snapshot_date >= $2
		  AND snapshot_date <= $3
		ORDER BY snapshot_date ASC
	`)).WithArgs(userID, startDate, endDate).WillReturnRows(rows)

	// Execute
	snapshots, err := repo.GetByUserIDAndDateRange(ctx, userID, startDate, endDate)

	// Assert
	assert.NoError(t, err)
	assert.Empty(t, snapshots)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_GetAggregatedHistory tests aggregating balance history by date
func TestBalanceSnapshotRepository_GetAggregatedHistory(t *testing.T) {
	db, mock, repo := setupSnapshotMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	startDate := time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 11, 3, 0, 0, 0, 0, time.UTC)
	accountTypes := []string{"depository", "credit"}

	// Mock aggregated data
	rows := sqlmock.NewRows([]string{"snapshot_date", "total_balance"}).
		AddRow(time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC), 5000.00).
		AddRow(time.Date(2024, 11, 2, 0, 0, 0, 0, time.UTC), 5150.00).
		AddRow(time.Date(2024, 11, 3, 0, 0, 0, 0, time.UTC), 5300.00)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT bs.snapshot_date, SUM(bs.current_balance) as total_balance
		FROM balance_snapshots bs
		JOIN accounts a ON a.id = bs.account_id
		WHERE bs.user_id = $1
		  AND bs.snapshot_date >= $2
		  AND bs.snapshot_date <= $3
		  AND a.type = ANY($4)
		GROUP BY bs.snapshot_date
		ORDER BY bs.snapshot_date ASC
	`)).WithArgs(userID, startDate, endDate, sqlmock.AnyArg()).WillReturnRows(rows)

	// Execute
	history, err := repo.GetAggregatedHistory(ctx, userID, startDate, endDate, accountTypes)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, history, 3)
	assert.Equal(t, 5000.00, history[0].TotalBalance)
	assert.Equal(t, 5150.00, history[1].TotalBalance)
	assert.Equal(t, 5300.00, history[2].TotalBalance)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_GetAggregatedHistory_AllAccountTypes tests aggregation without type filter
func TestBalanceSnapshotRepository_GetAggregatedHistory_AllAccountTypes(t *testing.T) {
	db, mock, repo := setupSnapshotMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	startDate := time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 11, 3, 0, 0, 0, 0, time.UTC)

	// Mock aggregated data (all account types)
	rows := sqlmock.NewRows([]string{"snapshot_date", "total_balance"}).
		AddRow(time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC), 10000.00).
		AddRow(time.Date(2024, 11, 2, 0, 0, 0, 0, time.UTC), 10200.00)

	// When accountTypes is nil or empty, query should not include type filter
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT bs.snapshot_date, SUM(bs.current_balance) as total_balance
		FROM balance_snapshots bs
		WHERE bs.user_id = $1
		  AND bs.snapshot_date >= $2
		  AND bs.snapshot_date <= $3
		GROUP BY bs.snapshot_date
		ORDER BY bs.snapshot_date ASC
	`)).WithArgs(userID, startDate, endDate).WillReturnRows(rows)

	// Execute (nil accountTypes means all types)
	history, err := repo.GetAggregatedHistory(ctx, userID, startDate, endDate, nil)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, history, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_GetLatestByUserID tests fetching the most recent snapshot for each account
func TestBalanceSnapshotRepository_GetLatestByUserID(t *testing.T) {
	db, mock, repo := setupSnapshotMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	account1 := uuid.New().String()
	account2 := uuid.New().String()

	// Mock latest snapshots (one per account)
	rows := sqlmock.NewRows([]string{
		"id", "account_id", "user_id", "current_balance", "available_balance",
		"currency_code", "snapshot_date", "source", "created_at",
	}).
		AddRow(
			uuid.New().String(), account1, userID, 1500.00, 1400.00,
			"USD", time.Date(2024, 11, 15, 0, 0, 0, 0, time.UTC), "plaid_webhook", time.Now(),
		).
		AddRow(
			uuid.New().String(), account2, userID, 2500.00, 2400.00,
			"USD", time.Date(2024, 11, 15, 0, 0, 0, 0, time.UTC), "plaid_webhook", time.Now(),
		)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT DISTINCT ON (account_id)
		       id, account_id, user_id, current_balance, available_balance,
		       currency_code, snapshot_date, source, created_at
		FROM balance_snapshots
		WHERE user_id = $1
		ORDER BY account_id, snapshot_date DESC
	`)).WithArgs(userID).WillReturnRows(rows)

	// Execute
	snapshots, err := repo.GetLatestByUserID(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, snapshots, 2)
	assert.Equal(t, account1, snapshots[0].AccountID)
	assert.Equal(t, account2, snapshots[1].AccountID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_DeleteByAccountID tests deleting snapshots for an account
func TestBalanceSnapshotRepository_DeleteByAccountID(t *testing.T) {
	db, mock, repo := setupSnapshotMockDB(t)
	defer db.Close()

	ctx := context.Background()
	accountID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`
		DELETE FROM balance_snapshots WHERE account_id = $1
	`)).WithArgs(accountID).WillReturnResult(sqlmock.NewResult(0, 5)) // 5 rows deleted

	// Execute
	err := repo.DeleteByAccountID(ctx, accountID)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
