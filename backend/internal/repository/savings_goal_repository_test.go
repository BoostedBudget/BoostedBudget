package repository

import (
	"context"
	
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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
		pgxmock.AnyArg(), // deadline
		goal.IsCompleted,
		goal.CompletedAt,
		pgxmock.AnyArg(), // created_at
		pgxmock.AnyArg(), // updated_at
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
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
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
		"currency_code", "account_id", "deadline", "is_completed", "completed_at",
		"created_at", "updated_at",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(goalID).
		WillReturnRows(pgxmock.NewRows(columns).AddRow(
			goalID,
			userID,
			"Vacation Fund",
			&desc,     // Use pointer for nullable description
			5000.00,
			1000.00,
			"USD",
			accountID,
			&deadline, // Use pointer for nullable deadline
			false,
			nil,
			now,
			now,
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
		"currency_code", "account_id", "deadline", "is_completed", "completed_at",
		"created_at", "updated_at",
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
				&deadline, // Use pointer for nullable deadline
				false,
				nil,
				now,
				now,
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
				nil, // nil deadline is fine
				false,
				nil,
				now,
				now,
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
		"currency_code", "account_id", "deadline", "is_completed", "completed_at",
		"created_at", "updated_at",
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
			nil, // nil deadline is fine
			false,
			nil,
			now,
			now,
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
		"currency_code", "account_id", "deadline", "is_completed", "completed_at",
		"created_at", "updated_at",
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
				nil,
				false,
				nil,
				now,
				now,
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
				nil,
				false,
				nil,
				now,
				now,
			))

	goals, err := repo.GetByAccountID(ctx, accountID)

	assert.NoError(t, err)
	assert.Len(t, goals, 2)
	for _, g := range goals {
		assert.Equal(t, accountID, g.AccountID)
	}
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_Update tests savings goal update
func TestSavingsGoalRepository_Update(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goal := createTestSavingsGoal(uuid.New().String(), uuid.New().String())
	goal.ID = uuid.New().String()
	goal.Name = "Updated Name"

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE savings_goals`)).
		WithArgs(
			goal.Name,
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			goal.ID,
			goal.UserID,
		).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

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
			goal.ID,
			goal.UserID,
		).WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.Update(ctx, goal)

	assert.Error(t, err)
	assert.Equal(t, domain.ErrSavingsGoalNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_Delete tests savings goal deletion
func TestSavingsGoalRepository_Delete(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()
	userID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM savings_goals`)).
		WithArgs(goalID, userID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.Delete(ctx, goalID, userID)

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

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM savings_goals`)).
		WithArgs(goalID, userID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.Delete(ctx, goalID, userID)

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

// TestSavingsGoalRepository_UpdateCurrentAmount tests current amount update
func TestSavingsGoalRepository_UpdateCurrentAmount(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()
	newCurrentAmount := 2500.00

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE savings_goals`)).
		WithArgs(newCurrentAmount, goalID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.UpdateCurrentAmount(ctx, goalID, newCurrentAmount)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSavingsGoalRepository_UpdateCurrentAmount_NotFound tests update on non-existent goal
func TestSavingsGoalRepository_UpdateCurrentAmount_NotFound(t *testing.T) {
	mock, repo := setupSavingsGoalMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	goalID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE savings_goals`)).
		WithArgs(2500.00, goalID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.UpdateCurrentAmount(ctx, goalID, 2500.00)

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
