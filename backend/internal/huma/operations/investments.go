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
}

// NewInvestmentHandler creates a new InvestmentHandler.
func NewInvestmentHandler(investmentSvc services.InvestmentService, analyticsSvc services.InvestmentAnalyticsService) *InvestmentHandler {
	return &InvestmentHandler{
		investmentService: investmentSvc,
		analyticsService:  analyticsSvc,
	}
}

// ============================================================================
// Input / Output types — Core operations
// ============================================================================

// GetHoldingsInput is the input for GET /investments/holdings.
type GetHoldingsInput struct {
	inputs.AuthenticatedInput
	AccountID string `query:"account_id" doc:"Filter holdings to a specific account ID (chain-expanded via user_account_id). Optional."`
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
type GetPortfolioHistoryOutput struct {
	Body struct {
		History []domain.PortfolioHistoryPoint `json:"history"`
	}
}

// GetInvestmentTransactionsInput is the input for GET /investments/investment-transactions.
type GetInvestmentTransactionsInput struct {
	inputs.AuthenticatedInput
	inputs.CursorPaginationInput
	AccountID     string `query:"account_id" doc:"Filter by account ID (chain-expanded)"`
	SecurityID    string `query:"security_id" doc:"Filter by security ID"`
	ActivityTypes string `query:"activity_types" doc:"Filter by normalized trade type (comma-separated: buy,sell,dividend,reinvest,fee,cash,other)"`
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
type GetPortfolioAllocationBreakdownInput struct {
	inputs.AuthenticatedInput
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

	holdings, err := h.investmentService.ListHoldings(ctx, input.UserID, accountID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetHoldingsOutput{
		Body: struct {
			Holdings []*domain.HoldingView `json:"holdings"`
			Count    int                   `json:"count"`
		}{Holdings: holdings, Count: len(holdings)},
	}, nil
}

// GetPortfolioSummary handles GET /api/v1/investments/portfolio/summary.
func (h *InvestmentHandler) GetPortfolioSummary(ctx context.Context, input *GetPortfolioSummaryInput) (*GetPortfolioSummaryOutput, error) {
	summary, err := h.investmentService.GetPortfolioSummary(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
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

	return &GetPortfolioHistoryOutput{
		Body: struct {
			History []domain.PortfolioHistoryPoint `json:"history"`
		}{History: history},
	}, nil
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

// GetPortfolioAllocationBreakdown handles GET /api/v1/investments/portfolio/allocation.
func (h *InvestmentHandler) GetPortfolioAllocationBreakdown(ctx context.Context, input *GetPortfolioAllocationBreakdownInput) (*GetPortfolioAllocationBreakdownOutput, error) {
	result, err := h.analyticsService.GetAllocationBreakdown(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
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
	return &GetTopMoversOutput{Body: result}, nil
}

// GetInvestmentGainLossSummary handles GET /api/v1/investments/portfolio/gain-loss.
func (h *InvestmentHandler) GetInvestmentGainLossSummary(ctx context.Context, input *GetInvestmentGainLossSummaryInput) (*GetInvestmentGainLossSummaryOutput, error) {
	result, err := h.analyticsService.GetGainLossSummary(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
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
		Description: "Returns investment transactions for the authenticated user using keyset (cursor) pagination. Supports filtering by account, security, activity type (buy/sell/dividend/reinvest/fee/cash/other), and date range.",
		Tags:        []string{"Investments"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetInvestmentTransactions)

	// ---- Analytics helpers: Analytics tag (load-bearing for MCP parity test) ----

	// GET /api/v1/investments/portfolio/allocation — allocation breakdown by security type.
	huma.Register(api, huma.Operation{
		OperationID: "getPortfolioAllocationBreakdown",
		Method:      http.MethodGet,
		Path:        "/api/v1/investments/portfolio/allocation",
		Summary:     "Get portfolio allocation breakdown",
		Description: "Returns portfolio value grouped by security type (stock, etf, bond, crypto, cash, other, unknown). Each bucket includes total value, percentage, and holding count. Single DB round-trip; designed as an AI-tool-callable function.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
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

