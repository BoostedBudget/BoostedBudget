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

func setupMortgageLiabilityMockDB(t *testing.T) (pgxmock.PgxPoolIface, MortgageLiabilityRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	return mock, NewMortgageLiabilityRepository(mock)
}

func makeMortgageLiability(userID, accountID string) *domain.MortgageLiability {
	acctNum := "MORT-001"
	lateeFee := 25.00
	escrow := 5000.00
	hasPMI := false
	hasPrepay := false
	rate := 3.75
	rateType := "fixed"
	lastPayment := 1800.00
	loanType := "conventional"
	loanTerm := "30 year"
	nextMonthly := 1800.00
	origPrincipal := 300000.00
	pastDue := 0.00
	street := "123 Main St"
	city := "Springfield"
	region := "IL"
	postal := "62701"
	country := "US"
	ytdInterest := 8000.00
	ytdPrincipal := 13600.00
	now := time.Now()
	return &domain.MortgageLiability{
		UserID:                     userID,
		AccountID:                  accountID,
		AccountNumber:              &acctNum,
		CurrentLateFee:             &lateeFee,
		EscrowBalance:              &escrow,
		HasPMI:                     &hasPMI,
		HasPrepaymentPenalty:       &hasPrepay,
		InterestRatePercentage:     &rate,
		InterestRateType:           &rateType,
		LastPaymentAmount:          &lastPayment,
		LastPaymentDate:            &now,
		LoanTypeDescription:        &loanType,
		LoanTerm:                   &loanTerm,
		NextMonthlyPayment:         &nextMonthly,
		NextPaymentDueDate:         &now,
		OriginationDate:            &now,
		OriginationPrincipalAmount: &origPrincipal,
		PastDueAmount:              &pastDue,
		PropertyAddressStreet:      &street,
		PropertyAddressCity:        &city,
		PropertyAddressRegion:      &region,
		PropertyAddressPostalCode:  &postal,
		PropertyAddressCountry:     &country,
		YTDInterestPaid:            &ytdInterest,
		YTDPrincipalPaid:           &ytdPrincipal,
	}
}

// TestMortgageLiabilityRepository_ReplaceForAccount_Success verifies the atomic
// delete-then-insert pattern for mortgage liabilities within a transaction.
func TestMortgageLiabilityRepository_ReplaceForAccount_Success(t *testing.T) {
	mock, repo := setupMortgageLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	liability := makeMortgageLiability(userID, accountID)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM mortgage_liabilities`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO mortgage_liabilities`)).
		WithArgs(anyArgs(30)...).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	err := repo.ReplaceForAccount(ctx, accountID, userID, liability)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMortgageLiabilityRepository_ReplaceForAccount_NilDeletesOnly verifies that passing nil
// performs a delete-only operation and commits without an INSERT.
func TestMortgageLiabilityRepository_ReplaceForAccount_NilDeletesOnly(t *testing.T) {
	mock, repo := setupMortgageLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM mortgage_liabilities`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectCommit()

	err := repo.ReplaceForAccount(ctx, accountID, userID, nil)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMortgageLiabilityRepository_ReplaceForAccount_RollbackOnError verifies rollback on failure.
func TestMortgageLiabilityRepository_ReplaceForAccount_RollbackOnError(t *testing.T) {
	mock, repo := setupMortgageLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	liability := makeMortgageLiability(userID, accountID)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM mortgage_liabilities`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO mortgage_liabilities`)).
		WithArgs(anyArgs(30)...).
		WillReturnError(errors.New("db constraint error"))
	mock.ExpectRollback()

	err := repo.ReplaceForAccount(ctx, accountID, userID, liability)

	require.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMortgageLiabilityRepository_GetByUserID verifies retrieval of mortgage liabilities for a user.
func TestMortgageLiabilityRepository_GetByUserID(t *testing.T) {
	mock, repo := setupMortgageLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	now := time.Now()

	acctNum := "MORT-001"
	rate := 3.75
	rateType := "fixed"
	lastPayment := 1800.00
	nextMonthly := 1800.00

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "account_number",
		"current_late_fee", "escrow_balance", "has_pmi", "has_prepayment_penalty",
		"interest_rate_percentage", "interest_rate_type",
		"last_payment_amount", "last_payment_date",
		"loan_type_description", "loan_term", "maturity_date",
		"next_monthly_payment", "next_payment_due_date",
		"origination_date", "origination_principal_amount",
		"past_due_amount",
		"property_address_street", "property_address_city",
		"property_address_region", "property_address_postal_code", "property_address_country",
		"ytd_interest_paid", "ytd_principal_paid",
		"synced_at", "created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), userID, accountID, &acctNum,
		nil, nil, nil, nil,
		&rate, &rateType,
		&lastPayment, &now,
		nil, nil, nil,
		&nextMonthly, &now,
		nil, nil,
		nil,
		nil, nil,
		nil, nil, nil,
		nil, nil,
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

// TestMortgageLiabilityRepository_GetByUserID_Empty verifies empty slice returned when no rows.
func TestMortgageLiabilityRepository_GetByUserID_Empty(t *testing.T) {
	mock, repo := setupMortgageLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "account_number",
		"current_late_fee", "escrow_balance", "has_pmi", "has_prepayment_penalty",
		"interest_rate_percentage", "interest_rate_type",
		"last_payment_amount", "last_payment_date",
		"loan_type_description", "loan_term", "maturity_date",
		"next_monthly_payment", "next_payment_due_date",
		"origination_date", "origination_principal_amount",
		"past_due_amount",
		"property_address_street", "property_address_city",
		"property_address_region", "property_address_postal_code", "property_address_country",
		"ytd_interest_paid", "ytd_principal_paid",
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

// TestMortgageLiabilityRepository_GetByAccountID verifies retrieval for a specific account.
func TestMortgageLiabilityRepository_GetByAccountID(t *testing.T) {
	mock, repo := setupMortgageLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	now := time.Now()

	acctNum := "MORT-002"
	rate := 4.25
	rateType := "variable"
	lastPayment := 2000.00

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "account_number",
		"current_late_fee", "escrow_balance", "has_pmi", "has_prepayment_penalty",
		"interest_rate_percentage", "interest_rate_type",
		"last_payment_amount", "last_payment_date",
		"loan_type_description", "loan_term", "maturity_date",
		"next_monthly_payment", "next_payment_due_date",
		"origination_date", "origination_principal_amount",
		"past_due_amount",
		"property_address_street", "property_address_city",
		"property_address_region", "property_address_postal_code", "property_address_country",
		"ytd_interest_paid", "ytd_principal_paid",
		"synced_at", "created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), userID, accountID, &acctNum,
		nil, nil, nil, nil,
		&rate, &rateType,
		&lastPayment, &now,
		nil, nil, nil,
		nil, &now,
		nil, nil,
		nil,
		nil, nil,
		nil, nil, nil,
		nil, nil,
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

// TestMortgageLiabilityRepository_GetByAccountID_NotFound verifies nil returned when not found.
func TestMortgageLiabilityRepository_GetByAccountID_NotFound(t *testing.T) {
	mock, repo := setupMortgageLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "account_number",
		"current_late_fee", "escrow_balance", "has_pmi", "has_prepayment_penalty",
		"interest_rate_percentage", "interest_rate_type",
		"last_payment_amount", "last_payment_date",
		"loan_type_description", "loan_term", "maturity_date",
		"next_monthly_payment", "next_payment_due_date",
		"origination_date", "origination_principal_amount",
		"past_due_amount",
		"property_address_street", "property_address_city",
		"property_address_region", "property_address_postal_code", "property_address_country",
		"ytd_interest_paid", "ytd_principal_paid",
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
