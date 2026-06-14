package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupContributionMockDB creates a mock database for contribution repository testing.
func setupContributionMockDB(t *testing.T) (pgxmock.PgxPoolIface, SavingsContributionRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := NewSavingsContributionRepository(mock)
	return mock, repo
}

// contributionColumns is the canonical column slice mirroring contributionSelectCols.
var contributionColumns = []string{
	"id", "savings_goal_id", "transaction_id", "amount", "source", "status",
	"managed_by_reconciliation", "confirmed_by_user", "occurred_on", "created_at", "updated_at",
}

// buildContribRow returns a row suitable for WillReturnRows with contributionColumns.
func buildContribRow(id, goalID string, txnID *string, amount float64, source, status string) []interface{} {
	now := time.Now()
	return []interface{}{
		id, goalID, txnID, amount, source, status,
		false, false, "2024-01-15", now, now,
	}
}

// TestSavingsContributionRepository_Insert_Basic tests a simple contribution insert.
func TestSavingsContributionRepository_Insert_Basic(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()
	contrib := &domain.SavingsGoalContribution{
		SavingsGoalID: goalID,
		Amount:        500.00,
		Source:        "manual",
		Status:        "confirmed",
		OccurredOn:    "2024-01-15",
	}

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO savings_goal_contributions`)).
		WithArgs(
			pgxmock.AnyArg(), // id
			goalID,
			contrib.TransactionID, // nil
			contrib.Amount,
			contrib.Source,
			contrib.Status,
			contrib.ManagedByReconciliation,
			contrib.ConfirmedByUser,
			contrib.OccurredOn,
			pgxmock.AnyArg(), // created_at
			pgxmock.AnyArg(), // updated_at
		).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Insert(ctx, contrib)

	assert.NoError(t, err)
	assert.NotEmpty(t, contrib.ID, "ID should be generated")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_Insert_IDPreserved tests that a pre-set ID is not overwritten.
func TestSavingsContributionRepository_Insert_IDPreserved(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	existingID := uuid.New().String()
	contrib := &domain.SavingsGoalContribution{
		ID:            existingID,
		SavingsGoalID: uuid.New().String(),
		Amount:        100.00,
		Source:        "manual",
		Status:        "confirmed",
		OccurredOn:    "2024-02-01",
	}

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO savings_goal_contributions`)).
		WithArgs(
			existingID, // pre-set ID must be preserved
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
		).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Insert(ctx, contrib)
	require.NoError(t, err)
	assert.Equal(t, existingID, contrib.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_Insert_DefaultStatusConfirmed tests that missing status defaults to "confirmed".
func TestSavingsContributionRepository_Insert_DefaultStatusConfirmed(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	contrib := &domain.SavingsGoalContribution{
		SavingsGoalID: uuid.New().String(),
		Amount:        250.00,
		Source:        "manual",
		// Status intentionally left empty → should default to "confirmed"
		OccurredOn: "2024-03-01",
	}

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO savings_goal_contributions`)).
		WithArgs(
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			"confirmed", // default status
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
		).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Insert(ctx, contrib)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", contrib.Status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_UpsertFromTransaction_Success tests the happy path
// including the transaction-level lock and capacity check (Codex C2).
func TestSavingsContributionRepository_UpsertFromTransaction_Success(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()
	txnID := uuid.New().String()
	txnAmount := 1000.00
	contribAmount := 400.00

	mock.ExpectBegin()

	// Step 1: lock query returns transaction magnitude.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ABS(amount) FROM transactions WHERE id = $1 FOR UPDATE`)).
		WithArgs(txnID).
		WillReturnRows(pgxmock.NewRows([]string{"abs"}).AddRow(txnAmount))

	// Step 2: capacity query — nothing allocated elsewhere yet.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(amount), 0)`)).
		WithArgs(txnID, goalID).
		WillReturnRows(pgxmock.NewRows([]string{"coalesce"}).AddRow(0.0))

	// Step 3: upsert.
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO savings_goal_contributions`)).
		WithArgs(goalID, txnID, contribAmount, "confirmed_transfer", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	mock.ExpectCommit()

	err := repo.UpsertFromTransaction(ctx, goalID, txnID, contribAmount, "confirmed_transfer")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_UpsertFromTransaction_TransactionNotFound tests
// that ErrContributionNotFound is returned when the transaction row does not exist.
func TestSavingsContributionRepository_UpsertFromTransaction_TransactionNotFound(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()
	txnID := uuid.New().String()

	mock.ExpectBegin()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ABS(amount) FROM transactions WHERE id = $1 FOR UPDATE`)).
		WithArgs(txnID).
		WillReturnError(pgx.ErrNoRows)

	mock.ExpectRollback()

	err := repo.UpsertFromTransaction(ctx, goalID, txnID, 200.00, "confirmed_transfer")
	assert.ErrorIs(t, err, domain.ErrContributionNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_UpsertFromTransaction_OverAllocated tests that
// ErrContributionOverAllocated is returned when the allocation would exceed the txn amount
// (Codex C2 — race-safe allocation cap).
func TestSavingsContributionRepository_UpsertFromTransaction_OverAllocated(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()
	txnID := uuid.New().String()

	mock.ExpectBegin()

	// Transaction magnitude is 500.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ABS(amount) FROM transactions WHERE id = $1 FOR UPDATE`)).
		WithArgs(txnID).
		WillReturnRows(pgxmock.NewRows([]string{"abs"}).AddRow(500.0))

	// Already 450 allocated to other goals.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(amount), 0)`)).
		WithArgs(txnID, goalID).
		WillReturnRows(pgxmock.NewRows([]string{"coalesce"}).AddRow(450.0))

	mock.ExpectRollback()

	// Attempt to allocate 100 — only 50 remaining capacity.
	err := repo.UpsertFromTransaction(ctx, goalID, txnID, 100.00, "confirmed_transfer")
	assert.ErrorIs(t, err, domain.ErrContributionOverAllocated)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_UpsertFromTransaction_ExactCapacity tests that
// allocating exactly the remaining capacity is allowed (boundary condition).
func TestSavingsContributionRepository_UpsertFromTransaction_ExactCapacity(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()
	txnID := uuid.New().String()

	mock.ExpectBegin()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ABS(amount) FROM transactions WHERE id = $1 FOR UPDATE`)).
		WithArgs(txnID).
		WillReturnRows(pgxmock.NewRows([]string{"abs"}).AddRow(500.0))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(amount), 0)`)).
		WithArgs(txnID, goalID).
		WillReturnRows(pgxmock.NewRows([]string{"coalesce"}).AddRow(300.0))

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO savings_goal_contributions`)).
		WithArgs(goalID, txnID, 200.0, "confirmed_transfer", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	mock.ExpectCommit()

	// Exactly 200 remaining — should succeed.
	err := repo.UpsertFromTransaction(ctx, goalID, txnID, 200.0, "confirmed_transfer")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_SumConfirmedByGoal tests contribution sum aggregation.
func TestSavingsContributionRepository_SumConfirmedByGoal(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(amount), 0)`)).
		WithArgs(goalID).
		WillReturnRows(pgxmock.NewRows([]string{"coalesce"}).AddRow(1250.50))

	total, err := repo.SumConfirmedByGoal(ctx, goalID)

	require.NoError(t, err)
	assert.InDelta(t, 1250.50, total, 0.001)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_SumConfirmedByGoal_NoContributions tests that
// a goal with no contributions returns 0 (not an error).
func TestSavingsContributionRepository_SumConfirmedByGoal_NoContributions(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(amount), 0)`)).
		WithArgs(goalID).
		WillReturnRows(pgxmock.NewRows([]string{"coalesce"}).AddRow(0.0))

	total, err := repo.SumConfirmedByGoal(ctx, goalID)

	require.NoError(t, err)
	assert.Equal(t, 0.0, total)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_SumConfirmedByGoalInPeriod tests period-filtered sum.
func TestSavingsContributionRepository_SumConfirmedByGoalInPeriod(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(amount), 0)`)).
		WithArgs(goalID, start, end).
		WillReturnRows(pgxmock.NewRows([]string{"coalesce"}).AddRow(750.00))

	total, err := repo.SumConfirmedByGoalInPeriod(ctx, goalID, start, end)

	require.NoError(t, err)
	assert.InDelta(t, 750.00, total, 0.001)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_ListByGoal tests listing contributions.
func TestSavingsContributionRepository_ListByGoal(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()
	txnID1 := uuid.New().String()
	now := time.Now()

	rows := pgxmock.NewRows(contributionColumns).
		AddRow(
			uuid.New().String(), goalID, &txnID1, 500.00, "confirmed_transfer", "confirmed",
			true, false, "2024-01-15", now, now,
		).
		AddRow(
			uuid.New().String(), goalID, nil, 200.00, "manual", "confirmed",
			false, true, "2024-01-20", now, now,
		)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(goalID).
		WillReturnRows(rows)

	contribs, err := repo.ListByGoal(ctx, goalID)

	require.NoError(t, err)
	require.Len(t, contribs, 2)
	assert.Equal(t, "confirmed_transfer", contribs[0].Source)
	assert.True(t, contribs[0].ManagedByReconciliation)
	assert.Equal(t, "manual", contribs[1].Source)
	assert.False(t, contribs[1].ManagedByReconciliation)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_ListByGoal_Empty tests that an empty result returns an empty slice.
func TestSavingsContributionRepository_ListByGoal_Empty(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(goalID).
		WillReturnRows(pgxmock.NewRows(contributionColumns))

	contribs, err := repo.ListByGoal(ctx, goalID)

	require.NoError(t, err)
	assert.Empty(t, contribs)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_DeleteManagedForTransaction tests that only
// managed_by_reconciliation rows are deleted (Codex C4 — user rows survive).
func TestSavingsContributionRepository_DeleteManagedForTransaction(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	txnID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM savings_goal_contributions`)).
		WithArgs(txnID).
		WillReturnResult(pgxmock.NewResult("DELETE", 2))

	err := repo.DeleteManagedForTransaction(ctx, txnID)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_DeleteManagedForTransaction_NoRows tests that
// deleting for a transaction with no managed rows is not an error.
func TestSavingsContributionRepository_DeleteManagedForTransaction_NoRows(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	txnID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM savings_goal_contributions`)).
		WithArgs(txnID).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))

	err := repo.DeleteManagedForTransaction(ctx, txnID)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_ArchiveByGoal tests basis-switch archiving (Codex C10).
// Only managed_by_reconciliation rows are flipped; user-entered rows are unchanged.
func TestSavingsContributionRepository_ArchiveByGoal(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE savings_goal_contributions`)).
		WithArgs(goalID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 3))

	err := repo.ArchiveByGoal(ctx, goalID)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_ArchiveByGoal_NoManagedRows tests that archiving
// a goal with no managed rows is not an error.
func TestSavingsContributionRepository_ArchiveByGoal_NoManagedRows(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE savings_goal_contributions`)).
		WithArgs(goalID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.ArchiveByGoal(ctx, goalID)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
