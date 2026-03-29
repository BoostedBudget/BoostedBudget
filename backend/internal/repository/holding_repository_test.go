package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupHoldingMockDB(t *testing.T) (pgxmock.PgxPoolIface, HoldingRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	return mock, NewHoldingRepository(mock)
}

func makeTestHolding(userID, accountID, securityID string) domain.Holding {
	costBasis := 1500.00
	instValue := 1650.00
	instPrice := 165.00
	return domain.Holding{
		UserID:           userID,
		AccountID:        accountID,
		SecurityID:       securityID,
		Quantity:         10.0,
		CostBasis:        &costBasis,
		InstitutionValue: &instValue,
		InstitutionPrice: &instPrice,
	}
}

// TestHoldingRepository_ReplaceForAccount_DeletesOldInsertNew verifies the atomic
// delete-then-insert pattern within a transaction.
func TestHoldingRepository_ReplaceForAccount_DeletesOldInsertNew(t *testing.T) {
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	holding := makeTestHolding(userID, accountID, uuid.New().String())

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM holdings`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 2))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO holdings`)).
		WithArgs(anyArgs(10)...).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	err := repo.ReplaceForAccount(ctx, accountID, userID, []domain.Holding{holding})

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestHoldingRepository_ReplaceForAccount_EmptyHoldings verifies that passing an empty
// holdings slice still deletes existing holdings (account was fully liquidated).
func TestHoldingRepository_ReplaceForAccount_EmptyHoldings(t *testing.T) {
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM holdings`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 3))
	mock.ExpectCommit()

	err := repo.ReplaceForAccount(ctx, accountID, userID, []domain.Holding{})

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestHoldingRepository_ReplaceForAccount_RollbackOnInsertError verifies that if
// the insert fails, the transaction is rolled back (preserving old holdings).
func TestHoldingRepository_ReplaceForAccount_RollbackOnInsertError(t *testing.T) {
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	holding := makeTestHolding(userID, accountID, uuid.New().String())

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM holdings`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO holdings`)).
		WithArgs(anyArgs(10)...).
		WillReturnError(errors.New("db constraint error"))
	mock.ExpectRollback()

	err := repo.ReplaceForAccount(ctx, accountID, userID, []domain.Holding{holding})

	require.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestHoldingRepository_GetByUserID verifies retrieval of all holdings for a user.
func TestHoldingRepository_GetByUserID(t *testing.T) {
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	secID := uuid.New().String()
	now := time.Now()

	costBasis := 1500.00
	instValue := 1650.00
	instPrice := 165.00

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "security_id",
		"quantity", "cost_basis", "institution_value", "institution_price",
		"created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), userID, accountID, secID,
		10.0, &costBasis, &instValue, &instPrice,
		now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID).
		WillReturnRows(rows)

	holdings, err := repo.GetByUserID(ctx, userID)

	require.NoError(t, err)
	require.Len(t, holdings, 1)
	assert.Equal(t, userID, holdings[0].UserID)
	assert.Equal(t, 10.0, holdings[0].Quantity)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestHoldingRepository_GetByAccountID(t *testing.T) {
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	secID := uuid.New().String()
	now := time.Now()

	costBasis := 500.00
	instValue := 600.00
	instPrice := 60.00

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "security_id",
		"quantity", "cost_basis", "institution_value", "institution_price",
		"created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), userID, accountID, secID,
		5.0, &costBasis, &instValue, &instPrice,
		now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(accountID).
		WillReturnRows(rows)

	holdings, err := repo.GetByAccountID(ctx, accountID)

	require.NoError(t, err)
	require.Len(t, holdings, 1)
	assert.Equal(t, accountID, holdings[0].AccountID)
	assert.Equal(t, 5.0, holdings[0].Quantity)
	assert.NoError(t, mock.ExpectationsWereMet())
}
