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

func setupCreditLiabilityMockDB(t *testing.T) (pgxmock.PgxPoolIface, CreditLiabilityRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	return mock, NewCreditLiabilityRepository(mock)
}

func makeCreditLiability(userID, accountID string) *domain.CreditLiability {
	isOverdue := false
	lastPayment := 250.00
	lastBalance := 1500.00
	minPayment := 25.00
	rate := 19.99
	now := time.Now()
	return &domain.CreditLiability{
		UserID:               userID,
		AccountID:            accountID,
		IsOverdue:            &isOverdue,
		LastPaymentAmount:    &lastPayment,
		LastPaymentDate:      &now,
		LastStatementBalance: &lastBalance,
		MinimumPaymentAmount: &minPayment,
		APRs: []domain.CreditLiabilityAPR{
			{
				RatePercentage: rate,
				RateType:       "variable",
			},
		},
	}
}

// TestCreditLiabilityRepository_ReplaceForAccount_Success verifies the atomic
// delete-then-insert pattern for credit liabilities and APRs within a transaction.
func TestCreditLiabilityRepository_ReplaceForAccount_Success(t *testing.T) {
	mock, repo := setupCreditLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	liability := makeCreditLiability(userID, accountID)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM credit_liability_aprs`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM credit_liabilities`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO credit_liabilities`)).
		WithArgs(anyArgs(13)...).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO credit_liability_aprs`)).
		WithArgs(anyArgs(8)...).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	err := repo.ReplaceForAccount(ctx, accountID, userID, liability)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreditLiabilityRepository_ReplaceForAccount_NoAPRs verifies replace with no APRs.
func TestCreditLiabilityRepository_ReplaceForAccount_NoAPRs(t *testing.T) {
	mock, repo := setupCreditLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	liability := &domain.CreditLiability{
		UserID:    userID,
		AccountID: accountID,
	}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM credit_liability_aprs`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM credit_liabilities`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO credit_liabilities`)).
		WithArgs(anyArgs(13)...).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	err := repo.ReplaceForAccount(ctx, accountID, userID, liability)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreditLiabilityRepository_ReplaceForAccount_RollbackOnError verifies rollback on failure.
func TestCreditLiabilityRepository_ReplaceForAccount_RollbackOnError(t *testing.T) {
	mock, repo := setupCreditLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	liability := makeCreditLiability(userID, accountID)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM credit_liability_aprs`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM credit_liabilities`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO credit_liabilities`)).
		WithArgs(anyArgs(13)...).
		WillReturnError(errors.New("db constraint error"))
	mock.ExpectRollback()

	err := repo.ReplaceForAccount(ctx, accountID, userID, liability)

	require.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreditLiabilityRepository_ReplaceForAccount_NilDeletesOnly verifies that passing nil
// liability performs a delete-only operation (no INSERT) and commits the transaction.
func TestCreditLiabilityRepository_ReplaceForAccount_NilDeletesOnly(t *testing.T) {
	mock, repo := setupCreditLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM credit_liability_aprs`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM credit_liabilities`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectCommit()

	err := repo.ReplaceForAccount(ctx, accountID, userID, nil)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreditLiabilityRepository_GetByUserID verifies retrieval of credit liabilities for a user.
func TestCreditLiabilityRepository_GetByUserID(t *testing.T) {
	mock, repo := setupCreditLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	now := time.Now()

	isOverdue := false
	lastPayment := 250.00
	lastBalance := 1500.00
	minPayment := 25.00

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "is_overdue",
		"last_payment_amount", "last_payment_date",
		"last_statement_issue_date", "last_statement_balance",
		"minimum_payment_amount", "next_payment_due_date",
		"synced_at", "created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), userID, accountID, &isOverdue,
		&lastPayment, &now,
		&now, &lastBalance,
		&minPayment, &now,
		now, now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID).
		WillReturnRows(rows)

	liabilities, err := repo.GetByUserID(ctx, userID)

	require.NoError(t, err)
	require.Len(t, liabilities, 1)
	assert.Equal(t, userID, liabilities[0].UserID)
	assert.Equal(t, accountID, liabilities[0].AccountID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreditLiabilityRepository_GetByUserID_Empty verifies empty slice returned when no rows.
func TestCreditLiabilityRepository_GetByUserID_Empty(t *testing.T) {
	mock, repo := setupCreditLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "is_overdue",
		"last_payment_amount", "last_payment_date",
		"last_statement_issue_date", "last_statement_balance",
		"minimum_payment_amount", "next_payment_due_date",
		"synced_at", "created_at", "updated_at",
	})

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID).
		WillReturnRows(rows)

	liabilities, err := repo.GetByUserID(ctx, userID)

	require.NoError(t, err)
	assert.Empty(t, liabilities)
	assert.NotNil(t, liabilities)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreditLiabilityRepository_GetByAccountID verifies retrieval for a specific account.
func TestCreditLiabilityRepository_GetByAccountID(t *testing.T) {
	mock, repo := setupCreditLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	now := time.Now()

	isOverdue := true
	lastPayment := 100.00
	lastBalance := 800.00
	minPayment := 15.00

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "is_overdue",
		"last_payment_amount", "last_payment_date",
		"last_statement_issue_date", "last_statement_balance",
		"minimum_payment_amount", "next_payment_due_date",
		"synced_at", "created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), userID, accountID, &isOverdue,
		&lastPayment, &now,
		&now, &lastBalance,
		&minPayment, &now,
		now, now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(accountID).
		WillReturnRows(rows)

	liability, err := repo.GetByAccountID(ctx, accountID)

	require.NoError(t, err)
	require.NotNil(t, liability)
	assert.Equal(t, accountID, liability.AccountID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreditLiabilityRepository_GetByAccountID_NotFound verifies nil returned when not found.
func TestCreditLiabilityRepository_GetByAccountID_NotFound(t *testing.T) {
	mock, repo := setupCreditLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "is_overdue",
		"last_payment_amount", "last_payment_date",
		"last_statement_issue_date", "last_statement_balance",
		"minimum_payment_amount", "next_payment_due_date",
		"synced_at", "created_at", "updated_at",
	})

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(accountID).
		WillReturnRows(rows)

	liability, err := repo.GetByAccountID(ctx, accountID)

	require.NoError(t, err)
	assert.Nil(t, liability)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// Chain-aware tests for Issue #1109

func TestCreditLiabilityRepository_GetByUserID_ActiveAccountsOnly(t *testing.T) {
	// RED: GetByUserID must JOIN accounts and only return liabilities for active accounts
	mock, repo := setupCreditLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	now := time.Now()

	isOverdue := false
	lastPayment := 250.00
	lastBalance := 1500.00
	minPayment := 25.00

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "is_overdue",
		"last_payment_amount", "last_payment_date",
		"last_statement_issue_date", "last_statement_balance",
		"minimum_payment_amount", "next_payment_due_date",
		"synced_at", "created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), userID, accountID, &isOverdue,
		&lastPayment, &now,
		&now, &lastBalance,
		&minPayment, &now,
		now, now, now,
	)

	// The query must JOIN accounts and filter status='active'
	mock.ExpectQuery(regexp.QuoteMeta(`JOIN accounts a ON a.id = credit_liabilities.account_id`)).
		WithArgs(userID).
		WillReturnRows(rows)

	liabilities, err := repo.GetByUserID(ctx, userID)

	require.NoError(t, err)
	require.Len(t, liabilities, 1)
	assert.Equal(t, accountID, liabilities[0].AccountID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCreditLiabilityRepository_GetByUserID_ReplacedAccountExcluded(t *testing.T) {
	// RED: Replaced credit liability accounts must NOT appear in GetByUserID
	mock, repo := setupCreditLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "is_overdue",
		"last_payment_amount", "last_payment_date",
		"last_statement_issue_date", "last_statement_balance",
		"minimum_payment_amount", "next_payment_due_date",
		"synced_at", "created_at", "updated_at",
	})

	mock.ExpectQuery(regexp.QuoteMeta(`JOIN accounts a ON a.id = credit_liabilities.account_id`)).
		WithArgs(userID).
		WillReturnRows(rows)

	liabilities, err := repo.GetByUserID(ctx, userID)

	require.NoError(t, err)
	assert.Empty(t, liabilities)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreditLiabilityRepository_GetByAccountID_PrefersActiveRow verifies that when
// both a replaced and an active chain member have a liability row, GetByAccountID
// returns the active row. The query must include ORDER BY active-first so the
// result is deterministic. Issue #1109 W4-A.
func TestCreditLiabilityRepository_GetByAccountID_PrefersActiveRow(t *testing.T) {
	mock, repo := setupCreditLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	activeAccountID := uuid.New().String()
	replacedAccountID := uuid.New().String()
	now := time.Now()

	isOverdue := false
	lastPayment := 300.00
	lastBalance := 2000.00
	minPayment := 30.00

	cols := []string{
		"id", "user_id", "account_id", "is_overdue",
		"last_payment_amount", "last_payment_date",
		"last_statement_issue_date", "last_statement_balance",
		"minimum_payment_amount", "next_payment_due_date",
		"synced_at", "created_at", "updated_at",
	}

	// Query must include ORDER BY so the active row is returned first when both
	// replaced and active rows exist in the chain.
	mock.ExpectQuery("ORDER BY").
		WithArgs(activeAccountID).
		WillReturnRows(pgxmock.NewRows(cols).
			AddRow( // active row returned first by ORDER BY
				uuid.New().String(), userID, activeAccountID, &isOverdue,
				&lastPayment, &now,
				&now, &lastBalance,
				&minPayment, &now,
				now, now, now,
			).
			AddRow( // replaced row — must NOT be returned
				uuid.New().String(), userID, replacedAccountID, &isOverdue,
				&lastPayment, &now,
				&now, &lastBalance,
				&minPayment, &now,
				now, now, now,
			))

	result, err := repo.GetByAccountID(ctx, activeAccountID)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, activeAccountID, result.AccountID,
		"active row must be returned first when both replaced and active rows exist")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestCreditLiabilityRepository_GetByAccountID_ChainExpand verifies that filtering
// by active account_id returns liabilities recorded under a replaced account that
// shares the same user_account_id. Issue #1109: B.4 fix.
func TestCreditLiabilityRepository_GetByAccountID_ChainExpand(t *testing.T) {
	mock, repo := setupCreditLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	activeAccountID := uuid.New().String()
	replacedAccountID := uuid.New().String()
	now := time.Now()

	isOverdue := false
	lastPayment := 300.00
	lastBalance := 2000.00
	minPayment := 30.00

	// Liability was recorded under the replaced account but must be returned
	// when the caller queries by the active account_id via chain-expansion.
	mock.ExpectQuery("account_id IN.*user_account_id.*ORDER BY").
		WithArgs(activeAccountID).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "user_id", "account_id", "is_overdue",
			"last_payment_amount", "last_payment_date",
			"last_statement_issue_date", "last_statement_balance",
			"minimum_payment_amount", "next_payment_due_date",
			"synced_at", "created_at", "updated_at",
		}).AddRow(
			uuid.New().String(), userID, replacedAccountID, &isOverdue,
			&lastPayment, &now,
			&now, &lastBalance,
			&minPayment, &now,
			now, now, now,
		))

	result, err := repo.GetByAccountID(ctx, activeAccountID)
	require.NoError(t, err)
	require.NotNil(t, result, "pre-relink liability must be returned when filtering by active account_id")
	assert.Equal(t, replacedAccountID, result.AccountID)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestCreditLiabilityRepository_ReplaceForAccount_RejectsForeignLiability proves the repository
// itself refuses a liability owned by someone other than the acting user (#1566). The DELETE was
// scoped to userID while the INSERT wrote liability.UserID.
func TestCreditLiabilityRepository_ReplaceForAccount_RejectsForeignLiability(t *testing.T) {
	mock, repo := setupCreditLiabilityMockDB(t)
	defer mock.Close()

	accountID := uuid.New().String()
	actor := uuid.New().String()
	liability := makeCreditLiability(uuid.New().String(), accountID) // owned by someone else

	err := repo.ReplaceForAccount(context.Background(), accountID, actor, liability)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrReplaceTenantMismatch)
	assert.Contains(t, err.Error(), "creditLiabilityRepository.ReplaceForAccount")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreditLiabilityRepository_ReplaceForAccount_RejectsEmptyActor covers both the vacuous
// empty-vs-empty match and the nil (delete-only) path, which is still tenant-scoped.
func TestCreditLiabilityRepository_ReplaceForAccount_RejectsEmptyActor(t *testing.T) {
	for name, liability := range map[string]*domain.CreditLiability{
		"unowned liability": makeCreditLiability("", uuid.New().String()),
		"delete only":       nil,
	} {
		t.Run(name, func(t *testing.T) {
			mock, repo := setupCreditLiabilityMockDB(t)
			defer mock.Close()

			err := repo.ReplaceForAccount(context.Background(), uuid.New().String(), "", liability)

			require.Error(t, err)
			assert.ErrorIs(t, err, ErrReplaceTenantMismatch)
			assert.Contains(t, err.Error(), "no acting user_id")
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestCreditLiabilityRepository_ReplaceForAccount_RejectsForeignAccount proves the account half of the DELETE/INSERT
// split is enforced too (#1568): the DELETE is scoped to the scalar accountID while the INSERT
// writes liability.AccountID, so replacing account A with a credit liability carrying account B cleared A
// and attributed the row to B. Same tenant, so not a leak — but the row moved between the user's
// own accounts, which "replace for account" must never do.
func TestCreditLiabilityRepository_ReplaceForAccount_RejectsForeignAccount(t *testing.T) {
	mock, repo := setupCreditLiabilityMockDB(t)
	defer mock.Close()

	userID := uuid.New().String()
	accountA := uuid.New().String()
	accountB := uuid.New().String()
	liability := makeCreditLiability(userID, accountB) // belongs to a different account of the same user

	// No mock expectations: not a single statement may run.
	err := repo.ReplaceForAccount(context.Background(), accountA, userID, liability)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrReplaceAccountMismatch)
	assert.Contains(t, err.Error(), "creditLiabilityRepository.ReplaceForAccount")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreditLiabilityRepository_ReplaceForAccount_RejectsEmptyAccountID closes the vacuous-match hole on the account
// axis and covers the nil (delete-only) path, whose DELETE is itself scoped by account_id.
func TestCreditLiabilityRepository_ReplaceForAccount_RejectsEmptyAccountID(t *testing.T) {
	for name, liability := range map[string]*domain.CreditLiability{
		"unattributed liability": makeCreditLiability("u", ""),
		"delete only":            nil,
	} {
		t.Run(name, func(t *testing.T) {
			mock, repo := setupCreditLiabilityMockDB(t)
			defer mock.Close()

			err := repo.ReplaceForAccount(context.Background(), "", "u", liability)

			require.Error(t, err)
			assert.ErrorIs(t, err, ErrReplaceAccountMismatch)
			assert.Contains(t, err.Error(), "no account_id")
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
