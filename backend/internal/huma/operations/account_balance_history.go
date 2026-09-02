// Package operations — retained per-account balance history for a closed account (Issue #1569).
//
//	GET /api/v1/accounts/{id}/balance-history
//
// ONE operation, in its own file and behind its own handler, deliberately separate from the
// account operations in plaid.go and from getBalanceHistory in the dashboard. Those answer
// CURRENT-STATE or aggregate questions — getBalanceHistory takes account_types and has never
// taken an account_id, and accountBalanceProjection nulls a terminal account's balance pair on
// purpose. This one answers a HISTORICAL question about exactly the accounts those reads exist to
// exclude. The split mirrors the retained holdings history route and #1569 T2's deleted-goal
// ledger: the live path stays structurally incapable of returning historical figures, so nothing
// can widen into it by accident later.
package operations

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// AccountBalanceHistoryHandler serves the retained per-account balance history read.
type AccountBalanceHistoryHandler struct {
	historyService services.AccountBalanceHistoryService
}

// NewAccountBalanceHistoryHandler creates a new AccountBalanceHistoryHandler.
func NewAccountBalanceHistoryHandler(svc services.AccountBalanceHistoryService) *AccountBalanceHistoryHandler {
	return &AccountBalanceHistoryHandler{historyService: svc}
}

// GetAccountBalanceHistoryInput is the input for GET /accounts/{id}/balance-history.
type GetAccountBalanceHistoryInput struct {
	inputs.AuthenticatedInput
	ID        string `path:"id" doc:"Account whose retained balance history to read."`
	StartDate string `query:"start_date" doc:"Inclusive UTC calendar day (YYYY-MM-DD) to read from. Omit for the earliest day retained."`
	EndDate   string `query:"end_date" doc:"Inclusive UTC calendar day (YYYY-MM-DD) to read to. Omit for the most recent day retained."`
}

// GetAccountBalanceHistoryOutput is the response for GET /accounts/{id}/balance-history.
type GetAccountBalanceHistoryOutput struct {
	Body *domain.AccountBalanceHistory
}

// parseHistoryDay validates a YYYY-MM-DD UTC calendar day with time.Parse — not a shape-only
// regex, which passes impossible dates like 2026-02-30 straight through to the ::date cast.
// Returns nil for the empty string, which means "unbounded".
func parseHistoryDay(field, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	d, err := time.Parse(isoDateLayout, value)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid " + field + ": must be a valid UTC calendar day formatted YYYY-MM-DD")
	}
	return &d, nil
}

// GetAccountBalanceHistory handles GET /api/v1/accounts/{id}/balance-history.
//
// Tenancy is enforced in SQL by the repository (user_id AND account_id on the one statement). An
// account belonging to somebody else simply has no retained history from this user's seat, so it
// returns the same empty series as an account with none — no existence is disclosed either way,
// and there is deliberately no 404-for-unknown-account branch that would leak one.
//
// An account with nothing retained returns 200 with an empty series rather than 404: "we kept
// nothing for this account" is a true answer about the user's money, and a 404 would render as a
// failure on a screen whose entire purpose is to show that nothing was destroyed.
func (h *AccountBalanceHistoryHandler) GetAccountBalanceHistory(ctx context.Context, input *GetAccountBalanceHistoryInput) (*GetAccountBalanceHistoryOutput, error) {
	if _, err := uuid.Parse(input.ID); err != nil {
		return nil, huma.Error400BadRequest("invalid account id: must be a valid UUID")
	}

	start, err := parseHistoryDay("start_date", input.StartDate)
	if err != nil {
		return nil, err
	}
	end, err := parseHistoryDay("end_date", input.EndDate)
	if err != nil {
		return nil, err
	}
	// An inverted window can only ever return an empty series, and an empty series on this screen
	// says "nothing was retained" — a false statement about the user's money. Refused rather than
	// answered.
	if start != nil && end != nil && end.Before(*start) {
		return nil, huma.Error400BadRequest("invalid range: end_date must not be before start_date")
	}

	history, err := h.historyService.GetAccountBalanceHistory(ctx, input.UserID, input.ID, start, end)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &GetAccountBalanceHistoryOutput{Body: history}, nil
}

// RegisterAccountBalanceHistoryRoutes registers the retained-balance-history route.
//
// ⚠ Called from BOTH operations.RegisterAllRoutesForSpec (which is the only thing
// `make generate-openapi` reads — a route wired solely in main.go produces a zero-byte spec diff)
// and cmd/server/main.go (which is the only thing that serves it). Both, always.
func RegisterAccountBalanceHistoryRoutes(api huma.API, handler *AccountBalanceHistoryHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "getAccountBalanceHistory",
		Method:      http.MethodGet,
		Path:        "/api/v1/accounts/{id}/balance-history",
		Summary:     "Get an account's retained balance history",
		Description: "Returns one account's retained day-by-day balances, oldest day first, with the UTC day each " +
			"balance was true on (Issue #1569). Removing a linked bank account or disconnecting a bank connection " +
			"retains its balance history rather than erasing it, and this is the only endpoint that reads it back per " +
			"account: getBalanceHistory answers an aggregated CURRENT-STATE question, takes account_types rather than " +
			"an account id, and deliberately excludes closed accounts. " +
			"An account with nothing retained returns an empty series rather than 404. " +
			"⚠ These are HISTORICAL figures. Each is inseparable from its date and none of them is what the account " +
			"holds now — a closed account has no current balance at all.",
		Tags:     []string{"Accounts"},
		Security: []map[string][]string{{"clerk": {}}},
	}, handler.GetAccountBalanceHistory)
}
