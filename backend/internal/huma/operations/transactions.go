package operations

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/dateutil"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/money"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// TransactionHandler handles transaction-related HTTP requests.
type TransactionHandler struct {
	service         services.TransactionService
	db              repository.DB
	mcpAuditRepo    repository.MCPAuditRepository
	mcpWorkerSecret string
}

type TransactionHandlerDeps struct {
	DB              repository.DB
	MCPAuditRepo    repository.MCPAuditRepository
	MCPWorkerSecret string
}

// NewTransactionHandler creates a new TransactionHandler instance.
func NewTransactionHandler(service services.TransactionService, deps ...TransactionHandlerDeps) *TransactionHandler {
	h := &TransactionHandler{service: service}
	if len(deps) > 0 {
		h.db = deps[0].DB
		h.mcpAuditRepo = deps[0].MCPAuditRepo
		h.mcpWorkerSecret = deps[0].MCPWorkerSecret
	}
	return h
}

// --- Input/Output Types ---

// GetTransactionsInput represents the input for listing transactions.
type GetTransactionsInput struct {
	inputs.AuthenticatedInput
	inputs.CursorPaginationInput

	// Filter parameters (all optional - empty string means not set)
	AccountID         string `query:"account_id" doc:"Filter by specific account ID (deprecated: use account_ids)"`
	AccountIDs        string `query:"account_ids" doc:"Filter by multiple account IDs (comma-separated UUIDs)"`
	StartDate         string `query:"start_date" doc:"Filter transactions on or after this date (YYYY-MM-DD)"`
	EndDate           string `query:"end_date" doc:"Filter transactions on or before this date (YYYY-MM-DD)"`
	PFCPrimaries      string `query:"pfc_primaries" doc:"Filter by effective PFC primary enum(s) (comma-separated, e.g. FOOD_AND_DRINK,TRAVEL). OR-joined with pfc_detaileds, custom_category_ids and uncategorized: a transaction matches if ANY of them matches."`
	PFCDetaileds      string `query:"pfc_detaileds" doc:"Filter by effective PFC detailed sub-category enum(s) (comma-separated, e.g. FOOD_AND_DRINK_FAST_FOOD,FOOD_AND_DRINK_COFFEE). OR-joined with pfc_primaries, custom_category_ids and uncategorized."`
	CustomCategoryIDs string `query:"custom_category_ids" doc:"Filter by custom category UUIDs (comma-separated). Each id also matches its child categories. OR-joined with pfc_primaries, pfc_detaileds and uncategorized — combining them returns the union, not the (always empty) intersection."`
	Uncategorized     string `query:"uncategorized" doc:"When true, also match transactions with NO category at all — the rows analytics reports as 'Uncategorized'. OR-joined with pfc_primaries, pfc_detaileds and custom_category_ids. Omitted or false adds no filter; it never means 'exclude uncategorized'. Issue #1626."`
	Search            string `query:"search" doc:"Search in merchant name or transaction name"`
	Pending           string `query:"pending" doc:"Filter by pending status (true/false)"`
	Reviewed          string `query:"reviewed" doc:"Filter by review state: true (reviewed), false (unreviewed inbox), omit for both"`
	IncludeReversed   string `query:"include_reversed" doc:"Include transactions the bank REVERSED after they posted (default false). Reversed rows are returned with reversed=true and reversed_at set, and MUST be rendered distinctly — an unlabelled reversal reads as a live transaction. Transactions removed while still pending are never returned, by any value of this flag: their posted twin is already in the feed. Issue #1569."`
	CategorySources   string `query:"category_source" doc:"Filter by category origin (comma-separated): plaid, rule, manual"`
	MinAmount         string `query:"min_amount" doc:"Filter by minimum absolute amount"`
	MaxAmount         string `query:"max_amount" doc:"Filter by maximum absolute amount"`
	AmountType        string `query:"amount_type" enum:"income,expense,all" doc:"Filter by transaction type"`
	UserAccountID     string `query:"user_account_id" doc:"Filter by logical account ID (user_accounts.id) — stable across re-links; returns transactions from all chain members"`
	MerchantID        string `query:"merchant_id" doc:"Filter transactions by merchant UUID" example:"550e8400-e29b-41d4-a716-446655440000"`

	// Sort parameters
	SortBy  string `query:"sort_by"  enum:"date,merchant,amount,category" default:"date"  doc:"Column to sort by"`
	SortDir string `query:"sort_dir" enum:"asc,desc"                      default:"desc" doc:"Sort direction"`

	// Investment activity — Issue #138.
	// IncludeInvestmentActivity merges investment_transactions into the response feed.
	// Default: true (unified feed). When omitted on a non-date sort the flag silently
	// degrades to false (graceful degradation — existing non-date-sort clients are
	// unaffected). An explicit "true" on a non-date sort returns 400.
	IncludeInvestmentActivity string `query:"include_investment_activity" doc:"Merge investment transactions into feed (true/false, default true). When omitted with a non-date sort, silently degrades to false. Explicit true with non-date sort returns 400. Requires sort_by=date for explicit merge."`

	// ActivityTypes filters investment activity by normalized trade type when
	// include_investment_activity=true. Values: buy, sell, dividend, reinvest, interest, fee,
	// transfer, cash, other. #1631 added interest and transfer.
	ActivityTypes string `query:"activity_types" doc:"Filter investment activity by type (comma-separated: buy,sell,dividend,reinvest,interest,fee,transfer,cash,other). Only used when include_investment_activity=true."`

	// Scope must never be "household" — members must never drill into another member's
	// raw transactions (Issue #1369 decision #5). Rejected with 400 in the handler.
	Scope string `query:"scope" enum:"personal,household" doc:"Must be omitted or 'personal' — this endpoint does not support household scope (Issue #1369 decision #5)"`
}

// GetTransactionsOutput represents the response for listing transactions.
// Transactions is []*domain.ActivityItem — a superset of Transaction that adds
// Source ("banking"|"investment") and investment-specific fields. All banking
// fields are always populated for banking rows; investment fields are null.
// This is a backward-compatible change: existing banking consumers see all
// the same fields they previously consumed from []*domain.Transaction.
type GetTransactionsOutput struct {
	Body struct {
		Transactions []*domain.ActivityItem `json:"transactions"`
		NextCursor   *string                `json:"next_cursor,omitempty"`
		Limit        int                    `json:"limit"`
		// TotalCount and TotalAmount describe the WHOLE filtered set, not this page, and are
		// present only on the FIRST page (no cursor supplied) — re-aggregating the set on every
		// page of the busiest list in the app would cost real time for a number that does not
		// change as the user pages.
		//
		// ⚠ POINTERS so that "not computed" (absent) and "computed, and zero" (present zero) are
		// different responses. With non-pointer scalars the zero value IS the omitted value, so a
		// first page whose filter matched nothing could not report "0 results, $0.00", and the
		// default mixed income/expense feed — which nets to exactly zero under PFCv2 signs — sent
		// a count with no amount beside it. Pinned by
		// TestGetTransactions_WholeSetTotals_OmittedNeverZeroed.
		//
		// TotalAmount is SIGNED, following the rows (PFCv2: expenses positive, inflows negative),
		// so a client must not assume it is a magnitude. The chart-side comparison it exists to
		// make checkable is therefore on MAGNITUDES: a category-hierarchy node's amount is
		// SUM(ABS(amount)) — an unsigned chart quantity — so the property that holds is
		// |node amount| == |total_amount| over the same filtered set, not equality of the two
		// numbers. Added in #1626 so the reports Sankey's click-through is something a consumer —
		// or a test — can actually check.
		TotalCount  *int     `json:"total_count,omitempty" doc:"Number of transactions in the whole filtered set. First page only; absent means not computed, a present 0 means the set is empty."`
		TotalAmount *float64 `json:"total_amount,omitempty" doc:"Summed amount of the whole filtered set, SIGNED like the rows (PFCv2: expenses positive, inflows negative) — not a magnitude. A Sankey node's amount is an ABS sum, so the property that holds is |node amount| == |total_amount|. First page only; absent means not computed, a present 0 means the set nets to zero."`
	}
}

// GetTransactionByIDInput represents the input for getting a transaction by ID.
type GetTransactionByIDInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Transaction UUID"`
	// Scope must never be "household" (Issue #1369 decision #5). Rejected with 400.
	Scope string `query:"scope" enum:"personal,household" doc:"Must be omitted or 'personal' — this endpoint does not support household scope (Issue #1369 decision #5)"`
}

// GetTransactionByIDOutput represents the response for getting a transaction.
type GetTransactionByIDOutput struct {
	Body *domain.Transaction
}

// UpdateTransactionTagsInput represents the input for updating transaction tags.
type UpdateTransactionTagsInput struct {
	inputs.AuthenticatedInput
	ID   string                    `path:"id" doc:"Transaction UUID"`
	Body UpdateTransactionTagsBody `json:"body"`
}

// UpdateTransactionTagsBody represents the request body for updating tags.
type UpdateTransactionTagsBody struct {
	Tags []string `json:"tags" doc:"Array of tag names to set on the transaction"`
}

// UpdateTransactionTagsOutput represents the response for updating tags.
type UpdateTransactionTagsOutput struct {
	Body *domain.Transaction
}

// UpdateTransactionCategoryInput represents the input for updating transaction category.
type UpdateTransactionCategoryInput struct {
	inputs.AuthenticatedInput
	inputs.MCPContextInput
	ID   string                        `path:"id" doc:"Transaction UUID"`
	Body UpdateTransactionCategoryBody `json:"body"`
}

// UpdateTransactionCategoryBody represents the request body for updating category.
// kind must be "pfc", "custom", or "clear".
// When kind=pfc: pfc_primary is required, pfc_detailed is optional.
// When kind=custom: custom_category_id is required.
// When kind=clear: all override fields are removed.
type UpdateTransactionCategoryBody struct {
	Kind             string  `json:"kind" enum:"pfc,custom,clear" doc:"Category override kind"`
	PFCPrimary       *string `json:"pfc_primary,omitempty" doc:"PFC primary enum (required when kind=pfc)"`
	PFCDetailed      *string `json:"pfc_detailed,omitempty" doc:"PFC detailed enum (optional when kind=pfc)"`
	CustomCategoryID *string `json:"custom_category_id,omitempty" doc:"Custom category UUID (required when kind=custom)"`
}

// UpdateTransactionCategoryOutput represents the response for updating category.
type UpdateTransactionCategoryOutput struct {
	Body *domain.Transaction
}

// SetTransactionReviewedInput updates the review state of a single transaction (Issue #1203).
type SetTransactionReviewedInput struct {
	inputs.AuthenticatedInput
	inputs.MCPContextInput
	ID   string `path:"id" doc:"Transaction UUID"`
	Body struct {
		Reviewed bool `json:"reviewed" doc:"true to mark reviewed, false to move back to the unreviewed inbox"`
	} `json:"body"`
}

// BulkSetTransactionReviewedInput updates the review state of many transactions (Issue #1203).
type BulkSetTransactionReviewedInput struct {
	inputs.AuthenticatedInput
	inputs.MCPContextInput
	Body struct {
		TransactionIDs []string `json:"transaction_ids" doc:"Transaction UUIDs to update (owned by the caller)"`
		Reviewed       bool     `json:"reviewed" doc:"true to mark reviewed, false to move back to the unreviewed inbox"`
	} `json:"body"`
}

// SetTransactionReviewedOutput reports how many transactions were updated.
type SetTransactionReviewedOutput struct {
	Body struct {
		Updated int64 `json:"updated" doc:"Number of transactions whose review state changed"`
	}
}

// BulkUpdateTransactionCategoryInput represents the request body for bulk recategorization (Issue #1210).
type BulkUpdateTransactionCategoryInput struct {
	inputs.AuthenticatedInput
	inputs.MCPContextInput
	Body struct {
		// TransactionIDs is the list of transaction UUIDs to recategorize (max 500).
		TransactionIDs []string `json:"transaction_ids" doc:"Transaction UUIDs to recategorize (max 500, owned by the caller)"`
		// Kind is the discriminated-union selector: pfc, custom, or clear.
		Kind             string  `json:"kind" enum:"pfc,custom,clear" doc:"Category override kind"`
		PFCPrimary       *string `json:"pfc_primary,omitempty" doc:"PFC primary enum (required when kind=pfc)"`
		PFCDetailed      *string `json:"pfc_detailed,omitempty" doc:"PFC detailed enum (optional when kind=pfc)"`
		CustomCategoryID *string `json:"custom_category_id,omitempty" doc:"Custom category UUID (required when kind=custom)"`
	} `json:"body"`
}

// BulkUpdateTransactionCategoryOutput reports requested vs. actually-updated counts.
type BulkUpdateTransactionCategoryOutput struct {
	Body struct {
		// Requested is the de-duplicated count of transaction IDs in the request.
		Requested int64 `json:"requested" doc:"De-duplicated count of transaction IDs in the request"`
		// Updated is the number of owned transactions that were actually updated.
		Updated int64 `json:"updated" doc:"Number of owned transactions updated; unowned/unknown IDs are silently ignored"`
	}
}

// GetTransactionReviewSummaryInput represents the input for the review-summary endpoint (Issue #1210).
type GetTransactionReviewSummaryInput struct {
	inputs.AuthenticatedInput
	// StartDate optionally scopes the summary to transactions on or after this date (YYYY-MM-DD).
	StartDate string `query:"start_date" doc:"Optional: scope summary to transactions on or after this date (YYYY-MM-DD)"`
	// EndDate optionally scopes the summary to transactions on or before this date (YYYY-MM-DD).
	EndDate string `query:"end_date" doc:"Optional: scope summary to transactions on or before this date (YYYY-MM-DD)"`
}

// GetTransactionReviewSummaryOutput wraps the ReviewSummaryResponse (Issue #1210).
type GetTransactionReviewSummaryOutput struct {
	Body *domain.ReviewSummaryResponse
}

// GetPlaidCategoriesInput represents the input for getting Plaid categories.
type GetPlaidCategoriesInput struct {
	inputs.AuthenticatedInput
}

// GetPlaidCategoriesOutput represents the response for getting Plaid categories.
type GetPlaidCategoriesOutput struct {
	Body []string
}

// --- Handler Methods ---

// GetTransactions handles GET /api/v1/transactions requests.
// Retrieves transactions for the authenticated user with optional filters and cursor-based pagination.
func (h *TransactionHandler) GetTransactions(ctx context.Context, input *GetTransactionsInput) (*GetTransactionsOutput, error) {
	if err := rejectHouseholdScope(input.Scope); err != nil {
		return nil, err
	}
	// Decode cursor if present
	var cursor *domain.TransactionCursor
	if input.Cursor != "" {
		c, err := domain.DecodeTransactionCursor(input.Cursor)
		if err != nil {
			if errors.Is(err, domain.ErrCursorVersionUnsupported) {
				return nil, huma.Error400BadRequest("cursor version not supported")
			}
			return nil, huma.Error400BadRequest("invalid cursor")
		}
		// Validate cursor matches current sort params
		sortBy := input.SortBy
		if sortBy == "" {
			sortBy = "date"
		}
		sortDir := input.SortDir
		if sortDir == "" {
			sortDir = "desc"
		}
		if c.K != sortBy || c.D != sortDir {
			return nil, huma.Error400BadRequest("cursor does not match sort_by/sort_dir")
		}
		cursor = c
	}

	// Build filters from query parameters
	filters := domain.TransactionFilters{}

	// Account ID filter (single, deprecated)
	if input.AccountID != "" {
		filters.AccountID = &input.AccountID
	}

	// Account IDs filter (comma-separated)
	if input.AccountIDs != "" {
		ids := parseCommaSeparated(input.AccountIDs)
		if len(ids) > 0 {
			filters.AccountIDs = ids
		}
	}

	// Date range filters
	startDate, endDate, err := dateutil.ParseOptionalDateRange(input.StartDate, input.EndDate)
	if err != nil {
		return nil, err
	}
	filters.StartDate = startDate
	filters.EndDate = endDate

	// PFC primary filter (comma-separated enum values)
	if input.PFCPrimaries != "" {
		primaries := parseCommaSeparated(input.PFCPrimaries)
		if len(primaries) > 0 {
			filters.PFCPrimaries = primaries
		}
	}

	// PFC detailed sub-category filter (comma-separated enum values)
	if input.PFCDetaileds != "" {
		detaileds := parseCommaSeparated(input.PFCDetaileds)
		if len(detaileds) > 0 {
			filters.PFCDetaileds = detaileds
		}
	}

	// Custom category ID filter (comma-separated UUIDs)
	if input.CustomCategoryIDs != "" {
		ids := parseCommaSeparated(input.CustomCategoryIDs)
		if len(ids) > 0 {
			filters.CustomCategoryIDs = ids
		}
	}

	// Search filter
	if input.Search != "" {
		filters.Search = &input.Search
	}

	// Pending filter
	if input.Pending != "" {
		pending, err := strconv.ParseBool(input.Pending)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid pending parameter (expected true or false)")
		}
		filters.Pending = &pending
	}

	// Reviewed filter (Issue #1203)
	if input.Reviewed != "" {
		reviewed, err := strconv.ParseBool(input.Reviewed)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid reviewed parameter (expected true or false)")
		}
		filters.Reviewed = &reviewed
	}

	// Reversal surface (Issue #1569). Off by default: a retained reversal appearing unbidden in a
	// total the user has already reconciled is the same silent change this issue exists to stop,
	// arriving from the other direction. An invalid value is rejected rather than coerced to
	// false — silently ignoring it would show the client a list it did not ask for and believe it
	// did.
	if input.IncludeReversed != "" {
		includeReversed, err := strconv.ParseBool(input.IncludeReversed)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid include_reversed parameter (expected true or false)")
		}
		filters.IncludeReversed = includeReversed
	}

	// Uncategorized (#1626 Phase 2) — parsed like include_reversed above, and for the same reason:
	// the raw string is the ONLY place absence can be distinguished from an explicit false, and an
	// invalid value is rejected rather than coerced. `false` is a no-op, never an exclusion.
	if input.Uncategorized != "" {
		uncategorized, err := strconv.ParseBool(input.Uncategorized)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid uncategorized parameter (expected true or false)")
		}
		filters.Uncategorized = uncategorized
	}

	// Category source filter (Issue #1210) — comma-separated, validated enum tokens
	if input.CategorySources != "" {
		sources := parseCommaSeparated(input.CategorySources)
		for _, s := range sources {
			if s != "plaid" && s != "rule" && s != "manual" {
				return nil, huma.Error400BadRequest("Invalid category_source value: " + s + " (allowed: plaid, rule, manual)")
			}
		}
		if len(sources) > 0 {
			filters.CategorySources = sources
		}
	}

	// Min amount filter
	if input.MinAmount != "" {
		minAmount, err := parseFloat64(input.MinAmount)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid min_amount parameter (expected number)")
		}
		filters.MinAmount = &minAmount
	}

	// Max amount filter
	if input.MaxAmount != "" {
		maxAmount, err := parseFloat64(input.MaxAmount)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid max_amount parameter (expected number)")
		}
		filters.MaxAmount = &maxAmount
	}

	// Amount type filter
	if input.AmountType != "" && input.AmountType != "all" {
		filters.AmountType = &input.AmountType
	}

	// User account ID filter (Issue #1109: re-link transparency)
	// Stable identifier that returns transactions from all chain members
	if input.UserAccountID != "" {
		filters.UserAccountID = &input.UserAccountID
	}

	// Merchant ID filter (Issue #1163: PII URL leak fix)
	if input.MerchantID != "" {
		filters.MerchantID = &input.MerchantID
	}

	// Sort parameters — apply defaults if Huma did not populate them (e.g. direct handler calls)
	filters.SortBy = input.SortBy
	if filters.SortBy == "" {
		filters.SortBy = "date"
	}
	filters.SortDir = input.SortDir
	if filters.SortDir == "" {
		filters.SortDir = "desc"
	}

	// Investment activity filter — Issue #138.
	// Default is true (unified feed). When explicitly set, parse the bool; an invalid
	// value returns 400. When omitted on a non-date sort, silently degrade to false
	// so existing non-date-sort clients never receive a 400 due to the default flip.
	// An explicit "true" + non-date sort is handled by the service (ErrInvestmentMergeRequiresDateSort → 400).
	if input.IncludeInvestmentActivity != "" {
		incl, parseErr := strconv.ParseBool(input.IncludeInvestmentActivity)
		if parseErr != nil {
			return nil, huma.Error400BadRequest("Invalid include_investment_activity parameter (expected true or false)")
		}
		filters.IncludeInvestmentActivity = incl
	} else if filters.SortBy == "date" || filters.SortBy == "" {
		// Param omitted — default true for date sort. Non-date sorts degrade to false (zero value).
		filters.IncludeInvestmentActivity = true
	}

	// Activity type filter (comma-separated normalized trade types)
	if input.ActivityTypes != "" {
		filters.ActivityTypes = parseCommaSeparated(input.ActivityTypes)
	}

	// Call service layer (cursor-based pagination)
	result, err := h.service.ListTransactionsByCursor(ctx, input.UserID, filters, cursor, input.Limit)
	if err != nil {
		if errors.Is(err, domain.ErrInvestmentMergeRequiresDateSort) {
			return nil, huma.Error400BadRequest("include_investment_activity requires sort_by=date")
		}
		if errors.Is(err, domain.ErrCursorFilterMismatch) {
			return nil, huma.Error400BadRequest("cursor filter mismatch: restart pagination")
		}
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// #1636 rounding boundary — money_rounding.go, extended to this feed by #1631 Gap A2.
	// ⚠ ActivityItem carries the INVESTMENT money fields too, so this non-investment handler is
	// on the same boundary as the investment surfaces: fees and amounts fold, while the
	// DECIMAL(18,8) quantity and per-unit price stay exact. TotalAmount is the whole-set sum and
	// folds with them; money.ToCentsPtr keeps "not computed" absent rather than turning it into
	// 0.00 (ADR Decision 4).
	roundActivityItems(result.Transactions)

	out := &GetTransactionsOutput{}
	out.Body.Transactions = result.Transactions
	out.Body.NextCursor = result.NextCursor
	out.Body.Limit = result.Limit
	out.Body.TotalCount = result.TotalCount
	out.Body.TotalAmount = money.ToCentsPtr(result.TotalAmount)
	return out, nil
}

// GetTransactionByID handles GET /api/v1/transactions/:id requests.
// Retrieves a specific transaction by ID.
func (h *TransactionHandler) GetTransactionByID(ctx context.Context, input *GetTransactionByIDInput) (*GetTransactionByIDOutput, error) {
	if err := rejectHouseholdScope(input.Scope); err != nil {
		return nil, err
	}
	tx, err := h.service.GetTransactionByID(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetTransactionByIDOutput{Body: tx}, nil
}

// UpdateTransactionTags handles PUT /api/v1/transactions/:id/tags requests.
// Updates the tags on a specific transaction.
func (h *TransactionHandler) UpdateTransactionTags(ctx context.Context, input *UpdateTransactionTagsInput) (*UpdateTransactionTagsOutput, error) {
	// Ensure tags is not nil
	tags := input.Body.Tags
	if tags == nil {
		tags = []string{}
	}

	tx, err := h.service.UpdateTransactionTags(ctx, input.UserID, input.ID, tags)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &UpdateTransactionTagsOutput{Body: tx}, nil
}

// UpdateTransactionCategory handles PUT /api/v1/transactions/:id/category requests.
// Updates the category override on a specific transaction using the PFC v2 discriminated union.
func (h *TransactionHandler) UpdateTransactionCategory(ctx context.Context, input *UpdateTransactionCategoryInput) (*UpdateTransactionCategoryOutput, error) {
	// Validate the discriminated union before hitting the DB so malformed combinations
	// return 400 rather than tripping a DB CHECK/FK and surfacing as a 500 (#1203).
	b := input.Body
	switch b.Kind {
	case "custom":
		if b.CustomCategoryID == nil || *b.CustomCategoryID == "" {
			return nil, huma.Error400BadRequest("custom_category_id is required when kind=custom")
		}
		if b.PFCPrimary != nil || b.PFCDetailed != nil {
			return nil, huma.Error400BadRequest("pfc_primary/pfc_detailed must be omitted when kind=custom")
		}
	case "pfc":
		if b.PFCPrimary == nil || *b.PFCPrimary == "" {
			return nil, huma.Error400BadRequest("pfc_primary is required when kind=pfc")
		}
		if b.CustomCategoryID != nil {
			return nil, huma.Error400BadRequest("custom_category_id must be omitted when kind=pfc")
		}
	case "clear":
		if (b.PFCPrimary != nil && *b.PFCPrimary != "") || (b.PFCDetailed != nil && *b.PFCDetailed != "") || b.CustomCategoryID != nil {
			return nil, huma.Error400BadRequest("pfc_primary/pfc_detailed/custom_category_id must be omitted when kind=clear")
		}
	}

	tx, err := h.service.UpdateTransactionCategory(ctx, input.UserID, input.ID, input.Body.Kind, input.Body.PFCPrimary, input.Body.PFCDetailed, input.Body.CustomCategoryID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	// MCP audit (fire-and-forget; entity update is value-idempotent so retries are safe,
	// but each retry still writes an audit row since no idempotency key is available on PATCH).
	if input.IsMCPOrigin(h.mcpWorkerSecret) && h.mcpAuditRepo != nil {
		if auditErr := recordMCPWriteAudit(ctx, h.mcpAuditRepo, input.UserID, input.MCPGrantID, "updateTransactionCategory", ""); auditErr != nil {
			slog.WarnContext(ctx, "MCP audit write failed (non-atomic post-write)", "operation", "updateTransactionCategory", "error", auditErr)
		}
	}
	return &UpdateTransactionCategoryOutput{Body: tx}, nil
}

// SetTransactionReviewed handles PATCH /api/v1/transactions/{id}/review (Issue #1203).
func (h *TransactionHandler) SetTransactionReviewed(ctx context.Context, input *SetTransactionReviewedInput) (*SetTransactionReviewedOutput, error) {
	n, err := h.service.SetReviewed(ctx, input.UserID, []string{input.ID}, input.Body.Reviewed)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	// MCP audit (fire-and-forget; value-idempotent; Issue #1210)
	if input.IsMCPOrigin(h.mcpWorkerSecret) && h.mcpAuditRepo != nil {
		if auditErr := recordMCPWriteAudit(ctx, h.mcpAuditRepo, input.UserID, input.MCPGrantID, "setTransactionReviewed", ""); auditErr != nil {
			slog.WarnContext(ctx, "MCP audit write failed", "operation", "setTransactionReviewed", "error", auditErr)
		}
	}
	out := &SetTransactionReviewedOutput{}
	out.Body.Updated = n
	return out, nil
}

// BulkSetTransactionReviewed handles PATCH /api/v1/transactions/review (Issue #1203).
func (h *TransactionHandler) BulkSetTransactionReviewed(ctx context.Context, input *BulkSetTransactionReviewedInput) (*SetTransactionReviewedOutput, error) {
	if len(input.Body.TransactionIDs) == 0 {
		return nil, huma.Error400BadRequest("transaction_ids must not be empty")
	}
	n, err := h.service.SetReviewed(ctx, input.UserID, input.Body.TransactionIDs, input.Body.Reviewed)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	// MCP audit (fire-and-forget; value-idempotent; Issue #1210)
	if input.IsMCPOrigin(h.mcpWorkerSecret) && h.mcpAuditRepo != nil {
		if auditErr := recordMCPWriteAudit(ctx, h.mcpAuditRepo, input.UserID, input.MCPGrantID, "bulkSetTransactionReviewed", ""); auditErr != nil {
			slog.WarnContext(ctx, "MCP audit write failed", "operation", "bulkSetTransactionReviewed", "error", auditErr)
		}
	}
	out := &SetTransactionReviewedOutput{}
	out.Body.Updated = n
	return out, nil
}

// BulkUpdateTransactionCategory handles PUT /api/v1/transactions/category (Issue #1210).
// Best-effort bulk recategorize: applies to all owned rows; unowned/unknown IDs are ignored.
// Always returns 200 with {requested, updated}; callers needing strictness compare requested==updated.
func (h *TransactionHandler) BulkUpdateTransactionCategory(ctx context.Context, input *BulkUpdateTransactionCategoryInput) (*BulkUpdateTransactionCategoryOutput, error) {
	b := input.Body

	// Validate: at least one transaction ID required
	if len(b.TransactionIDs) == 0 {
		return nil, huma.Error400BadRequest("transaction_ids must not be empty")
	}

	// Validate: max batch size
	const maxBulkCategoryBatch = 500
	if len(b.TransactionIDs) > maxBulkCategoryBatch {
		return nil, huma.Error400BadRequest("transaction_ids exceeds maximum batch size of 500")
	}

	// De-duplicate IDs; requested count = unique entries
	seen := make(map[string]struct{}, len(b.TransactionIDs))
	uniqueIDs := make([]string, 0, len(b.TransactionIDs))
	for _, id := range b.TransactionIDs {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			uniqueIDs = append(uniqueIDs, id)
		}
	}
	requested := int64(len(uniqueIDs))

	// Validate the discriminated union (mirrors UpdateTransactionCategory)
	switch b.Kind {
	case "custom":
		if b.CustomCategoryID == nil || *b.CustomCategoryID == "" {
			return nil, huma.Error400BadRequest("custom_category_id is required when kind=custom")
		}
		if b.PFCPrimary != nil || b.PFCDetailed != nil {
			return nil, huma.Error400BadRequest("pfc_primary/pfc_detailed must be omitted when kind=custom")
		}
	case "pfc":
		if b.PFCPrimary == nil || *b.PFCPrimary == "" {
			return nil, huma.Error400BadRequest("pfc_primary is required when kind=pfc")
		}
		if b.CustomCategoryID != nil {
			return nil, huma.Error400BadRequest("custom_category_id must be omitted when kind=pfc")
		}
	case "clear":
		if (b.PFCPrimary != nil && *b.PFCPrimary != "") || (b.PFCDetailed != nil && *b.PFCDetailed != "") || b.CustomCategoryID != nil {
			return nil, huma.Error400BadRequest("pfc_primary/pfc_detailed/custom_category_id must be omitted when kind=clear")
		}
	}

	updated, err := h.service.BulkUpdateTransactionCategory(ctx, input.UserID, uniqueIDs, b.Kind, b.PFCPrimary, b.PFCDetailed, b.CustomCategoryID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// MCP audit (fire-and-forget; value-idempotent writes)
	if input.IsMCPOrigin(h.mcpWorkerSecret) && h.mcpAuditRepo != nil {
		if auditErr := recordMCPWriteAudit(ctx, h.mcpAuditRepo, input.UserID, input.MCPGrantID, "bulkUpdateTransactionCategory", ""); auditErr != nil {
			slog.WarnContext(ctx, "MCP audit write failed (non-atomic post-write)", "operation", "bulkUpdateTransactionCategory", "error", auditErr)
		}
	}

	out := &BulkUpdateTransactionCategoryOutput{}
	out.Body.Requested = requested
	out.Body.Updated = updated
	return out, nil
}

// GetTransactionReviewSummary handles GET /api/v1/transactions/review-summary (Issue #1210).
// Returns pre-computed review-state counts and top unreviewed categories for the authenticated user.
// Dates are optional — omitting them returns all-time data (transfers included).
// Designed for AI-tool consumption: single-responsibility, token-efficient, one DB round-trip.
func (h *TransactionHandler) GetTransactionReviewSummary(ctx context.Context, input *GetTransactionReviewSummaryInput) (*GetTransactionReviewSummaryOutput, error) {
	filters := domain.ReviewSummaryFilters{}

	if input.StartDate != "" {
		t, err := dateutil.ParseOptionalDate(input.StartDate, "start_date")
		if err != nil {
			return nil, huma.Error400BadRequest("invalid start_date: " + err.Error())
		}
		filters.StartDate = t
	}
	if input.EndDate != "" {
		t, err := dateutil.ParseOptionalDate(input.EndDate, "end_date")
		if err != nil {
			return nil, huma.Error400BadRequest("invalid end_date: " + err.Error())
		}
		filters.EndDate = t
	}

	summary, err := h.service.GetReviewSummary(ctx, input.UserID, filters)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &GetTransactionReviewSummaryOutput{Body: summary}, nil
}

// GetPlaidCategories handles GET /api/v1/categories/plaid requests.
// Returns distinct Plaid category names from the user's transactions.
func (h *TransactionHandler) GetPlaidCategories(ctx context.Context, input *GetPlaidCategoriesInput) (*GetPlaidCategoriesOutput, error) {
	categories, err := h.service.GetDistinctPlaidCategories(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Return empty array instead of null
	if categories == nil {
		categories = []string{}
	}

	return &GetPlaidCategoriesOutput{Body: categories}, nil
}

// --- Helper Functions ---

// parseCommaSeparated splits a comma-separated string into a slice of trimmed, non-empty strings.
func parseCommaSeparated(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// parseFloat64 parses a string to float64.
func parseFloat64(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}

// RegisterTransactionRoutes registers transaction routes with the Huma API.
// Note: These routes require the Clerk middleware to be applied.
func RegisterTransactionRoutes(api huma.API, handler *TransactionHandler) {
	// GET /api/v1/transactions - List transactions with filters
	huma.Register(api, huma.Operation{
		OperationID: "getTransactions",
		Method:      http.MethodGet,
		Path:        "/api/v1/transactions",
		Summary:     "List transactions",
		Description: "Retrieves transactions for the authenticated user with optional filters and pagination.",
		Tags:        []string{"Transactions"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetTransactions)

	// GET /api/v1/transactions/review-summary - Review summary (Issue #1210).
	// Registered BEFORE the {id} route so "review-summary" is not captured as an {id}.
	huma.Register(api, huma.Operation{
		OperationID: "getTransactionReviewSummary",
		Method:      http.MethodGet,
		Path:        "/api/v1/transactions/review-summary",
		Summary:     "Get transaction review summary",
		Description: "Returns pre-computed review-state counts and top unreviewed categories. Dates are optional (all-time default). Transfers included. Designed for AI-tool consumption.",
		Tags:        []string{"Transactions"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetTransactionReviewSummary)

	// GET /api/v1/transactions/{id} - Get transaction by ID
	huma.Register(api, huma.Operation{
		OperationID: "getTransactionByID",
		Method:      http.MethodGet,
		Path:        "/api/v1/transactions/{id}",
		Summary:     "Get transaction by ID",
		Description: "Retrieves a specific transaction by its UUID.",
		Tags:        []string{"Transactions"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetTransactionByID)

	// PUT /api/v1/transactions/{id}/tags - Update transaction tags
	huma.Register(api, huma.Operation{
		OperationID: "updateTransactionTags",
		Method:      http.MethodPut,
		Path:        "/api/v1/transactions/{id}/tags",
		Summary:     "Update transaction tags",
		Description: "Updates the tags on a specific transaction.",
		Tags:        []string{"Transactions"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.UpdateTransactionTags)

	// PUT /api/v1/transactions/category - Bulk update transaction category (Issue #1210).
	// Registered BEFORE the {id}/category route so "category" is not captured as an {id}.
	huma.Register(api, huma.Operation{
		OperationID: "bulkUpdateTransactionCategory",
		Method:      http.MethodPut,
		Path:        "/api/v1/transactions/category",
		Summary:     "Bulk update transaction category",
		Description: "Recategorizes multiple transactions for the authenticated user. Best-effort: unowned/unknown IDs are silently ignored. Returns requested (de-duplicated input count) and updated (rows changed).",
		Tags:        []string{"Transactions"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.BulkUpdateTransactionCategory)

	// PUT /api/v1/transactions/{id}/category - Update transaction category
	huma.Register(api, huma.Operation{
		OperationID: "updateTransactionCategory",
		Method:      http.MethodPut,
		Path:        "/api/v1/transactions/{id}/category",
		Summary:     "Update transaction category",
		Description: "Updates the category override on a specific transaction.",
		Tags:        []string{"Transactions"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.UpdateTransactionCategory)

	// PATCH /api/v1/transactions/review - Bulk set review state (Issue #1203).
	// Registered before the {id} variant so "review" is not captured as an {id}.
	huma.Register(api, huma.Operation{
		OperationID: "bulkSetTransactionReviewed",
		Method:      http.MethodPatch,
		Path:        "/api/v1/transactions/review",
		Summary:     "Bulk update transaction review state",
		Description: "Marks multiple transactions reviewed or unreviewed for the authenticated user.",
		Tags:        []string{"Transactions"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.BulkSetTransactionReviewed)

	// PATCH /api/v1/transactions/{id}/review - Set review state for one transaction (Issue #1203).
	huma.Register(api, huma.Operation{
		OperationID: "setTransactionReviewed",
		Method:      http.MethodPatch,
		Path:        "/api/v1/transactions/{id}/review",
		Summary:     "Update transaction review state",
		Description: "Marks a transaction reviewed or unreviewed (moves it out of / into the unreviewed inbox).",
		Tags:        []string{"Transactions"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.SetTransactionReviewed)

	// GET /api/v1/categories/plaid - Get distinct Plaid categories
	huma.Register(api, huma.Operation{
		OperationID: "getPlaidCategories",
		Method:      http.MethodGet,
		Path:        "/api/v1/categories/plaid",
		Summary:     "Get Plaid categories",
		Description: "Returns distinct Plaid category names from the user's transactions.",
		Tags:        []string{"Categories"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetPlaidCategories)
}
