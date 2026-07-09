package operations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// HouseholdShareHandler handles per-account household-visibility HTTP requests.
// Depends on the narrow services.HouseholdAccountShareService interface (not the broad
// services.HouseholdService) so this handler's dependency, and any hand-written mock of
// it, stays scoped to exactly the two methods it needs. #1368 — Phase 4: account sharing
// grants. No pre-join hide: this endpoint is active-member-only — a member configures
// sharing AFTER joining (the accept transaction's hidden_account_ids remains the only
// join-time opt-out, unchanged by this phase).
type HouseholdShareHandler struct {
	service services.HouseholdAccountShareService
}

// NewHouseholdShareHandler creates a new HouseholdShareHandler instance.
func NewHouseholdShareHandler(service services.HouseholdAccountShareService) *HouseholdShareHandler {
	return &HouseholdShareHandler{service: service}
}

// --- Input/Output Types ---

// SetHouseholdShareVisibilityRequestBody is the PATCH request body.
type SetHouseholdShareVisibilityRequestBody struct {
	Visibility string `json:"visibility" enum:"shared,hidden" doc:"shared: included in the household aggregate. hidden: excluded, but the member stays in the household."`
}

// SetHouseholdShareVisibilityInput represents the input for toggling a user account's
// household-share visibility.
type SetHouseholdShareVisibilityInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"User Account UUID"`
	Body SetHouseholdShareVisibilityRequestBody
}

// SetHouseholdShareVisibilityOutput represents the response for the PATCH endpoint.
type SetHouseholdShareVisibilityOutput struct {
	Body *domain.HouseholdAccountShare
}

// ListMyAccountSharesInput represents the input for listing the caller's own
// household-share state across their accounts.
type ListMyAccountSharesInput struct {
	inputs.AuthenticatedInput
}

// ListMyAccountSharesOutput represents the response for the GET endpoint.
type ListMyAccountSharesOutput struct {
	Body []domain.UserAccountShareView
}

// --- Handler Methods ---

// SetHouseholdShareVisibility handles PATCH /api/v1/user-accounts/:id/household-share
// requests. Active-member-only, owner-of-account-only.
func (h *HouseholdShareHandler) SetHouseholdShareVisibility(ctx context.Context, input *SetHouseholdShareVisibilityInput) (*SetHouseholdShareVisibilityOutput, error) {
	share, err := h.service.SetAccountShareVisibility(ctx, input.UserID, input.ID, domain.ShareVisibility(input.Body.Visibility))
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &SetHouseholdShareVisibilityOutput{Body: share}, nil
}

// ListMyAccountShares handles GET /api/v1/households/me/account-shares requests.
// Returns the caller's own accounts with their current household-share state, including
// accounts with no explicit grant yet (nil Visibility) — e.g. linked after joining.
func (h *HouseholdShareHandler) ListMyAccountShares(ctx context.Context, input *ListMyAccountSharesInput) (*ListMyAccountSharesOutput, error) {
	views, err := h.service.ListMyAccountShares(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	if views == nil {
		views = []domain.UserAccountShareView{}
	}
	return &ListMyAccountSharesOutput{Body: views}, nil
}

// RegisterHouseholdShareRoutes registers household account-share routes with the Huma
// API. Note: These routes require the Clerk middleware to be applied.
func RegisterHouseholdShareRoutes(api huma.API, handler *HouseholdShareHandler) {
	// PATCH /api/v1/user-accounts/:id/household-share - toggle sharing for one account
	huma.Register(api, huma.Operation{
		OperationID:   "setHouseholdShareVisibility",
		Method:        http.MethodPatch,
		Path:          "/api/v1/user-accounts/{id}/household-share",
		Summary:       "Set household share visibility for a user account",
		Description:   "Toggles whether this account is included in the household aggregate seen by other members. Active-member-only; the caller must own the account. Returns 404 if the caller is not an active member of a household, 403 if the account does not belong to the caller.",
		Tags:          []string{"Household"},
		DefaultStatus: http.StatusOK,
		Security: []map[string][]string{
			{"clerk": {}},
		},
	}, handler.SetHouseholdShareVisibility)

	// GET /api/v1/households/me/account-shares - list the caller's own share state
	huma.Register(api, huma.Operation{
		OperationID: "listMyAccountShares",
		Method:      http.MethodGet,
		Path:        "/api/v1/households/me/account-shares",
		Summary:     "List household share visibility for the caller's own accounts",
		Description: "Returns the caller's own accounts with their current household-share state. An account with no explicit grant yet (e.g. linked after joining) has a null visibility. Returns 404 if the caller is not an active member of a household.",
		Tags:        []string{"Household"},
		Security: []map[string][]string{
			{"clerk": {}},
		},
	}, handler.ListMyAccountShares)
}
