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
	//
	// Kept as time.Time and NOT serialized: it is the source of the V:2 keyset cursor sort value
	// (repository folds it via time.DateOnly, and the cursor also carries it as SVT), so it has to
	// stay a real time.Time. SortDateDisplay below is what clients see as "sort_date".
	SortDate time.Time `json:"-"`
	// SortDateDisplay is SortDate as a UTC calendar day (YYYY-MM-DD) — the value that actually
	// serializes as "sort_date". See domain/date_display.go for why a DATE column may not be
	// rendered through a timestamp (#1631).
	SortDateDisplay string `json:"sort_date" format:"date" doc:"UTC calendar day (YYYY-MM-DD) this item sorts on"`

	// --- Core fields (always populated) ---
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	AccountID string `json:"account_id"`
	// Date is the row's own DATE column, kept as time.Time for the same cursor/bind reasons as
	// SortDate; DateDisplay is what serializes as "date" (#1631).
	Date        time.Time `json:"-"`
	DateDisplay string    `json:"date" format:"date" doc:"UTC calendar day (YYYY-MM-DD) of this item"`
	Amount      float64   `json:"amount"`
	Name        string    `json:"name"`

	// --- Banking-only fields (null for investment rows) ---
	UserAccountID *string `json:"user_account_id,omitempty"`
	LinkedItemID  *string `json:"linked_item_id,omitempty"`
	// AuthorizedDate stays a time.Time (it is the preferred half of the banking sort key);
	// AuthorizedDateDisplay is what serializes as "authorized_date". A *string rather than a
	// string so NULL stays an OMITTED key: the two most prominent UI sites render
	// formatDate(authorized_date || date), and an empty string would win that fallback and print
	// "Invalid Date" (#1631).
	AuthorizedDate        *time.Time `json:"-"`
	AuthorizedDateDisplay *string    `json:"authorized_date,omitempty" format:"date" doc:"UTC calendar day (YYYY-MM-DD) the transaction was authorized; omitted for investment rows and when the provider gave none"`
	MerchantName          *string    `json:"merchant_name,omitempty"`
	MerchantID            *string    `json:"merchant_id,omitempty"`
	EffectiveMerchantName *string    `json:"effective_merchant_name,omitempty"`
	DataSource            *string    `json:"data_source,omitempty"` // "plaid" | "manual" (TransactionSource)
	ExternalTransactionID *string    `json:"external_transaction_id,omitempty"`
	PFCPrimary            *string    `json:"pfc_primary,omitempty"`
	PFCDetailed           *string    `json:"pfc_detailed,omitempty"`
	PFCConfidenceLevel    *string    `json:"pfc_confidence_level,omitempty"`
	PFCCategoryIconURL    *string    `json:"pfc_category_icon_url,omitempty"`
	// PFC override raw values — needed to compute PFCPrimaryOverrideDisplay/PFCDetailedOverrideDisplay.
	PFCPrimaryOverride  *string `json:"pfc_primary_override,omitempty"`
	PFCDetailedOverride *string `json:"pfc_detailed_override,omitempty"`
	// Custom category override ID (used to look up CustomCategoryOverrideDisplay).
	CustomCategoryOverrideID *string `json:"custom_category_override_id,omitempty"`
	// Human-readable PFC display names (set by PopulatePFCDisplay; null for investment rows).
	PFCPrimaryDisplay          *string `json:"pfc_primary_display,omitempty"`
	PFCDetailedDisplay         *string `json:"pfc_detailed_display,omitempty"`
	PFCPrimaryOverrideDisplay  *string `json:"pfc_primary_override_display,omitempty"`
	PFCDetailedOverrideDisplay *string `json:"pfc_detailed_override_display,omitempty"`
	// Custom category display name (fetched from custom_categories; null when no override).
	CustomCategoryOverrideDisplay *string    `json:"custom_category_override_display,omitempty"`
	PaymentChannel                *string    `json:"payment_channel,omitempty"`
	Pending                       *bool      `json:"pending,omitempty"`
	UserNotes                     *string    `json:"user_notes,omitempty"`
	CategoryOverrideKind          *string    `json:"category_override_kind,omitempty"`
	EffectivePFCPrimary           *string    `json:"effective_pfc_primary,omitempty"`
	EffectivePFCDetailed          *string    `json:"effective_pfc_detailed,omitempty"`
	EffectiveCategoryName         *string    `json:"effective_category_name,omitempty"`
	Reviewed                      *bool      `json:"reviewed,omitempty"`
	ReviewedAt                    *time.Time `json:"reviewed_at,omitempty"`
	Tags                          []string   `json:"tags,omitempty"`
	Account                       *Account   `json:"account,omitempty"`

	// Reversed marks a banking row the PROVIDER withdrew after it had already posted — a bank
	// reversal or correction. Issue #1569.
	//
	// It is only ever true when the caller asked for reversals (include_reversed); every read
	// excludes tombstoned rows by default. A settled pending (a row removed while still pending)
	// is never returned at all and therefore never appears here: its posted twin is already in
	// the feed, and showing both would double-count the same money.
	//
	// A client that requests reversals MUST render this distinctly. An unlabelled reversal sitting
	// in a list is worse than a hidden one — it reads as a live transaction.
	Reversed   *bool      `json:"reversed,omitempty"`
	ReversedAt *time.Time `json:"reversed_at,omitempty"`

	// --- Investment-only fields (null for banking rows) ---
	// SecurityID is set for equity/fund transactions; null for cash transactions.
	SecurityID   *string `json:"security_id,omitempty"`
	TickerSymbol *string `json:"ticker_symbol,omitempty"`
	SecurityName *string `json:"security_name,omitempty"`
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
	reversed := t.Reversed()
	item := &ActivityItem{
		Source:                        FeedSourceBanking,
		SortDate:                      sortDate,
		ID:                            t.ID,
		UserID:                        t.UserID,
		AccountID:                     t.AccountID,
		Date:                          t.Date,
		Amount:                        t.Amount,
		Name:                          t.Name,
		UserAccountID:                 t.UserAccountID,
		LinkedItemID:                  &t.LinkedItemID,
		AuthorizedDate:                t.AuthorizedDate,
		MerchantName:                  t.MerchantName,
		MerchantID:                    t.MerchantID,
		EffectiveMerchantName:         t.EffectiveMerchantName,
		DataSource:                    &src,
		ExternalTransactionID:         t.ExternalTransactionID,
		PFCPrimary:                    t.PFCPrimary,
		PFCDetailed:                   t.PFCDetailed,
		PFCConfidenceLevel:            t.PFCConfidenceLevel,
		PFCCategoryIconURL:            t.PFCCategoryIconURL,
		PFCPrimaryOverride:            t.PFCPrimaryOverride,
		PFCDetailedOverride:           t.PFCDetailedOverride,
		CustomCategoryOverrideID:      t.CustomCategoryOverrideID,
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
		Reversed:                      &reversed,
	}
	if reversed {
		item.ReversedAt = t.DeletedAt
	}
	// Derived from this item own SortDate/Date/AuthorizedDate rather than copied from the
	// Transaction display fields, so the wrapper is correct even for a Transaction that never went
	// through the scan finalizer (#1631).
	item.PopulateDateDisplay()
	return item
}
