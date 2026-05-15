package repository

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPaddleTransactionMockDB(t *testing.T) (pgxmock.PgxPoolIface, PaddleTransactionRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	enc := newTestFieldEncryptor(t)
	return mock, NewPaddleTransactionRepository(mock, enc)
}

func newTestPaddleTransaction() *domain.PaddleTransaction {
	subID := "sub_01h1234567890abcdef"
	custID := "ctm_01h1234567890abcdef"
	methodType := "card"
	billingStart := time.Now().Add(-30 * 24 * time.Hour)
	billingEnd := time.Now().Add(1 * 24 * time.Hour)
	return &domain.PaddleTransaction{
		UserID:             "user_2abc123def456",
		TransactionID:      "txn_01h1234567890completed",
		SubscriptionID:     &subID,
		CustomerID:         &custID,
		Amount:             8.99,
		Currency:           "USD",
		Status:             "completed",
		PaymentMethodType:  &methodType,
		BillingPeriodStart: &billingStart,
		BillingPeriodEnd:   &billingEnd,
		Payload:            json.RawMessage(`{"id":"txn_01h1234567890completed"}`),
	}
}

// TestPaddleTransactionCreate_Success tests successful transaction creation.
func TestPaddleTransactionCreate_Success(t *testing.T) {
	mock, repo := setupPaddleTransactionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now()
	txn := newTestPaddleTransaction()

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO paddle_transactions`)).
		WithArgs(
			pgxmock.AnyArg(), // id (generated UUID)
			txn.UserID,
			pgxmock.AnyArg(), // encrypted transaction_id
			pgxmock.AnyArg(), // transaction_id_blind_idx
			pgxmock.AnyArg(), // encrypted subscription_id
			pgxmock.AnyArg(), // encrypted customer_id
			txn.Amount,
			txn.Currency,
			txn.Status,
			txn.PaymentMethodType,
			txn.BillingPeriodStart,
			txn.BillingPeriodEnd,
			pgxmock.AnyArg(), // encrypted payload
		).
		WillReturnRows(pgxmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))

	err := repo.Create(ctx, txn)

	require.NoError(t, err)
	assert.NotEmpty(t, txn.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestPaddleTransactionCreate_GeneratesID tests that a UUID is generated when ID is empty.
func TestPaddleTransactionCreate_GeneratesID(t *testing.T) {
	mock, repo := setupPaddleTransactionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now()
	txn := newTestPaddleTransaction()
	txn.ID = "" // ensure ID is empty to trigger generation

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO paddle_transactions`)).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))

	err := repo.Create(ctx, txn)

	require.NoError(t, err)
	assert.NotEmpty(t, txn.ID)
	_, parseErr := uuid.Parse(txn.ID)
	assert.NoError(t, parseErr, "generated ID should be a valid UUID")
}

// TestPaddleTransactionCreate_Duplicate tests duplicate transaction_id error.
func TestPaddleTransactionCreate_Duplicate(t *testing.T) {
	mock, repo := setupPaddleTransactionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	txn := newTestPaddleTransaction()

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO paddle_transactions`)).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(&pgconn.PgError{Code: "23505"})

	err := repo.Create(ctx, txn)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPaddleTransactionDuplicate)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestPaddleTransactionGetByTransactionID_Success tests successful lookup.
func TestPaddleTransactionGetByTransactionID_Success(t *testing.T) {
	mock, repo := setupPaddleTransactionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now()
	txn := newTestPaddleTransaction()
	txn.ID = uuid.New().String()

	// DB stores encrypted values; repo decrypts on read
	encTransactionID := "ENC$TENC[" + txn.TransactionID + "]"
	encSubscriptionID := "ENC$TENC[" + *txn.SubscriptionID + "]"
	encCustomerID := "ENC$TENC[" + *txn.CustomerID + "]"

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "transaction_id", "subscription_id", "customer_id",
		"amount", "currency", "status", "payment_method_type",
		"billing_period_start", "billing_period_end", "payload",
		"created_at", "updated_at",
	}).AddRow(
		txn.ID, txn.UserID, encTransactionID, &encSubscriptionID, &encCustomerID,
		txn.Amount, txn.Currency, txn.Status, txn.PaymentMethodType,
		txn.BillingPeriodStart, txn.BillingPeriodEnd, `ENC$TENC[{"id":"txn_01h1234567890completed"}]`,
		now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, transaction_id`)).
		WithArgs(pgxmock.AnyArg()). // blind index value
		WillReturnRows(rows)

	result, err := repo.GetByTransactionID(ctx, txn.TransactionID)

	require.NoError(t, err)
	assert.Equal(t, txn.ID, result.ID)
	assert.Equal(t, txn.TransactionID, result.TransactionID) // decrypted
	assert.Equal(t, txn.Amount, result.Amount)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestPaddleTransactionGetByTransactionID_NotFound tests not-found error.
func TestPaddleTransactionGetByTransactionID_NotFound(t *testing.T) {
	mock, repo := setupPaddleTransactionMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, transaction_id`)).
		WithArgs(pgxmock.AnyArg()). // blind index value
		WillReturnRows(pgxmock.NewRows([]string{})) // empty result set triggers ErrNoRows

	result, err := repo.GetByTransactionID(ctx, "txn_nonexistent")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPaddleTransactionNotFound)
	assert.Nil(t, result)
}

// TestPaddleTransactionGetByUserID_Success tests successful user transaction listing.
func TestPaddleTransactionGetByUserID_Success(t *testing.T) {
	mock, repo := setupPaddleTransactionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now()
	userID := "user_2abc123def456"
	txn := newTestPaddleTransaction()
	txn.ID = uuid.New().String()

	encTransactionID := "ENC$TENC[" + txn.TransactionID + "]"
	encSubscriptionID := "ENC$TENC[" + *txn.SubscriptionID + "]"
	encCustomerID := "ENC$TENC[" + *txn.CustomerID + "]"

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "transaction_id", "subscription_id", "customer_id",
		"amount", "currency", "status", "payment_method_type",
		"billing_period_start", "billing_period_end", "payload",
		"created_at", "updated_at",
	}).AddRow(
		txn.ID, txn.UserID, encTransactionID, &encSubscriptionID, &encCustomerID,
		txn.Amount, txn.Currency, txn.Status, txn.PaymentMethodType,
		txn.BillingPeriodStart, txn.BillingPeriodEnd, `ENC$TENC[{"id":"txn_01h1234567890completed"}]`,
		now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, transaction_id`)).
		WithArgs(userID).
		WillReturnRows(rows)

	results, err := repo.GetByUserID(ctx, userID)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, txn.TransactionID, results[0].TransactionID) // decrypted
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestPaddleTransactionGetByUserID_Empty tests empty result for user with no transactions.
func TestPaddleTransactionGetByUserID_Empty(t *testing.T) {
	mock, repo := setupPaddleTransactionMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, transaction_id`)).
		WithArgs("user_no_transactions").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "user_id", "transaction_id", "subscription_id", "customer_id",
			"amount", "currency", "status", "payment_method_type",
			"billing_period_start", "billing_period_end", "payload",
			"created_at", "updated_at",
		})) // empty rows

	results, err := repo.GetByUserID(ctx, "user_no_transactions")

	require.NoError(t, err)
	assert.Empty(t, results)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetFailedTransactions_Success tests retrieving failed/past_due/canceled transactions.
func TestGetFailedTransactions_Success(t *testing.T) {
	mock, repo := setupPaddleTransactionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now()
	txn := newTestPaddleTransaction()
	txn.ID = uuid.New().String()
	txn.Status = "failed"

	encTransactionID := "ENC$TENC[" + txn.TransactionID + "]"
	encSubscriptionID := "ENC$TENC[" + *txn.SubscriptionID + "]"
	encCustomerID := "ENC$TENC[" + *txn.CustomerID + "]"

	dataRows := pgxmock.NewRows([]string{
		"id", "user_id", "transaction_id", "subscription_id", "customer_id",
		"amount", "currency", "status", "payment_method_type",
		"billing_period_start", "billing_period_end", "payload",
		"created_at", "updated_at",
	}).AddRow(
		txn.ID, txn.UserID, encTransactionID, &encSubscriptionID, &encCustomerID,
		txn.Amount, txn.Currency, txn.Status, txn.PaymentMethodType,
		txn.BillingPeriodStart, txn.BillingPeriodEnd, `ENC$TENC[{"id":"txn_01h1234567890completed"}]`,
		now, now,
	)

	countRows := pgxmock.NewRows([]string{"count"}).AddRow(1)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM paddle_transactions`)).
		WillReturnRows(countRows)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, transaction_id`)).
		WithArgs(10, 0).
		WillReturnRows(dataRows)

	results, total, err := repo.GetFailedTransactions(ctx, 10, 0)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, 1, total)
	assert.Equal(t, "failed", results[0].Status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetFailedTransactions_Empty tests empty result when no failed transactions exist.
func TestGetFailedTransactions_Empty(t *testing.T) {
	mock, repo := setupPaddleTransactionMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	countRows := pgxmock.NewRows([]string{"count"}).AddRow(0)
	dataRows := pgxmock.NewRows([]string{
		"id", "user_id", "transaction_id", "subscription_id", "customer_id",
		"amount", "currency", "status", "payment_method_type",
		"billing_period_start", "billing_period_end", "payload",
		"created_at", "updated_at",
	}) // empty — no rows

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM paddle_transactions`)).
		WillReturnRows(countRows)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, transaction_id`)).
		WithArgs(10, 0).
		WillReturnRows(dataRows)

	results, total, err := repo.GetFailedTransactions(ctx, 10, 0)

	require.NoError(t, err)
	assert.Empty(t, results)
	assert.Equal(t, 0, total)
	assert.NoError(t, mock.ExpectationsWereMet())
}
