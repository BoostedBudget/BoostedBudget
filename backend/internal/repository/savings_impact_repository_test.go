package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupSavingsImpactMockDB creates a mock DB and repo for savings impact testing.
func setupSavingsImpactMockDB(t *testing.T) (pgxmock.PgxPoolIface, SavingsImpactRepository) {
	t.Helper()
	mockDB, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := NewSavingsImpactRepository(mockDB)
	return mockDB, repo
}

// TestSavingsImpactRepository_WriteSnapshot verifies that WriteSnapshot executes
// the idempotent INSERT ... ON CONFLICT DO UPDATE statement.
func TestSavingsImpactRepository_WriteSnapshot(t *testing.T) {
	mockDB, repo := setupSavingsImpactMockDB(t)
	defer mockDB.Close()

	ctx := context.Background()
	snapshot := &domain.SavingsImpactSnapshot{
		ID:             "test-id-1",
		ComputedAt:     time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
		TotalAmount:    1500.75,
		UserCount:      42,
		CompletedCount: 87,
	}

	mockDB.ExpectExec(regexp.QuoteMeta(`INSERT INTO savings_impact_snapshots`)).
		WithArgs(
			pgxmock.AnyArg(), // id (UUID generated in repo)
			snapshot.ComputedAt,
			snapshot.TotalAmount,
			snapshot.UserCount,
			snapshot.CompletedCount,
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.WriteSnapshot(ctx, snapshot)

	require.NoError(t, err)
	require.NoError(t, mockDB.ExpectationsWereMet())
}

// TestSavingsImpactRepository_WriteSnapshot_Idempotent verifies that WriteSnapshot
// with a duplicate UTC date (ON CONFLICT DO UPDATE) returns no error.
func TestSavingsImpactRepository_WriteSnapshot_Idempotent(t *testing.T) {
	mockDB, repo := setupSavingsImpactMockDB(t)
	defer mockDB.Close()

	ctx := context.Background()
	snapshot := &domain.SavingsImpactSnapshot{
		ComputedAt:     time.Date(2026, 1, 15, 18, 0, 0, 0, time.UTC), // same day, different time
		TotalAmount:    2000.00,
		UserCount:      45,
		CompletedCount: 90,
	}

	// ON CONFLICT DO UPDATE still returns 1 affected row (UPDATE path)
	mockDB.ExpectExec(regexp.QuoteMeta(`INSERT INTO savings_impact_snapshots`)).
		WithArgs(
			pgxmock.AnyArg(),
			snapshot.ComputedAt,
			snapshot.TotalAmount,
			snapshot.UserCount,
			snapshot.CompletedCount,
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.WriteSnapshot(ctx, snapshot)

	require.NoError(t, err)
	require.NoError(t, mockDB.ExpectationsWereMet())
}

// TestSavingsImpactRepository_WriteSnapshot_DBError verifies that DB errors
// are propagated correctly.
func TestSavingsImpactRepository_WriteSnapshot_DBError(t *testing.T) {
	mockDB, repo := setupSavingsImpactMockDB(t)
	defer mockDB.Close()

	ctx := context.Background()
	snapshot := &domain.SavingsImpactSnapshot{
		ComputedAt:     time.Now(),
		TotalAmount:    100.0,
		UserCount:      1,
		CompletedCount: 1,
	}

	mockDB.ExpectExec(regexp.QuoteMeta(`INSERT INTO savings_impact_snapshots`)).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(errMock("connection timeout"))

	err := repo.WriteSnapshot(ctx, snapshot)

	require.Error(t, err)
}

// TestSavingsImpactRepository_GetLatestSnapshot verifies that GetLatestSnapshot
// returns the most recently computed snapshot.
func TestSavingsImpactRepository_GetLatestSnapshot(t *testing.T) {
	mockDB, repo := setupSavingsImpactMockDB(t)
	defer mockDB.Close()

	ctx := context.Background()
	expectedID := "snapshot-uuid-123"
	expectedTime := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	rows := pgxmock.NewRows([]string{"id", "computed_at", "total_amount", "user_count", "completed_count"}).
		AddRow(expectedID, expectedTime, 1500.75, 42, 87)

	mockDB.ExpectQuery(regexp.QuoteMeta(`SELECT id, computed_at, total_amount, user_count, completed_count`)).
		WillReturnRows(rows)

	snapshot, err := repo.GetLatestSnapshot(ctx)

	require.NoError(t, err)
	require.NotNil(t, snapshot)
	assert.Equal(t, expectedID, snapshot.ID)
	assert.Equal(t, 1500.75, snapshot.TotalAmount)
	assert.Equal(t, 42, snapshot.UserCount)
	assert.Equal(t, 87, snapshot.CompletedCount)
	require.NoError(t, mockDB.ExpectationsWereMet())
}

// TestSavingsImpactRepository_GetLatestSnapshot_Empty verifies that GetLatestSnapshot
// returns nil when no snapshots exist.
func TestSavingsImpactRepository_GetLatestSnapshot_Empty(t *testing.T) {
	mockDB, repo := setupSavingsImpactMockDB(t)
	defer mockDB.Close()

	ctx := context.Background()

	rows := pgxmock.NewRows([]string{"id", "computed_at", "total_amount", "user_count", "completed_count"})
	mockDB.ExpectQuery(regexp.QuoteMeta(`SELECT id, computed_at, total_amount, user_count, completed_count`)).
		WillReturnRows(rows)

	snapshot, err := repo.GetLatestSnapshot(ctx)

	require.NoError(t, err)
	assert.Nil(t, snapshot)
	require.NoError(t, mockDB.ExpectationsWereMet())
}

// TestSavingsImpactRepository_GetUserImpact verifies that GetUserImpact returns
// correct per-user savings totals and recent items.
func TestSavingsImpactRepository_GetUserImpact(t *testing.T) {
	mockDB, repo := setupSavingsImpactMockDB(t)
	defer mockDB.Close()

	ctx := context.Background()
	userID := "user-abc-123"

	// First query: totals (total_amount, completed_count, month_amount, prev_month_amount)
	// The repo passes (userID, monthStart, prevMonthStart) — use AnyArg for the time values.
	totalRows := pgxmock.NewRows([]string{"total_amount", "completed_count", "month_amount", "prev_month_amount"}).
		AddRow(750.50, 5, 200.00, 550.50)
	mockDB.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID, pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(totalRows)

	// Second query: recent items — 10 columns including the three new name-resolution fields
	// (GAP CA): plaid_account_name, ua_nickname, ua_hint_encrypted.
	completedAt := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	source := "automatic"
	toAcctID := "acct-xyz"
	plaidName := "Chase Checking"
	itemRows := pgxmock.NewRows([]string{
		"id", "amount", "actual_amount", "to_account_id", "to_user_account_id",
		"completed_at", "completion_source",
		"plaid_account_name", "ua_nickname", "ua_hint_encrypted",
	}).AddRow("sugg-1", 200.00, 200.00, &toAcctID, nil, &completedAt, &source, &plaidName, nil, nil)
	mockDB.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID).
		WillReturnRows(itemRows)

	impact, err := repo.GetUserImpact(ctx, userID)

	require.NoError(t, err)
	require.NotNil(t, impact)
	assert.Equal(t, 750.50, impact.TotalAmount)
	assert.Equal(t, 5, impact.CompletedCount)
	assert.Equal(t, 200.00, impact.MonthAmount)
	assert.Equal(t, 550.50, impact.PrevMonthAmount)
	require.Len(t, impact.RecentItems, 1)
	assert.Equal(t, "sugg-1", impact.RecentItems[0].ID)
	// GAP CA: ToAccountName resolved from the Plaid account's name column.
	require.NotNil(t, impact.RecentItems[0].ToAccountName)
	assert.Equal(t, "Chase Checking", *impact.RecentItems[0].ToAccountName)
	require.NoError(t, mockDB.ExpectationsWereMet())
}
