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

// setupSavingsGoalMockDB creates a mock database for savings goal repository testing
func setupSavingsGoalMockDB(t *testing.T) (pgxmock.PgxPoolIface, SavingsGoalRepository) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)

	repo := NewSavingsGoalRepository(mock)
	return mock, repo
}

// createTestSavingsGoal creates a test savings goal
func createTestSavingsGoal(userID, accountID string) *domain.SavingsGoal {
	desc := "Save for vacation"
	deadline := time.Now().AddDate(1, 0, 0)
	return &domain.SavingsGoal{
		ID:            uuid.New().String(),
		UserID:        userID,
		Name:          "Vacation Fund",
		Description:   &desc,
		TargetAmount:  5000.00,
		CurrentAmount: 1000.00,
		CurrencyCode:  "USD",
		AccountID:     accountID,
		Deadline:      &deadline,
		IsCompleted:   false,
	}
}

// TestSavingsGoalRepository_Create tests successful savings goal creation
func TestSavingsGoalRepository_Create(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goal := createTestSavingsGoal(uuid.New().String(), uuid.New().String())

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO savings_goals`)).WithArgs(
		pgxmock.AnyArg(), // id
		goal.UserID,
		goal.Name,
		goal.Description,
		goal.TargetAmount,
		goal.CurrentAmount,
		pgxmock.AnyArg(), // currency_code
		goal.AccountID,
		goal.UserAccountID, // user_account_id (Issue #1109)
		pgxmock.AnyArg(),   // deadline
		goal.IsCompleted,
		goal.CompletedAt,
		pgxmock.AnyArg(), // created_at
		pgxmock.AnyArg(), // updated_at
		pgxmock.AnyArg(), // progress_basis
		pgxmock.AnyArg(), // account_percentage
		pgxmock.AnyArg(), // last_calculated_at
		pgxmock.AnyArg(), // balance_as_of
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Create(ctx, goal)

	assert.NoError(t, err)
	assert.NotEmpty(t, goal.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_Create_DuplicateName tests duplicate name error
func TestSavingsGoalRepository_Create_DuplicateName(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goal := createTestSavingsGoal(uuid.New().String(), uuid.New().String())

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO savings_goals`)).WithArgs(
		pgxmock.AnyArg(),
		goal.UserID,
		goal.Name,
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(), // user_account_id (Issue #1109)
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(), // progress_basis
		pgxmock.AnyArg(), // account_percentage
		pgxmock.AnyArg(), // last_calculated_at
		pgxmock.AnyArg(), // balance_as_of
	).WillReturnError(pgx.ErrNoRows) // Simulating constraint violation

	err := repo.Create(ctx, goal)

	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_GetByID tests retrieval by ID
func TestSavingsGoalRepository_GetByID(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	now := time.Now()
	deadline := now.AddDate(1, 0, 0)
	desc := "Save for vacation"

	columns := []string{
		"id", "user_id", "name", "description", "target_amount", "current_amount",
		"currency_code", "account_id", "user_account_id", "deadline", "is_completed", "completed_at",
		"created_at", "updated_at",
		"progress_basis", "account_percentage", "last_calculated_at", "balance_as_of",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(goalID).
		WillReturnRows(pgxmock.NewRows(columns).AddRow(
			goalID,
			userID,
			"Vacation Fund",
			&desc, // Use pointer for nullable description
			5000.00,
			1000.00,
			"USD",
			accountID,
			nil,       // user_account_id (nullable)
			&deadline, // Use pointer for nullable deadline
			false,
			nil,
			now,
			now,
			"manual", // progress_basis
			nil,      // account_percentage
			nil,      // last_calculated_at
			nil,      // balance_as_of
		))

	goal, err := repo.GetByID(ctx, goalID)

	assert.NoError(t, err)
	assert.NotNil(t, goal)
	assert.Equal(t, goalID, goal.ID)
	assert.Equal(t, userID, goal.UserID)
	assert.Equal(t, "Vacation Fund", goal.Name)
	assert.Equal(t, accountID, goal.AccountID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_GetByID_NotFound tests retrieval of non-existent goal
func TestSavingsGoalRepository_GetByID_NotFound(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(goalID).
		WillReturnError(pgx.ErrNoRows)

	goal, err := repo.GetByID(ctx, goalID)

	assert.Error(t, err)
	assert.Nil(t, goal)
	assert.Equal(t, domain.ErrSavingsGoalNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_GetByUserID tests retrieval by user ID
func TestSavingsGoalRepository_GetByUserID(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()
	deadline := now.AddDate(1, 0, 0)
	desc1, desc2 := "Desc 1", "Desc 2"

	columns := []string{
		"id", "user_id", "name", "description", "target_amount", "current_amount",
		"currency_code", "account_id", "user_account_id", "deadline", "is_completed", "completed_at",
		"created_at", "updated_at",
		"progress_basis", "account_percentage", "last_calculated_at", "balance_as_of",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID).
		WillReturnRows(pgxmock.NewRows(columns).
			AddRow(
				uuid.New().String(),
				userID,
				"Goal 1",
				&desc1, // Use pointer for nullable description
				5000.00,
				1000.00,
				"USD",
				uuid.New().String(),
				nil,       // user_account_id (nullable)
				&deadline, // Use pointer for nullable deadline
				false,
				nil,
				now,
				now,
				"manual", // progress_basis
				nil,      // account_percentage
				nil,      // last_calculated_at
				nil,      // balance_as_of
			).
			AddRow(
				uuid.New().String(),
				userID,
				"Goal 2",
				&desc2, // Use pointer for nullable description
				10000.00,
				5000.00,
				"USD",
				uuid.New().String(),
				nil, // user_account_id (nullable)
				nil, // nil deadline is fine
				false,
				nil,
				now,
				now,
				"manual", // progress_basis
				nil,      // account_percentage
				nil,      // last_calculated_at
				nil,      // balance_as_of
			))

	goals, err := repo.GetByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Len(t, goals, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_GetActiveByUserID tests retrieval of active goals
func TestSavingsGoalRepository_GetActiveByUserID(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()
	desc := "Desc"

	columns := []string{
		"id", "user_id", "name", "description", "target_amount", "current_amount",
		"currency_code", "account_id", "user_account_id", "deadline", "is_completed", "completed_at",
		"created_at", "updated_at",
		"progress_basis", "account_percentage", "last_calculated_at", "balance_as_of",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID).
		WillReturnRows(pgxmock.NewRows(columns).AddRow(
			uuid.New().String(),
			userID,
			"Active Goal",
			&desc, // Use pointer for nullable description
			5000.00,
			1000.00,
			"USD",
			uuid.New().String(),
			nil, // user_account_id (nullable)
			nil, // nil deadline is fine
			false,
			nil,
			now,
			now,
			"manual", // progress_basis
			nil,      // account_percentage
			nil,      // last_calculated_at
			nil,      // balance_as_of
		))

	goals, err := repo.GetActiveByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Len(t, goals, 1)
	assert.False(t, goals[0].IsCompleted)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_GetByAccountID tests retrieval by account ID
func TestSavingsGoalRepository_GetByAccountID(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()
	now := time.Now()
	desc1, desc2 := "Desc 1", "Desc 2"

	columns := []string{
		"id", "user_id", "name", "description", "target_amount", "current_amount",
		"currency_code", "account_id", "user_account_id", "deadline", "is_completed", "completed_at",
		"created_at", "updated_at",
		"progress_basis", "account_percentage", "last_calculated_at", "balance_as_of",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(accountID).
		WillReturnRows(pgxmock.NewRows(columns).
			AddRow(
				uuid.New().String(),
				userID,
				"Goal 1",
				&desc1, // Use pointer for nullable description
				5000.00,
				1000.00,
				"USD",
				accountID,
				nil, // user_account_id (nullable)
				nil, // deadline (nullable)
				false,
				nil,
				now,
				now,
				"manual", // progress_basis
				nil,      // account_percentage
				nil,      // last_calculated_at
				nil,      // balance_as_of
			).
			AddRow(
				uuid.New().String(),
				userID,
				"Goal 2",
				&desc2, // Use pointer for nullable description
				10000.00,
				5000.00,
				"USD",
				accountID,
				nil, // user_account_id (nullable)
				nil, // deadline (nullable)
				false,
				nil,
				now,
				now,
				"manual", // progress_basis
				nil,      // account_percentage
				nil,      // last_calculated_at
				nil,      // balance_as_of
			))

	goals, err := repo.GetByAccountID(ctx, accountID)

	assert.NoError(t, err)
	assert.Len(t, goals, 2)
	for _, g := range goals {
		assert.Equal(t, accountID, g.AccountID)
	}
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_Update tests savings goal update, including user_account_id persistence.
func TestSavingsGoalRepository_Update(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goal := createTestSavingsGoal(uuid.New().String(), uuid.New().String())
	goal.ID = uuid.New().String()
	goal.Name = "Updated Name"
	uaID := "ua-stable-123"
	goal.UserAccountID = &uaID

	// user_account_id must be bound as $11, preceding progress_basis ($12)…balance_as_of ($15), id ($16) and user_id ($17).
	// #1625 Task 17: Update is transactional and reads the stored progress_basis under
	// SELECT … FOR UPDATE first, so an unguarded basis change can be refused before any write.
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
		WithArgs(goal.ID, goal.UserID).
		WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow(goal.ProgressBasis))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE savings_goals`)).
		WithArgs(
			goal.Name,
			pgxmock.AnyArg(),   // description
			pgxmock.AnyArg(),   // target_amount
			pgxmock.AnyArg(),   // current_amount
			pgxmock.AnyArg(),   // currency_code
			pgxmock.AnyArg(),   // account_id
			pgxmock.AnyArg(),   // deadline
			pgxmock.AnyArg(),   // is_completed
			pgxmock.AnyArg(),   // completed_at
			pgxmock.AnyArg(),   // updated_at
			goal.UserAccountID, // $11 user_account_id — must be persisted
			pgxmock.AnyArg(),   // progress_basis
			pgxmock.AnyArg(),   // account_percentage
			pgxmock.AnyArg(),   // last_calculated_at
			pgxmock.AnyArg(),   // balance_as_of
			goal.ID,
			goal.UserID,
		).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectCommit()

	err := repo.Update(ctx, goal)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_Update_UserAccountIDPersisted verifies chain-stable id survives round-trip.
func TestSavingsGoalRepository_Update_UserAccountIDPersisted(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goal := createTestSavingsGoal(uuid.New().String(), uuid.New().String())
	goal.ID = uuid.New().String()
	uaIDA := "ua-chain-a"
	uaIDB := "ua-chain-b"
	goal.UserAccountID = &uaIDA

	// Simulate account change: update UserAccountID to chain B.
	goal.UserAccountID = &uaIDB

	// #1625 Task 17: Update is transactional and reads the stored progress_basis under
	// SELECT … FOR UPDATE first, so an unguarded basis change can be refused before any write.
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
		WithArgs(goal.ID, goal.UserID).
		WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow(goal.ProgressBasis))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE savings_goals`)).
		WithArgs(
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
			&uaIDB,           // $11 must be chain B's id, not chain A's
			pgxmock.AnyArg(), // progress_basis
			pgxmock.AnyArg(), // account_percentage
			pgxmock.AnyArg(), // last_calculated_at
			pgxmock.AnyArg(), // balance_as_of
			goal.ID,
			goal.UserID,
		).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectCommit()

	err := repo.Update(ctx, goal)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_Update_NotFound tests update of non-existent goal
func TestSavingsGoalRepository_Update_NotFound(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goal := createTestSavingsGoal(uuid.New().String(), uuid.New().String())
	goal.ID = uuid.New().String()

	// #1625 Task 17: Update is transactional and reads the stored progress_basis under
	// SELECT … FOR UPDATE first, so an unguarded basis change can be refused before any write.
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
		WithArgs(goal.ID, goal.UserID).
		WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow(goal.ProgressBasis))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE savings_goals`)).
		WithArgs(
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
			pgxmock.AnyArg(), // user_account_id
			pgxmock.AnyArg(), // progress_basis
			pgxmock.AnyArg(), // account_percentage
			pgxmock.AnyArg(), // last_calculated_at
			pgxmock.AnyArg(), // balance_as_of
			goal.ID,
			goal.UserID,
		).WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectRollback()

	err := repo.Update(ctx, goal)

	assert.Error(t, err)
	assert.Equal(t, domain.ErrSavingsGoalNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_Delete tests savings goal deletion. Issue #1569 Task 8: it is a
// TOMBSTONE now, not a DELETE — the statement must be an UPDATE, and it must be followed by the
// budget-category cascade inside the same transaction.
func TestSavingsGoalRepository_Delete(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()
	userID := uuid.New().String()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE savings_goals`)).
		WithArgs(goalID, userID, domain.SoftDeleteReasonUser, domain.SoftDeletedByUser).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budget_categories`)).
		WithArgs(goalID, domain.SoftDeleteReasonParent, domain.SoftDeletedByUser).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectCommit()

	err := repo.Delete(ctx, goalID, userID, nil)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_Delete_NotFound tests deletion of non-existent goal
func TestSavingsGoalRepository_Delete_NotFound(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()
	userID := uuid.New().String()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE savings_goals`)).
		WithArgs(goalID, userID, domain.SoftDeleteReasonUser, domain.SoftDeletedByUser).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectRollback()

	err := repo.Delete(ctx, goalID, userID, nil)

	assert.Error(t, err)
	assert.Equal(t, domain.ErrSavingsGoalNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_CountByUserID tests counting goals by user
func TestSavingsGoalRepository_CountByUserID(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT`)).
		WithArgs(userID).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(4))

	count, err := repo.CountByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Equal(t, 4, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_UpdateCurrentAmount tests current amount update (Issue #1300)
func TestSavingsGoalRepository_UpdateCurrentAmount(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()
	newCurrentAmount := 2500.00
	now := time.Now()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE savings_goals`)).
		WithArgs(newCurrentAmount, pgxmock.AnyArg(), pgxmock.AnyArg(), goalID,
			domain.ProgressBasisContributions, domain.ProgressBasisManual).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.UpdateCurrentAmount(ctx, goalID, domain.ProgressBasisContributions, newCurrentAmount, now, nil)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_UpdateCurrentAmount_NotFound tests update on non-existent goal
func TestSavingsGoalRepository_UpdateCurrentAmount_NotFound(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()
	now := time.Now()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE savings_goals`)).
		WithArgs(2500.00, pgxmock.AnyArg(), pgxmock.AnyArg(), goalID,
			domain.ProgressBasisContributions, domain.ProgressBasisManual).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	// #1625: zero rows is now ambiguous — gone, or moved to another basis. The repository probes
	// to tell them apart, and a probe that finds nothing means genuinely not found.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(NULLIF(progress_basis, ''), $2) FROM savings_goals`)).
		WithArgs(goalID, domain.ProgressBasisManual).
		WillReturnError(pgx.ErrNoRows)

	err := repo.UpdateCurrentAmount(ctx, goalID, domain.ProgressBasisContributions, 2500.00, now, nil)

	assert.Error(t, err)
	assert.Equal(t, domain.ErrSavingsGoalNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_MarkCompleted tests marking goal as completed
func TestSavingsGoalRepository_MarkCompleted(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE savings_goals`)).
		WithArgs(pgxmock.AnyArg(), goalID). // now timestamp and goalID
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.MarkCompleted(ctx, goalID)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_MarkCompleted_NotFound tests marking non-existent goal as completed
func TestSavingsGoalRepository_MarkCompleted_NotFound(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE savings_goals`)).
		WithArgs(pgxmock.AnyArg(), goalID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.MarkCompleted(ctx, goalID)

	assert.Error(t, err)
	assert.Equal(t, domain.ErrSavingsGoalNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_GetByAccountID_ChainExpand verifies that filtering
// by active account_id returns goals associated with replaced (pre-relink) accounts
// that share the same user_account_id. Issue #1109: B.4 fix.
func TestSavingsGoalRepository_GetByAccountID_ChainExpand(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	activeAccountID := uuid.New().String()
	replacedAccountID := uuid.New().String()
	userID := uuid.New().String()
	userAccountID := uuid.New().String()
	now := time.Now()

	columns := []string{
		"id", "user_id", "name", "description", "target_amount", "current_amount",
		"currency_code", "account_id", "user_account_id", "deadline", "is_completed", "completed_at",
		"created_at", "updated_at",
		"progress_basis", "account_percentage", "last_calculated_at", "balance_as_of",
	}

	// Goal is linked to the replaced account but has the stable user_account_id.
	// When the caller filters by activeAccountID, the goal must still be returned.
	mock.ExpectQuery(regexp.QuoteMeta(`user_account_id = (SELECT user_account_id FROM accounts WHERE id = $1`)).
		WithArgs(activeAccountID).
		WillReturnRows(pgxmock.NewRows(columns).
			AddRow(
				uuid.New().String(),
				userID,
				"Chain Goal",
				nil,
				8000.00,
				1000.00,
				"USD",
				replacedAccountID, // pre-relink account_id
				&userAccountID,    // stable user_account_id
				nil,
				false,
				nil,
				now,
				now,
				"manual", // progress_basis
				nil,      // account_percentage
				nil,      // last_calculated_at
				nil,      // balance_as_of
			))

	goals, err := repo.GetByAccountID(ctx, activeAccountID)
	require.NoError(t, err)
	require.Len(t, goals, 1, "pre-relink goal must be returned when filtering by active account_id")
	assert.Equal(t, replacedAccountID, goals[0].AccountID)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_Update_RefusesABasisChange closes the LAST bypass around #1625's
// basis-switch transaction, at the layer where it cannot be routed around.
//
// Task 17 put the switch behind a lock and a predicate in UpdateWithBasisGuardInTx, but the
// service only took that path when its optional basis-switch ledger repo had been wired by a
// setter. A construction site that forgot the setter — and two existed — sent a basis change
// straight through the plain Update: no lock, no predicate, no archive, no seed. A runtime hook
// protecting a compile-time property, silently degrading to the pre-#1625 behaviour with every
// test green.
//
// The remedy is here rather than in the service because here it is UNREPRESENTABLE: Update reads
// the stored basis under the SAME `SELECT … FOR UPDATE` the switch takes, and refuses outright if
// the goal it was handed disagrees. No caller — service, handler, worker, one written next year —
// can change a progress_basis except through the guarded path.
//
// ErrUnguardedBasisChange is deliberately NOT mapped to a 4xx: it is unreachable from any correct
// wiring, so it is a genuine fault and a 500 with a Sentry event is the honest answer. That is the
// opposite call from ErrSavingsGoalBasisConflict, which IS a precondition a user can hit.
func TestSavingsGoalRepository_Update_RefusesABasisChange(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goal := createTestSavingsGoal(uuid.New().String(), uuid.New().String())
	goal.ID = uuid.New().String()
	goal.ProgressBasis = domain.ProgressBasisContributions

	mock.ExpectBegin()
	// The row is still manual; the caller is trying to write "contributions" through the
	// unguarded path.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
		WithArgs(goal.ID, goal.UserID).
		WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow(domain.ProgressBasisManual))
	mock.ExpectRollback()

	err := repo.Update(ctx, goal)

	require.Error(t, err, "a basis change through Update must be refused, not silently applied")
	assert.ErrorIs(t, err, ErrUnguardedBasisChange)
	assert.Contains(t, err.Error(), "UpdateWithBasisGuardInTx",
		"the error must name the path the caller should have taken")
	assert.NoError(t, mock.ExpectationsWereMet(),
		"no UPDATE may have been issued — the refusal happens before the write")
}

// TestSavingsGoalRepository_Update_AllowsAnUnchangedBasis is the other half: the guard must refuse
// a CHANGE, not every update. A blank stored column counts as manual, the same normalisation every
// other write path in this repository applies, or a legacy row would become unwritable.
func TestSavingsGoalRepository_Update_AllowsAnUnchangedBasis(t *testing.T) {
	for _, stored := range []string{domain.ProgressBasisManual, ""} {
		t.Run("stored="+stored, func(t *testing.T) {
			mock, repo := setupSavingsGoalMockDB(t)
			defer mock.Close()

			ctx := context.Background()
			goal := createTestSavingsGoal(uuid.New().String(), uuid.New().String())
			goal.ID = uuid.New().String()
			goal.ProgressBasis = domain.ProgressBasisManual

			mock.ExpectBegin()
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT progress_basis`)).
				WithArgs(goal.ID, goal.UserID).
				WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow(stored))
			mock.ExpectExec(regexp.QuoteMeta(`UPDATE savings_goals`)).
				WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
				WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			mock.ExpectCommit()

			require.NoError(t, repo.Update(ctx, goal))
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
