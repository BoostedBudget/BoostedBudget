package domain

import (
	"errors"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/pfc"
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

// CategorySource represents where the current category came from
type CategorySource string

const (
	// CategorySourcePlaid is the original category from Plaid API
	CategorySourcePlaid CategorySource = "plaid"

	// CategorySourceRule is a category applied by an automation rule
	CategorySourceRule CategorySource = "rule"

	// CategorySourceManual is a category manually set by the user
	CategorySourceManual CategorySource = "manual"
)

// TagSource tracks the origin of a tag on a transaction
type TagSource struct {
	Tag    string  `json:"tag"`
	Source string  `json:"source"` // "manual" or "rule"
	RuleID *string `json:"rule_id,omitempty"`
}

// Transaction represents a financial transaction from any provider (Plaid, manual, or future providers).
// This is a provider-agnostic model that standardizes transaction data across all sources.
//
// Design: The struct uses generic field names (source, external_transaction_id) instead of
// provider-specific names (plaid_transaction_id) to support multiple providers and manual
// transactions without schema changes.
//
// Issue #171: Added UserAccountID for stable queries across re-links.
// The UserAccountID links to a user_accounts record that survives when a user
// re-links a bank (which assigns new item_id and account_id values).
type Transaction struct {
	// Core identifiers
	ID           string `json:"id" db:"id"`
	UserID       string `json:"user_id" db:"user_id"`
	AccountID    string `json:"account_id" db:"account_id"`
	LinkedItemID string `json:"linked_item_id" db:"linked_item_id"`

	// Stable reference for re-link handling (Issue #171)
	// Links to user_accounts table which survives Plaid Item re-links
	UserAccountID *string `json:"user_account_id,omitempty" db:"user_account_id"`

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
	Amount         float64    `json:"amount" db:"amount"`                                    // Positive=outflow, Negative=inflow (Plaid PFCv2)
	Date           time.Time  `json:"date" db:"date"`
	AuthorizedDate *time.Time `json:"authorized_date,omitempty" db:"authorized_date"`
	Name           string     `json:"name" db:"name"`
	MerchantName   *string    `json:"merchant_name,omitempty" db:"merchant_name"`
	MerchantID     *string    `json:"merchant_id,omitempty" db:"merchant_id"`

	// PFCv2 source columns (from Plaid)
	PFCPrimary           *string `json:"pfc_primary,omitempty" db:"pfc_primary"`
	PFCDetailed          *string `json:"pfc_detailed,omitempty" db:"pfc_detailed"`
	PFCConfidenceLevel   *string `json:"pfc_confidence_level,omitempty" db:"pfc_confidence_level"`
	PFCCategoryIconURL   *string `json:"pfc_category_icon_url,omitempty" db:"pfc_category_icon_url"`

	// PFC display names (not stored in DB)
	PFCPrimaryDisplay          *string `json:"pfc_primary_display,omitempty" db:"-"`
	PFCDetailedDisplay         *string `json:"pfc_detailed_display,omitempty" db:"-"`
	PFCPrimaryOverrideDisplay  *string `json:"pfc_primary_override_display,omitempty" db:"-"`
	PFCDetailedOverrideDisplay *string `json:"pfc_detailed_override_display,omitempty" db:"-"`

	// Custom category display name (not stored in DB)
	CustomCategoryOverrideDisplay *string `json:"custom_category_override_display,omitempty" db:"-"`

	// Payment details
	PaymentChannel *string `json:"payment_channel,omitempty" db:"payment_channel"`
	Pending        bool    `json:"pending" db:"pending"`

	// User customization
	UserNotes *string `json:"user_notes,omitempty" db:"user_notes"`

	// Category override — discriminated union (category_override_kind determines which branch is active)
	CategoryOverrideKind       *string        `json:"category_override_kind,omitempty" db:"category_override_kind"`
	PFCPrimaryOverride         *string        `json:"pfc_primary_override,omitempty" db:"pfc_primary_override"`
	PFCDetailedOverride        *string        `json:"pfc_detailed_override,omitempty" db:"pfc_detailed_override"`
	CustomCategoryOverrideID   *string        `json:"custom_category_override_id,omitempty" db:"custom_category_override_id"`
	CategorySourceField        CategorySource `json:"category_source" db:"category_source"`
	CategoryOverrideDate       *time.Time     `json:"category_override_date,omitempty" db:"category_override_date"`

	// Effective category columns (denormalized, generated or app-maintained)
	EffectivePFCPrimary   *string `json:"effective_pfc_primary,omitempty" db:"effective_pfc_primary"`
	EffectivePFCDetailed  *string `json:"effective_pfc_detailed,omitempty" db:"effective_pfc_detailed"`
	EffectiveCategoryName *string `json:"effective_category_name,omitempty" db:"effective_category_name"`

	// Tags
	Tags       []string    `json:"tags" db:"tags"`
	TagsSource []TagSource `json:"tags_source" db:"tags_source"`

	// Timestamps
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`

	// Associated data (populated on demand, not stored in DB)
	Account *Account `json:"account,omitempty" db:"-"`
}

// EffectiveCategoryKind is the discriminator for which category dimension is active.
type EffectiveCategoryKind string

const (
	EffectiveCategoryKindPFC    EffectiveCategoryKind = "pfc"
	EffectiveCategoryKindCustom EffectiveCategoryKind = "custom"
)

// EffectiveCategory is the resolved category for a transaction — either PFC or custom, never both.
type EffectiveCategory struct {
	Kind               EffectiveCategoryKind `json:"kind"`
	PFCPrimary         *string               `json:"pfc_primary,omitempty"`
	PFCDetailed        *string               `json:"pfc_detailed,omitempty"`
	PFCPrimaryDisplay  *string               `json:"pfc_primary_display,omitempty"`
	PFCDetailedDisplay *string               `json:"pfc_detailed_display,omitempty"`
	CustomCategoryID   *string               `json:"custom_category_id,omitempty"`
}

// GetEffectiveCategory resolves the active category for this transaction.
// Priority: override (custom > pfc) > source PFC > nil (uncategorized).
// Display fields on the returned EffectiveCategory are populated from the pfc registry.
func (t *Transaction) GetEffectiveCategory() *EffectiveCategory {
	if t.CategoryOverrideKind != nil {
		switch *t.CategoryOverrideKind {
		case "custom":
			return &EffectiveCategory{Kind: EffectiveCategoryKindCustom, CustomCategoryID: t.CustomCategoryOverrideID}
		case "pfc":
			ec := &EffectiveCategory{Kind: EffectiveCategoryKindPFC, PFCPrimary: t.PFCPrimaryOverride, PFCDetailed: t.PFCDetailedOverride}
			if t.PFCPrimaryOverride != nil && *t.PFCPrimaryOverride != "" {
				d := pfc.PrimaryDisplayName(*t.PFCPrimaryOverride)
				ec.PFCPrimaryDisplay = &d
			}
			if t.PFCPrimaryOverride != nil && t.PFCDetailedOverride != nil && *t.PFCDetailedOverride != "" {
				d := pfc.DetailedDisplayName(*t.PFCPrimaryOverride, *t.PFCDetailedOverride)
				ec.PFCDetailedDisplay = &d
			}
			return ec
		}
	}
	if t.PFCPrimary != nil {
		ec := &EffectiveCategory{Kind: EffectiveCategoryKindPFC, PFCPrimary: t.PFCPrimary, PFCDetailed: t.PFCDetailed}
		if *t.PFCPrimary != "" {
			d := pfc.PrimaryDisplayName(*t.PFCPrimary)
			ec.PFCPrimaryDisplay = &d
		}
		if t.PFCDetailed != nil && *t.PFCDetailed != "" {
			d := pfc.DetailedDisplayName(*t.PFCPrimary, *t.PFCDetailed)
			ec.PFCDetailedDisplay = &d
		}
		return ec
	}
	return nil
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

// IsOutflow returns true when the transaction is an outflow (positive amount = spending/debit under Plaid PFCv2).
func (t Transaction) IsOutflow() bool { return t.Amount > 0 }

// IsInflow returns true when the transaction is an inflow (negative amount = refund/income under Plaid PFCv2).
func (t Transaction) IsInflow() bool { return t.Amount < 0 }

// ExpenseAmount returns the transaction amount when it is an outflow, and 0 otherwise.
// Always non-negative.
func (t Transaction) ExpenseAmount() float64 {
	if t.Amount > 0 {
		return t.Amount
	}
	return 0
}

// IncomeAmount returns the absolute transaction amount when it is an inflow, and 0 otherwise.
// Always non-negative.
func (t Transaction) IncomeAmount() float64 {
	if t.Amount < 0 {
		return -t.Amount
	}
	return 0
}

// TransactionFilters represents optional filters for querying transactions.
// All fields are optional pointers - nil means no filter applied for that field.
// Used by repository layer to build dynamic WHERE clauses.
type TransactionFilters struct {
	// AccountID filters transactions by specific account (deprecated: use AccountIDs)
	AccountID *string

	// AccountIDs filters transactions by multiple accounts (OR logic within)
	// Issue #529: Multi-account filter support
	AccountIDs []string

	// StartDate filters transactions on or after this date (inclusive)
	StartDate *time.Time

	// EndDate filters transactions on or before this date (inclusive)
	EndDate *time.Time

	// PFCPrimaries filters by effective PFC primary enum(s) (OR logic within).
	// Matches the effective category: override if set, else source pfc_primary.
	PFCPrimaries []string

	// CustomCategoryIDs filters by effective custom category override ID(s) (OR logic within).
	CustomCategoryIDs []string

	// Search filters transactions by merchant name or transaction name
	// Uses ILIKE for case-insensitive partial matching
	Search *string

	// Pending filters transactions by pending status
	// True = only pending, False = only settled, nil = both
	Pending *bool

	// MinAmount filters transactions with absolute amount >= this value
	// Issue #529: Amount range filter support
	MinAmount *float64

	// MaxAmount filters transactions with absolute amount <= this value
	// Issue #529: Amount range filter support
	MaxAmount *float64

	// AmountType filters transactions by direction: "income" (amount < 0, inflow), "expense" (amount > 0, outflow), or nil for both
	// Issue #529: Transaction type filter support
	AmountType *string

	// UserAccountID filters transactions by logical account identity (user_accounts.id).
	// Stable across re-links: returns transactions from both old and new chain members
	// for this logical account. Use instead of AccountID when the caller has a
	// user_account_id (e.g. the frontend after re-link transparency work in #1109).
	UserAccountID *string

	// MerchantID filters transactions by a specific merchant UUID.
	// Nil means no filter; non-nil matches exactly the given merchant_id.
	MerchantID *string

	// SortBy specifies the column to sort by. Allowed values: date, merchant, amount, category.
	// Defaults to "date" when empty. Validated by Huma enum tag at the operation layer.
	SortBy string

	// SortDir specifies sort direction. Allowed values: asc, desc. Defaults to "desc" when empty.
	SortDir string
}
