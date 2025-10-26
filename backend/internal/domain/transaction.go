package domain

import (
	"errors"
	"time"
)

// TransactionSource represents the source of a transaction data
// Supports multiple providers and manual transactions
type TransactionSource string

const (
	// SourcePlaid represents transactions synced from Plaid API (current implementation)
	SourcePlaid TransactionSource = "plaid"

	// SourceManual represents manually-entered transactions by users (future)
	SourceManual TransactionSource = "manual"

	// SourceTeller represents transactions synced from Teller API (future provider)
	SourceTeller TransactionSource = "teller"

	// SourceFinicity represents transactions synced from Finicity API (future provider)
	SourceFinicity TransactionSource = "finicity"

	// SourceYodlee represents transactions synced from Yodlee API (future provider)
	SourceYodlee TransactionSource = "yodlee"
)

// Transaction represents a financial transaction from any provider (Plaid, manual, or future providers).
// This is a provider-agnostic model that standardizes transaction data across all sources.
//
// Design: The struct uses generic field names (source, external_transaction_id) instead of
// provider-specific names (plaid_transaction_id) to support multiple providers and manual
// transactions without schema changes.
type Transaction struct {
	// Core identifiers
	ID           string `json:"id" db:"id"`
	UserID       string `json:"user_id" db:"user_id"`
	AccountID    string `json:"account_id" db:"account_id"`
	LinkedItemID string `json:"linked_item_id" db:"linked_item_id"`

	// Provider abstraction
	// Source identifies where this transaction came from (plaid, manual, teller, etc.)
	Source TransactionSource `json:"source" db:"source"`

	// ExternalTransactionID is the provider's unique transaction identifier.
	// For Plaid: transaction_id from Plaid API
	// For Teller: transaction_id from Teller API
	// For manual: NULL (users don't have external IDs)
	ExternalTransactionID *string `json:"external_transaction_id,omitempty" db:"external_transaction_id"`

	// ExternalMetadata stores provider-specific data as JSON
	// For Plaid: logo_url, website, location data, etc.
	// For Teller: provider-specific enrichment data
	// For manual: NULL or user-added structured data
	ExternalMetadata *string `json:"external_metadata,omitempty" db:"external_metadata"`

	// Core transaction fields (standardized across all providers)
	Amount         float64    `json:"amount" db:"amount"`                                             // Positive=credit, Negative=debit (Plaid convention)
	Date           time.Time  `json:"date" db:"date"`                                                 // Transaction date
	AuthorizedDate *time.Time `json:"authorized_date,omitempty" db:"authorized_date"`                 // Authorization date (may differ from settlement)
	Name           string     `json:"name" db:"name"`                                                 // Transaction description
	MerchantName   *string    `json:"merchant_name,omitempty" db:"merchant_name"`                     // Merchant name if available

	// Categorization (standardized format)
	Category                *string `json:"category,omitempty" db:"category"`                           // JSON array of category strings
	PersonalFinanceCategory *string `json:"personal_finance_category,omitempty" db:"personal_finance_category"` // Detailed categorization with confidence

	// Payment details
	PaymentChannel *string `json:"payment_channel,omitempty" db:"payment_channel"` // online, in store, other
	Pending        bool    `json:"pending" db:"pending"`                           // Whether transaction is pending

	// User customization
	UserNotes *string `json:"user_notes,omitempty" db:"user_notes"` // User-added notes or annotations

	// Timestamps
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// Validate checks that the Transaction has all required fields and valid values.
// Returns an error if validation fails.
//
// Validation rules:
//   - UserID is required
//   - Source is required and must be a valid TransactionSource
//   - ExternalTransactionID is required for all non-manual transactions
//   - Name is required
func (t *Transaction) Validate() error {
	if t.UserID == "" {
		return errors.New("user_id is required")
	}

	if t.Source == "" {
		return errors.New("source is required")
	}

	// Validate source is a known provider
	validSources := map[TransactionSource]bool{
		SourcePlaid:    true,
		SourceManual:   true,
		SourceTeller:   true,
		SourceFinicity: true,
		SourceYodlee:   true,
	}
	if !validSources[t.Source] {
		return errors.New("invalid transaction source")
	}

	// External ID is required for all non-manual transactions
	// Manual transactions are user-created and don't have provider IDs
	if t.Source != SourceManual {
		if t.ExternalTransactionID == nil || *t.ExternalTransactionID == "" {
			return errors.New("external_transaction_id required for non-manual transactions")
		}
	}

	if t.Name == "" {
		return errors.New("name is required")
	}

	return nil
}
