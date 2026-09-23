// Package operations — investment portfolio endpoints (Issue #138).
// Four core operations (tag: Investments):
//   - GET /api/v1/investments/holdings
//   - GET /api/v1/investments/portfolio/summary
//   - GET /api/v1/investments/portfolio/history
//   - GET /api/v1/investments/investment-transactions
//
// Four analytics helpers (tag: Analytics — load-bearing for MCP parity test):
//   - GET /api/v1/investments/portfolio/allocation
//   - GET /api/v1/investments/portfolio/top-movers
//   - GET /api/v1/investments/portfolio/gain-loss
//   - GET /api/v1/investments/portfolio/income
package operations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/dateutil"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// InvestmentHandler handles all investment portfolio HTTP requests.
type InvestmentHandler struct {
	investmentService services.InvestmentService
	analyticsService  services.InvestmentAnalyticsService
	householdSvc      services.HouseholdService
}

// NewInvestmentHandler creates a new InvestmentHandler.
func NewInvestmentHandler(investmentSvc services.InvestmentService, analyticsSvc services.InvestmentAnalyticsService) *InvestmentHandler {
	return &InvestmentHandler{
		investmentService: investmentSvc,
		analyticsService:  analyticsSvc,
	}
}

// SetHouseholdService wires the household service used to resolve `scope=household` on the
// holdings and portfolio-summary reads (#1706). Nil-safe: personal scope never calls it.
func (h *InvestmentHandler) SetHouseholdService(svc services.HouseholdService) {
	h.householdSvc = svc
}

// ============================================================================
// Input / Output types — Core operations
// ============================================================================

// GetHoldingsInput is the input for GET /investments/holdings.
type GetHoldingsInput struct {
	inputs.AuthenticatedInput
	AccountID string `query:"account_id" doc:"Filter holdings to a specific account ID (chain-expanded via user_account_id). Optional."`
	Scope     string `query:"scope" enum:"personal,household" default:"personal" doc:"Aggregation scope: personal (default) or household (requires active household membership). Household scope returns only accounts shared into the household."`
}

// GetHoldingsOutput is the response for GET /investments/holdings.
// Returns at most the user's full portfolio — unbounded by design since a typical
// retail portfolio has O(10-100) holdings and the join is indexed on user_id.
type GetHoldingsOutput struct {
	Body struct {
		Holdings []*domain.HoldingView `json:"holdings"`
		Count    int                   `json:"count"`
	}
}

// GetPortfolioSummaryInput is the input for GET /investments/portfolio/summary.
type GetPortfolioSummaryInput struct {
	inputs.AuthenticatedInput
	Scope string `query:"scope" enum:"personal,household" default:"personal" doc:"Aggregation scope: personal (default) or household (requires active household membership). Household scope omits day_change (no household snapshot exists)."`
}

// GetPortfolioSummaryOutput is the response for GET /investments/portfolio/summary.
type GetPortfolioSummaryOutput struct {
	Body *domain.PortfolioSummary
}

// GetPortfolioHistoryInput is the input for GET /investments/portfolio/history.
type GetPortfolioHistoryInput struct {
	inputs.AuthenticatedInput
	StartDate string `query:"start_date" required:"true" doc:"Start of date range (YYYY-MM-DD, inclusive)"`
	EndDate   string `query:"end_date" required:"true" doc:"End of date range (YYYY-MM-DD, inclusive)"`
}

// GetPortfolioHistoryOutput is the response for GET /investments/portfolio/history.
//
// ⚠ The body stays an ANONYMOUS struct rather than becoming domain.PortfolioHistory directly, and
// that is deliberate (#1631 M8a fix round 2, I-2). Huma names a schema after the named type it is
// given, so handing it domain.PortfolioHistory would rename the generated model
// GetPortfolioHistoryOutputBody -> PortfolioHistory and break every client import of the old name.
// Restating the three fields here keeps the generated model name stable and makes the change
// purely ADDITIVE, which is what every other surface in this task achieved. The fields mirror
// domain.PortfolioHistory exactly; see that type for why the currency is a property of the SERIES
// rather than of each point.
type GetPortfolioHistoryOutput struct {
	Body struct {
		History         []domain.PortfolioHistoryPoint `json:"history"`
		CurrencyCode    *string                        `json:"currency_code,omitempty"`
		IsMixedCurrency bool                           `json:"is_mixed_currency"`
	}
}

// GetInvestmentTransactionsInput is the input for GET /investments/investment-transactions.
type GetInvestmentTransactionsInput struct {
	inputs.AuthenticatedInput
	inputs.CursorPaginationInput
	AccountID     string `query:"account_id" doc:"Filter by account ID (chain-expanded)"`
	SecurityID    string `query:"security_id" doc:"Filter by security ID"`
	ActivityTypes string `query:"activity_types" doc:"Filter by normalized trade type (comma-separated: buy,sell,dividend,reinvest,interest,fee,transfer,cash,other)"`
	StartDate     string `query:"start_date" doc:"Filter transactions on or after this date (YYYY-MM-DD)"`
	EndDate       string `query:"end_date" doc:"Filter transactions on or before this date (YYYY-MM-DD)"`
}

// GetInvestmentTransactionsOutput is the response for GET /investments/investment-transactions.
type GetInvestmentTransactionsOutput struct {
	Body struct {
		Transactions []domain.InvestmentTransaction `json:"transactions"`
		NextCursor   *string                        `json:"next_cursor,omitempty"`
		Limit        int                            `json:"limit"`
	}
}

// ============================================================================
// Input / Output types — Analytics operations (tag: Analytics, MCP-visible)
// ============================================================================

// GetPortfolioAllocationBreakdownInput is the input for GET /investments/portfolio/allocation.
//
// New optional params (#1296 — holistic configurable allocation):
//   - include  (csv): which additional asset classes to union in.
//     Allowed values: "cash" (depository accounts), "liabilities" (credit/loan accounts).
//     Default: empty — investment holdings only (backward-compatible with #138).
//   - group_by: how to bucket the result.
//     Allowed values: "security_type" (default), "asset_class", "account".
//   - basis:    whether liabilities are netted out of the total.
//     Allowed values: "assets" (default), "net".
//
// Unknown values for group_by or basis return HTTP 422. Unknown values in include also return 422.
type GetPortfolioAllocationBreakdownInput struct {
	inputs.AuthenticatedInput
	// Include is a comma-separated list of additional asset classes to union into the breakdown.
	// Each element must be one of: 'cash' (depository accounts), 'liabilities' (credit/loan accounts).
	// Default: empty — investment holdings only (backward-compatible with existing consumers).
	Include string `query:"include" doc:"Comma-separated list of additional asset classes to include. Each element must be one of: 'cash' (depository accounts), 'liabilities' (credit/loan accounts). Default: empty (investment holdings only, backward-compatible)."`
	// GroupBy controls how the results are bucketed.
	// Default: 'security_type'. Allowed values: 'security_type', 'asset_class', 'account'.
	GroupBy string `query:"group_by" doc:"How to bucket the result. Default: 'security_type'." enum:"security_type,asset_class,account"`
	// Basis controls whether liabilities are netted against assets in the headline total.
	// Default: 'assets'. Allowed values: 'assets' (gross assets), 'net' (assets minus liabilities).
	Basis string `query:"basis" doc:"Whether liabilities reduce the total. Default: 'assets'." enum:"assets,net"`
}

// GetPortfolioAllocationBreakdownOutput is the response for GET /investments/portfolio/allocation.
type GetPortfolioAllocationBreakdownOutput struct {
	Body *domain.AllocationBreakdown
}

// GetTopMoversInput is the input for GET /investments/portfolio/top-movers.
type GetTopMoversInput struct {
	inputs.AuthenticatedInput
	Limit int `query:"limit" default:"5" minimum:"1" maximum:"20" doc:"Number of top gainers and losers to return (default 5, max 20)"`
}

// GetTopMoversOutput is the response for GET /investments/portfolio/top-movers.
type GetTopMoversOutput struct {
	Body *domain.TopMovers
}

// GetInvestmentGainLossSummaryInput is the input for GET /investments/portfolio/gain-loss.
type GetInvestmentGainLossSummaryInput struct {
	inputs.AuthenticatedInput
}

// GetInvestmentGainLossSummaryOutput is the response for GET /investments/portfolio/gain-loss.
// realized_gain_loss_supported is always false — Plaid provides no per-lot cost basis.
type GetInvestmentGainLossSummaryOutput struct {
	Body *domain.GainLossSummary
}

// GetInvestmentIncomeSummaryInput is the input for GET /investments/portfolio/income.
type GetInvestmentIncomeSummaryInput struct {
	inputs.AuthenticatedInput
	StartDate string `query:"start_date" required:"true" doc:"Start of date range (YYYY-MM-DD, inclusive)"`
	EndDate   string `query:"end_date" required:"true" doc:"End of date range (YYYY-MM-DD, inclusive)"`
}

// GetInvestmentIncomeSummaryOutput is the response for GET /investments/portfolio/income.
// Income includes dividends, interest, and reinvested dividends/interest (all economically income).
type GetInvestmentIncomeSummaryOutput struct {
	Body *domain.IncomeSummary
}

// ============================================================================
// Handlers — Core operations
// ============================================================================

// GetHoldings handles GET /api/v1/investments/holdings.
func (h *InvestmentHandler) GetHoldings(ctx context.Context, input *GetHoldingsInput) (*GetHoldingsOutput, error) {
	var accountID *string
	if input.AccountID != "" {
		if _, err := uuid.Parse(input.AccountID); err != nil {
			return nil, huma.Error400BadRequest("invalid account_id: must be a valid UUID")
		}
		accountID = &input.AccountID
	}

	scope, err := resolveHouseholdScopeIfRequested(ctx, h.householdSvc, input.UserID, input.Scope)
	if err != nil {
		return nil, err
	}

	holdings, err := h.investmentService.ListHoldings(ctx, input.UserID, accountID, scope)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// The rounding boundary (#1636) — see money_rounding.go. Quantity and institution_price are
	// deliberately left exact.
	roundHoldingViews(holdings)

	return &GetHoldingsOutput{
		Body: struct {
			Holdings []*domain.HoldingView `json:"holdings"`
			Count    int                   `json:"count"`
		}{Holdings: holdings, Count: len(holdings)},
	}, nil
}

// GetPortfolioSummary handles GET /api/v1/investments/portfolio/summary.
func (h *InvestmentHandler) GetPortfolioSummary(ctx context.Context, input *GetPortfolioSummaryInput) (*GetPortfolioSummaryOutput, error) {
	scope, err := resolveHouseholdScopeIfRequested(ctx, h.householdSvc, input.UserID, input.Scope)
	if err != nil {
		return nil, err
	}
	summary, err := h.investmentService.GetPortfolioSummary(ctx, input.UserID, scope)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	roundPortfolioSummary(summary) // #1636 rounding boundary — money_rounding.go
	return &GetPortfolioSummaryOutput{Body: summary}, nil
}

// GetPortfolioHistory handles GET /api/v1/investments/portfolio/history.
func (h *InvestmentHandler) GetPortfolioHistory(ctx context.Context, input *GetPortfolioHistoryInput) (*GetPortfolioHistoryOutput, error) {
	startDate, endDate, err := dateutil.ParseOptionalDateRange(input.StartDate, input.EndDate)
	if err != nil {
		return nil, err
	}
	if startDate == nil || endDate == nil {
		return nil, huma.Error400BadRequest("start_date and end_date are required")
	}
	if endDate.Before(*startDate) {
		return nil, huma.Error400BadRequest("end_date must be on or after start_date")
	}

	history, err := h.investmentService.GetPortfolioHistory(ctx, input.UserID, *startDate, *endDate)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	roundPortfolioHistoryPoints(history.Points) // #1636 rounding boundary — money_rounding.go

	out := &GetPortfolioHistoryOutput{}
	out.Body.History = history.Points
	out.Body.CurrencyCode = history.CurrencyCode
	out.Body.IsMixedCurrency = history.IsMixedCurrency
	return out, nil
}

// GetInvestmentTransactions handles GET /api/v1/investments/investment-transactions.
func (h *InvestmentHandler) GetInvestmentTransactions(ctx context.Context, input *GetInvestmentTransactionsInput) (*GetInvestmentTransactionsOutput, error) {
	// Decode cursor if present
	var cursor *domain.InvestmentTransactionCursor
	if input.Cursor != "" {
		c, err := domain.DecodeInvestmentTransactionCursor(input.Cursor)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid cursor")
		}
		cursor = c
	}

	startDate, endDate, err := dateutil.ParseOptionalDateRange(input.StartDate, input.EndDate)
	if err != nil {
		return nil, err
	}
	if startDate != nil && endDate != nil && endDate.Before(*startDate) {
		return nil, huma.Error400BadRequest("end_date must be on or after start_date")
	}

	filters := domain.InvestmentTransactionFilters{}
	if input.AccountID != "" {
		if _, err := uuid.Parse(input.AccountID); err != nil {
			return nil, huma.Error400BadRequest("invalid account_id: must be a valid UUID")
		}
		filters.AccountID = &input.AccountID
	}
	if input.SecurityID != "" {
		if _, err := uuid.Parse(input.SecurityID); err != nil {
			return nil, huma.Error400BadRequest("invalid security_id: must be a valid UUID")
		}
		filters.SecurityID = input.SecurityID
	}
	if input.ActivityTypes != "" {
		filters.ActivityTypes = parseCommaSeparated(input.ActivityTypes)
	}
	filters.StartDate = startDate
	filters.EndDate = endDate

	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	txns, nextCursor, err := h.investmentService.ListInvestmentTransactions(ctx, input.UserID, filters, cursor, limit)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// #1636 rounding boundary — money_rounding.go. ⚠ Quantity and Price stay exact: they are
	// DECIMAL(18,8) and sit directly beside Amount, which is money.
	roundInvestmentTransactions(txns)

	return &GetInvestmentTransactionsOutput{
		Body: struct {
			Transactions []domain.InvestmentTransaction `json:"transactions"`
			NextCursor   *string                        `json:"next_cursor,omitempty"`
			Limit        int                            `json:"limit"`
		}{Transactions: txns, NextCursor: nextCursor, Limit: limit},
	}, nil
}

// ============================================================================
// Handlers — Analytics operations
// ============================================================================

// validAllocationGroupByValues is the set of accepted group_by values.
var validAllocationGroupByValues = map[string]bool{
	domain.AllocationGroupBySecurityType: true,
	domain.AllocationGroupByAssetClass:   true,
	domain.AllocationGroupByAccount:      true,
}

// validAllocationBasisValues is the set of accepted basis values.
var validAllocationBasisValues = map[string]bool{
	domain.AllocationBasisAssets: true,
	domain.AllocationBasisNet:    true,
}

// validAllocationIncludeValues is the set of accepted include values.
var validAllocationIncludeValues = map[string]bool{
	"cash":        true,
	"liabilities": true,
}

// GetPortfolioAllocationBreakdown handles GET /api/v1/investments/portfolio/allocation.
// Supports optional include/group_by/basis query params (#1296).
func (h *InvestmentHandler) GetPortfolioAllocationBreakdown(ctx context.Context, input *GetPortfolioAllocationBreakdownInput) (*GetPortfolioAllocationBreakdownOutput, error) {
	// Build and validate AllocationOptions from query params.
	opts := domain.AllocationOptions{}

	if input.GroupBy != "" {
		if !validAllocationGroupByValues[input.GroupBy] {
			return nil, huma.Error422UnprocessableEntity("invalid group_by value '"+input.GroupBy+"': must be one of security_type, asset_class, account", nil)
		}
		opts.GroupBy = input.GroupBy
	}

	if input.Basis != "" {
		if !validAllocationBasisValues[input.Basis] {
			return nil, huma.Error422UnprocessableEntity("invalid basis value '"+input.Basis+"': must be one of assets, net", nil)
		}
		opts.Basis = input.Basis
	}

	if input.Include != "" {
		includeValues := parseCommaSeparated(input.Include)
		for _, v := range includeValues {
			if !validAllocationIncludeValues[v] {
				return nil, huma.Error422UnprocessableEntity("invalid include value '"+v+"': must be one of cash, liabilities", nil)
			}
		}
		opts.Include = includeValues
	}

	result, err := h.analyticsService.GetAllocationBreakdown(ctx, input.UserID, opts)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	roundAllocationBreakdown(result) // #1636 rounding boundary — money_rounding.go
	return &GetPortfolioAllocationBreakdownOutput{Body: result}, nil
}

// GetTopMovers handles GET /api/v1/investments/portfolio/top-movers.
func (h *InvestmentHandler) GetTopMovers(ctx context.Context, input *GetTopMoversInput) (*GetTopMoversOutput, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 5
	}

	result, err := h.analyticsService.GetTopMovers(ctx, input.UserID, limit)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	roundTopMovers(result) // #1636 rounding boundary — money_rounding.go
	return &GetTopMoversOutput{Body: result}, nil
}

// GetInvestmentGainLossSummary handles GET /api/v1/investments/portfolio/gain-loss.
func (h *InvestmentHandler) GetInvestmentGainLossSummary(ctx context.Context, input *GetInvestmentGainLossSummaryInput) (*GetInvestmentGainLossSummaryOutput, error) {
	result, err := h.analyticsService.GetGainLossSummary(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	roundGainLossSummary(result) // #1636 rounding boundary — money_rounding.go
	return &GetInvestmentGainLossSummaryOutput{Body: result}, nil
}

// GetInvestmentIncomeSummary handles GET /api/v1/investments/portfolio/income.
func (h *InvestmentHandler) GetInvestmentIncomeSummary(ctx context.Context, input *GetInvestmentIncomeSummaryInput) (*GetInvestmentIncomeSummaryOutput, error) {
	startDate, endDate, err := dateutil.ParseOptionalDateRange(input.StartDate, input.EndDate)
	if err != nil {
		return nil, err
	}
	if startDate == nil || endDate == nil {
		return nil, huma.Error400BadRequest("start_date and end_date are required")
	}
	if endDate.Before(*startDate) {
		return nil, huma.Error400BadRequest("end_date must be on or after start_date")
	}

	result, err := h.analyticsService.GetIncomeSummary(ctx, input.UserID, *startDate, *endDate)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	roundIncomeSummary(result) // #1636 rounding boundary — money_rounding.go
	return &GetInvestmentIncomeSummaryOutput{Body: result}, nil
}

// ============================================================================
// Route registration
// ============================================================================

// RegisterInvestmentRoutes registers all 8 investment routes with the Huma API.
// The 4 analytic helpers are tagged "Analytics" (load-bearing for the MCP manifest
// parity test); the 4 core operations are tagged "Investments".
func RegisterInvestmentRoutes(api huma.API, handler *InvestmentHandler) {
	// ---- Core: Investments tag ----

	// GET /api/v1/investments/holdings — list portfolio holdings with security metadata.
	huma.Register(api, huma.Operation{
		OperationID: "getHoldings",
		Method:      http.MethodGet,
		Path:        "/api/v1/investments/holdings",
		Summary:     "List portfolio holdings",
		Description: "Returns all holdings for the authenticated user, joined with security metadata (ticker, name, type) and pre-computed gain/loss and day_change values. Optionally filter to a single account via account_id.",
		Tags:        []string{"Investments"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetHoldings)

	// GET /api/v1/investments/portfolio/summary — live portfolio summary.
	huma.Register(api, huma.Operation{
		OperationID: "getPortfolioSummary",
		Method:      http.MethodGet,
		Path:        "/api/v1/investments/portfolio/summary",
		Summary:     "Get portfolio summary",
		Description: "Returns live portfolio totals (value, cost basis, unrealized gain/loss) computed from current holdings. day_change is derived from the most recent prior portfolio snapshot; null when no prior snapshot exists.",
		Tags:        []string{"Investments"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetPortfolioSummary)

	// GET /api/v1/investments/portfolio/history — portfolio value time series.
	huma.Register(api, huma.Operation{
		OperationID: "getPortfolioHistory",
		Method:      http.MethodGet,
		Path:        "/api/v1/investments/portfolio/history",
		Summary:     "Get portfolio value history",
		Description: "Returns portfolio value snapshots between start_date and end_date (inclusive), ordered by date ascending. Each point is one daily portfolio_snapshots row. Use for chart data.",
		Tags:        []string{"Investments"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetPortfolioHistory)

	// GET /api/v1/investments/investment-transactions — keyset-paginated investment transactions.
	huma.Register(api, huma.Operation{
		OperationID: "getInvestmentTransactions",
		Method:      http.MethodGet,
		Path:        "/api/v1/investments/investment-transactions",
		Summary:     "List investment transactions",
		Description: "Returns investment transactions for the authenticated user using keyset (cursor) pagination. Supports filtering by account, security, activity type (buy/sell/dividend/reinvest/interest/fee/transfer/cash/other), and date range.",
		Tags:        []string{"Investments"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetInvestmentTransactions)

	// ---- Analytics helpers: Analytics tag (load-bearing for MCP parity test) ----

	// GET /api/v1/investments/portfolio/allocation — configurable allocation breakdown.
	huma.Register(api, huma.Operation{
		OperationID: "getPortfolioAllocationBreakdown",
		Method:      http.MethodGet,
		Path:        "/api/v1/investments/portfolio/allocation",
		Summary:     "Get portfolio allocation breakdown",
		Description: `Returns portfolio value grouped by bucket type. By default (no params) returns investment holdings only, grouped by security type — backward-compatible with the #138 behavior.

Optional query parameters:
- include (csv): additional asset classes to union into the result.
  Values: "cash" (depository/checking/savings account balances), "liabilities" (credit card + loan balances as absolute value).
  Example: include=cash,liabilities
- group_by: bucketing dimension.
  Values: "security_type" (default — groups by securities.type, which carries Plaid's enum
          verbatim: cash, cryptocurrency, derivative, equity, etf, fixed income, loan,
          mutual fund, other; plus "unknown" when Plaid sends none, and the synthetic
          "liabilities" bucket when include=liabilities), "asset_class" (collapses into
          investments/cash/liabilities), "account".
- basis: whether liabilities reduce the grand total.
  Values: "assets" (default — show asset-side allocation only), "net" (net of liabilities).

Each bucket includes: security_type, asset_class (when group_by=asset_class), total_value, percentage, holding_count.
Single DB round-trip; designed as an AI-tool-callable function.`,
		Tags:     []string{"Analytics"},
		Security: []map[string][]string{{"clerk": {}}},
	}, handler.GetPortfolioAllocationBreakdown)

	// GET /api/v1/investments/portfolio/top-movers — top N gainers and losers by day_change.
	huma.Register(api, huma.Operation{
		OperationID: "getTopMovers",
		Method:      http.MethodGet,
		Path:        "/api/v1/investments/portfolio/top-movers",
		Summary:     "Get top portfolio movers",
		Description: "Returns the top N gainers and losers by day_change (today's institution_price vs prior close_price). Holdings with NULL close_price are excluded. Default limit is 5; max is 20. Single DB round-trip; designed as an AI-tool-callable function.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetTopMovers)

	// GET /api/v1/investments/portfolio/gain-loss — unrealized gain/loss summary.
	huma.Register(api, huma.Operation{
		OperationID: "getInvestmentGainLossSummary",
		Method:      http.MethodGet,
		Path:        "/api/v1/investments/portfolio/gain-loss",
		Summary:     "Get investment gain/loss summary",
		Description: "Returns unrealized gain/loss totals and a per-account breakdown. realized_gain_loss_supported is always false — Plaid does not provide per-lot cost basis and the 2-year transaction lookback truncates buy history. Single DB round-trip; designed as an AI-tool-callable function.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetInvestmentGainLossSummary)

	// GET /api/v1/investments/portfolio/income — investment income summary.
	huma.Register(api, huma.Operation{
		OperationID: "getInvestmentIncomeSummary",
		Method:      http.MethodGet,
		Path:        "/api/v1/investments/portfolio/income",
		Summary:     "Get investment income summary",
		Description: "Returns investment income (dividends, interest, and reinvested dividends/interest — all economically income) within a date range, grouped by normalized income type. Single DB round-trip; designed as an AI-tool-callable function.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetInvestmentIncomeSummary)
}
