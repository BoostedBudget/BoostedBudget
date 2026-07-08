package operations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
)

// entitlementSummaryGetter is the narrow capability HouseholdEntitlementHandler needs
// from *services.HouseholdEntitlementService — kept minimal for testability.
type entitlementSummaryGetter interface {
	GetEntitlementSummary(ctx context.Context, user *domain.User) (domain.HouseholdEntitlementSummary, error)
}

// entitlementUserLoader is the narrow user-lookup capability this handler needs.
// repository.ProfileRepository satisfies this structurally.
type entitlementUserLoader interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
}

// HouseholdEntitlementHandler handles the household entitlement summary endpoint.
type HouseholdEntitlementHandler struct {
	entitlementService entitlementSummaryGetter
	profileRepo        entitlementUserLoader
}

// NewHouseholdEntitlementHandler creates a new HouseholdEntitlementHandler instance.
func NewHouseholdEntitlementHandler(entitlementService entitlementSummaryGetter, profileRepo entitlementUserLoader) *HouseholdEntitlementHandler {
	return &HouseholdEntitlementHandler{entitlementService: entitlementService, profileRepo: profileRepo}
}

// GetHouseholdEntitlementInput represents the input for the entitlement summary endpoint.
type GetHouseholdEntitlementInput struct {
	inputs.AuthenticatedInput
}

// GetHouseholdEntitlementOutput represents the response for the entitlement summary endpoint.
type GetHouseholdEntitlementOutput struct {
	Body domain.HouseholdEntitlementSummary
}

// GetHouseholdEntitlement handles GET /api/v1/households/me/entitlement requests.
// Returns the authenticated user's effective tier, seat counts, and coverage source,
// accounting for household membership (Issue #1366). Solo users get a 200 with
// SeatsActive/SeatsPending 0 and IsOwner false — never a 404.
func (h *HouseholdEntitlementHandler) GetHouseholdEntitlement(ctx context.Context, input *GetHouseholdEntitlementInput) (*GetHouseholdEntitlementOutput, error) {
	user, err := h.profileRepo.GetUserByID(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	summary, err := h.entitlementService.GetEntitlementSummary(ctx, user)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetHouseholdEntitlementOutput{Body: summary}, nil
}

// RegisterHouseholdEntitlementRoutes registers household entitlement routes with the Huma API.
func RegisterHouseholdEntitlementRoutes(api huma.API, handler *HouseholdEntitlementHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "getHouseholdEntitlement",
		Method:      http.MethodGet,
		Path:        "/api/v1/households/me/entitlement",
		Summary:     "Get effective household entitlement",
		Description: "Returns the authenticated user's effective subscription tier, seat counts, and coverage source, accounting for household membership.",
		Tags:        []string{"Household"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetHouseholdEntitlement)
}
