package domain

import "time"

// FeedSource discriminates a unified-feed item as originating from the banking
// transaction table or the investment_transactions table.
const (
	FeedSourceBanking    = "banking"
	FeedSourceInvestment = "investment"
)

// ActivityItem is the superset item returned by GET /transactions when
// include_investment_activity=true (the default). Every item in the response
// carries the source discriminator; investment-specific fields are null for
// banking rows and vice versa.
//
// Fields are inlined explicitly (not embedded via anonymous struct) to ensure
// the Huma-generated OpenAPI schema is a flat object rather than an allOf
// reference, which guarantees backward compatibility with existing consumers
// of the transaction list response.
//
// Banking consumers: source="banking", all Transaction fields populated,
// investment-specific fields null.
//
// Investment consumers: source="investment", core date/amount/name/account_id
// populated, investment-specific fields populated, banking-specific fields null.
type ActivityItem struct {
	// Feed discriminator — always present.
	// "banking" for transactions rows, "investment" for investment_transactions rows.
	Source string `json:"source"`

	// SortDate is the shared ordering column used by the UNION keyset.
	// For banking: COALESCE(authorized_date, date)::date.
	// For investment: date::date.
	// Exposed so clients can rely on a consistent sort field.
	SortDate time.Time `json:"sort_date"`

	// --- Core fields (always populated) ---
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	AccountID string    `json:"account_id"`
	Date      time.Time `json:"date"`
	Amount    float64   `json:"amount"`
	Name      string    `json:"name"`

	// --- Banking-only fields (null for investment rows) ---
	UserAccountID         *string        `json:"user_account_id,omitempty"`
	LinkedItemID          *string        `json:"linked_item_id,omitempty"`
	AuthorizedDate        *time.Time     `json:"authorized_date,omitempty"`
	MerchantName          *string        `json:"merchant_name,omitempty"`
	MerchantID            *string        `json:"merchant_id,omitempty"`
	DataSource            *string        `json:"data_source,omitempty"` // "plaid" | "manual" (TransactionSource)
	ExternalTransactionID *string        `json:"external_transaction_id,omitempty"`
	PFCPrimary            *string        `json:"pfc_primary,omitempty"`
	PFCDetailed           *string        `json:"pfc_detailed,omitempty"`
	PFCConfidenceLevel    *string        `json:"pfc_confidence_level,omitempty"`
	PFCCategoryIconURL    *string        `json:"pfc_category_icon_url,omitempty"`
	// Human-readable PFC display names (set by PopulatePFCDisplay; null for investment rows).
	PFCPrimaryDisplay          *string `json:"pfc_primary_display,omitempty"`
	PFCDetailedDisplay         *string `json:"pfc_detailed_display,omitempty"`
	PFCPrimaryOverrideDisplay  *string `json:"pfc_primary_override_display,omitempty"`
	PFCDetailedOverrideDisplay *string `json:"pfc_detailed_override_display,omitempty"`
	// Custom category display name (fetched from custom_categories; null when no override).
	CustomCategoryOverrideDisplay *string `json:"custom_category_override_display,omitempty"`
	PaymentChannel        *string        `json:"payment_channel,omitempty"`
	Pending               *bool          `json:"pending,omitempty"`
	UserNotes             *string        `json:"user_notes,omitempty"`
	CategoryOverrideKind  *string        `json:"category_override_kind,omitempty"`
	EffectivePFCPrimary   *string        `json:"effective_pfc_primary,omitempty"`
	EffectivePFCDetailed  *string        `json:"effective_pfc_detailed,omitempty"`
	EffectiveCategoryName *string        `json:"effective_category_name,omitempty"`
	Reviewed              *bool          `json:"reviewed,omitempty"`
	ReviewedAt            *time.Time     `json:"reviewed_at,omitempty"`
	Tags                  []string       `json:"tags,omitempty"`
	Account               *Account       `json:"account,omitempty"`

	// --- Investment-only fields (null for banking rows) ---
	// SecurityID is set for equity/fund transactions; null for cash transactions.
	SecurityID         *string  `json:"security_id,omitempty"`
	TickerSymbol       *string  `json:"ticker_symbol,omitempty"`
	SecurityName       *string  `json:"security_name,omitempty"`
	// ActivityType is the normalized trade type: buy/sell/dividend/reinvest/fee/cash/other.
	ActivityType       *string  `json:"activity_type,omitempty"`
	InvestmentSubtype  *string  `json:"investment_subtype,omitempty"` // raw Plaid subtype
	InvestmentQuantity *float64 `json:"investment_quantity,omitempty"`
	InvestmentPrice    *float64 `json:"investment_price,omitempty"`
	InvestmentFees     *float64 `json:"investment_fees,omitempty"`
}

// NewBankingActivityItem wraps a Transaction as an ActivityItem with source="banking".
// SortDate is set to COALESCE(authorized_date, date) to match the SQL alias.
func NewBankingActivityItem(t *Transaction) *ActivityItem {
	sortDate := t.Date
	if t.AuthorizedDate != nil {
		sortDate = *t.AuthorizedDate
	}
	src := string(t.Source)
	pending := t.Pending
	reviewed := t.Reviewed
	linkedItemID := t.LinkedItemID
	return &ActivityItem{
		Source:                        FeedSourceBanking,
		SortDate:                      sortDate,
		ID:                            t.ID,
		UserID:                        t.UserID,
		AccountID:                     t.AccountID,
		Date:                          t.Date,
		Amount:                        t.Amount,
		Name:                          t.Name,
		UserAccountID:                 t.UserAccountID,
		LinkedItemID:                  &linkedItemID,
		AuthorizedDate:                t.AuthorizedDate,
		MerchantName:                  t.MerchantName,
		MerchantID:                    t.MerchantID,
		DataSource:                    &src,
		ExternalTransactionID:         t.ExternalTransactionID,
		PFCPrimary:                    t.PFCPrimary,
		PFCDetailed:                   t.PFCDetailed,
		PFCConfidenceLevel:            t.PFCConfidenceLevel,
		PFCCategoryIconURL:            t.PFCCategoryIconURL,
		PFCPrimaryDisplay:             t.PFCPrimaryDisplay,
		PFCDetailedDisplay:            t.PFCDetailedDisplay,
		PFCPrimaryOverrideDisplay:     t.PFCPrimaryOverrideDisplay,
		PFCDetailedOverrideDisplay:    t.PFCDetailedOverrideDisplay,
		CustomCategoryOverrideDisplay: t.CustomCategoryOverrideDisplay,
		PaymentChannel:                t.PaymentChannel,
		Pending:                       &pending,
		UserNotes:                     t.UserNotes,
		CategoryOverrideKind:          t.CategoryOverrideKind,
		EffectivePFCPrimary:           t.EffectivePFCPrimary,
		EffectivePFCDetailed:          t.EffectivePFCDetailed,
		EffectiveCategoryName:         t.EffectiveCategoryName,
		Reviewed:                      &reviewed,
		ReviewedAt:                    t.ReviewedAt,
		Tags:                          t.Tags,
		Account:                       t.Account,
	}
}
