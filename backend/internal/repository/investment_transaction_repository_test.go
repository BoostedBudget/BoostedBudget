package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupInvTxnMockDB(t *testing.T) (pgxmock.PgxPoolIface, InvestmentTransactionRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	return mock, NewInvestmentTransactionRepository(mock)
}

func makeTestInvTxn(userID, accountID string) domain.InvestmentTransaction {
	secID := uuid.New().String()
	qty := 10.0
	price := 150.00
	fees := 1.00
	subtype := "buy"
	return domain.InvestmentTransaction{
		UserID:                       userID,
		AccountID:                    accountID,
		SecurityID:                   &secID,
		PlaidInvestmentTransactionID: "plaid-inv-txn-" + uuid.New().String(),
		Type:                         "buy",
		Subtype:                      &subtype,
		Name:                         "Apple Inc.",
		Quantity:                     &qty,
		Amount:                       1500.00,
		Price:                        &price,
		Fees:                         &fees,
		Date:                         time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
	}
}

// TestInvestmentTransactionRepository_UpsertBatch_InsertNew verifies new transactions
// are inserted and the correct inserted count is returned.
func TestInvestmentTransactionRepository_UpsertBatch_InsertNew(t *testing.T) {
	mock, repo := setupInvTxnMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	txn := makeTestInvTxn(userID, accountID)

	// CTE returns 1 row inserted
	rows := pgxmock.NewRows([]string{"inserted"}).AddRow(1)
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO investment_transactions`)).
		WithArgs(anyArgs(14)...).
		WillReturnRows(rows)

	inserted, err := repo.UpsertBatch(ctx, []domain.InvestmentTransaction{txn})

	require.NoError(t, err)
	assert.Equal(t, 1, inserted)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestInvestmentTransactionRepository_UpsertBatch_SkipDuplicates verifies that
// transactions with existing plaid_investment_transaction_ids are silently skipped
// (ON CONFLICT DO NOTHING) and 0 is returned.
func TestInvestmentTransactionRepository_UpsertBatch_SkipDuplicates(t *testing.T) {
	mock, repo := setupInvTxnMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	txn := makeTestInvTxn(userID, accountID)

	// ON CONFLICT DO NOTHING — 0 rows inserted (already exists)
	rows := pgxmock.NewRows([]string{"inserted"}).AddRow(0)
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO investment_transactions`)).
		WithArgs(anyArgs(14)...).
		WillReturnRows(rows)

	inserted, err := repo.UpsertBatch(ctx, []domain.InvestmentTransaction{txn})

	require.NoError(t, err)
	assert.Equal(t, 0, inserted)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestInvestmentTransactionRepository_UpsertBatch_MixedNewAndDuplicate verifies that
// when a batch contains both new and duplicate transactions, only new ones are counted.
// Implementation processes each transaction individually (one query per txn).
func TestInvestmentTransactionRepository_UpsertBatch_MixedNewAndDuplicate(t *testing.T) {
	mock, repo := setupInvTxnMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	txns := []domain.InvestmentTransaction{
		makeTestInvTxn(userID, accountID),
		makeTestInvTxn(userID, accountID),
		makeTestInvTxn(userID, accountID),
	}

	// txn[0] = new, txn[1] = duplicate, txn[2] = new
	r0 := pgxmock.NewRows([]string{"inserted"}).AddRow(1)
	r1 := pgxmock.NewRows([]string{"inserted"}).AddRow(0)
	r2 := pgxmock.NewRows([]string{"inserted"}).AddRow(1)
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO investment_transactions`)).WithArgs(anyArgs(14)...).WillReturnRows(r0)
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO investment_transactions`)).WithArgs(anyArgs(14)...).WillReturnRows(r1)
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO investment_transactions`)).WithArgs(anyArgs(14)...).WillReturnRows(r2)

	inserted, err := repo.UpsertBatch(ctx, txns)

	require.NoError(t, err)
	assert.Equal(t, 2, inserted) // 2 new, 1 duplicate skipped
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestInvestmentTransactionRepository_UpsertBatch_Empty verifies empty slice returns 0
// without making any DB call.
func TestInvestmentTransactionRepository_UpsertBatch_Empty(t *testing.T) {
	mock, repo := setupInvTxnMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	inserted, err := repo.UpsertBatch(ctx, []domain.InvestmentTransaction{})

	require.NoError(t, err)
	assert.Equal(t, 0, inserted)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestInvestmentTransactionRepository_GetByUserID verifies paginated retrieval by user.
func TestInvestmentTransactionRepository_GetByUserID(t *testing.T) {
	mock, repo := setupInvTxnMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	secID := uuid.New().String()
	txnDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	qty := 10.0
	price := 150.00
	subtype := "buy"

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "security_id",
		"plaid_investment_transaction_id",
		"type", "subtype", "name",
		"quantity", "amount", "price", "fees", "date",
		"created_at",
	}).AddRow(
		uuid.New().String(), userID, accountID, &secID,
		"plaid-inv-txn-001",
		"buy", &subtype, "Apple Inc.",
		&qty, 1500.00, &price, (*float64)(nil), txnDate,
		txnDate,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID, 10, 0).
		WillReturnRows(rows)

	txns, err := repo.GetByUserID(ctx, userID, 10, 0)

	require.NoError(t, err)
	require.Len(t, txns, 1)
	assert.Equal(t, userID, txns[0].UserID)
	assert.Equal(t, "buy", txns[0].Type)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestInvestmentTransactionRepository_GetByAccountID verifies retrieval by account.
func TestInvestmentTransactionRepository_GetByAccountID(t *testing.T) {
	mock, repo := setupInvTxnMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	secID := uuid.New().String()
	txnDate := time.Date(2024, 2, 10, 0, 0, 0, 0, time.UTC)
	qty := 5.0
	price := 200.00
	subtype := "sell"

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "security_id",
		"plaid_investment_transaction_id",
		"type", "subtype", "name",
		"quantity", "amount", "price", "fees", "date",
		"created_at",
	}).AddRow(
		uuid.New().String(), userID, accountID, &secID,
		"plaid-inv-txn-002",
		"sell", &subtype, "Tesla Inc.",
		&qty, 1000.00, &price, (*float64)(nil), txnDate,
		txnDate,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(accountID).
		WillReturnRows(rows)

	txns, err := repo.GetByAccountID(ctx, accountID)

	require.NoError(t, err)
	require.Len(t, txns, 1)
	assert.Equal(t, accountID, txns[0].AccountID)
	assert.Equal(t, "sell", txns[0].Type)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestInvestmentTransactionRepository_UpsertBatch_NullSecurityID verifies that
// cash transactions with nil security_id are stored correctly.
func TestInvestmentTransactionRepository_UpsertBatch_NullSecurityID(t *testing.T) {
	mock, repo := setupInvTxnMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	cashTxn := domain.InvestmentTransaction{
		UserID:                       uuid.New().String(),
		AccountID:                    uuid.New().String(),
		SecurityID:                   nil, // cash transaction
		PlaidInvestmentTransactionID: "plaid-cash-001",
		Type:                         "cash",
		Name:                         "Cash Deposit",
		Amount:                       500.00,
		Date:                         time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC),
	}

	rows := pgxmock.NewRows([]string{"inserted"}).AddRow(1)
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO investment_transactions`)).
		WithArgs(anyArgs(14)...).
		WillReturnRows(rows)

	inserted, err := repo.UpsertBatch(ctx, []domain.InvestmentTransaction{cashTxn})

	require.NoError(t, err)
	assert.Equal(t, 1, inserted)
	assert.NoError(t, mock.ExpectationsWereMet())
}
