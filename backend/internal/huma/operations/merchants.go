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

// MerchantServiceInterface is the dependency the merchant handler requires.
// *services.MerchantService satisfies this interface.
type MerchantServiceInterface interface {
	ListMerchants(ctx context.Context, userID string) ([]domain.MerchantSummary, error)
	GetDuplicateSuggestions(ctx context.Context, userID string) ([][]domain.MerchantSummary, error)
	SetOverride(ctx context.Context, userID, merchantID string, displayName *string, mergedIntoMerchantID *string) (*domain.MerchantNameOverride, error)
	ClearOverride(ctx context.Context, userID, merchantID string) error
	BulkSetOverrides(ctx context.Context, userID string, reqs []services.SetOverrideRequest) ([]domain.MerchantNameOverride, error)
	MergeMerchants(ctx context.Context, userID string, merchantIDs []string, targetMerchantID string) ([]domain.MerchantNameOverride, error)
}

// MerchantHandler handles HTTP requests for merchant name management.
type MerchantHandler struct {
	service MerchantServiceInterface
}

// NewMerchantHandler constructs a MerchantHandler.
func NewMerchantHandler(svc MerchantServiceInterface) *MerchantHandler {
	return &MerchantHandler{service: svc}
}

// merchantAuth is an embeddable struct for user ID resolution.
// It is populated by resolveMerchantUserID — not a huma.Resolver itself.
type merchantAuth struct {
	UserID string `json:"-" doc:"Authenticated user ID"`
}

// resolveMerchantUserID fills auth.UserID from the Go context set by Clerk middleware.
// Matches AuthenticatedInput.Resolve — context-only, no header fallback.
func resolveMerchantUserID(ctx huma.Context, auth *merchantAuth) []error {
	uid, ok := ctx.Context().Value(inputs.UserIDKey).(string)
	if !ok || uid == "" {
		return []error{huma.Error401Unauthorized("User not authenticated")}
	}
	auth.UserID = uid
	return nil
}

// ── Input / Output types ───────────────────────────────────────────────────

type GetMerchantsInput struct {
	merchantAuth
}

func (i *GetMerchantsInput) Resolve(ctx huma.Context) []error {
	return resolveMerchantUserID(ctx, &i.merchantAuth)
}

var _ huma.Resolver = (*GetMerchantsInput)(nil)

type GetMerchantsOutput struct {
	Body []domain.MerchantSummary
}

type GetDuplicateSuggestionsInput struct {
	merchantAuth
}

func (i *GetDuplicateSuggestionsInput) Resolve(ctx huma.Context) []error {
	return resolveMerchantUserID(ctx, &i.merchantAuth)
}

var _ huma.Resolver = (*GetDuplicateSuggestionsInput)(nil)

type GetDuplicateSuggestionsOutput struct {
	Body [][]domain.MerchantSummary
}

type SetMerchantOverrideBody struct {
	DisplayName          *string `json:"display_name,omitempty" doc:"Custom display name for the merchant"`
	MergedIntoMerchantID *string `json:"merged_into_merchant_id,omitempty" doc:"Target merchant UUID to merge this merchant into"`
}

type SetMerchantOverrideInput struct {
	merchantAuth
	MerchantID string                  `path:"merchantId" doc:"Merchant UUID"`
	Body       SetMerchantOverrideBody `json:"body"`
}

func (i *SetMerchantOverrideInput) Resolve(ctx huma.Context) []error {
	if errs := resolveMerchantUserID(ctx, &i.merchantAuth); len(errs) > 0 {
		return errs
	}
	if i.Body.DisplayName == nil && i.Body.MergedIntoMerchantID == nil {
		return []error{huma.Error400BadRequest("at least one of display_name or merged_into_merchant_id must be set")}
	}
	return nil
}

var _ huma.Resolver = (*SetMerchantOverrideInput)(nil)

type SetMerchantOverrideOutput struct {
	Body *domain.MerchantNameOverride
}

type ClearMerchantOverrideInput struct {
	merchantAuth
	MerchantID string `path:"merchantId" doc:"Merchant UUID"`
}

func (i *ClearMerchantOverrideInput) Resolve(ctx huma.Context) []error {
	return resolveMerchantUserID(ctx, &i.merchantAuth)
}

var _ huma.Resolver = (*ClearMerchantOverrideInput)(nil)

type BulkSetOverridesBody struct {
	Overrides []services.SetOverrideRequest `json:"overrides" doc:"List of merchant overrides to upsert"`
}

type BulkSetOverridesInput struct {
	merchantAuth
	Body BulkSetOverridesBody `json:"body"`
}

func (i *BulkSetOverridesInput) Resolve(ctx huma.Context) []error {
	return resolveMerchantUserID(ctx, &i.merchantAuth)
}

var _ huma.Resolver = (*BulkSetOverridesInput)(nil)

type BulkSetOverridesOutput struct {
	Body []domain.MerchantNameOverride
}

type MergeMerchantsBody struct {
	MerchantIDs      []string `json:"merchant_ids" doc:"Source merchant UUIDs to merge (the duplicates)"`
	TargetMerchantID string   `json:"target_merchant_id" doc:"Target merchant UUID to merge into (the canonical)"`
}

type MergeMerchantsInput struct {
	merchantAuth
	Body MergeMerchantsBody `json:"body"`
}

func (i *MergeMerchantsInput) Resolve(ctx huma.Context) []error {
	if errs := resolveMerchantUserID(ctx, &i.merchantAuth); len(errs) > 0 {
		return errs
	}
	if len(i.Body.MerchantIDs) == 0 {
		return []error{huma.Error400BadRequest("merchant_ids must not be empty")}
	}
	return nil
}

var _ huma.Resolver = (*MergeMerchantsInput)(nil)

type MergeMerchantsOutput struct {
	Body []domain.MerchantNameOverride
}

// ── Handler methods ────────────────────────────────────────────────────────

// GetMerchants handles GET /api/v1/merchants.
func (h *MerchantHandler) GetMerchants(ctx context.Context, input *GetMerchantsInput) (*GetMerchantsOutput, error) {
	merchants, err := h.service.ListMerchants(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	if merchants == nil {
		merchants = []domain.MerchantSummary{}
	}
	return &GetMerchantsOutput{Body: merchants}, nil
}

// GetDuplicateSuggestions handles GET /api/v1/merchants/duplicate-suggestions.
func (h *MerchantHandler) GetDuplicateSuggestions(ctx context.Context, input *GetDuplicateSuggestionsInput) (*GetDuplicateSuggestionsOutput, error) {
	groups, err := h.service.GetDuplicateSuggestions(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	if groups == nil {
		groups = [][]domain.MerchantSummary{}
	}
	return &GetDuplicateSuggestionsOutput{Body: groups}, nil
}

// SetMerchantOverride handles PUT /api/v1/merchants/{merchantId}/override.
func (h *MerchantHandler) SetMerchantOverride(ctx context.Context, input *SetMerchantOverrideInput) (*SetMerchantOverrideOutput, error) {
	override, err := h.service.SetOverride(ctx, input.UserID, input.MerchantID, input.Body.DisplayName, input.Body.MergedIntoMerchantID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &SetMerchantOverrideOutput{Body: override}, nil
}

// ClearMerchantOverride handles DELETE /api/v1/merchants/{merchantId}/override.
func (h *MerchantHandler) ClearMerchantOverride(ctx context.Context, input *ClearMerchantOverrideInput) (*struct{}, error) {
	if err := h.service.ClearOverride(ctx, input.UserID, input.MerchantID); err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return nil, nil
}

// BulkSetMerchantOverrides handles POST /api/v1/merchants/overrides/bulk.
func (h *MerchantHandler) BulkSetMerchantOverrides(ctx context.Context, input *BulkSetOverridesInput) (*BulkSetOverridesOutput, error) {
	results, err := h.service.BulkSetOverrides(ctx, input.UserID, input.Body.Overrides)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	if results == nil {
		results = []domain.MerchantNameOverride{}
	}
	return &BulkSetOverridesOutput{Body: results}, nil
}

// MergeMerchants handles POST /api/v1/merchants/merge.
func (h *MerchantHandler) MergeMerchants(ctx context.Context, input *MergeMerchantsInput) (*MergeMerchantsOutput, error) {
	overrides, err := h.service.MergeMerchants(ctx, input.UserID, input.Body.MerchantIDs, input.Body.TargetMerchantID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	if overrides == nil {
		overrides = []domain.MerchantNameOverride{}
	}
	return &MergeMerchantsOutput{Body: overrides}, nil
}

// RegisterMerchantRoutes registers all merchant endpoints with the Huma API.
func RegisterMerchantRoutes(api huma.API, handler *MerchantHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "getMerchants",
		Method:      http.MethodGet,
		Path:        "/api/v1/merchants",
		Summary:     "List merchants",
		Description: "Lists all distinct merchants for the authenticated user with spend totals and override status.",
		Tags:        []string{"Merchants"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetMerchants)

	huma.Register(api, huma.Operation{
		OperationID: "getMerchantDuplicateSuggestions",
		Method:      http.MethodGet,
		Path:        "/api/v1/merchants/duplicate-suggestions",
		Summary:     "Get merchant duplicate suggestions",
		Description: "Returns groups of merchants that appear to be duplicates based on fuzzy name similarity.",
		Tags:        []string{"Merchants"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetDuplicateSuggestions)

	huma.Register(api, huma.Operation{
		OperationID: "setMerchantOverride",
		Method:      http.MethodPut,
		Path:        "/api/v1/merchants/{merchantId}/override",
		Summary:     "Set merchant name override",
		Description: "Creates or updates a per-user merchant name override or merge pointer.",
		Tags:        []string{"Merchants"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.SetMerchantOverride)

	huma.Register(api, huma.Operation{
		OperationID:   "clearMerchantOverride",
		Method:        http.MethodDelete,
		Path:          "/api/v1/merchants/{merchantId}/override",
		Summary:       "Clear merchant name override",
		Description:   "Removes the per-user override for a merchant, reverting to the system normalized name.",
		Tags:          []string{"Merchants"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.ClearMerchantOverride)

	huma.Register(api, huma.Operation{
		OperationID: "bulkSetMerchantOverrides",
		Method:      http.MethodPost,
		Path:        "/api/v1/merchants/overrides/bulk",
		Summary:     "Bulk set merchant name overrides",
		Description: "Upserts multiple merchant name overrides in a single request. MCP-friendly.",
		Tags:        []string{"Merchants"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.BulkSetMerchantOverrides)

	huma.Register(api, huma.Operation{
		OperationID: "mergeMerchants",
		Method:      http.MethodPost,
		Path:        "/api/v1/merchants/merge",
		Summary:     "Merge merchants",
		Description: "Creates per-user merge overrides so source merchants display under the target merchant name.",
		Tags:        []string{"Merchants"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.MergeMerchants)
}
