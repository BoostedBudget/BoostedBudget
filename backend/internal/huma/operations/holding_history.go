// Package operations — retained holdings history for a closed account (Issue #1569).
//
//	GET /api/v1/investments/holdings/history
//
// ONE operation, in its own file and behind its own handler, deliberately separate from the eight
// in investments.go. Those all answer CURRENT-STATE questions through repositories that render
// currentStateAccountLeg; this one answers a HISTORICAL question about accounts those reads exist
// to exclude. The split mirrors #1569 T2's choice to give a deleted goal's retained ledger its own
// route rather than a flag on the live listing: the live path stays structurally incapable of
// returning historical figures, so nothing can widen into it by accident later.
package operations

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// HoldingHistoryHandler serves the retained holdings history read.
type HoldingHistoryHandler struct {
	historyService services.HoldingHistoryService
}

// NewHoldingHistoryHandler creates a new HoldingHistoryHandler.
func NewHoldingHistoryHandler(svc services.HoldingHistoryService) *HoldingHistoryHandler {
	return &HoldingHistoryHandler{historyService: svc}
}

// isoDateLayout is the reference layout for a YYYY-MM-DD UTC calendar day, validated with
// time.Parse (not a shape-only regex) before it reaches the ::date cast. A regex that only checks
// digit positions passes impossible calendar dates like 2026-02-30 or 2026-13-01 straight through
// to the database; time.Parse rejects both, along with malformed shapes and trailing garbage,
// because it actually constructs the calendar date rather than pattern-matching its shape.
const isoDateLayout = "2006-01-02"

// GetAccountHoldingsHistoryInput is the input for GET /investments/holdings/history.
type GetAccountHoldingsHistoryInput struct {
	inputs.AuthenticatedInput
	AccountID string `query:"account_id" required:"true" doc:"Account whose retained holdings history to read. Required — this endpoint is per-account by design; there is no whole-portfolio historical read, because a portfolio total mixing live and closed accounts is a current-state figure."`
	AsOf      string `query:"as_of" doc:"UTC calendar day (YYYY-MM-DD) to read. Omit for the most recent retained day. A day nothing was retained for returns 404 rather than an empty list; a day the account was observed holding nothing returns 200 with an empty position list, because those are different facts."`
}

// GetAccountHoldingsHistoryOutput is the response for GET /investments/holdings/history.
type GetAccountHoldingsHistoryOutput struct {
	Body *domain.HoldingSnapshotHistory
}

// GetAccountHoldingsHistory handles GET /api/v1/investments/holdings/history.
//
// Tenancy is enforced in SQL by the repository (user_id on every statement), identically to the
// live holdings read. An account belonging to somebody else simply has no retained history from
// this user's seat, so it returns the same empty answer as an account with none — no existence is
// disclosed either way.
func (h *HoldingHistoryHandler) GetAccountHoldingsHistory(ctx context.Context, input *GetAccountHoldingsHistoryInput) (*GetAccountHoldingsHistoryOutput, error) {
	if _, err := uuid.Parse(input.AccountID); err != nil {
		return nil, huma.Error400BadRequest("invalid account_id: must be a valid UUID")
	}
	// parseHistoryDay (account_balance_history.go) does the same time.Parse-based validation this
	// endpoint needs; only its error is used here — the string form of AsOf, not the *time.Time,
	// is what the service below takes.
	if _, err := parseHistoryDay("as_of", input.AsOf); err != nil {
		return nil, err
	}

	history, err := h.historyService.GetAccountHoldingsHistory(ctx, input.UserID, input.AccountID, input.AsOf)
	if err != nil {
		if errors.Is(err, domain.ErrHoldingHistoryDateNotRetained) {
			return nil, huma.Error404NotFound("no retained holdings history for that date")
		}
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	// #1636 rounding boundary — money_rounding.go. ⚠ Quantity and InstitutionPrice stay exact:
	// holding_snapshots gives them DECIMAL(18,8) so a past fractional position is retained as it
	// stood.
	roundHoldingSnapshotHistory(history)
	return &GetAccountHoldingsHistoryOutput{Body: history}, nil
}

// RegisterHoldingHistoryRoutes registers the retained-holdings-history route.
//
// ⚠ Called from BOTH operations.RegisterAllRoutesForSpec (which is the only thing
// `make generate-openapi` reads — a route wired solely in main.go produces a zero-byte spec diff)
// and cmd/server/main.go (which is the only thing that serves it). Both, always.
func RegisterHoldingHistoryRoutes(api huma.API, handler *HoldingHistoryHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "getAccountHoldingsHistory",
		Method:      http.MethodGet,
		Path:        "/api/v1/investments/holdings/history",
		Summary:     "Get an account's retained holdings history",
		Description: "Returns the investment positions retained for one account as they stood on a past UTC day, " +
			"with the list of every other day that can be asked for and the day's pre-computed total value (Issue #1569). " +
			"Removing a linked bank account retains its holdings history rather than erasing it, and this is the only " +
			"endpoint that reads it back: GET /investments/holdings answers a CURRENT-STATE question and deliberately " +
			"returns nothing for a closed account. " +
			"Omit as_of for the most recent retained day. A day NOTHING was retained for returns 404 rather than an " +
			"empty list, because \"we kept nothing for that day\" is not \"the account held nothing that day\" — and the " +
			"second fact has its own answer: a day on which the account was observed holding nothing (a completed " +
			"liquidation, whose rows are retained and marked absent) is listed in available_dates and returns 200 with " +
			"an empty positions list. " +
			"⚠ These are HISTORICAL figures. They carry no day-change, because day-change is measured against today's " +
			"prior close, and they must never be presented as what the account holds now.",
		Tags:     []string{"Investments"},
		Security: []map[string][]string{{"clerk": {}}},
	}, handler.GetAccountHoldingsHistory)
}
