package repository

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Tests in this file cover balance snapshot repository functionality including
// re-link chain deduplication (Issue #171: stable user_account_id across Plaid re-links).
// All snapshots include user_account_id for stable reference across account re-links.

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
		(id, account_id, user_account_id, user_id, current_balance, available_balance, currency_code, snapshot_at, source, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`)).WithArgs(
		snapshot.ID,
		snapshot.AccountID,
		snapshot.UserAccountID,
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
	// Issue #171: Includes user_account_id for stable reference across Plaid re-links
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO balance_snapshots
		(id, account_id, user_account_id, user_id, current_balance, available_balance, currency_code, snapshot_at, source, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`)).WithArgs(
		snapshot1.ID, snapshot1.AccountID, snapshot1.UserAccountID, snapshot1.UserID,
		snapshot1.CurrentBalance, snapshot1.AvailableBalance, snapshot1.CurrencyCode,
		snapshot1.SnapshotAt, snapshot1.Source, pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO balance_snapshots
		(id, account_id, user_account_id, user_id, current_balance, available_balance, currency_code, snapshot_at, source, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`)).WithArgs(
		snapshot2.ID, snapshot2.AccountID, snapshot2.UserAccountID, snapshot2.UserID,
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

// TestBalanceSnapshotRepository_GetAggregatedHistory tests aggregating balance history by date
func TestBalanceSnapshotRepository_GetAggregatedHistory(t *testing.T) {
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	startDate := time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 11, 3, 23, 59, 59, 0, time.UTC)
	accountTypes := []string{"depository", "credit"}

	// New SQL emits one row per (snapshot_date, account_type).
	rows := pgxmock.NewRows([]string{"snapshot_date", "account_type", "type_balance"}).
		AddRow(time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC), "depository", 5000.00).
		AddRow(time.Date(2024, 11, 2, 0, 0, 0, 0, time.UTC), "depository", 5150.00).
		AddRow(time.Date(2024, 11, 3, 0, 0, 0, 0, time.UTC), "depository", 5300.00)

	mock.ExpectQuery(regexp.QuoteMeta(fmt.Sprintf(aggregatedHistorySQLBase, "AND a.type = ANY($4)"))).
		WithArgs(userID, startDate, endDate, pgxmock.AnyArg()).WillReturnRows(rows)

	history, err := repo.GetAggregatedHistory(ctx, userID, startDate, endDate, accountTypes)

	assert.NoError(t, err)
	assert.Len(t, history, 3)
	assert.InDelta(t, 5000.00, history[0].TotalBalance, 0.01)
	assert.InDelta(t, 5150.00, history[1].TotalBalance, 0.01)
	assert.InDelta(t, 5300.00, history[2].TotalBalance, 0.01)
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

	// New SQL emits one row per (snapshot_date, account_type).
	rows := pgxmock.NewRows([]string{"snapshot_date", "account_type", "type_balance"}).
		AddRow(time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC), "depository", 10000.00).
		AddRow(time.Date(2024, 11, 2, 0, 0, 0, 0, time.UTC), "depository", 10200.00)

	// When accountTypes is nil or empty, query uses the unfiltered variant (no AND a.type clause).
	mock.ExpectQuery(regexp.QuoteMeta(fmt.Sprintf(aggregatedHistorySQLBase, ""))).
		WithArgs(userID, startDate, endDate).WillReturnRows(rows)

	history, err := repo.GetAggregatedHistory(ctx, userID, startDate, endDate, nil)

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

	// Helper for nullable float64 values
	bal1, avail1 := 1500.00, 1400.00
	bal2, avail2 := 2500.00, 2400.00
	userAccountID1 := uuid.New().String()
	userAccountID2 := uuid.New().String()

	// Mock latest snapshots (one per account)
	rows := pgxmock.NewRows([]string{
		"id", "account_id", "user_account_id", "user_id", "current_balance", "available_balance",
		"currency_code", "snapshot_at", "source", "created_at",
	}).
		AddRow(
			uuid.New().String(), account1, &userAccountID1, userID, &bal1, &avail1,
			"USD", time.Date(2024, 11, 15, 14, 30, 0, 0, time.UTC), "plaid_webhook", time.Now(),
		).
		AddRow(
			uuid.New().String(), account2, &userAccountID2, userID, &bal2, &avail2,
			"USD", time.Date(2024, 11, 15, 14, 30, 0, 0, time.UTC), "plaid_webhook", time.Now(),
		)

	mock.ExpectQuery(regexp.QuoteMeta(personalLatestSnapshotSQL(""))).WithArgs(userID).WillReturnRows(rows)

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

	// Helper for nullable float64 values
	balance, available := 1500.00, 1400.00
	userAccountID := uuid.New().String()

	// Mock latest snapshot for account
	rows := pgxmock.NewRows([]string{
		"id", "account_id", "user_account_id", "user_id", "current_balance", "available_balance",
		"currency_code", "snapshot_at", "source", "created_at",
	}).AddRow(
		uuid.New().String(), accountID, &userAccountID, userID, &balance, &available,
		"USD", snapshotAt, "plaid_webhook", time.Now(),
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, account_id, user_account_id, user_id, current_balance, available_balance,
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
		"id", "account_id", "user_account_id", "user_id", "current_balance", "available_balance",
		"currency_code", "snapshot_at", "source", "created_at",
	})

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, account_id, user_account_id, user_id, current_balance, available_balance,
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
	// Issue #171: Includes user_account_id for stable reference across Plaid re-links
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO balance_snapshots
		(id, account_id, user_account_id, user_id, current_balance, available_balance, currency_code, snapshot_at, source, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`)).WithArgs(
		snapshot.ID,
		snapshot.AccountID,
		snapshot.UserAccountID,
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

// TestBalanceSnapshotRepository_GetAggregatedHistory_MultipleSnapshotsPerDay tests aggregation sums all snapshots per day
func TestBalanceSnapshotRepository_GetAggregatedHistory_MultipleSnapshotsPerDay(t *testing.T) {
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	startDate := time.Date(2024, 11, 15, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 11, 15, 23, 59, 59, 0, time.UTC)

	// The CTE deduplicates by (chain_key, day) before aggregating, so only the latest
	// snapshot per account per day is included in the SUM.
	rows := pgxmock.NewRows([]string{"snapshot_date", "account_type", "type_balance"}).
		AddRow(time.Date(2024, 11, 15, 0, 0, 0, 0, time.UTC), "depository", 3300.00)

	mock.ExpectQuery(regexp.QuoteMeta(fmt.Sprintf(aggregatedHistorySQLBase, ""))).WithArgs(userID, startDate, endDate).WillReturnRows(rows)

	history, err := repo.GetAggregatedHistory(ctx, userID, startDate, endDate, nil)

	assert.NoError(t, err)
	assert.Len(t, history, 1)
	assert.InDelta(t, 3300.00, history[0].TotalBalance, 0.01)
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
	rows := pgxmock.NewRows([]string{"snapshot_date", "account_type", "type_balance"}).
		AddRow(time.Date(2024, 11, 14, 0, 0, 0, 0, time.UTC), "depository", 1000.00).
		AddRow(time.Date(2024, 11, 15, 0, 0, 0, 0, time.UTC), "depository", 2000.00).
		AddRow(time.Date(2024, 11, 16, 0, 0, 0, 0, time.UTC), "depository", 3000.00)

	mock.ExpectQuery(regexp.QuoteMeta(fmt.Sprintf(aggregatedHistorySQLBase, ""))).WithArgs(userID, startDate, endDate).WillReturnRows(rows)

	history, err := repo.GetAggregatedHistory(ctx, userID, startDate, endDate, nil)

	assert.NoError(t, err)
	assert.Len(t, history, 3)
	for _, h := range history {
		assert.Equal(t, time.UTC, h.Date.Location())
	}
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_GetLatestByAccountID_SubSecondPrecision tests that latest uses full timestamp precision

// --- Re-link chain transparency tests (Issue #1109) ---

func TestBalanceSnapshotRepository_GetLatestByUserID_ChainDepth2(t *testing.T) {
	// RED: Test that GetLatestByUserID deduplicates by user_account_id so that
	// a depth-2 chain (old_account → new_account, same user_account_id) returns
	// only 1 snapshot (the latest one belonging to the new account).
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	oldAccountID := uuid.New().String()
	newAccountID := uuid.New().String()
	sharedUserAccountID := uuid.New().String() // Same logical account, different physical accounts

	bal := 2000.00
	avail := 1900.00

	// Only the latest snapshot (new_account) should be returned after DISTINCT ON user_account_id
	rows := pgxmock.NewRows([]string{
		"id", "account_id", "user_account_id", "user_id", "current_balance", "available_balance",
		"currency_code", "snapshot_at", "source", "created_at",
	}).AddRow(
		uuid.New().String(), newAccountID, &sharedUserAccountID, userID, &bal, &avail,
		"USD", time.Date(2024, 11, 20, 14, 30, 0, 0, time.UTC), "plaid_webhook", time.Now(),
	)
	_ = oldAccountID // used to clarify test setup intent

	mock.ExpectQuery(regexp.QuoteMeta(personalLatestSnapshotSQL(""))).WithArgs(userID).WillReturnRows(rows)

	snapshots, err := repo.GetLatestByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Len(t, snapshots, 1, "Chain depth-2 should return only 1 snapshot (deduped by user_account_id)")
	assert.Equal(t, newAccountID, snapshots[0].AccountID, "Should return snapshot from the latest (new) account")
	assert.Equal(t, &sharedUserAccountID, snapshots[0].UserAccountID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBalanceSnapshotRepository_GetAggregatedHistory_NoDoubleCountOnRelinkDay(t *testing.T) {
	// RED: Test that GetAggregatedHistory does not double-count on the re-link day
	// when both old_account and new_account have snapshots on the same date.
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	startDate := time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 11, 30, 23, 59, 59, 0, time.UTC)

	// The CTE-based query returns pre-deduped results:
	// - Nov 10: old_account only → 1000.00
	// - Nov 15 (re-link day): old+new present, only latest counted → 1050.00
	// - Nov 20: new_account only → 1100.00
	rows := pgxmock.NewRows([]string{"snapshot_date", "account_type", "type_balance"}).
		AddRow(time.Date(2024, 11, 10, 0, 0, 0, 0, time.UTC), "depository", 1000.00).
		AddRow(time.Date(2024, 11, 15, 0, 0, 0, 0, time.UTC), "depository", 1050.00).
		AddRow(time.Date(2024, 11, 20, 0, 0, 0, 0, time.UTC), "depository", 1100.00)

	mock.ExpectQuery(regexp.QuoteMeta(fmt.Sprintf(aggregatedHistorySQLBase, ""))).WithArgs(userID, startDate, endDate).WillReturnRows(rows)

	history, err := repo.GetAggregatedHistory(ctx, userID, startDate, endDate, nil)

	assert.NoError(t, err)
	assert.Len(t, history, 3)
	assert.InDelta(t, 1000.00, history[0].TotalBalance, 0.01, "Pre-relink day: old account only")
	assert.InDelta(t, 1050.00, history[1].TotalBalance, 0.01, "Re-link day: only latest snapshot counted, no double-count")
	assert.InDelta(t, 1100.00, history[2].TotalBalance, 0.01, "Post-relink: new account only")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBalanceSnapshotRepository_GetAggregatedHistory_LegacyNullUserAccountID(t *testing.T) {
	// RED: Test that legacy rows where user_account_id IS NULL still work normally.
	// The COALESCE falls back to account_id, so each account deduplicates independently.
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	startDate := time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 11, 30, 23, 59, 59, 0, time.UTC)

	rows := pgxmock.NewRows([]string{"snapshot_date", "account_type", "type_balance"}).
		AddRow(time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC), "depository", 5000.00).
		AddRow(time.Date(2024, 11, 15, 0, 0, 0, 0, time.UTC), "depository", 5200.00)

	mock.ExpectQuery(regexp.QuoteMeta(fmt.Sprintf(aggregatedHistorySQLBase, ""))).WithArgs(userID, startDate, endDate).WillReturnRows(rows)

	history, err := repo.GetAggregatedHistory(ctx, userID, startDate, endDate, nil)

	assert.NoError(t, err)
	assert.Len(t, history, 2, "Legacy null user_account_id accounts should still aggregate correctly")
	assert.InDelta(t, 5000.00, history[0].TotalBalance, 0.01)
	assert.InDelta(t, 5200.00, history[1].TotalBalance, 0.01)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_GetAggregatedHistory_PopulatesByType asserts that the repo
// emits per-account-type rows so the service can derive assets, liabilities, and net worth.
// Regression for Bug A in Issue #1173: ByType was never populated by the old SQL.
func TestBalanceSnapshotRepository_GetAggregatedHistory_PopulatesByType(t *testing.T) {
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	day1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// New SQL emits one row per (snapshot_date, account_type).
	rows := pgxmock.NewRows([]string{"snapshot_date", "account_type", "type_balance"}).
		AddRow(day1, "credit", -2000.00).
		AddRow(day1, "depository", 5000.00).
		AddRow(day1, "investment", 8000.00)

	mock.ExpectQuery(regexp.QuoteMeta(fmt.Sprintf(aggregatedHistorySQLBase, ""))).WithArgs(userID, startDate, endDate).WillReturnRows(rows)

	history, err := repo.GetAggregatedHistory(ctx, userID, startDate, endDate, nil)

	assert.NoError(t, err)
	require.Len(t, history, 1)
	// TotalBalance is the raw sum across all types (including negative liabilities).
	assert.InDelta(t, 11000.00, history[0].TotalBalance, 0.01)
	assert.InDelta(t, 5000.00, history[0].ByType["depository"], 0.01)
	assert.InDelta(t, 8000.00, history[0].ByType["investment"], 0.01)
	assert.InDelta(t, -2000.00, history[0].ByType["credit"], 0.01)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_GetAggregatedHistory_AccountTypesFilter_PopulatesByType verifies
// the filter path also populates ByType.
func TestBalanceSnapshotRepository_GetAggregatedHistory_AccountTypesFilter_PopulatesByType(t *testing.T) {
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	day1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	accountTypes := []string{"depository", "investment"}

	rows := pgxmock.NewRows([]string{"snapshot_date", "account_type", "type_balance"}).
		AddRow(day1, "depository", 5000.00).
		AddRow(day1, "investment", 8000.00)

	mock.ExpectQuery(regexp.QuoteMeta(fmt.Sprintf(aggregatedHistorySQLBase, "AND a.type = ANY($4)"))).WithArgs(userID, startDate, endDate, pgxmock.AnyArg()).WillReturnRows(rows)

	history, err := repo.GetAggregatedHistory(ctx, userID, startDate, endDate, accountTypes)

	assert.NoError(t, err)
	require.Len(t, history, 1)
	assert.InDelta(t, 13000.00, history[0].TotalBalance, 0.01)
	assert.InDelta(t, 5000.00, history[0].ByType["depository"], 0.01)
	assert.InDelta(t, 8000.00, history[0].ByType["investment"], 0.01)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBalanceSnapshotRepository_GetAggregatedHistory_OrphanSnapshot verifies that snapshots
// whose account row was deleted (NULL type from LEFT JOIN) still appear in TotalBalance
// but are excluded from ByType.
func TestBalanceSnapshotRepository_GetAggregatedHistory_OrphanSnapshot(t *testing.T) {
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	day1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// COALESCE(NULL, '') → "" for orphaned snapshot.
	rows := pgxmock.NewRows([]string{"snapshot_date", "account_type", "type_balance"}).
		AddRow(day1, "", 3000.00)

	mock.ExpectQuery(regexp.QuoteMeta(fmt.Sprintf(aggregatedHistorySQLBase, ""))).WithArgs(userID, startDate, endDate).WillReturnRows(rows)

	history, err := repo.GetAggregatedHistory(ctx, userID, startDate, endDate, nil)

	assert.NoError(t, err)
	require.Len(t, history, 1)
	assert.InDelta(t, 3000.00, history[0].TotalBalance, 0.01, "Orphan balance must appear in TotalBalance")
	assert.Empty(t, history[0].ByType, "Orphan snapshot must be excluded from ByType")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBalanceSnapshotRepository_GetLatestByAccountID_SubSecondPrecision(t *testing.T) {
	mock, repo := setupSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	// Latest snapshot with microsecond precision
	snapshotAt := time.Date(2024, 11, 15, 14, 30, 45, 999999000, time.UTC)

	// Helper for nullable float64 values
	balance, available := 1500.00, 1400.00
	userAccountID := uuid.New().String()

	rows := pgxmock.NewRows([]string{
		"id", "account_id", "user_account_id", "user_id", "current_balance", "available_balance",
		"currency_code", "snapshot_at", "source", "created_at",
	}).AddRow(
		uuid.New().String(), accountID, &userAccountID, userID, &balance, &available,
		"USD", snapshotAt, "plaid_webhook", time.Now(),
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, account_id, user_account_id, user_id, current_balance, available_balance,
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

// ── #1569 T10-FIX item 7: what the pgxmock expectations above can and cannot catch ─────────────
//
// Every GetAggregatedHistory test in this file builds its ExpectQuery from `aggregatedHistorySQLBase`
// — the SAME package-level template production renders. That is deliberate (a hand-copied 90-line
// statement is its own maintenance trap) and it has a consequence that must be stated where the
// tests are, not inferred: a regression in the template updates production and the expectation
// TOGETHER, and every one of those tests stays green. They pin the Go-side plumbing — the argument
// list, the row scan, the grouping into BalanceHistoryPoint, the type-filter branch choosing the
// right rendering — and nothing whatsoever about whether the SQL is correct.
//
// The real protection for the SQL is the integration suite, against a real table:
// TestMasklessPair_PersonalBalanceReads_CountBothAccounts,
// TestGetAggregatedHistoryForHousehold_RelinkChain_ASnapshotKeepsItsOwnAccountsType and the
// portfolio chain-topology family. If you change this statement, that is where you find out.
//
// TestAggregatedHistorySQLBase_PinsItsLoadBearingFragments is the cheap half of the gap: the four
// decisions in the template that a silent edit could reverse are asserted here as literal
// fragments, so at least those cannot move in lockstep with an expectation.
func TestAggregatedHistorySQLBase_PinsItsLoadBearingFragments(t *testing.T) {
	sql := aggregatedHistorySQLBase

	// 1. The grain is the PHYSICAL ACCOUNT. Collapsing to the chain key deletes one of two maskless
	//    siblings from every chart point (#1569 T10.2).
	assert.Contains(t, sql, "DISTINCT ON ((bs.snapshot_at AT TIME ZONE 'UTC')::date, "+snapshotIdentityKey+")",
		"the per-day dedup must be per physical account")
	assert.Equal(t, "COALESCE(bs.account_id::text, bs.user_account_id::text, '')", snapshotIdentityKey,
		"all three COALESCE arms are load-bearing — see snapshotIdentityKey. A bare bs.account_id "+
			"puts every orphaned snapshot in ONE DISTINCT ON bucket and deletes all but one")

	// 2. The type comes from the snapshot's OWN account, not from any chain member.
	assert.Contains(t, sql, "LEFT JOIN accounts a ON a.id = bs.account_id",
		"a chain-wide join needs a preference to pick a type, and that preference has been wrong "+
			"in both directions once each")
	assert.NotContains(t, sql, "a.user_account_id = bs.user_account_id",
		"the chain-wide join must not come back")

	// 3. Succession is applied, and by the shared rule rather than a second copy of it.
	assert.Contains(t, sql, accountChainSupersessionCTESQL("TRUE"),
		"there is ONE succession rule in the backend; a second copy is how the balance side and "+
			"the portfolio side came to disagree about the same two rows")
	assert.Contains(t, sql, "SELECT 1 FROM superseded x",
		"the superseded rows must actually be filtered out, not merely computed")

	// 4. The optional predicate is the ONLY hole, so the type filter cannot be rendered anywhere else.
	assert.Equal(t, 1, strings.Count(sql, "%s"), "exactly one format hole")
}
