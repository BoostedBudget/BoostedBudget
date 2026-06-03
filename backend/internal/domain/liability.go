package domain

import (
	"errors"
	"time"
)

// CreditLiability represents credit card liability data from Plaid.
type CreditLiability struct {
	ID                     string               `json:"id" db:"id"`
	UserID                 string               `json:"user_id" db:"user_id"`
	AccountID              string               `json:"account_id" db:"account_id"`
	IsOverdue              *bool                `json:"is_overdue,omitempty" db:"is_overdue"`
	LastPaymentAmount      *float64             `json:"last_payment_amount,omitempty" db:"last_payment_amount"`
	LastPaymentDate        *time.Time           `json:"last_payment_date,omitempty" db:"last_payment_date"`
	LastStatementIssueDate *time.Time           `json:"last_statement_issue_date,omitempty" db:"last_statement_issue_date"`
	LastStatementBalance   *float64             `json:"last_statement_balance,omitempty" db:"last_statement_balance"`
	MinimumPaymentAmount   *float64             `json:"minimum_payment_amount,omitempty" db:"minimum_payment_amount"`
	NextPaymentDueDate     *time.Time           `json:"next_payment_due_date,omitempty" db:"next_payment_due_date"`
	APRs                   []CreditLiabilityAPR `json:"aprs,omitempty" db:"-"` // transient: not stored in credit_liabilities row
	SyncedAt               time.Time            `json:"synced_at" db:"synced_at"`
	CreatedAt              time.Time            `json:"created_at" db:"created_at"`
	UpdatedAt              time.Time            `json:"updated_at" db:"updated_at"`
}

// Validate checks that the CreditLiability has all required fields.
func (c *CreditLiability) Validate() error {
	if c.UserID == "" {
		return errors.New("user_id is required")
	}
	if c.AccountID == "" {
		return errors.New("account_id is required")
	}
	return nil
}

// CreditLiabilityAPR represents an APR entry for a credit liability.
type CreditLiabilityAPR struct {
	ID                   string    `json:"id" db:"id"`
	CreditLiabilityID    string    `json:"credit_liability_id" db:"credit_liability_id"`
	RatePercentage       float64   `json:"rate_percentage" db:"rate_percentage"`
	RateType             string    `json:"rate_type" db:"rate_type"`
	BalanceSubjectToRate *float64  `json:"balance_subject_to_rate,omitempty" db:"balance_subject_to_rate"`
	InterestChargeAmount *float64  `json:"interest_charge_amount,omitempty" db:"interest_charge_amount"`
	CreatedAt            time.Time `json:"created_at" db:"created_at"`
	UpdatedAt            time.Time `json:"updated_at" db:"updated_at"`
}

// MortgageLiability represents mortgage liability data from Plaid.
type MortgageLiability struct {
	ID                         string     `json:"id" db:"id"`
	UserID                     string     `json:"user_id" db:"user_id"`
	AccountID                  string     `json:"account_id" db:"account_id"`
	AccountNumber              *string    `json:"account_number,omitempty" db:"account_number"`
	CurrentLateFee             *float64   `json:"current_late_fee,omitempty" db:"current_late_fee"`
	EscrowBalance              *float64   `json:"escrow_balance,omitempty" db:"escrow_balance"`
	HasPMI                     *bool      `json:"has_pmi,omitempty" db:"has_pmi"`
	HasPrepaymentPenalty       *bool      `json:"has_prepayment_penalty,omitempty" db:"has_prepayment_penalty"`
	InterestRatePercentage     *float64   `json:"interest_rate_percentage,omitempty" db:"interest_rate_percentage"`
	InterestRateType           *string    `json:"interest_rate_type,omitempty" db:"interest_rate_type"`
	LastPaymentAmount          *float64   `json:"last_payment_amount,omitempty" db:"last_payment_amount"`
	LastPaymentDate            *time.Time `json:"last_payment_date,omitempty" db:"last_payment_date"`
	LoanTypeDescription        *string    `json:"loan_type_description,omitempty" db:"loan_type_description"`
	LoanTerm                   *string    `json:"loan_term,omitempty" db:"loan_term"`
	MaturityDate               *time.Time `json:"maturity_date,omitempty" db:"maturity_date"`
	NextMonthlyPayment         *float64   `json:"next_monthly_payment,omitempty" db:"next_monthly_payment"`
	NextPaymentDueDate         *time.Time `json:"next_payment_due_date,omitempty" db:"next_payment_due_date"`
	OriginationDate            *time.Time `json:"origination_date,omitempty" db:"origination_date"`
	OriginationPrincipalAmount *float64   `json:"origination_principal_amount,omitempty" db:"origination_principal_amount"`
	PastDueAmount              *float64   `json:"past_due_amount,omitempty" db:"past_due_amount"`
	PropertyAddressStreet      *string    `json:"property_address_street,omitempty" db:"property_address_street"`
	PropertyAddressCity        *string    `json:"property_address_city,omitempty" db:"property_address_city"`
	PropertyAddressRegion      *string    `json:"property_address_region,omitempty" db:"property_address_region"`
	PropertyAddressPostalCode  *string    `json:"property_address_postal_code,omitempty" db:"property_address_postal_code"`
	PropertyAddressCountry     *string    `json:"property_address_country,omitempty" db:"property_address_country"`
	YTDInterestPaid            *float64   `json:"ytd_interest_paid,omitempty" db:"ytd_interest_paid"`
	YTDPrincipalPaid           *float64   `json:"ytd_principal_paid,omitempty" db:"ytd_principal_paid"`
	SyncedAt                   time.Time  `json:"synced_at" db:"synced_at"`
	CreatedAt                  time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt                  time.Time  `json:"updated_at" db:"updated_at"`
}

// Validate checks that the MortgageLiability has all required fields.
func (m *MortgageLiability) Validate() error {
	if m.UserID == "" {
		return errors.New("user_id is required")
	}
	if m.AccountID == "" {
		return errors.New("account_id is required")
	}
	return nil
}

// StudentLoanLiability represents student loan liability data from Plaid.
type StudentLoanLiability struct {
	ID                           string     `json:"id" db:"id"`
	UserID                       string     `json:"user_id" db:"user_id"`
	AccountID                    string     `json:"account_id" db:"account_id"`
	AccountNumber                *string    `json:"account_number,omitempty" db:"account_number"`
	DisbursementDates            []string   `json:"disbursement_dates,omitempty" db:"disbursement_dates"` // DATE[] stored as []string "YYYY-MM-DD"
	ExpectedPayoffDate           *time.Time `json:"expected_payoff_date,omitempty" db:"expected_payoff_date"`
	Guarantor                    *string    `json:"guarantor,omitempty" db:"guarantor"`
	InterestRatePercentage       *float64   `json:"interest_rate_percentage,omitempty" db:"interest_rate_percentage"`
	IsOverdue                    *bool      `json:"is_overdue,omitempty" db:"is_overdue"`
	LastPaymentAmount            *float64   `json:"last_payment_amount,omitempty" db:"last_payment_amount"`
	LastPaymentDate              *time.Time `json:"last_payment_date,omitempty" db:"last_payment_date"`
	LastStatementIssueDate       *time.Time `json:"last_statement_issue_date,omitempty" db:"last_statement_issue_date"`
	LastStatementBalance         *float64   `json:"last_statement_balance,omitempty" db:"last_statement_balance"`
	LoanName                     *string    `json:"loan_name,omitempty" db:"loan_name"`
	LoanStatusType               *string    `json:"loan_status_type,omitempty" db:"loan_status_type"`
	LoanStatusEndDate            *time.Time `json:"loan_status_end_date,omitempty" db:"loan_status_end_date"`
	MinimumPaymentAmount         *float64   `json:"minimum_payment_amount,omitempty" db:"minimum_payment_amount"`
	NextPaymentDueDate           *time.Time `json:"next_payment_due_date,omitempty" db:"next_payment_due_date"`
	OriginationDate              *time.Time `json:"origination_date,omitempty" db:"origination_date"`
	OriginationPrincipalAmount   *float64   `json:"origination_principal_amount,omitempty" db:"origination_principal_amount"`
	OutstandingInterestAmount    *float64   `json:"outstanding_interest_amount,omitempty" db:"outstanding_interest_amount"`
	PaymentReferenceNumber       *string    `json:"payment_reference_number,omitempty" db:"payment_reference_number"`
	PSLFEstimatedEligibilityDate *time.Time `json:"pslf_estimated_eligibility_date,omitempty" db:"pslf_estimated_eligibility_date"`
	PSLFPaymentsMade             *int       `json:"pslf_payments_made,omitempty" db:"pslf_payments_made"`
	PSLFPaymentsRemaining        *int       `json:"pslf_payments_remaining,omitempty" db:"pslf_payments_remaining"`
	RepaymentPlanType            *string    `json:"repayment_plan_type,omitempty" db:"repayment_plan_type"`
	RepaymentPlanDescription     *string    `json:"repayment_plan_description,omitempty" db:"repayment_plan_description"`
	SequenceNumber               *string    `json:"sequence_number,omitempty" db:"sequence_number"`
	ServicerAddressStreet        *string    `json:"servicer_address_street,omitempty" db:"servicer_address_street"`
	ServicerAddressCity          *string    `json:"servicer_address_city,omitempty" db:"servicer_address_city"`
	ServicerAddressRegion        *string    `json:"servicer_address_region,omitempty" db:"servicer_address_region"`
	ServicerAddressPostalCode    *string    `json:"servicer_address_postal_code,omitempty" db:"servicer_address_postal_code"`
	ServicerAddressCountry       *string    `json:"servicer_address_country,omitempty" db:"servicer_address_country"`
	YTDInterestPaid              *float64   `json:"ytd_interest_paid,omitempty" db:"ytd_interest_paid"`
	YTDPrincipalPaid             *float64   `json:"ytd_principal_paid,omitempty" db:"ytd_principal_paid"`
	SyncedAt                     time.Time  `json:"synced_at" db:"synced_at"`
	CreatedAt                    time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt                    time.Time  `json:"updated_at" db:"updated_at"`
}

// Validate checks that the StudentLoanLiability has all required fields.
func (s *StudentLoanLiability) Validate() error {
	if s.UserID == "" {
		return errors.New("user_id is required")
	}
	if s.AccountID == "" {
		return errors.New("account_id is required")
	}
	return nil
}

// LiabilitySyncResult contains the results of a liabilities sync operation.
type LiabilitySyncResult struct {
	CreditProcessed       int
	MortgageProcessed     int
	StudentLoansProcessed int
	AccountsProcessed     int
	Errors                []string
}
