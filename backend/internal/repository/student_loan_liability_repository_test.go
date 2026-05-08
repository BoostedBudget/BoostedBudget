package repository

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// studentLoanTestEncSvc is a simple prefix-based mock encryption service for testing.
type studentLoanTestEncSvc struct{}

func (t *studentLoanTestEncSvc) Encrypt(p string) (string, error) { return "ENC$" + p, nil }
func (t *studentLoanTestEncSvc) Decrypt(c string) (string, error) {
	if strings.HasPrefix(c, "ENC$") {
		return strings.TrimPrefix(c, "ENC$"), nil
	}
	return c, nil
}

func newStudentLoanTestEncryptor(t *testing.T) *appcrypto.FieldEncryptor {
	t.Helper()
	key := []byte("12345678901234567890123456789012") // 32 bytes
	enc, err := appcrypto.NewFieldEncryptor(&studentLoanTestEncSvc{}, key)
	require.NoError(t, err)
	return enc
}

func setupStudentLoanLiabilityMockDB(t *testing.T) (pgxmock.PgxPoolIface, StudentLoanLiabilityRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	enc := newStudentLoanTestEncryptor(t)
	return mock, NewStudentLoanLiabilityRepository(mock, enc)
}

func makeStudentLoanLiability(userID, accountID string) *domain.StudentLoanLiability {
	acctNum := "SL-001"
	guarantor := "Dept of Education"
	rate := 4.5
	isOverdue := false
	lastPayment := 350.00
	lastBalance := 25000.00
	loanName := "Stafford Loan"
	statusType := "repayment"
	minPayment := 300.00
	origPrincipal := 30000.00
	outstanding := 2500.00
	refNum := "REF-12345"
	pslfMade := 24
	pslfRemaining := 96
	planType := "standard"
	planDesc := "Standard Repayment Plan"
	seqNum := "1"
	street := "456 College Ave"
	city := "Education City"
	region := "CA"
	postal := "90210"
	country := "US"
	ytdInterest := 1100.00
	ytdPrincipal := 3100.00
	now := time.Now()
	return &domain.StudentLoanLiability{
		UserID:                     userID,
		AccountID:                  accountID,
		AccountNumber:              &acctNum,
		DisbursementDates:          []string{"2020-09-01", "2021-09-01"},
		ExpectedPayoffDate:         &now,
		Guarantor:                  &guarantor,
		InterestRatePercentage:     &rate,
		IsOverdue:                  &isOverdue,
		LastPaymentAmount:          &lastPayment,
		LastPaymentDate:            &now,
		LastStatementBalance:       &lastBalance,
		LoanName:                   &loanName,
		LoanStatusType:             &statusType,
		MinimumPaymentAmount:       &minPayment,
		NextPaymentDueDate:         &now,
		OriginationPrincipalAmount: &origPrincipal,
		OutstandingInterestAmount:  &outstanding,
		PaymentReferenceNumber:     &refNum,
		PSLFPaymentsMade:           &pslfMade,
		PSLFPaymentsRemaining:      &pslfRemaining,
		RepaymentPlanType:          &planType,
		RepaymentPlanDescription:   &planDesc,
		SequenceNumber:             &seqNum,
		ServicerAddressStreet:      &street,
		ServicerAddressCity:        &city,
		ServicerAddressRegion:      &region,
		ServicerAddressPostalCode:  &postal,
		ServicerAddressCountry:     &country,
		YTDInterestPaid:            &ytdInterest,
		YTDPrincipalPaid:           &ytdPrincipal,
	}
}

// TestStudentLoanLiabilityRepository_ReplaceForAccount_Success verifies the atomic
// delete-then-insert pattern for student loan liabilities within a transaction.
func TestStudentLoanLiabilityRepository_ReplaceForAccount_Success(t *testing.T) {
	mock, repo := setupStudentLoanLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	liability := makeStudentLoanLiability(userID, accountID)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM student_loan_liabilities`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO student_loan_liabilities`)).
		WithArgs(anyArgs(38)...).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	err := repo.ReplaceForAccount(ctx, accountID, userID, liability)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestStudentLoanLiabilityRepository_ReplaceForAccount_NilDeletesOnly verifies that passing nil
// performs a delete-only operation and commits without an INSERT.
func TestStudentLoanLiabilityRepository_ReplaceForAccount_NilDeletesOnly(t *testing.T) {
	mock, repo := setupStudentLoanLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM student_loan_liabilities`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectCommit()

	err := repo.ReplaceForAccount(ctx, accountID, userID, nil)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestStudentLoanLiabilityRepository_ReplaceForAccount_RollbackOnError verifies rollback on failure.
func TestStudentLoanLiabilityRepository_ReplaceForAccount_RollbackOnError(t *testing.T) {
	mock, repo := setupStudentLoanLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	liability := makeStudentLoanLiability(userID, accountID)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM student_loan_liabilities`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO student_loan_liabilities`)).
		WithArgs(anyArgs(38)...).
		WillReturnError(errors.New("db constraint error"))
	mock.ExpectRollback()

	err := repo.ReplaceForAccount(ctx, accountID, userID, liability)

	require.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestStudentLoanLiabilityRepository_GetByUserID verifies retrieval of student loan liabilities for a user.
func TestStudentLoanLiabilityRepository_GetByUserID(t *testing.T) {
	mock, repo := setupStudentLoanLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	now := time.Now()

	// The mock DB returns encrypted values; repo should decrypt them.
	encAcctNum := "ENC$SL-001"
	encLoanName := "ENC$Stafford Loan"
	rate := 4.5
	isOverdue := false
	lastPayment := 350.00
	minPayment := 300.00

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "account_number",
		"disbursement_dates", "expected_payoff_date", "guarantor",
		"interest_rate_percentage", "is_overdue",
		"last_payment_amount", "last_payment_date",
		"last_statement_issue_date", "last_statement_balance",
		"loan_name", "loan_status_type", "loan_status_end_date",
		"minimum_payment_amount", "next_payment_due_date",
		"origination_date", "origination_principal_amount",
		"outstanding_interest_amount", "payment_reference_number",
		"pslf_estimated_eligibility_date", "pslf_payments_made", "pslf_payments_remaining",
		"repayment_plan_type", "repayment_plan_description",
		"sequence_number",
		"servicer_address_street", "servicer_address_city",
		"servicer_address_region", "servicer_address_postal_code", "servicer_address_country",
		"ytd_interest_paid", "ytd_principal_paid",
		"synced_at", "created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), userID, accountID, &encAcctNum,
		[]string{"2020-09-01"}, &now, nil,
		&rate, &isOverdue,
		&lastPayment, &now,
		nil, nil,
		&encLoanName, nil, nil,
		&minPayment, &now,
		nil, nil,
		nil, nil,
		nil, nil, nil,
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
	// Verify decryption of PII fields
	require.NotNil(t, liabilities[0].AccountNumber)
	assert.Equal(t, "SL-001", *liabilities[0].AccountNumber)
	require.NotNil(t, liabilities[0].LoanName)
	assert.Equal(t, "Stafford Loan", *liabilities[0].LoanName)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestStudentLoanLiabilityRepository_GetByUserID_Empty verifies empty slice returned when no rows.
func TestStudentLoanLiabilityRepository_GetByUserID_Empty(t *testing.T) {
	mock, repo := setupStudentLoanLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "account_number",
		"disbursement_dates", "expected_payoff_date", "guarantor",
		"interest_rate_percentage", "is_overdue",
		"last_payment_amount", "last_payment_date",
		"last_statement_issue_date", "last_statement_balance",
		"loan_name", "loan_status_type", "loan_status_end_date",
		"minimum_payment_amount", "next_payment_due_date",
		"origination_date", "origination_principal_amount",
		"outstanding_interest_amount", "payment_reference_number",
		"pslf_estimated_eligibility_date", "pslf_payments_made", "pslf_payments_remaining",
		"repayment_plan_type", "repayment_plan_description",
		"sequence_number",
		"servicer_address_street", "servicer_address_city",
		"servicer_address_region", "servicer_address_postal_code", "servicer_address_country",
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

// TestStudentLoanLiabilityRepository_GetByAccountID verifies retrieval for a specific account.
func TestStudentLoanLiabilityRepository_GetByAccountID(t *testing.T) {
	mock, repo := setupStudentLoanLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	now := time.Now()

	// The mock DB returns encrypted values; repo should decrypt them.
	encAcctNum := "ENC$SL-002"
	rate := 5.0
	isOverdue := true
	lastPayment := 400.00

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "account_number",
		"disbursement_dates", "expected_payoff_date", "guarantor",
		"interest_rate_percentage", "is_overdue",
		"last_payment_amount", "last_payment_date",
		"last_statement_issue_date", "last_statement_balance",
		"loan_name", "loan_status_type", "loan_status_end_date",
		"minimum_payment_amount", "next_payment_due_date",
		"origination_date", "origination_principal_amount",
		"outstanding_interest_amount", "payment_reference_number",
		"pslf_estimated_eligibility_date", "pslf_payments_made", "pslf_payments_remaining",
		"repayment_plan_type", "repayment_plan_description",
		"sequence_number",
		"servicer_address_street", "servicer_address_city",
		"servicer_address_region", "servicer_address_postal_code", "servicer_address_country",
		"ytd_interest_paid", "ytd_principal_paid",
		"synced_at", "created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), userID, accountID, &encAcctNum,
		[]string(nil), nil, nil,
		&rate, &isOverdue,
		&lastPayment, &now,
		nil, nil,
		nil, nil, nil,
		nil, nil,
		nil, nil,
		nil, nil,
		nil, nil, nil,
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
	// Verify decryption: "ENC$SL-002" → "SL-002"
	require.NotNil(t, liability.AccountNumber)
	assert.Equal(t, "SL-002", *liability.AccountNumber)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestStudentLoanLiabilityRepository_GetByAccountID_NotFound verifies nil returned when not found.
func TestStudentLoanLiabilityRepository_GetByAccountID_NotFound(t *testing.T) {
	mock, repo := setupStudentLoanLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "account_number",
		"disbursement_dates", "expected_payoff_date", "guarantor",
		"interest_rate_percentage", "is_overdue",
		"last_payment_amount", "last_payment_date",
		"last_statement_issue_date", "last_statement_balance",
		"loan_name", "loan_status_type", "loan_status_end_date",
		"minimum_payment_amount", "next_payment_due_date",
		"origination_date", "origination_principal_amount",
		"outstanding_interest_amount", "payment_reference_number",
		"pslf_estimated_eligibility_date", "pslf_payments_made", "pslf_payments_remaining",
		"repayment_plan_type", "repayment_plan_description",
		"sequence_number",
		"servicer_address_street", "servicer_address_city",
		"servicer_address_region", "servicer_address_postal_code", "servicer_address_country",
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

// Chain-aware tests for Issue #1109

func TestStudentLoanLiabilityRepository_GetByUserID_ActiveAccountsOnly(t *testing.T) {
	// RED: GetByUserID must JOIN accounts and only return liabilities for active accounts
	mock, repo := setupStudentLoanLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	now := time.Now()

	encAcctNum := "ENC$SL-001"
	encLoanName := "ENC$Stafford Loan"
	rate := 4.5
	isOverdue := false
	lastPayment := 350.00
	minPayment := 300.00

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "account_number",
		"disbursement_dates", "expected_payoff_date", "guarantor",
		"interest_rate_percentage", "is_overdue",
		"last_payment_amount", "last_payment_date",
		"last_statement_issue_date", "last_statement_balance",
		"loan_name", "loan_status_type", "loan_status_end_date",
		"minimum_payment_amount", "next_payment_due_date",
		"origination_date", "origination_principal_amount",
		"outstanding_interest_amount", "payment_reference_number",
		"pslf_estimated_eligibility_date", "pslf_payments_made", "pslf_payments_remaining",
		"repayment_plan_type", "repayment_plan_description",
		"sequence_number",
		"servicer_address_street", "servicer_address_city",
		"servicer_address_region", "servicer_address_postal_code", "servicer_address_country",
		"ytd_interest_paid", "ytd_principal_paid",
		"synced_at", "created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), userID, accountID, &encAcctNum,
		[]string{"2020-09-01"}, &now, nil,
		&rate, &isOverdue,
		&lastPayment, &now,
		nil, nil,
		&encLoanName, nil, nil,
		&minPayment, &now,
		nil, nil,
		nil, nil,
		nil, nil, nil,
		nil, nil,
		nil,
		nil, nil,
		nil, nil, nil,
		nil, nil,
		now, now, now,
	)

	// The query must JOIN accounts and filter status='active'
	mock.ExpectQuery(regexp.QuoteMeta(`JOIN accounts a ON a.id = student_loan_liabilities.account_id`)).
		WithArgs(userID).
		WillReturnRows(rows)

	liabilities, err := repo.GetByUserID(ctx, userID)

	require.NoError(t, err)
	require.Len(t, liabilities, 1)
	assert.Equal(t, accountID, liabilities[0].AccountID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestStudentLoanLiabilityRepository_GetByUserID_ReplacedAccountExcluded(t *testing.T) {
	// RED: Replaced student loan liability accounts must NOT appear in GetByUserID
	mock, repo := setupStudentLoanLiabilityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "account_number",
		"disbursement_dates", "expected_payoff_date", "guarantor",
		"interest_rate_percentage", "is_overdue",
		"last_payment_amount", "last_payment_date",
		"last_statement_issue_date", "last_statement_balance",
		"loan_name", "loan_status_type", "loan_status_end_date",
		"minimum_payment_amount", "next_payment_due_date",
		"origination_date", "origination_principal_amount",
		"outstanding_interest_amount", "payment_reference_number",
		"pslf_estimated_eligibility_date", "pslf_payments_made", "pslf_payments_remaining",
		"repayment_plan_type", "repayment_plan_description",
		"sequence_number",
		"servicer_address_street", "servicer_address_city",
		"servicer_address_region", "servicer_address_postal_code", "servicer_address_country",
		"ytd_interest_paid", "ytd_principal_paid",
		"synced_at", "created_at", "updated_at",
	})

	mock.ExpectQuery(regexp.QuoteMeta(`JOIN accounts a ON a.id = student_loan_liabilities.account_id`)).
		WithArgs(userID).
		WillReturnRows(rows)

	liabilities, err := repo.GetByUserID(ctx, userID)

	require.NoError(t, err)
	assert.Empty(t, liabilities)
	assert.NoError(t, mock.ExpectationsWereMet())
}
