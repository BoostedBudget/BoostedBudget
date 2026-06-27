package domain

import "time"

// Merchant represents a merchant entity with a stable UUID.
// Merchants are populated from the merchant_name field on transactions
// and linked via the merchant_id FK.
type Merchant struct {
	ID                  string    `json:"id" db:"id"`
	Name                string    `json:"name" db:"name"`
	NormalizedName      *string   `json:"normalized_name,omitempty" db:"normalized_name"`
	CanonicalMerchantID *string   `json:"canonical_merchant_id,omitempty" db:"canonical_merchant_id"`
	CreatedAt           time.Time `json:"created_at" db:"created_at"`
}

// MerchantNameOverride is a per-user override that renames or merges a merchant.
// At least one of DisplayName or MergedIntoMerchantID must be non-nil (enforced by DB CHECK).
type MerchantNameOverride struct {
	ID                   string    `json:"id" db:"id"`
	UserID               string    `json:"user_id" db:"user_id"`
	MerchantID           string    `json:"merchant_id" db:"merchant_id"`
	DisplayName          *string   `json:"display_name,omitempty" db:"display_name"`
	MergedIntoMerchantID *string   `json:"merged_into_merchant_id,omitempty" db:"merged_into_merchant_id"`
	CreatedAt            time.Time `json:"created_at" db:"created_at"`
	UpdatedAt            time.Time `json:"updated_at" db:"updated_at"`
}

// MerchantSummary is a read-time aggregated view of a merchant from the
// perspective of a specific user. It incorporates user overrides (rename/merge)
// and aggregates transaction statistics. Used by ListMerchants.
type MerchantSummary struct {
	MerchantID           string   `json:"merchant_id"`
	NormalizedName       string   `json:"normalized_name"`
	EffectiveDisplayName string   `json:"effective_display_name"`
	RawVariants          []string `json:"raw_variants"`
	TransactionCount     int64    `json:"transaction_count"`
	TotalSpend           float64  `json:"total_spend"`
	DominantCategory     *string  `json:"dominant_category,omitempty"`
	HasOverride          bool     `json:"has_override"`
	IsMerged             bool     `json:"is_merged"`
	MergedIntoMerchantID *string  `json:"merged_into_merchant_id,omitempty"`
}
