package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// MortgageLiabilityRepository defines the interface for mortgage liability data access.
type MortgageLiabilityRepository interface {
	// ReplaceForAccount atomically replaces the mortgage liability for an account.
	// Deletes the existing row and inserts the new one within a single DB transaction.
	ReplaceForAccount(ctx context.Context, accountID, userID string, liability *domain.MortgageLiability) error

	// GetByUserID retrieves all mortgage liabilities for a user.
	GetByUserID(ctx context.Context, userID string) ([]domain.MortgageLiability, error)

	// GetByAccountID retrieves the mortgage liability for a specific account.
	// Returns nil, nil when not found.
	GetByAccountID(ctx context.Context, accountID string) (*domain.MortgageLiability, error)
}

type mortgageLiabilityRepository struct {
	db DB
}

// NewMortgageLiabilityRepository creates a new MortgageLiabilityRepository instance.
func NewMortgageLiabilityRepository(db DB) MortgageLiabilityRepository {
	return &mortgageLiabilityRepository{db: db}
}

// ReplaceForAccount atomically replaces the mortgage liability for an account.
func (r *mortgageLiabilityRepository) ReplaceForAccount(ctx context.Context, accountID, userID string, liability *domain.MortgageLiability) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	_, err = tx.Exec(ctx,
		`DELETE FROM mortgage_liabilities WHERE account_id = $1 AND user_id = $2`,
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
		`INSERT INTO mortgage_liabilities (id, user_id, account_id, account_number, current_late_fee, escrow_balance, has_pmi, has_prepayment_penalty, interest_rate_percentage, interest_rate_type, last_payment_amount, last_payment_date, loan_type_description, loan_term, maturity_date, next_monthly_payment, next_payment_due_date, origination_date, origination_principal_amount, past_due_amount, property_address_street, property_address_city, property_address_region, property_address_postal_code, property_address_country, ytd_interest_paid, ytd_principal_paid, synced_at, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30)`,
		liability.ID, liability.UserID, liability.AccountID,
		liability.AccountNumber, liability.CurrentLateFee, liability.EscrowBalance,
		liability.HasPMI, liability.HasPrepaymentPenalty,
		liability.InterestRatePercentage, liability.InterestRateType,
		liability.LastPaymentAmount, liability.LastPaymentDate,
		liability.LoanTypeDescription, liability.LoanTerm, liability.MaturityDate,
		liability.NextMonthlyPayment, liability.NextPaymentDueDate,
		liability.OriginationDate, liability.OriginationPrincipalAmount,
		liability.PastDueAmount,
		liability.PropertyAddressStreet, liability.PropertyAddressCity,
		liability.PropertyAddressRegion, liability.PropertyAddressPostalCode, liability.PropertyAddressCountry,
		liability.YTDInterestPaid, liability.YTDPrincipalPaid,
		liability.SyncedAt, liability.CreatedAt, liability.UpdatedAt)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// GetByUserID retrieves all mortgage liabilities for a user.
func (r *mortgageLiabilityRepository) GetByUserID(ctx context.Context, userID string) ([]domain.MortgageLiability, error) {
	query := `
		SELECT id, user_id, account_id, account_number,
			current_late_fee, escrow_balance, has_pmi, has_prepayment_penalty,
			interest_rate_percentage, interest_rate_type,
			last_payment_amount, last_payment_date,
			loan_type_description, loan_term, maturity_date,
			next_monthly_payment, next_payment_due_date,
			origination_date, origination_principal_amount,
			past_due_amount,
			property_address_street, property_address_city,
			property_address_region, property_address_postal_code, property_address_country,
			ytd_interest_paid, ytd_principal_paid,
			synced_at, created_at, updated_at
		FROM mortgage_liabilities
		WHERE user_id = $1
		ORDER BY account_id
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	liabilities := []domain.MortgageLiability{}
	for rows.Next() {
		ml := domain.MortgageLiability{}
		if err := rows.Scan(
			&ml.ID, &ml.UserID, &ml.AccountID, &ml.AccountNumber,
			&ml.CurrentLateFee, &ml.EscrowBalance, &ml.HasPMI, &ml.HasPrepaymentPenalty,
			&ml.InterestRatePercentage, &ml.InterestRateType,
			&ml.LastPaymentAmount, &ml.LastPaymentDate,
			&ml.LoanTypeDescription, &ml.LoanTerm, &ml.MaturityDate,
			&ml.NextMonthlyPayment, &ml.NextPaymentDueDate,
			&ml.OriginationDate, &ml.OriginationPrincipalAmount,
			&ml.PastDueAmount,
			&ml.PropertyAddressStreet, &ml.PropertyAddressCity,
			&ml.PropertyAddressRegion, &ml.PropertyAddressPostalCode, &ml.PropertyAddressCountry,
			&ml.YTDInterestPaid, &ml.YTDPrincipalPaid,
			&ml.SyncedAt, &ml.CreatedAt, &ml.UpdatedAt,
		); err != nil {
			return nil, err
		}
		liabilities = append(liabilities, ml)
	}

	return liabilities, nil
}

// GetByAccountID retrieves the mortgage liability for a specific account.
// Returns nil, nil when not found.
func (r *mortgageLiabilityRepository) GetByAccountID(ctx context.Context, accountID string) (*domain.MortgageLiability, error) {
	query := `
		SELECT id, user_id, account_id, account_number,
			current_late_fee, escrow_balance, has_pmi, has_prepayment_penalty,
			interest_rate_percentage, interest_rate_type,
			last_payment_amount, last_payment_date,
			loan_type_description, loan_term, maturity_date,
			next_monthly_payment, next_payment_due_date,
			origination_date, origination_principal_amount,
			past_due_amount,
			property_address_street, property_address_city,
			property_address_region, property_address_postal_code, property_address_country,
			ytd_interest_paid, ytd_principal_paid,
			synced_at, created_at, updated_at
		FROM mortgage_liabilities
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

	ml := &domain.MortgageLiability{}
	if err := rows.Scan(
		&ml.ID, &ml.UserID, &ml.AccountID, &ml.AccountNumber,
		&ml.CurrentLateFee, &ml.EscrowBalance, &ml.HasPMI, &ml.HasPrepaymentPenalty,
		&ml.InterestRatePercentage, &ml.InterestRateType,
		&ml.LastPaymentAmount, &ml.LastPaymentDate,
		&ml.LoanTypeDescription, &ml.LoanTerm, &ml.MaturityDate,
		&ml.NextMonthlyPayment, &ml.NextPaymentDueDate,
		&ml.OriginationDate, &ml.OriginationPrincipalAmount,
		&ml.PastDueAmount,
		&ml.PropertyAddressStreet, &ml.PropertyAddressCity,
		&ml.PropertyAddressRegion, &ml.PropertyAddressPostalCode, &ml.PropertyAddressCountry,
		&ml.YTDInterestPaid, &ml.YTDPrincipalPaid,
		&ml.SyncedAt, &ml.CreatedAt, &ml.UpdatedAt,
	); err != nil {
		return nil, err
	}

	return ml, nil
}
