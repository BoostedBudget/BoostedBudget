package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
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
	db  DB
	enc *appcrypto.FieldEncryptor
}

// NewStudentLoanLiabilityRepository creates a new StudentLoanLiabilityRepository instance.
func NewStudentLoanLiabilityRepository(db DB, enc *appcrypto.FieldEncryptor) StudentLoanLiabilityRepository {
	return &studentLoanLiabilityRepository{db: db, enc: enc}
}

// encryptedStudentLoanPIIFields holds the encrypted PII strings for a write operation.
type encryptedStudentLoanPIIFields struct {
	accountNumber       *string
	servicerStreet      *string
	servicerCity        *string
	servicerRegion      *string
	servicerPostal      *string
	servicerCountry     *string
	paymentReferenceNum *string
	guarantor           *string
	loanName            *string
	sequenceNumber      *string
}

// encryptStudentLoanPIIFields encrypts all PII fields before writing to the database.
func (r *studentLoanLiabilityRepository) encryptStudentLoanPIIFields(liability *domain.StudentLoanLiability) (encryptedStudentLoanPIIFields, error) {
	if r.enc == nil {
		return encryptedStudentLoanPIIFields{
			accountNumber:       liability.AccountNumber,
			servicerStreet:      liability.ServicerAddressStreet,
			servicerCity:        liability.ServicerAddressCity,
			servicerRegion:      liability.ServicerAddressRegion,
			servicerPostal:      liability.ServicerAddressPostalCode,
			servicerCountry:     liability.ServicerAddressCountry,
			paymentReferenceNum: liability.PaymentReferenceNumber,
			guarantor:           liability.Guarantor,
			loanName:            liability.LoanName,
			sequenceNumber:      liability.SequenceNumber,
		}, nil
	}

	var (
		fields encryptedStudentLoanPIIFields
		err    error
	)

	fields.accountNumber, err = r.enc.EncryptNullableString(liability.AccountNumber)
	if err != nil {
		return fields, fmt.Errorf("encrypt account_number: %w", err)
	}
	fields.servicerStreet, err = r.enc.EncryptNullableString(liability.ServicerAddressStreet)
	if err != nil {
		return fields, fmt.Errorf("encrypt servicer_address_street: %w", err)
	}
	fields.servicerCity, err = r.enc.EncryptNullableString(liability.ServicerAddressCity)
	if err != nil {
		return fields, fmt.Errorf("encrypt servicer_address_city: %w", err)
	}
	fields.servicerRegion, err = r.enc.EncryptNullableString(liability.ServicerAddressRegion)
	if err != nil {
		return fields, fmt.Errorf("encrypt servicer_address_region: %w", err)
	}
	fields.servicerPostal, err = r.enc.EncryptNullableString(liability.ServicerAddressPostalCode)
	if err != nil {
		return fields, fmt.Errorf("encrypt servicer_address_postal_code: %w", err)
	}
	fields.servicerCountry, err = r.enc.EncryptNullableString(liability.ServicerAddressCountry)
	if err != nil {
		return fields, fmt.Errorf("encrypt servicer_address_country: %w", err)
	}
	fields.paymentReferenceNum, err = r.enc.EncryptNullableString(liability.PaymentReferenceNumber)
	if err != nil {
		return fields, fmt.Errorf("encrypt payment_reference_number: %w", err)
	}
	fields.guarantor, err = r.enc.EncryptNullableString(liability.Guarantor)
	if err != nil {
		return fields, fmt.Errorf("encrypt guarantor: %w", err)
	}
	fields.loanName, err = r.enc.EncryptNullableString(liability.LoanName)
	if err != nil {
		return fields, fmt.Errorf("encrypt loan_name: %w", err)
	}
	fields.sequenceNumber, err = r.enc.EncryptNullableString(liability.SequenceNumber)
	if err != nil {
		return fields, fmt.Errorf("encrypt sequence_number: %w", err)
	}

	return fields, nil
}

// decryptStudentLoanPIIFields decrypts all PII fields on a scanned student loan liability.
func (r *studentLoanLiabilityRepository) decryptStudentLoanPIIFields(sl *domain.StudentLoanLiability) error {
	if r.enc == nil {
		return nil
	}

	var err error
	sl.AccountNumber, err = r.enc.DecryptNullableString(sl.AccountNumber)
	if err != nil {
		return fmt.Errorf("decrypt account_number: %w", err)
	}
	sl.ServicerAddressStreet, err = r.enc.DecryptNullableString(sl.ServicerAddressStreet)
	if err != nil {
		return fmt.Errorf("decrypt servicer_address_street: %w", err)
	}
	sl.ServicerAddressCity, err = r.enc.DecryptNullableString(sl.ServicerAddressCity)
	if err != nil {
		return fmt.Errorf("decrypt servicer_address_city: %w", err)
	}
	sl.ServicerAddressRegion, err = r.enc.DecryptNullableString(sl.ServicerAddressRegion)
	if err != nil {
		return fmt.Errorf("decrypt servicer_address_region: %w", err)
	}
	sl.ServicerAddressPostalCode, err = r.enc.DecryptNullableString(sl.ServicerAddressPostalCode)
	if err != nil {
		return fmt.Errorf("decrypt servicer_address_postal_code: %w", err)
	}
	sl.ServicerAddressCountry, err = r.enc.DecryptNullableString(sl.ServicerAddressCountry)
	if err != nil {
		return fmt.Errorf("decrypt servicer_address_country: %w", err)
	}
	sl.PaymentReferenceNumber, err = r.enc.DecryptNullableString(sl.PaymentReferenceNumber)
	if err != nil {
		return fmt.Errorf("decrypt payment_reference_number: %w", err)
	}
	sl.Guarantor, err = r.enc.DecryptNullableString(sl.Guarantor)
	if err != nil {
		return fmt.Errorf("decrypt guarantor: %w", err)
	}
	sl.LoanName, err = r.enc.DecryptNullableString(sl.LoanName)
	if err != nil {
		return fmt.Errorf("decrypt loan_name: %w", err)
	}
	sl.SequenceNumber, err = r.enc.DecryptNullableString(sl.SequenceNumber)
	if err != nil {
		return fmt.Errorf("decrypt sequence_number: %w", err)
	}
	return nil
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

	// nil means delete-only (clear stale data when Plaid no longer reports this liability)
	if liability == nil {
		return tx.Commit(ctx)
	}

	if liability.ID == "" {
		liability.ID = uuid.New().String()
	}
	now := time.Now()
	liability.SyncedAt = now
	liability.CreatedAt = now
	liability.UpdatedAt = now

	enc, err := r.encryptStudentLoanPIIFields(liability)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO student_loan_liabilities (id, user_id, account_id, account_number, disbursement_dates, expected_payoff_date, guarantor, interest_rate_percentage, is_overdue, last_payment_amount, last_payment_date, last_statement_issue_date, last_statement_balance, loan_name, loan_status_type, loan_status_end_date, minimum_payment_amount, next_payment_due_date, origination_date, origination_principal_amount, outstanding_interest_amount, payment_reference_number, pslf_estimated_eligibility_date, pslf_payments_made, pslf_payments_remaining, repayment_plan_type, repayment_plan_description, sequence_number, servicer_address_street, servicer_address_city, servicer_address_region, servicer_address_postal_code, servicer_address_country, ytd_interest_paid, ytd_principal_paid, synced_at, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38)`,
		liability.ID, liability.UserID, liability.AccountID,
		enc.accountNumber, liability.DisbursementDates, liability.ExpectedPayoffDate,
		enc.guarantor, liability.InterestRatePercentage, liability.IsOverdue,
		liability.LastPaymentAmount, liability.LastPaymentDate,
		liability.LastStatementIssueDate, liability.LastStatementBalance,
		enc.loanName, liability.LoanStatusType, liability.LoanStatusEndDate,
		liability.MinimumPaymentAmount, liability.NextPaymentDueDate,
		liability.OriginationDate, liability.OriginationPrincipalAmount,
		liability.OutstandingInterestAmount, enc.paymentReferenceNum,
		liability.PSLFEstimatedEligibilityDate, liability.PSLFPaymentsMade, liability.PSLFPaymentsRemaining,
		liability.RepaymentPlanType, liability.RepaymentPlanDescription,
		enc.sequenceNumber,
		enc.servicerStreet, enc.servicerCity, enc.servicerRegion, enc.servicerPostal, enc.servicerCountry,
		liability.YTDInterestPaid, liability.YTDPrincipalPaid,
		liability.SyncedAt, liability.CreatedAt, liability.UpdatedAt)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// GetByUserID retrieves all student loan liabilities for a user.
// Joins accounts to filter out replaced accounts (Issue #1109).
func (r *studentLoanLiabilityRepository) GetByUserID(ctx context.Context, userID string) ([]domain.StudentLoanLiability, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("liabilities").Inc()

	query := `
		SELECT student_loan_liabilities.id, student_loan_liabilities.user_id, student_loan_liabilities.account_id, student_loan_liabilities.account_number,
			student_loan_liabilities.disbursement_dates, student_loan_liabilities.expected_payoff_date, student_loan_liabilities.guarantor,
			student_loan_liabilities.interest_rate_percentage, student_loan_liabilities.is_overdue,
			student_loan_liabilities.last_payment_amount, student_loan_liabilities.last_payment_date,
			student_loan_liabilities.last_statement_issue_date, student_loan_liabilities.last_statement_balance,
			student_loan_liabilities.loan_name, student_loan_liabilities.loan_status_type, student_loan_liabilities.loan_status_end_date,
			student_loan_liabilities.minimum_payment_amount, student_loan_liabilities.next_payment_due_date,
			student_loan_liabilities.origination_date, student_loan_liabilities.origination_principal_amount,
			student_loan_liabilities.outstanding_interest_amount, student_loan_liabilities.payment_reference_number,
			student_loan_liabilities.pslf_estimated_eligibility_date, student_loan_liabilities.pslf_payments_made, student_loan_liabilities.pslf_payments_remaining,
			student_loan_liabilities.repayment_plan_type, student_loan_liabilities.repayment_plan_description,
			student_loan_liabilities.sequence_number,
			student_loan_liabilities.servicer_address_street, student_loan_liabilities.servicer_address_city,
			student_loan_liabilities.servicer_address_region, student_loan_liabilities.servicer_address_postal_code, student_loan_liabilities.servicer_address_country,
			student_loan_liabilities.ytd_interest_paid, student_loan_liabilities.ytd_principal_paid,
			student_loan_liabilities.synced_at, student_loan_liabilities.created_at, student_loan_liabilities.updated_at
		FROM student_loan_liabilities
		JOIN accounts a ON a.id = student_loan_liabilities.account_id
		WHERE student_loan_liabilities.user_id = $1
		  AND a.status = 'active'
		ORDER BY student_loan_liabilities.account_id
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
		if err := r.decryptStudentLoanPIIFields(&sl); err != nil {
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
		   OR account_id IN (
				SELECT id FROM accounts
				WHERE user_account_id = (
					SELECT user_account_id FROM accounts WHERE id = $1 AND user_account_id IS NOT NULL
				)
		   )
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

	if err := r.decryptStudentLoanPIIFields(sl); err != nil {
		return nil, err
	}

	return sl, nil
}
