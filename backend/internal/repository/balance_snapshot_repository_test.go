package repository

import (
	"context"
	
	"regexp"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupSnapshotMockDB creates a mock database and sqlmock instance for balance snapshot repository testing
func setupSnapshotMockDB(t *testing.T) (pgxmock.PgxPoolIface, BalanceSnapshotRepository) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)

	repo := NewBalanceSnapshotRepository(mock)
	return mock, repo
}

// TestBalanceSnapshotRepository_Create_NewSnapshot tests inserting a new snapshot
func TestBalanceSnapshotRepository_Create_NewSnapshot(t *testing.T) {
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	snapshotAt := time.Date(2024, 11, 15, 10, 30, 45, 0, time.UTC)
	balance := 1500.50
	available := 1450.25

	snapshot := &domain.BalanceSnapshot{
		ID:               uuid.New().String(),
		AccountID:        uuid.New().String(),
		UserID:           uuid.New().String(),
		CurrentBalance:   &balance,
		AvailableBalance: &available,
		CurrencyCode:     "USD",
		SnapshotAt:       snapshotAt,
		Source:           domain.SnapshotSourcePlaidWebhook,
		CreatedAt:        now,
	}

	// Expect simple INSERT query (no ON CONFLICT - multiple snapshots per day allowed)
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO balance_snapshots
		(id, account_id, user_id, current_balance, available_balance, currency_code, snapshot_at, source, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`)).WithArgs(
		snapshot.ID,
		snapshot.AccountID,
		snapshot.UserID,
		snapshot.CurrentBalance,
		snapshot.AvailableBalance,
		snapshot.CurrencyCode,
		snapshot.SnapshotAt,
		snapshot.Source,
		pgxmock.AnyArg(), // created_at
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Execute
	err := repo.Create(ctx, snapshot)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_Create_MultiplePerDay tests creating multiple snapshots for same account same day
func TestBalanceSnapshotRepository_Create_MultiplePerDay(t *testing.T) {
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	// Create two snapshots for same day, different times (high-frequency sync scenario)
	balance1 := 1000.00
	balance2 := 1050.00
	snapshot1 := &domain.BalanceSnapshot{
		ID:             uuid.New().String(),
		AccountID:      accountID,
		UserID:         userID,
		CurrentBalance: &balance1,
		CurrencyCode:   "USD",
		SnapshotAt:     time.Date(2024, 11, 15, 10, 0, 0, 0, time.UTC),
		Source:         domain.SnapshotSourcePlaidWebhook,
	}

	snapshot2 := &domain.BalanceSnapshot{
		ID:             uuid.New().String(),
		AccountID:      accountID,
		UserID:         userID,
		CurrentBalance: &balance2,
		CurrencyCode:   "USD",
		SnapshotAt:     time.Date(2024, 11, 15, 14, 0, 0, 0, time.UTC), // Same day, 4 hours later
		Source:         domain.SnapshotSourcePlaidWebhook,
	}

	// Expect two separate INSERT queries (no conflict handling)
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO balance_snapshots
		(id, account_id, user_id, current_balance, available_balance, currency_code, snapshot_at, source, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`)).WithArgs(
		snapshot1.ID, snapshot1.AccountID, snapshot1.UserID,
		snapshot1.CurrentBalance, snapshot1.AvailableBalance, snapshot1.CurrencyCode,
		snapshot1.SnapshotAt, snapshot1.Source, pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO balance_snapshots
		(id, account_id, user_id, current_balance, available_balance, currency_code, snapshot_at, source, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`)).WithArgs(
		snapshot2.ID, snapshot2.AccountID, snapshot2.UserID,
		snapshot2.CurrentBalance, snapshot2.AvailableBalance, snapshot2.CurrencyCode,
		snapshot2.SnapshotAt, snapshot2.Source, pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Execute both inserts
	err := repo.Create(ctx, snapshot1)
	assert.NoError(t, err)

	err = repo.Create(ctx, snapshot2)
	assert.NoError(t, err)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_GetByUserIDAndDateRange tests fetching snapshots for a user within date range
func TestBalanceSnapshotRepository_GetByUserIDAndDateRange(t *testing.T) {
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	startDate := time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 11, 15, 23, 59, 59, 0, time.UTC)

	// Mock data
	balance1 := 1000.00
	balance2 := 1100.00
	rows := pgxmock.NewRows([]string{
		"id", "account_id", "user_id", "current_balance", "available_balance",
		"currency_code", "snapshot_at", "source", "created_at",
	}).
		AddRow(
			uuid.New().String(), accountID, userID, 1000.00, 950.00,
			"USD", time.Date(2024, 11, 1, 10, 30, 0, 0, time.UTC), "plaid_webhook", time.Now(),
		).
		AddRow(
			uuid.New().String(), accountID, userID, 1100.00, 1050.00,
			"USD", time.Date(2024, 11, 15, 14, 0, 0, 0, time.UTC), "plaid_webhook", time.Now(),
		)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, account_id, user_id, current_balance, available_balance,
		       currency_code, snapshot_at, source, created_at
		FROM balance_snapshots
		WHERE user_id = $1
		  AND snapshot_at >= $2
		  AND snapshot_at <= $3
		ORDER BY snapshot_at ASC
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
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	// Mock empty result
	rows := pgxmock.NewRows([]string{
		"id", "account_id", "user_id", "current_balance", "available_balance",
		"currency_code", "snapshot_at", "source", "created_at",
	})

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, account_id, user_id, current_balance, available_balance,
		       currency_code, snapshot_at, source, created_at
		FROM balance_snapshots
		WHERE user_id = $1
		  AND snapshot_at >= $2
		  AND snapshot_at <= $3
		ORDER BY snapshot_at ASC
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
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	startDate := time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 11, 3, 23, 59, 59, 0, time.UTC)
	accountTypes := []string{"depository", "credit"}

	// Mock aggregated data (grouped by DATE(snapshot_at))
	rows := pgxmock.NewRows([]string{"snapshot_date", "total_balance"}).
		AddRow(time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC), 5000.00).
		AddRow(time.Date(2024, 11, 2, 0, 0, 0, 0, time.UTC), 5150.00).
		AddRow(time.Date(2024, 11, 3, 0, 0, 0, 0, time.UTC), 5300.00)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT (bs.snapshot_at AT TIME ZONE 'UTC')::date as snapshot_date, SUM(bs.current_balance) as total_balance
		FROM balance_snapshots bs
		JOIN accounts a ON a.id = bs.account_id
		WHERE bs.user_id = $1
		  AND bs.snapshot_at >= $2
		  AND bs.snapshot_at <= $3
		  AND a.type = ANY($4)
		GROUP BY (bs.snapshot_at AT TIME ZONE 'UTC')::date
		ORDER BY (bs.snapshot_at AT TIME ZONE 'UTC')::date ASC
	`)).WithArgs(userID, startDate, endDate, pgxmock.AnyArg()).WillReturnRows(rows)

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
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	startDate := time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 11, 3, 23, 59, 59, 0, time.UTC)

	// Mock aggregated data (all account types, grouped by DATE(snapshot_at))
	rows := pgxmock.NewRows([]string{"snapshot_date", "total_balance"}).
		AddRow(time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC), 10000.00).
		AddRow(time.Date(2024, 11, 2, 0, 0, 0, 0, time.UTC), 10200.00)

	// When accountTypes is nil or empty, query should not include type filter
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT (bs.snapshot_at AT TIME ZONE 'UTC')::date as snapshot_date, SUM(bs.current_balance) as total_balance
		FROM balance_snapshots bs
		WHERE bs.user_id = $1
		  AND bs.snapshot_at >= $2
		  AND bs.snapshot_at <= $3
		GROUP BY (bs.snapshot_at AT TIME ZONE 'UTC')::date
		ORDER BY (bs.snapshot_at AT TIME ZONE 'UTC')::date ASC
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
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	account1 := uuid.New().String()
	account2 := uuid.New().String()

	// Mock latest snapshots (one per account)
	rows := pgxmock.NewRows([]string{
		"id", "account_id", "user_id", "current_balance", "available_balance",
		"currency_code", "snapshot_at", "source", "created_at",
	}).
		AddRow(
			uuid.New().String(), account1, userID, 1500.00, 1400.00,
			"USD", time.Date(2024, 11, 15, 14, 30, 0, 0, time.UTC), "plaid_webhook", time.Now(),
		).
		AddRow(
			uuid.New().String(), account2, userID, 2500.00, 2400.00,
			"USD", time.Date(2024, 11, 15, 14, 30, 0, 0, time.UTC), "plaid_webhook", time.Now(),
		)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT DISTINCT ON (account_id)
		       id, account_id, user_id, current_balance, available_balance,
		       currency_code, snapshot_at, source, created_at
		FROM balance_snapshots
		WHERE user_id = $1
		ORDER BY account_id, snapshot_at DESC
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

// TestBalanceSnapshotRepository_GetLatestByAccountID tests fetching the most recent snapshot for a specific account
func TestBalanceSnapshotRepository_GetLatestByAccountID(t *testing.T) {
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()
	snapshotAt := time.Date(2024, 11, 15, 14, 30, 45, 0, time.UTC)

	// Mock latest snapshot for account
	rows := pgxmock.NewRows([]string{
		"id", "account_id", "user_id", "current_balance", "available_balance",
		"currency_code", "snapshot_at", "source", "created_at",
	}).AddRow(
		uuid.New().String(), accountID, userID, 1500.00, 1400.00,
		"USD", snapshotAt, "plaid_webhook", time.Now(),
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, account_id, user_id, current_balance, available_balance,
		       currency_code, snapshot_at, source, created_at
		FROM balance_snapshots
		WHERE account_id = $1
		ORDER BY snapshot_at DESC
		LIMIT 1
	`)).WithArgs(accountID).WillReturnRows(rows)

	// Execute
	snapshot, err := repo.GetLatestByAccountID(ctx, accountID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.Equal(t, accountID, snapshot.AccountID)
	assert.Equal(t, snapshotAt, snapshot.SnapshotAt)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_GetLatestByAccountID_NoSnapshot tests when no snapshot exists for account
func TestBalanceSnapshotRepository_GetLatestByAccountID_NoSnapshot(t *testing.T) {
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()

	// Mock empty result
	rows := pgxmock.NewRows([]string{
		"id", "account_id", "user_id", "current_balance", "available_balance",
		"currency_code", "snapshot_at", "source", "created_at",
	})

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, account_id, user_id, current_balance, available_balance,
		       currency_code, snapshot_at, source, created_at
		FROM balance_snapshots
		WHERE account_id = $1
		ORDER BY snapshot_at DESC
		LIMIT 1
	`)).WithArgs(accountID).WillReturnRows(rows)

	// Execute
	snapshot, err := repo.GetLatestByAccountID(ctx, accountID)

	// Assert - should return nil, nil (no error, no snapshot)
	assert.NoError(t, err)
	assert.Nil(t, snapshot)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_DeleteByAccountID tests deleting snapshots for an account
func TestBalanceSnapshotRepository_DeleteByAccountID(t *testing.T) {
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`
		DELETE FROM balance_snapshots WHERE account_id = $1
	`)).WithArgs(accountID).WillReturnResult(pgxmock.NewResult("UPDATE", 5)) // 5 rows deleted

	// Execute
	err := repo.DeleteByAccountID(ctx, accountID)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_Create_PreservesMicroseconds tests that microsecond precision is preserved
func TestBalanceSnapshotRepository_Create_PreservesMicroseconds(t *testing.T) {
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	// Create timestamp with microsecond precision
	snapshotAt := time.Date(2024, 11, 15, 10, 30, 45, 123456000, time.UTC) // 123456 microseconds
	balance := 1500.50

	snapshot := &domain.BalanceSnapshot{
		ID:             uuid.New().String(),
		AccountID:      uuid.New().String(),
		UserID:         uuid.New().String(),
		CurrentBalance: &balance,
		CurrencyCode:   "USD",
		SnapshotAt:     snapshotAt,
		Source:         domain.SnapshotSourcePlaidWebhook,
	}

	// Expect INSERT with exact timestamp including microseconds
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO balance_snapshots
		(id, account_id, user_id, current_balance, available_balance, currency_code, snapshot_at, source, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`)).WithArgs(
		snapshot.ID,
		snapshot.AccountID,
		snapshot.UserID,
		snapshot.CurrentBalance,
		snapshot.AvailableBalance,
		snapshot.CurrencyCode,
		snapshotAt, // Microseconds should be preserved
		snapshot.Source,
		pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Execute
	err := repo.Create(ctx, snapshot)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, 123456000, snapshot.SnapshotAt.Nanosecond()) // Verify microseconds preserved in struct
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_GetByUserIDAndDateRange_SubSecondOrdering tests ordering of snapshots less than 1 second apart
func TestBalanceSnapshotRepository_GetByUserIDAndDateRange_SubSecondOrdering(t *testing.T) {
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	startDate := time.Date(2024, 11, 15, 10, 30, 45, 0, time.UTC)
	endDate := time.Date(2024, 11, 15, 10, 30, 46, 0, time.UTC)

	// Two snapshots within same second, different microseconds
	time1 := time.Date(2024, 11, 15, 10, 30, 45, 100000000, time.UTC) // .100 seconds
	time2 := time.Date(2024, 11, 15, 10, 30, 45, 900000000, time.UTC) // .900 seconds

	rows := pgxmock.NewRows([]string{
		"id", "account_id", "user_id", "current_balance", "available_balance",
		"currency_code", "snapshot_at", "source", "created_at",
	}).
		AddRow(uuid.New().String(), accountID, userID, 1000.00, nil, "USD", time1, "plaid_webhook", time.Now()).
		AddRow(uuid.New().String(), accountID, userID, 1001.00, nil, "USD", time2, "plaid_webhook", time.Now())

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, account_id, user_id, current_balance, available_balance,
		       currency_code, snapshot_at, source, created_at
		FROM balance_snapshots
		WHERE user_id = $1
		  AND snapshot_at >= $2
		  AND snapshot_at <= $3
		ORDER BY snapshot_at ASC
	`)).WithArgs(userID, startDate, endDate).WillReturnRows(rows)

	// Execute
	snapshots, err := repo.GetByUserIDAndDateRange(ctx, userID, startDate, endDate)

	// Assert - verify ordering is correct even within same second
	assert.NoError(t, err)
	assert.Len(t, snapshots, 2)
	assert.True(t, snapshots[0].SnapshotAt.Before(snapshots[1].SnapshotAt), "Snapshots should be ordered by timestamp ASC")
	assert.Equal(t, 1000.00, *snapshots[0].CurrentBalance)
	assert.Equal(t, 1001.00, *snapshots[1].CurrentBalance)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_GetByUserIDAndDateRange_BoundaryConditions tests inclusive range boundaries
func TestBalanceSnapshotRepository_GetByUserIDAndDateRange_BoundaryConditions(t *testing.T) {
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()

	// Define exact boundaries
	startDate := time.Date(2024, 11, 15, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 11, 15, 23, 59, 59, 999999000, time.UTC)

	// Snapshots at exact boundaries
	atStart := time.Date(2024, 11, 15, 0, 0, 0, 0, time.UTC)
	atEnd := time.Date(2024, 11, 15, 23, 59, 59, 999999000, time.UTC)
	middle := time.Date(2024, 11, 15, 12, 0, 0, 0, time.UTC)

	rows := pgxmock.NewRows([]string{
		"id", "account_id", "user_id", "current_balance", "available_balance",
		"currency_code", "snapshot_at", "source", "created_at",
	}).
		AddRow(uuid.New().String(), accountID, userID, 1000.00, nil, "USD", atStart, "plaid_webhook", time.Now()).
		AddRow(uuid.New().String(), accountID, userID, 1100.00, nil, "USD", middle, "plaid_webhook", time.Now()).
		AddRow(uuid.New().String(), accountID, userID, 1200.00, nil, "USD", atEnd, "plaid_webhook", time.Now())

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, account_id, user_id, current_balance, available_balance,
		       currency_code, snapshot_at, source, created_at
		FROM balance_snapshots
		WHERE user_id = $1
		  AND snapshot_at >= $2
		  AND snapshot_at <= $3
		ORDER BY snapshot_at ASC
	`)).WithArgs(userID, startDate, endDate).WillReturnRows(rows)

	// Execute
	snapshots, err := repo.GetByUserIDAndDateRange(ctx, userID, startDate, endDate)

	// Assert - all three should be included (inclusive boundaries)
	assert.NoError(t, err)
	assert.Len(t, snapshots, 3)
	assert.Equal(t, 1000.00, *snapshots[0].CurrentBalance) // Start boundary
	assert.Equal(t, 1100.00, *snapshots[1].CurrentBalance) // Middle
	assert.Equal(t, 1200.00, *snapshots[2].CurrentBalance) // End boundary
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_GetAggregatedHistory_MultipleSnapshotsPerDay tests aggregation sums all snapshots per day
func TestBalanceSnapshotRepository_GetAggregatedHistory_MultipleSnapshotsPerDay(t *testing.T) {
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	startDate := time.Date(2024, 11, 15, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 11, 15, 23, 59, 59, 0, time.UTC)

	// Mock aggregated data - the repository aggregates by date
	// If there are 3 snapshots with balances 1000, 1100, 1200 for same account on same day,
	// the SUM would be 3300 (this is the current implementation behavior)
	// NOTE: This tests the current behavior; if the intention was to use latest balance per day,
	// the implementation would need to change
	rows := pgxmock.NewRows([]string{"snapshot_date", "total_balance"}).
		AddRow(time.Date(2024, 11, 15, 0, 0, 0, 0, time.UTC), 3300.00) // Sum of 1000+1100+1200

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT (bs.snapshot_at AT TIME ZONE 'UTC')::date as snapshot_date, SUM(bs.current_balance) as total_balance
		FROM balance_snapshots bs
		WHERE bs.user_id = $1
		  AND bs.snapshot_at >= $2
		  AND bs.snapshot_at <= $3
		GROUP BY (bs.snapshot_at AT TIME ZONE 'UTC')::date
		ORDER BY (bs.snapshot_at AT TIME ZONE 'UTC')::date ASC
	`)).WithArgs(userID, startDate, endDate).WillReturnRows(rows)

	// Execute
	history, err := repo.GetAggregatedHistory(ctx, userID, startDate, endDate, nil)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, history, 1)
	assert.Equal(t, 3300.00, history[0].TotalBalance)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_GetAggregatedHistory_TimezoneConsistency tests UTC conversion for aggregation
func TestBalanceSnapshotRepository_GetAggregatedHistory_TimezoneConsistency(t *testing.T) {
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	// Query in UTC
	startDate := time.Date(2024, 11, 14, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 11, 16, 23, 59, 59, 0, time.UTC)

	// Mock aggregated data - the AT TIME ZONE 'UTC' casting ensures
	// all timestamps are normalized to UTC before date extraction
	rows := pgxmock.NewRows([]string{"snapshot_date", "total_balance"}).
		AddRow(time.Date(2024, 11, 14, 0, 0, 0, 0, time.UTC), 1000.00).
		AddRow(time.Date(2024, 11, 15, 0, 0, 0, 0, time.UTC), 2000.00).
		AddRow(time.Date(2024, 11, 16, 0, 0, 0, 0, time.UTC), 3000.00)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT (bs.snapshot_at AT TIME ZONE 'UTC')::date as snapshot_date, SUM(bs.current_balance) as total_balance
		FROM balance_snapshots bs
		WHERE bs.user_id = $1
		  AND bs.snapshot_at >= $2
		  AND bs.snapshot_at <= $3
		GROUP BY (bs.snapshot_at AT TIME ZONE 'UTC')::date
		ORDER BY (bs.snapshot_at AT TIME ZONE 'UTC')::date ASC
	`)).WithArgs(userID, startDate, endDate).WillReturnRows(rows)

	// Execute
	history, err := repo.GetAggregatedHistory(ctx, userID, startDate, endDate, nil)

	// Assert - all dates should be UTC
	assert.NoError(t, err)
	assert.Len(t, history, 3)

	// Verify dates are in UTC
	for _, h := range history {
		assert.Equal(t, time.UTC, h.Date.Location())
	}
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_GetLatestByAccountID_SubSecondPrecision tests that latest uses full timestamp precision
func TestBalanceSnapshotRepository_GetLatestByAccountID_SubSecondPrecision(t *testing.T) {
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	// Latest snapshot with microsecond precision
	snapshotAt := time.Date(2024, 11, 15, 14, 30, 45, 999999000, time.UTC)

	rows := pgxmock.NewRows([]string{
		"id", "account_id", "user_id", "current_balance", "available_balance",
		"currency_code", "snapshot_at", "source", "created_at",
	}).AddRow(
		uuid.New().String(), accountID, userID, 1500.00, 1400.00,
		"USD", snapshotAt, "plaid_webhook", time.Now(),
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, account_id, user_id, current_balance, available_balance,
		       currency_code, snapshot_at, source, created_at
		FROM balance_snapshots
		WHERE account_id = $1
		ORDER BY snapshot_at DESC
		LIMIT 1
	`)).WithArgs(accountID).WillReturnRows(rows)

	// Execute
	snapshot, err := repo.GetLatestByAccountID(ctx, accountID)

	// Assert - microseconds should be preserved
	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.Equal(t, snapshotAt, snapshot.SnapshotAt)
	assert.Equal(t, 999999000, snapshot.SnapshotAt.Nanosecond())
	assert.NoError(t, mock.ExpectationsWereMet())
}
