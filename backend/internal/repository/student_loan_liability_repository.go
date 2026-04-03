package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// StudentLoanLiabilityRepository defines the interface for student loan liability data access.
type StudentLoanLiabilityRepository interface {
	// ReplaceForAccount atomically replaces the student loan liability for an account.
	// Deletes the existing row and inserts the new one within a single DB transaction.
	ReplaceForAccount(ctx context.Context, accountID, userID string, liability *domain.StudentLoanLiability) error

	// GetByUserID retrieves all student loan liabilities for a user.
	GetByUserID(ctx context.Context, userID string) ([]domain.StudentLoanLiability, error)

	// GetByAccountID retrieves the student loan liability for a specific account.
	// Returns nil, nil when not found.
	GetByAccountID(ctx context.Context, accountID string) (*domain.StudentLoanLiability, error)
}

type studentLoanLiabilityRepository struct {
	db DB
}

// NewStudentLoanLiabilityRepository creates a new StudentLoanLiabilityRepository instance.
func NewStudentLoanLiabilityRepository(db DB) StudentLoanLiabilityRepository {
	return &studentLoanLiabilityRepository{db: db}
}

// ReplaceForAccount atomically replaces the student loan liability for an account.
func (r *studentLoanLiabilityRepository) ReplaceForAccount(ctx context.Context, accountID, userID string, liability *domain.StudentLoanLiability) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	_, err = tx.Exec(ctx,
		`DELETE FROM student_loan_liabilities WHERE account_id = $1 AND user_id = $2`,
		accountID, userID)
	if err != nil {
		return err
	}

	if liability.ID == "" {
		liability.ID = uuid.New().String()
	}
	now := time.Now()
	liability.SyncedAt = now
	liability.CreatedAt = now
	liability.UpdatedAt = now

	_, err = tx.Exec(ctx,
		`INSERT INTO student_loan_liabilities (id, user_id, account_id, account_number, disbursement_dates, expected_payoff_date, guarantor, interest_rate_percentage, is_overdue, last_payment_amount, last_payment_date, last_statement_issue_date, last_statement_balance, loan_name, loan_status_type, loan_status_end_date, minimum_payment_amount, next_payment_due_date, origination_date, origination_principal_amount, outstanding_interest_amount, payment_reference_number, pslf_estimated_eligibility_date, pslf_payments_made, pslf_payments_remaining, repayment_plan_type, repayment_plan_description, sequence_number, servicer_address_street, servicer_address_city, servicer_address_region, servicer_address_postal_code, servicer_address_country, ytd_interest_paid, ytd_principal_paid, synced_at, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38)`,
		liability.ID, liability.UserID, liability.AccountID,
		liability.AccountNumber, liability.DisbursementDates, liability.ExpectedPayoffDate,
		liability.Guarantor, liability.InterestRatePercentage, liability.IsOverdue,
		liability.LastPaymentAmount, liability.LastPaymentDate,
		liability.LastStatementIssueDate, liability.LastStatementBalance,
		liability.LoanName, liability.LoanStatusType, liability.LoanStatusEndDate,
		liability.MinimumPaymentAmount, liability.NextPaymentDueDate,
		liability.OriginationDate, liability.OriginationPrincipalAmount,
		liability.OutstandingInterestAmount, liability.PaymentReferenceNumber,
		liability.PSLFEstimatedEligibilityDate, liability.PSLFPaymentsMade, liability.PSLFPaymentsRemaining,
		liability.RepaymentPlanType, liability.RepaymentPlanDescription,
		liability.SequenceNumber,
		liability.ServicerAddressStreet, liability.ServicerAddressCity,
		liability.ServicerAddressRegion, liability.ServicerAddressPostalCode, liability.ServicerAddressCountry,
		liability.YTDInterestPaid, liability.YTDPrincipalPaid,
		liability.SyncedAt, liability.CreatedAt, liability.UpdatedAt)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// GetByUserID retrieves all student loan liabilities for a user.
func (r *studentLoanLiabilityRepository) GetByUserID(ctx context.Context, userID string) ([]domain.StudentLoanLiability, error) {
	query := `
		SELECT id, user_id, account_id, account_number,
			disbursement_dates, expected_payoff_date, guarantor,
			interest_rate_percentage, is_overdue,
			last_payment_amount, last_payment_date,
			last_statement_issue_date, last_statement_balance,
			loan_name, loan_status_type, loan_status_end_date,
			minimum_payment_amount, next_payment_due_date,
			origination_date, origination_principal_amount,
			outstanding_interest_amount, payment_reference_number,
			pslf_estimated_eligibility_date, pslf_payments_made, pslf_payments_remaining,
			repayment_plan_type, repayment_plan_description,
			sequence_number,
			servicer_address_street, servicer_address_city,
			servicer_address_region, servicer_address_postal_code, servicer_address_country,
			ytd_interest_paid, ytd_principal_paid,
			synced_at, created_at, updated_at
		FROM student_loan_liabilities
		WHERE user_id = $1
		ORDER BY account_id
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	liabilities := []domain.StudentLoanLiability{}
	for rows.Next() {
		sl := domain.StudentLoanLiability{}
		if err := rows.Scan(
			&sl.ID, &sl.UserID, &sl.AccountID, &sl.AccountNumber,
			&sl.DisbursementDates, &sl.ExpectedPayoffDate, &sl.Guarantor,
			&sl.InterestRatePercentage, &sl.IsOverdue,
			&sl.LastPaymentAmount, &sl.LastPaymentDate,
			&sl.LastStatementIssueDate, &sl.LastStatementBalance,
			&sl.LoanName, &sl.LoanStatusType, &sl.LoanStatusEndDate,
			&sl.MinimumPaymentAmount, &sl.NextPaymentDueDate,
			&sl.OriginationDate, &sl.OriginationPrincipalAmount,
			&sl.OutstandingInterestAmount, &sl.PaymentReferenceNumber,
			&sl.PSLFEstimatedEligibilityDate, &sl.PSLFPaymentsMade, &sl.PSLFPaymentsRemaining,
			&sl.RepaymentPlanType, &sl.RepaymentPlanDescription,
			&sl.SequenceNumber,
			&sl.ServicerAddressStreet, &sl.ServicerAddressCity,
			&sl.ServicerAddressRegion, &sl.ServicerAddressPostalCode, &sl.ServicerAddressCountry,
			&sl.YTDInterestPaid, &sl.YTDPrincipalPaid,
			&sl.SyncedAt, &sl.CreatedAt, &sl.UpdatedAt,
		); err != nil {
			return nil, err
		}
		liabilities = append(liabilities, sl)
	}

	return liabilities, nil
}

// GetByAccountID retrieves the student loan liability for a specific account.
// Returns nil, nil when not found.
func (r *studentLoanLiabilityRepository) GetByAccountID(ctx context.Context, accountID string) (*domain.StudentLoanLiability, error) {
	query := `
		SELECT id, user_id, account_id, account_number,
			disbursement_dates, expected_payoff_date, guarantor,
			interest_rate_percentage, is_overdue,
			last_payment_amount, last_payment_date,
			last_statement_issue_date, last_statement_balance,
			loan_name, loan_status_type, loan_status_end_date,
			minimum_payment_amount, next_payment_due_date,
			origination_date, origination_principal_amount,
			outstanding_interest_amount, payment_reference_number,
			pslf_estimated_eligibility_date, pslf_payments_made, pslf_payments_remaining,
			repayment_plan_type, repayment_plan_description,
			sequence_number,
			servicer_address_street, servicer_address_city,
			servicer_address_region, servicer_address_postal_code, servicer_address_country,
			ytd_interest_paid, ytd_principal_paid,
			synced_at, created_at, updated_at
		FROM student_loan_liabilities
		WHERE account_id = $1
	`

	rows, err := r.db.Query(ctx, query, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}

	sl := &domain.StudentLoanLiability{}
	if err := rows.Scan(
		&sl.ID, &sl.UserID, &sl.AccountID, &sl.AccountNumber,
		&sl.DisbursementDates, &sl.ExpectedPayoffDate, &sl.Guarantor,
		&sl.InterestRatePercentage, &sl.IsOverdue,
		&sl.LastPaymentAmount, &sl.LastPaymentDate,
		&sl.LastStatementIssueDate, &sl.LastStatementBalance,
		&sl.LoanName, &sl.LoanStatusType, &sl.LoanStatusEndDate,
		&sl.MinimumPaymentAmount, &sl.NextPaymentDueDate,
		&sl.OriginationDate, &sl.OriginationPrincipalAmount,
		&sl.OutstandingInterestAmount, &sl.PaymentReferenceNumber,
		&sl.PSLFEstimatedEligibilityDate, &sl.PSLFPaymentsMade, &sl.PSLFPaymentsRemaining,
		&sl.RepaymentPlanType, &sl.RepaymentPlanDescription,
		&sl.SequenceNumber,
		&sl.ServicerAddressStreet, &sl.ServicerAddressCity,
		&sl.ServicerAddressRegion, &sl.ServicerAddressPostalCode, &sl.ServicerAddressCountry,
		&sl.YTDInterestPaid, &sl.YTDPrincipalPaid,
		&sl.SyncedAt, &sl.CreatedAt, &sl.UpdatedAt,
	); err != nil {
		return nil, err
	}

	return sl, nil
}
