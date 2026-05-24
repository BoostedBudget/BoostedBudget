package operations

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/dateutil"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// TransactionHandler handles transaction-related HTTP requests.
type TransactionHandler struct {
	service services.TransactionService
}

// NewTransactionHandler creates a new TransactionHandler instance.
func NewTransactionHandler(service services.TransactionService) *TransactionHandler {
	return &TransactionHandler{service: service}
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
	PFCPrimaries      string `query:"pfc_primaries" doc:"Filter by effective PFC primary enum(s) (comma-separated, e.g. FOOD_AND_DRINK,TRAVEL)"`
	CustomCategoryIDs string `query:"custom_category_ids" doc:"Filter by custom category UUIDs (comma-separated)"`
	Search            string `query:"search" doc:"Search in merchant name or transaction name"`
	Pending           string `query:"pending" doc:"Filter by pending status (true/false)"`
	MinAmount         string `query:"min_amount" doc:"Filter by minimum absolute amount"`
	MaxAmount         string `query:"max_amount" doc:"Filter by maximum absolute amount"`
	AmountType        string `query:"amount_type" enum:"income,expense,all" doc:"Filter by transaction type"`
	UserAccountID     string `query:"user_account_id" doc:"Filter by logical account ID (user_accounts.id) — stable across re-links; returns transactions from all chain members"`
	MerchantID        string `query:"merchant_id" doc:"Filter transactions by merchant UUID" example:"550e8400-e29b-41d4-a716-446655440000"`

	// Sort parameters
	SortBy  string `query:"sort_by"  enum:"date,merchant,amount,category" default:"date"  doc:"Column to sort by"`
	SortDir string `query:"sort_dir" enum:"asc,desc"                      default:"desc" doc:"Sort direction"`
}

// GetTransactionsOutput represents the response for listing transactions.
type GetTransactionsOutput struct {
	Body struct {
		Transactions []*domain.Transaction `json:"transactions"`
		NextCursor   *string               `json:"next_cursor,omitempty"`
		Limit        int                   `json:"limit"`
	}
}

// GetTransactionByIDInput represents the input for getting a transaction by ID.
type GetTransactionByIDInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Transaction UUID"`
}

// GetTransactionByIDOutput represents the response for getting a transaction.
type GetTransactionByIDOutput struct {
	Body *domain.Transaction
}

// UpdateTransactionTagsInput represents the input for updating transaction tags.
type UpdateTransactionTagsInput struct {
	inputs.AuthenticatedInput
	ID   string                     `path:"id" doc:"Transaction UUID"`
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
	ID   string                          `path:"id" doc:"Transaction UUID"`
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

	// Call service layer (cursor-based pagination)
	result, err := h.service.ListTransactionsByCursor(ctx, input.UserID, filters, cursor, input.Limit)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	out := &GetTransactionsOutput{}
	out.Body.Transactions = result.Transactions
	out.Body.NextCursor = result.NextCursor
	out.Body.Limit = result.Limit
	return out, nil
}

// GetTransactionByID handles GET /api/v1/transactions/:id requests.
// Retrieves a specific transaction by ID.
func (h *TransactionHandler) GetTransactionByID(ctx context.Context, input *GetTransactionByIDInput) (*GetTransactionByIDOutput, error) {
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
	tx, err := h.service.UpdateTransactionCategory(ctx, input.UserID, input.ID, input.Body.Kind, input.Body.PFCPrimary, input.Body.PFCDetailed, input.Body.CustomCategoryID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &UpdateTransactionCategoryOutput{Body: tx}, nil
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
