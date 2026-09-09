package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
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
	// #1625 migration 000190: marks a row as belonging to a CLOSED contributions period.
	"archived_at",
}

// TestSavingsContributionRepository_Insert_Basic tests a simple contribution insert.
func TestSavingsContributionRepository_Insert_Basic(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	goalID := uuid.New().String()
	contrib := &domain.SavingsGoalContribution{
		SavingsGoalID: goalID,
		Amount:        500.00,
		Source:        "manual",
		Status:        "confirmed",
		OccurredOn:    "2024-01-15",
	}

	// #1625: Insert is transactional and its ownership probe became a locked basis assertion —
	// the handler's contributions-basis gate was a TOCTOU on its own.
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
		WithArgs(goalID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow(domain.ProgressBasisContributions))
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
	mock.ExpectCommit()

	err := repo.Insert(ctx, userID, contrib)

	assert.NoError(t, err)
	assert.NotEmpty(t, contrib.ID, "ID should be generated")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_Insert_IDPreserved tests that a pre-set ID is not overwritten.
func TestSavingsContributionRepository_Insert_IDPreserved(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	existingID := uuid.New().String()
	goalID := uuid.New().String()
	contrib := &domain.SavingsGoalContribution{
		ID:            existingID,
		SavingsGoalID: goalID,
		Amount:        100.00,
		Source:        "manual",
		Status:        "confirmed",
		OccurredOn:    "2024-02-01",
	}

	// #1625: Insert is transactional and its ownership probe became a locked basis assertion —
	// the handler's contributions-basis gate was a TOCTOU on its own.
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
		WithArgs(goalID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow(domain.ProgressBasisContributions))
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
	mock.ExpectCommit()

	err := repo.Insert(ctx, userID, contrib)
	require.NoError(t, err)
	assert.Equal(t, existingID, contrib.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_Insert_DefaultStatusConfirmed tests that missing status defaults to "confirmed".
func TestSavingsContributionRepository_Insert_DefaultStatusConfirmed(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	goalID := uuid.New().String()
	contrib := &domain.SavingsGoalContribution{
		SavingsGoalID: goalID,
		Amount:        250.00,
		Source:        "manual",
		// Status intentionally left empty → should default to "confirmed"
		OccurredOn: "2024-03-01",
	}

	// #1625: Insert is transactional and its ownership probe became a locked basis assertion —
	// the handler's contributions-basis gate was a TOCTOU on its own.
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
		WithArgs(goalID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow(domain.ProgressBasisContributions))
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
	mock.ExpectCommit()

	err := repo.Insert(ctx, userID, contrib)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", contrib.Status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_UpsertFromTransaction_Success tests the happy path
// including ownership pre-checks, transaction-level lock, and capacity check (Codex C1/C2).
func TestSavingsContributionRepository_UpsertFromTransaction_Success(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	goalID := uuid.New().String()
	txnID := uuid.New().String()
	txnAmount := 1000.00
	contribAmount := 400.00

	mock.ExpectBegin()

	// Step 0: locked basis assertion — ownership AND a still-live ledger (#1625 Task 22).
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
		WithArgs(goalID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow(domain.ProgressBasisContributions))

	// Step 1: lock query returns transaction magnitude (with user_id check).
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ABS(amount) FROM transactions WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL FOR UPDATE`)).
		WithArgs(txnID, userID).
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

	err := repo.UpsertFromTransaction(ctx, userID, goalID, txnID, contribAmount, "confirmed_transfer")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_UpsertFromTransaction_ConfirmedRowSlotIsNoOp pins the #1569
// round 12 task 3 fix at the SQL level: the ON CONFLICT DO UPDATE now carries a predicate
// (confirmed_by_user = false AND status != 'rejected'), and when that predicate is false — the
// conflict slot is held by a row a human already decided about — the upsert commits as a no-op
// (0 rows affected) rather than erroring or overwriting. Commit still happens; this is an expected,
// non-fatal race, not a failure.
func TestSavingsContributionRepository_UpsertFromTransaction_ConfirmedRowSlotIsNoOp(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	goalID := uuid.New().String()
	txnID := uuid.New().String()
	txnAmount := 1000.00
	contribAmount := 400.00

	mock.ExpectBegin()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
		WithArgs(goalID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow(domain.ProgressBasisContributions))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ABS(amount) FROM transactions WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL FOR UPDATE`)).
		WithArgs(txnID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"abs"}).AddRow(txnAmount))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(amount), 0)`)).
		WithArgs(txnID, goalID).
		WillReturnRows(pgxmock.NewRows([]string{"coalesce"}).AddRow(0.0))

	// The conflict slot is held by a user-confirmed row: neither the INSERT nor the DO UPDATE leg
	// fires, so the driver reports zero rows affected.
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO savings_goal_contributions`)).
		WithArgs(goalID, txnID, contribAmount, "boost_match", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 0))

	mock.ExpectCommit()

	err := repo.UpsertFromTransaction(ctx, userID, goalID, txnID, contribAmount, "boost_match")
	require.NoError(t, err, "a no-op upsert is an expected race, not an error")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_UpsertFromTransaction_TransactionNotFound tests
// that ErrContributionNotFound is returned when the transaction row does not exist for the user.
func TestSavingsContributionRepository_UpsertFromTransaction_TransactionNotFound(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	goalID := uuid.New().String()
	txnID := uuid.New().String()

	mock.ExpectBegin()

	// Step 0: locked basis assertion — ownership AND a still-live ledger (#1625 Task 22).
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
		WithArgs(goalID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow(domain.ProgressBasisContributions))

	// Step 1: lock fails — transaction not found for this user.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ABS(amount) FROM transactions WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL FOR UPDATE`)).
		WithArgs(txnID, userID).
		WillReturnError(pgx.ErrNoRows)

	mock.ExpectRollback()

	err := repo.UpsertFromTransaction(ctx, userID, goalID, txnID, 200.00, "confirmed_transfer")
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
	userID := uuid.New().String()
	goalID := uuid.New().String()
	txnID := uuid.New().String()

	mock.ExpectBegin()

	// Step 0: locked basis assertion — ownership AND a still-live ledger (#1625 Task 22).
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
		WithArgs(goalID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow(domain.ProgressBasisContributions))

	// Step 1: transaction magnitude is 500.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ABS(amount) FROM transactions WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL FOR UPDATE`)).
		WithArgs(txnID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"abs"}).AddRow(500.0))

	// Step 2: already 450 allocated to other goals.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(amount), 0)`)).
		WithArgs(txnID, goalID).
		WillReturnRows(pgxmock.NewRows([]string{"coalesce"}).AddRow(450.0))

	mock.ExpectRollback()

	// Attempt to allocate 100 — only 50 remaining capacity.
	err := repo.UpsertFromTransaction(ctx, userID, goalID, txnID, 100.00, "confirmed_transfer")
	assert.ErrorIs(t, err, domain.ErrContributionOverAllocated)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_UpsertFromTransaction_ExactCapacity tests that
// allocating exactly the remaining capacity is allowed (boundary condition).
func TestSavingsContributionRepository_UpsertFromTransaction_ExactCapacity(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	goalID := uuid.New().String()
	txnID := uuid.New().String()

	mock.ExpectBegin()

	// Step 0: locked basis assertion — ownership AND a still-live ledger (#1625 Task 22).
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
		WithArgs(goalID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow(domain.ProgressBasisContributions))

	// Step 1: transaction lock with user_id check.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ABS(amount) FROM transactions WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL FOR UPDATE`)).
		WithArgs(txnID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"abs"}).AddRow(500.0))

	// Step 2: 300 allocated elsewhere → 200 remaining.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(amount), 0)`)).
		WithArgs(txnID, goalID).
		WillReturnRows(pgxmock.NewRows([]string{"coalesce"}).AddRow(300.0))

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO savings_goal_contributions`)).
		WithArgs(goalID, txnID, 200.0, "confirmed_transfer", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	mock.ExpectCommit()

	// Exactly 200 remaining — should succeed.
	err := repo.UpsertFromTransaction(ctx, userID, goalID, txnID, 200.0, "confirmed_transfer")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_SumConfirmedByGoal tests contribution sum aggregation.
func TestSavingsContributionRepository_SumConfirmedByGoal(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	goalID := uuid.New().String()

	// Query now JOINs savings_goals for userID scoping (RG2 / M17).
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(c.amount), 0)`)).
		WithArgs(goalID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"coalesce"}).AddRow(1250.50))

	total, err := repo.SumConfirmedByGoal(ctx, userID, goalID)

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
	userID := uuid.New().String()
	goalID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(c.amount), 0)`)).
		WithArgs(goalID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"coalesce"}).AddRow(0.0))

	total, err := repo.SumConfirmedByGoal(ctx, userID, goalID)

	require.NoError(t, err)
	assert.Equal(t, 0.0, total)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_SumConfirmedByGoalInPeriod tests period-filtered sum.
func TestSavingsContributionRepository_SumConfirmedByGoalInPeriod(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	goalID := uuid.New().String()
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	// Query now JOINs savings_goals for userID scoping (RG2 / M17).
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(c.amount), 0)`)).
		WithArgs(goalID, userID, start, end).
		WillReturnRows(pgxmock.NewRows([]string{"coalesce"}).AddRow(750.00))

	total, err := repo.SumConfirmedByGoalInPeriod(ctx, userID, goalID, start, end)

	require.NoError(t, err)
	assert.InDelta(t, 750.00, total, 0.001)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_ListByGoal tests listing contributions.
// M20: userID param added to enforce ownership scoping (Codex C1).
func TestSavingsContributionRepository_ListByGoal(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	goalID := uuid.New().String()
	txnID1 := uuid.New().String()
	now := time.Now()

	rows := pgxmock.NewRows(contributionColumns).
		AddRow(
			uuid.New().String(), goalID, &txnID1, 500.00, "confirmed_transfer", "confirmed",
			true, false, "2024-01-15", now, now, nil,
		).
		AddRow(
			uuid.New().String(), goalID, nil, 200.00, "manual", "confirmed",
			false, true, "2024-01-20", now, now, nil,
		)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(goalID, userID).
		WillReturnRows(rows)

	contribs, err := repo.ListByGoal(ctx, userID, goalID)

	require.NoError(t, err)
	require.Len(t, contribs, 2)
	assert.Equal(t, "confirmed_transfer", contribs[0].Source)
	assert.True(t, contribs[0].ManagedByReconciliation)
	assert.Equal(t, "manual", contribs[1].Source)
	assert.False(t, contribs[1].ManagedByReconciliation)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_ListByGoal_Empty tests that an empty result returns an empty slice.
// M20: userID param added to enforce ownership scoping.
func TestSavingsContributionRepository_ListByGoal_Empty(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	goalID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(goalID, userID).
		WillReturnRows(pgxmock.NewRows(contributionColumns))

	contribs, err := repo.ListByGoal(ctx, userID, goalID)

	require.NoError(t, err)
	assert.Empty(t, contribs)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_DeleteManagedForTransaction tests that only
// managed_by_reconciliation rows on a LIVE goal are TOMBSTONED (Codex C4 — user rows survive;
// #1569 C1 — a tombstoned goal's retained ledger survives too; #1569 round 12 task 2 — the
// destructive method is now an UPDATE, not a DELETE, so the provider cannot hard-delete history).
//
// pgxmock matches the SQL as text, so this pins the SHAPE of the statement and nothing about its
// semantics. The behaviour lives in
// TestDeleteManagedForTransaction_DeletedGoalLedgerSurvives_Integration.
func TestSavingsContributionRepository_DeleteManagedForTransaction(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	txnID := uuid.New().String()

	mock.ExpectExec(`UPDATE savings_goal_contributions c\s+SET deleted_at = NOW\(\), deleted_reason = \$2[\s\S]*FROM savings_goals g[\s\S]*g\.deleted_at IS NULL[\s\S]*c\.deleted_at IS NULL`).
		WithArgs(txnID, "provider_removed_txn").
		WillReturnResult(pgxmock.NewResult("UPDATE", 2))

	err := repo.DeleteManagedForTransaction(ctx, txnID)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_DeleteManagedForTransaction_NoRows tests that
// tombstoning for a transaction with no managed rows is not an error.
func TestSavingsContributionRepository_DeleteManagedForTransaction_NoRows(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	txnID := uuid.New().String()

	mock.ExpectExec(`UPDATE savings_goal_contributions c\s+SET deleted_at = NOW\(\), deleted_reason = \$2[\s\S]*FROM savings_goals g[\s\S]*g\.deleted_at IS NULL[\s\S]*c\.deleted_at IS NULL`).
		WithArgs(txnID, "provider_removed_txn").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.DeleteManagedForTransaction(ctx, txnID)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_ArchiveByGoal tests basis-switch archiving (Codex C10).
// This is a wiring test only: it proves the Exec is issued with the goal ID. The claim that a
// user-entered row survives the basis switch unflagged is a claim about ROWS, which no pgxmock can
// make — it is proved against real Postgres by
// TestArchiveByGoal_Integration_LeavesUserEnteredRowsAlone.
func TestSavingsContributionRepository_ArchiveByGoal(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()

	mock.ExpectExec(`UPDATE savings_goal_contributions\s+SET managed_by_reconciliation = false`).
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

// ============================================================================
// M13 (Issue #1300): Ownership guard in UpsertFromTransaction (G7 / Codex C1)
// These tests drive the new userID parameter on UpsertFromTransaction.
// The query order changes: goal ownership check FIRST (SELECT from savings_goals),
// then the transaction lock; cross-user attempts are rejected with domain errors.
// ============================================================================

// TestSavingsContributionRepository_UpsertFromTransaction_CrossUserGoalRejected verifies
// that attempting to contribute to a goal owned by a different user returns ErrSavingsGoalNotFound.
func TestSavingsContributionRepository_UpsertFromTransaction_CrossUserGoalRejected(t *testing.T) {
	mockDB, repo := setupContributionMockDB(t)
	defer mockDB.Close()

	ctx := context.Background()
	callerUserID := uuid.New().String() // different user from goal owner
	goalID := uuid.New().String()
	txnID := uuid.New().String()

	mockDB.ExpectBegin()

	// Step 0 locked basis assertion: goal does NOT belong to callerUserID → ErrNoRows.
	mockDB.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
		WithArgs(goalID, callerUserID).
		WillReturnError(pgx.ErrNoRows)

	mockDB.ExpectRollback()

	err := repo.UpsertFromTransaction(ctx, callerUserID, goalID, txnID, 100.0, "confirmed_transfer")
	assert.ErrorIs(t, err, domain.ErrSavingsGoalNotFound)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

// TestSavingsContributionRepository_UpsertFromTransaction_CrossUserTransactionRejected verifies
// that contributing from a transaction owned by a different user returns ErrContributionNotFound.
func TestSavingsContributionRepository_UpsertFromTransaction_CrossUserTransactionRejected(t *testing.T) {
	mockDB, repo := setupContributionMockDB(t)
	defer mockDB.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	goalID := uuid.New().String()
	txnID := uuid.New().String() // transaction belongs to a different user

	mockDB.ExpectBegin()

	// Step 0 locked basis assertion passes (goal belongs to userID, still on the ledger).
	mockDB.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
		WithArgs(goalID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow(domain.ProgressBasisContributions))

	// Transaction lock fails: transaction.user_id != userID → ErrNoRows.
	mockDB.ExpectQuery(regexp.QuoteMeta(`SELECT ABS(amount) FROM transactions WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL FOR UPDATE`)).
		WithArgs(txnID, userID).
		WillReturnError(pgx.ErrNoRows)

	mockDB.ExpectRollback()

	err := repo.UpsertFromTransaction(ctx, userID, goalID, txnID, 100.0, "confirmed_transfer")
	assert.ErrorIs(t, err, domain.ErrContributionNotFound)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

// TestSavingsContributionRepository_UpsertFromTransaction_OwnershipCheckSuccess verifies
// the happy path with the new userID parameter and ownership pre-checks.
func TestSavingsContributionRepository_UpsertFromTransaction_OwnershipCheckSuccess(t *testing.T) {
	mockDB, repo := setupContributionMockDB(t)
	defer mockDB.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	goalID := uuid.New().String()
	txnID := uuid.New().String()
	txnAmount := 500.0
	contribAmount := 250.0

	mockDB.ExpectBegin()

	// Step 0 locked basis assertion passes.
	mockDB.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
		WithArgs(goalID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow(domain.ProgressBasisContributions))

	// Transaction lock — now requires user_id check.
	mockDB.ExpectQuery(regexp.QuoteMeta(`SELECT ABS(amount) FROM transactions WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL FOR UPDATE`)).
		WithArgs(txnID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"abs"}).AddRow(txnAmount))

	// Capacity check — nothing allocated elsewhere.
	mockDB.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(amount), 0)`)).
		WithArgs(txnID, goalID).
		WillReturnRows(pgxmock.NewRows([]string{"coalesce"}).AddRow(0.0))

	// Upsert.
	mockDB.ExpectExec(regexp.QuoteMeta(`INSERT INTO savings_goal_contributions`)).
		WithArgs(goalID, txnID, contribAmount, "confirmed_transfer", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	mockDB.ExpectCommit()

	err := repo.UpsertFromTransaction(ctx, userID, goalID, txnID, contribAmount, "confirmed_transfer")
	require.NoError(t, err)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

// =============================================================================
// R3-6: C3 hardening — managed_by_reconciliation guard on proposed upsert
// =============================================================================

// =============================================================================
// R3-7: RejectContribution — sticky tombstone
// =============================================================================

// TestSavingsContributionRepository_RejectContribution_Success verifies that
// RejectContribution updates status to 'rejected' and returns the updated row.
func TestSavingsContributionRepository_RejectContribution_Success(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	contribID := uuid.New().String()
	goalID := uuid.New().String()

	// The UPDATE … FROM savings_goals … RETURNING query. Three args since #1625: the goal is
	// bound into the statement so a contribution from another goal cannot be rejected here.
	// #1625: RejectContribution now opens a transaction and locks the goal row, asserting the
	// ledger is still live, before touching the contribution — an `AND g.progress_basis = …` on
	// the UPDATE … FROM would have been evaluated against a stale snapshot.
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
		WithArgs(goalID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow(domain.ProgressBasisContributions))
	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE savings_goal_contributions c`)).
		WithArgs(contribID, userID, goalID).
		WillReturnRows(pgxmock.NewRows(contributionColumns).
			AddRow(contribID, goalID, nil, 100.0, "confirmed_transfer", "rejected",
				true, false, "2026-06-14", time.Now(), time.Now(), nil))
	mock.ExpectCommit()

	result, err := repo.RejectContribution(ctx, userID, goalID, contribID)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "rejected", result.Status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_RejectContribution_NotFound verifies that
// attempting to reject a non-existent or cross-user contribution returns ErrContributionNotFound.
func TestSavingsContributionRepository_RejectContribution_NotFound(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	goalID := uuid.New().String()
	contribID := uuid.New().String()

	// The UPDATE returns no rows (not found, wrong user, or a different goal).
	// #1625: RejectContribution now opens a transaction and locks the goal row, asserting the
	// ledger is still live, before touching the contribution — an `AND g.progress_basis = …` on
	// the UPDATE … FROM would have been evaluated against a stale snapshot.
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
		WithArgs(goalID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow(domain.ProgressBasisContributions))
	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE savings_goal_contributions c`)).
		WithArgs(contribID, userID, goalID).
		WillReturnError(pgx.ErrNoRows)

	// The existence check also returns no rows.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM savings_goal_contributions c`)).
		WithArgs(contribID, userID, goalID).
		WillReturnError(pgx.ErrNoRows)

	_, err := repo.RejectContribution(ctx, userID, goalID, contribID)
	assert.ErrorIs(t, err, domain.ErrContributionNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_RejectContribution_NotProposed verifies that
// attempting to reject a confirmed/rejected row returns ErrContributionNotProposed.
func TestSavingsContributionRepository_RejectContribution_NotProposed(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	goalID := uuid.New().String()
	contribID := uuid.New().String()

	// The UPDATE returns no rows (status != 'proposed') but the row exists.
	// #1625: RejectContribution now opens a transaction and locks the goal row, asserting the
	// ledger is still live, before touching the contribution — an `AND g.progress_basis = …` on
	// the UPDATE … FROM would have been evaluated against a stale snapshot.
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
		WithArgs(goalID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow(domain.ProgressBasisContributions))
	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE savings_goal_contributions c`)).
		WithArgs(contribID, userID, goalID).
		WillReturnError(pgx.ErrNoRows)

	// Existence check finds the row (wrong status, but row exists for THIS goal — the check is
	// scoped the same way as the UPDATE, so it cannot report a different goal's row as existing).
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM savings_goal_contributions c`)).
		WithArgs(contribID, userID, goalID).
		WillReturnRows(pgxmock.NewRows([]string{"?column?"}).AddRow(1))

	_, err := repo.RejectContribution(ctx, userID, goalID, contribID)
	assert.ErrorIs(t, err, domain.ErrContributionNotProposed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsContributionRepository_UpsertProposedFromTransaction_C3Guard verifies that
// UpsertProposedFromTransaction only overwrites rows where managed_by_reconciliation = true
// (Codex C3 defense-in-depth, R3-6). A non-managed proposed row (managed_by_reconciliation=false)
// must NOT be mutated by the reconciler's upsert.
//
// Implementation: the ON CONFLICT DO UPDATE WHERE clause must include
// `AND savings_goal_contributions.managed_by_reconciliation = true` so that the update
// only fires for rows the reconciler owns, even if status='proposed'. This test can only see the
// SQL text; no fixture here creates the non-managed proposed row the guard is about.
func TestSavingsContributionRepository_UpsertProposedFromTransaction_C3Guard(t *testing.T) {
	mock, repo := setupContributionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	goalID := uuid.New().String()
	txnID := uuid.New().String()
	magnitude := 200.0

	// #1625 Task 22: this path became transactional so its Step 0 `SELECT … FOR UPDATE` on the
	// goal outlives the statement. Ownership and a still-live ledger are now one assertion.
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
		WithArgs(goalID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow(domain.ProgressBasisContributions))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM transactions WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`)).
		WithArgs(txnID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"?column?"}).AddRow(1))

	// The INSERT ... ON CONFLICT ... DO UPDATE WHERE must include managed_by_reconciliation = true
	// (C3 defense-in-depth). Matching the alias-qualified form keeps this from being satisfied by
	// the INSERT's own column value; the behavioural proof on real rows is
	// TestUpsertProposedFromTransaction_Integration_LeavesNonManagedProposedRowAlone.
	mock.ExpectExec(regexp.QuoteMeta(`AND savings_goal_contributions.managed_by_reconciliation = true`)).
		WithArgs(goalID, txnID, magnitude, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	err := repo.UpsertProposedFromTransaction(ctx, userID, goalID, txnID, magnitude)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
