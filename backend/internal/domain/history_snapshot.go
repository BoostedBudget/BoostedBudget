package domain

import (
	"encoding/json"
	"time"
)

// HoldingSnapshot is one investment position as it stood on one UTC calendar day (#1569 T6).
//
// The `holdings` table is DELETEd and re-INSERTed in full on every sync, so it only ever answers
// "what does this user hold right now". holding_snapshots answers "what did they hold then", and
// is written inside the very same transaction as that replace — never instead of it. Replace
// semantics on holdings are deliberately untouched: portfolio_snapshot_repository.go's Upsert
// re-derives a PAST day's total by SUMming the live holdings rows, and would return zero if those
// rows were moved out from under it.
type HoldingSnapshot struct {
	ID            string  `json:"id" db:"id"`
	UserID        string  `json:"user_id" db:"user_id"`
	AccountID     string  `json:"account_id" db:"account_id"`
	UserAccountID *string `json:"user_account_id,omitempty" db:"user_account_id"`
	SecurityID    string  `json:"security_id" db:"security_id"`

	// AsOfDate is the UTC calendar day this position is recorded for.
	AsOfDate time.Time `json:"as_of_date" db:"as_of_date"`
	// CapturedAt is the transaction timestamp of the capture that last wrote this row.
	//
	// ⚠ It is provenance, NOT a selection key. An earlier draft of this task asked readers to
	// filter on MAX(captured_at) per (account_id, as_of_date) to find a day's final position set;
	// that rule is GONE and must not be reintroduced. Present is what identifies the final set —
	// a capture marks everything it did not re-observe absent, so the present rows for a day ARE
	// its last capture, by construction. Filtering on captured_at instead would silently
	// resurrect a position that a confirmed-complete Plaid response had cleared.
	CapturedAt time.Time `json:"captured_at" db:"captured_at"`
	// Present is FALSE once a capture for this (account, as_of_date) stopped observing this
	// security — sold between two same-day captures, or cleared by a confirmed-complete Plaid
	// response. The row is kept rather than deleted so its last known values survive; any
	// reconstruction of a day's positions MUST filter on it.
	Present bool `json:"present" db:"present"`

	Quantity         float64  `json:"quantity" db:"quantity"`
	CostBasis        *float64 `json:"cost_basis,omitempty" db:"cost_basis"`
	InstitutionValue *float64 `json:"institution_value,omitempty" db:"institution_value"`
	InstitutionPrice *float64 `json:"institution_price,omitempty" db:"institution_price"`
	// CurrencyCode is the currency the three figures above were reported in, copied from
	// holdings.currency_code at capture time (#1631 M8a). NULL = unknown, which is the state of
	// every row captured before migration 000202. Frozen with the rest of the row deliberately:
	// re-reading it from the live holding would let a historical value silently change units.
	CurrencyCode *string `json:"currency_code,omitempty" db:"currency_code"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// Liability type discriminators for LiabilityStatement.LiabilityType. These are the only values
// chk_liability_statements_type accepts.
const (
	LiabilityTypeCredit      = "credit"
	LiabilityTypeMortgage    = "mortgage"
	LiabilityTypeStudentLoan = "student_loan"
)

// LiabilityStatement is one account's liability state on one UTC calendar day (#1569 T6).
//
// liability_sync_service.go clears a liability outright when Plaid stops reporting it, so a single
// partial response used to erase an APR schedule with no trace. This table is written beside those
// replaces and is never deleted by a sync.
type LiabilityStatement struct {
	ID            string `json:"id" db:"id"`
	UserID        string `json:"user_id" db:"user_id"`
	AccountID     string `json:"account_id" db:"account_id"`
	LiabilityType string `json:"liability_type" db:"liability_type"`

	AsOfDate   time.Time `json:"as_of_date" db:"as_of_date"`
	CapturedAt time.Time `json:"captured_at" db:"captured_at"`
	// Present is FALSE once the delete-on-absence path cleared this liability on this UTC day.
	// Details is deliberately left intact so the APR schedule survives the clear.
	Present bool `json:"present" db:"present"`

	IsOverdue              *bool      `json:"is_overdue,omitempty" db:"is_overdue"`
	LastPaymentAmount      *float64   `json:"last_payment_amount,omitempty" db:"last_payment_amount"`
	LastPaymentDate        *time.Time `json:"last_payment_date,omitempty" db:"last_payment_date"`
	LastStatementIssueDate *time.Time `json:"last_statement_issue_date,omitempty" db:"last_statement_issue_date"`
	LastStatementBalance   *float64   `json:"last_statement_balance,omitempty" db:"last_statement_balance"`
	MinimumPaymentAmount   *float64   `json:"minimum_payment_amount,omitempty" db:"minimum_payment_amount"`
	NextPaymentDueDate     *time.Time `json:"next_payment_due_date,omitempty" db:"next_payment_due_date"`
	InterestRatePercentage *float64   `json:"interest_rate_percentage,omitempty" db:"interest_rate_percentage"`

	// Details is to_jsonb() of the complete source liability row, plus an "aprs" array for credit.
	// The promoted columns above are a query-friendly projection of it, not a replacement.
	Details json.RawMessage `json:"details" db:"details"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}
