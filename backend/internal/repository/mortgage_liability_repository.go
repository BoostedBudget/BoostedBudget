package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
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
	db  DB
	enc *appcrypto.FieldEncryptor
}

// NewMortgageLiabilityRepository creates a new MortgageLiabilityRepository instance.
func NewMortgageLiabilityRepository(db DB, enc *appcrypto.FieldEncryptor) MortgageLiabilityRepository {
	return &mortgageLiabilityRepository{db: db, enc: enc}
}

// encryptMortgagePIIFields encrypts all PII fields on the liability before writing to the database.
// Returns encrypted copies of the nullable string fields.
func (r *mortgageLiabilityRepository) encryptMortgagePIIFields(liability *domain.MortgageLiability) (
	accountNumber, street, city, region, postal, country *string, err error,
) {
	if r.enc == nil {
		return liability.AccountNumber,
			liability.PropertyAddressStreet, liability.PropertyAddressCity,
			liability.PropertyAddressRegion, liability.PropertyAddressPostalCode, liability.PropertyAddressCountry,
			nil
	}

	accountNumber, err = r.enc.EncryptNullableString(liability.AccountNumber)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("encrypt account_number: %w", err)
	}
	street, err = r.enc.EncryptNullableString(liability.PropertyAddressStreet)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("encrypt property_address_street: %w", err)
	}
	city, err = r.enc.EncryptNullableString(liability.PropertyAddressCity)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("encrypt property_address_city: %w", err)
	}
	region, err = r.enc.EncryptNullableString(liability.PropertyAddressRegion)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("encrypt property_address_region: %w", err)
	}
	postal, err = r.enc.EncryptNullableString(liability.PropertyAddressPostalCode)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("encrypt property_address_postal_code: %w", err)
	}
	country, err = r.enc.EncryptNullableString(liability.PropertyAddressCountry)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("encrypt property_address_country: %w", err)
	}
	return accountNumber, street, city, region, postal, country, nil
}

// decryptMortgagePIIFields decrypts all PII fields on a scanned mortgage liability.
func (r *mortgageLiabilityRepository) decryptMortgagePIIFields(ml *domain.MortgageLiability) error {
	if r.enc == nil {
		return nil
	}

	var err error
	ml.AccountNumber, err = r.enc.DecryptNullableString(ml.AccountNumber)
	if err != nil {
		return fmt.Errorf("decrypt account_number: %w", err)
	}
	ml.PropertyAddressStreet, err = r.enc.DecryptNullableString(ml.PropertyAddressStreet)
	if err != nil {
		return fmt.Errorf("decrypt property_address_street: %w", err)
	}
	ml.PropertyAddressCity, err = r.enc.DecryptNullableString(ml.PropertyAddressCity)
	if err != nil {
		return fmt.Errorf("decrypt property_address_city: %w", err)
	}
	ml.PropertyAddressRegion, err = r.enc.DecryptNullableString(ml.PropertyAddressRegion)
	if err != nil {
		return fmt.Errorf("decrypt property_address_region: %w", err)
	}
	ml.PropertyAddressPostalCode, err = r.enc.DecryptNullableString(ml.PropertyAddressPostalCode)
	if err != nil {
		return fmt.Errorf("decrypt property_address_postal_code: %w", err)
	}
	ml.PropertyAddressCountry, err = r.enc.DecryptNullableString(ml.PropertyAddressCountry)
	if err != nil {
		return fmt.Errorf("decrypt property_address_country: %w", err)
	}
	return nil
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

	encAccountNumber, encStreet, encCity, encRegion, encPostal, encCountry, err := r.encryptMortgagePIIFields(liability)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO mortgage_liabilities (id, user_id, account_id, account_number, current_late_fee, escrow_balance, has_pmi, has_prepayment_penalty, interest_rate_percentage, interest_rate_type, last_payment_amount, last_payment_date, loan_type_description, loan_term, maturity_date, next_monthly_payment, next_payment_due_date, origination_date, origination_principal_amount, past_due_amount, property_address_street, property_address_city, property_address_region, property_address_postal_code, property_address_country, ytd_interest_paid, ytd_principal_paid, synced_at, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30)`,
		liability.ID, liability.UserID, liability.AccountID,
		encAccountNumber, liability.CurrentLateFee, liability.EscrowBalance,
		liability.HasPMI, liability.HasPrepaymentPenalty,
		liability.InterestRatePercentage, liability.InterestRateType,
		liability.LastPaymentAmount, liability.LastPaymentDate,
		liability.LoanTypeDescription, liability.LoanTerm, liability.MaturityDate,
		liability.NextMonthlyPayment, liability.NextPaymentDueDate,
		liability.OriginationDate, liability.OriginationPrincipalAmount,
		liability.PastDueAmount,
		encStreet, encCity, encRegion, encPostal, encCountry,
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
		if err := r.decryptMortgagePIIFields(&ml); err != nil {
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

	if err := r.decryptMortgagePIIFields(ml); err != nil {
		return nil, err
	}

	return ml, nil
}
