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

// setupRolloverHistoryMockDB creates a mock database for rollover history repository testing.
func setupRolloverHistoryMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock, RolloverHistoryRepository) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	repo := NewRolloverHistoryRepository(db)
	return db, mock, repo
}

// createTestRolloverHistory creates a test rollover history.
func createTestRolloverHistory(userID, budgetID, sourceCategoryID string) *domain.RolloverHistory {
	return &domain.RolloverHistory{
		ID:                     uuid.New().String(),
		UserID:                 userID,
		BudgetID:               budgetID,
		SourceBudgetCategoryID: sourceCategoryID,
		Amount:                 100.00,
		RolloverType:           domain.RolloverTypeFull,
		BudgetPeriodStart:      time.Now().AddDate(0, -1, 0),
		BudgetPeriodEnd:        time.Now(),
		Status:                 domain.RolloverStatusPending,
	}
}

// =============================================================================
// Create Tests
// =============================================================================

func TestRolloverHistoryRepository_Create(t *testing.T) {
	db, mock, repo := setupRolloverHistoryMockDB(t)
	defer db.Close()

	ctx := context.Background()
	rollover := createTestRolloverHistory(
		uuid.New().String(),
		uuid.New().String(),
		uuid.New().String(),
	)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO rollover_history`)).WithArgs(
		sqlmock.AnyArg(), // id
		rollover.UserID,
		rollover.BudgetID,
		rollover.RuleID,
		rollover.RuleExecutionID,
		rollover.SourceBudgetCategoryID,
		rollover.TargetBudgetCategoryID,
		rollover.Amount,
		rollover.RolloverType,
		sqlmock.AnyArg(), // budget_period_start
		sqlmock.AnyArg(), // budget_period_end
		rollover.Status,
		rollover.AppliedAt,
		sqlmock.AnyArg(), // created_at
	).WillReturnResult(sqlmock.NewResult(1, 1))

	err := repo.Create(ctx, rollover)

	assert.NoError(t, err)
	assert.NotEmpty(t, rollover.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRolloverHistoryRepository_Create_WithTargetCategory(t *testing.T) {
	db, mock, repo := setupRolloverHistoryMockDB(t)
	defer db.Close()

	ctx := context.Background()
	rollover := createTestRolloverHistory(
		uuid.New().String(),
		uuid.New().String(),
		uuid.New().String(),
	)
	targetCategoryID := uuid.New().String()
	rollover.TargetBudgetCategoryID = &targetCategoryID

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO rollover_history`)).WithArgs(
		sqlmock.AnyArg(),
		rollover.UserID,
		rollover.BudgetID,
		rollover.RuleID,
		rollover.RuleExecutionID,
		rollover.SourceBudgetCategoryID,
		rollover.TargetBudgetCategoryID,
		rollover.Amount,
		rollover.RolloverType,
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		rollover.Status,
		rollover.AppliedAt,
		sqlmock.AnyArg(),
	).WillReturnResult(sqlmock.NewResult(1, 1))

	err := repo.Create(ctx, rollover)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRolloverHistoryRepository_Create_WithRuleInfo(t *testing.T) {
	db, mock, repo := setupRolloverHistoryMockDB(t)
	defer db.Close()

	ctx := context.Background()
	rollover := createTestRolloverHistory(
		uuid.New().String(),
		uuid.New().String(),
		uuid.New().String(),
	)
	ruleID := uuid.New().String()
	execID := uuid.New().String()
	rollover.RuleID = &ruleID
	rollover.RuleExecutionID = &execID

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO rollover_history`)).WithArgs(
		sqlmock.AnyArg(),
		rollover.UserID,
		rollover.BudgetID,
		rollover.RuleID,
		rollover.RuleExecutionID,
		rollover.SourceBudgetCategoryID,
		rollover.TargetBudgetCategoryID,
		rollover.Amount,
		rollover.RolloverType,
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		rollover.Status,
		rollover.AppliedAt,
		sqlmock.AnyArg(),
	).WillReturnResult(sqlmock.NewResult(1, 1))

	err := repo.Create(ctx, rollover)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// =============================================================================
// GetByID Tests
// =============================================================================

func TestRolloverHistoryRepository_GetByID(t *testing.T) {
	db, mock, repo := setupRolloverHistoryMockDB(t)
	defer db.Close()

	ctx := context.Background()
	rolloverID := uuid.New().String()
	userID := uuid.New().String()
	budgetID := uuid.New().String()
	sourceCategoryID := uuid.New().String()
	now := time.Now()

	columns := []string{
		"id", "user_id", "budget_id", "rule_id", "rule_execution_id",
		"source_budget_category_id", "target_budget_category_id",
		"amount", "rollover_type", "budget_period_start", "budget_period_end",
		"status", "applied_at", "created_at",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(rolloverID).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(
			rolloverID,
			userID,
			budgetID,
			nil, // rule_id
			nil, // rule_execution_id
			sourceCategoryID,
			nil, // target_budget_category_id
			100.00,
			domain.RolloverTypeFull,
			now.AddDate(0, -1, 0),
			now,
			domain.RolloverStatusPending,
			nil, // applied_at
			now,
		))

	rollover, err := repo.GetByID(ctx, rolloverID)

	assert.NoError(t, err)
	assert.NotNil(t, rollover)
	assert.Equal(t, rolloverID, rollover.ID)
	assert.Equal(t, userID, rollover.UserID)
	assert.Equal(t, budgetID, rollover.BudgetID)
	assert.Equal(t, sourceCategoryID, rollover.SourceBudgetCategoryID)
	assert.Equal(t, 100.00, rollover.Amount)
	assert.Equal(t, domain.RolloverTypeFull, rollover.RolloverType)
	assert.Equal(t, domain.RolloverStatusPending, rollover.Status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRolloverHistoryRepository_GetByID_NotFound(t *testing.T) {
	db, mock, repo := setupRolloverHistoryMockDB(t)
	defer db.Close()

	ctx := context.Background()
	rolloverID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(rolloverID).
		WillReturnError(sql.ErrNoRows)

	rollover, err := repo.GetByID(ctx, rolloverID)

	assert.Error(t, err)
	assert.Nil(t, rollover)
	assert.Equal(t, domain.ErrRolloverHistoryNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// =============================================================================
// GetByUserID Tests
// =============================================================================

func TestRolloverHistoryRepository_GetByUserID(t *testing.T) {
	db, mock, repo := setupRolloverHistoryMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	columns := []string{
		"id", "user_id", "budget_id", "rule_id", "rule_execution_id",
		"source_budget_category_id", "target_budget_category_id",
		"amount", "rollover_type", "budget_period_start", "budget_period_end",
		"status", "applied_at", "created_at",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID, 10, 0).
		WillReturnRows(sqlmock.NewRows(columns).
			AddRow(
				uuid.New().String(),
				userID,
				uuid.New().String(),
				nil,
				nil,
				uuid.New().String(),
				nil,
				100.00,
				domain.RolloverTypeFull,
				now.AddDate(0, -1, 0),
				now,
				domain.RolloverStatusApplied,
				now,
				now,
			).
			AddRow(
				uuid.New().String(),
				userID,
				uuid.New().String(),
				nil,
				nil,
				uuid.New().String(),
				nil,
				50.00,
				domain.RolloverTypeCapped,
				now.AddDate(0, -2, 0),
				now.AddDate(0, -1, 0),
				domain.RolloverStatusPending,
				nil,
				now.AddDate(0, -1, 0),
			))

	rollovers, err := repo.GetByUserID(ctx, userID, 10, 0)

	assert.NoError(t, err)
	assert.Len(t, rollovers, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRolloverHistoryRepository_GetByUserID_Empty(t *testing.T) {
	db, mock, repo := setupRolloverHistoryMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	columns := []string{
		"id", "user_id", "budget_id", "rule_id", "rule_execution_id",
		"source_budget_category_id", "target_budget_category_id",
		"amount", "rollover_type", "budget_period_start", "budget_period_end",
		"status", "applied_at", "created_at",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID, 10, 0).
		WillReturnRows(sqlmock.NewRows(columns))

	rollovers, err := repo.GetByUserID(ctx, userID, 10, 0)

	assert.NoError(t, err)
	assert.Empty(t, rollovers)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// =============================================================================
// GetByBudgetID Tests
// =============================================================================

func TestRolloverHistoryRepository_GetByBudgetID(t *testing.T) {
	db, mock, repo := setupRolloverHistoryMockDB(t)
	defer db.Close()

	ctx := context.Background()
	budgetID := uuid.New().String()
	userID := uuid.New().String()
	now := time.Now()

	columns := []string{
		"id", "user_id", "budget_id", "rule_id", "rule_execution_id",
		"source_budget_category_id", "target_budget_category_id",
		"amount", "rollover_type", "budget_period_start", "budget_period_end",
		"status", "applied_at", "created_at",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(budgetID, 10, 0).
		WillReturnRows(sqlmock.NewRows(columns).
			AddRow(
				uuid.New().String(),
				userID,
				budgetID,
				nil,
				nil,
				uuid.New().String(),
				nil,
				100.00,
				domain.RolloverTypeFull,
				now.AddDate(0, -1, 0),
				now,
				domain.RolloverStatusApplied,
				now,
				now,
			))

	rollovers, err := repo.GetByBudgetID(ctx, budgetID, 10, 0)

	assert.NoError(t, err)
	assert.Len(t, rollovers, 1)
	assert.Equal(t, budgetID, rollovers[0].BudgetID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// =============================================================================
// GetPendingByUserID Tests
// =============================================================================

func TestRolloverHistoryRepository_GetPendingByUserID(t *testing.T) {
	db, mock, repo := setupRolloverHistoryMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	columns := []string{
		"id", "user_id", "budget_id", "rule_id", "rule_execution_id",
		"source_budget_category_id", "target_budget_category_id",
		"amount", "rollover_type", "budget_period_start", "budget_period_end",
		"status", "applied_at", "created_at",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows(columns).
			AddRow(
				uuid.New().String(),
				userID,
				uuid.New().String(),
				nil,
				nil,
				uuid.New().String(),
				nil,
				75.00,
				domain.RolloverTypePercentage,
				now.AddDate(0, -1, 0),
				now,
				domain.RolloverStatusPending,
				nil,
				now,
			))

	rollovers, err := repo.GetPendingByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Len(t, rollovers, 1)
	assert.Equal(t, domain.RolloverStatusPending, rollovers[0].Status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// =============================================================================
// UpdateStatus Tests
// =============================================================================

func TestRolloverHistoryRepository_UpdateStatus_Applied(t *testing.T) {
	db, mock, repo := setupRolloverHistoryMockDB(t)
	defer db.Close()

	ctx := context.Background()
	rolloverID := uuid.New().String()
	now := time.Now()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE rollover_history`)).
		WithArgs(domain.RolloverStatusApplied, sqlmock.AnyArg(), rolloverID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateStatus(ctx, rolloverID, domain.RolloverStatusApplied, &now)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRolloverHistoryRepository_UpdateStatus_Rejected(t *testing.T) {
	db, mock, repo := setupRolloverHistoryMockDB(t)
	defer db.Close()

	ctx := context.Background()
	rolloverID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE rollover_history`)).
		WithArgs(domain.RolloverStatusRejected, nil, rolloverID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateStatus(ctx, rolloverID, domain.RolloverStatusRejected, nil)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRolloverHistoryRepository_UpdateStatus_NotFound(t *testing.T) {
	db, mock, repo := setupRolloverHistoryMockDB(t)
	defer db.Close()

	ctx := context.Background()
	rolloverID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE rollover_history`)).
		WithArgs(domain.RolloverStatusApplied, nil, rolloverID).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.UpdateStatus(ctx, rolloverID, domain.RolloverStatusApplied, nil)

	assert.Error(t, err)
	assert.Equal(t, domain.ErrRolloverHistoryNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// =============================================================================
// GetByBudgetAndPeriod Tests
// =============================================================================

func TestRolloverHistoryRepository_GetByBudgetAndPeriod(t *testing.T) {
	db, mock, repo := setupRolloverHistoryMockDB(t)
	defer db.Close()

	ctx := context.Background()
	budgetID := uuid.New().String()
	userID := uuid.New().String()
	periodStart := time.Now().AddDate(0, -1, 0)
	periodEnd := time.Now()
	now := time.Now()

	columns := []string{
		"id", "user_id", "budget_id", "rule_id", "rule_execution_id",
		"source_budget_category_id", "target_budget_category_id",
		"amount", "rollover_type", "budget_period_start", "budget_period_end",
		"status", "applied_at", "created_at",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(budgetID, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(columns).
			AddRow(
				uuid.New().String(),
				userID,
				budgetID,
				nil,
				nil,
				uuid.New().String(),
				nil,
				100.00,
				domain.RolloverTypeFull,
				periodStart,
				periodEnd,
				domain.RolloverStatusApplied,
				now,
				now,
			))

	rollovers, err := repo.GetByBudgetAndPeriod(ctx, budgetID, periodStart, periodEnd)

	assert.NoError(t, err)
	assert.Len(t, rollovers, 1)
	assert.Equal(t, budgetID, rollovers[0].BudgetID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// =============================================================================
// GetBySourceCategory Tests
// =============================================================================

func TestRolloverHistoryRepository_GetBySourceCategory(t *testing.T) {
	db, mock, repo := setupRolloverHistoryMockDB(t)
	defer db.Close()

	ctx := context.Background()
	sourceCategoryID := uuid.New().String()
	userID := uuid.New().String()
	budgetID := uuid.New().String()
	now := time.Now()

	columns := []string{
		"id", "user_id", "budget_id", "rule_id", "rule_execution_id",
		"source_budget_category_id", "target_budget_category_id",
		"amount", "rollover_type", "budget_period_start", "budget_period_end",
		"status", "applied_at", "created_at",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(sourceCategoryID, 10, 0).
		WillReturnRows(sqlmock.NewRows(columns).
			AddRow(
				uuid.New().String(),
				userID,
				budgetID,
				nil,
				nil,
				sourceCategoryID,
				nil,
				100.00,
				domain.RolloverTypeFull,
				now.AddDate(0, -1, 0),
				now,
				domain.RolloverStatusApplied,
				now,
				now,
			))

	rollovers, err := repo.GetBySourceCategory(ctx, sourceCategoryID, 10, 0)

	assert.NoError(t, err)
	assert.Len(t, rollovers, 1)
	assert.Equal(t, sourceCategoryID, rollovers[0].SourceBudgetCategoryID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// =============================================================================
// CountPendingByUserID Tests
// =============================================================================

func TestRolloverHistoryRepository_CountPendingByUserID(t *testing.T) {
	db, mock, repo := setupRolloverHistoryMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT`)).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	count, err := repo.CountPendingByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Equal(t, 3, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}
