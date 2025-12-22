package operations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// GetSubscriptionTiersInput represents the input for the subscription tiers endpoint.
// This endpoint requires no parameters.
type GetSubscriptionTiersInput struct{}

// GetSubscriptionTiersOutput represents the subscription tiers response.
type GetSubscriptionTiersOutput struct {
	// CacheControl header for CDN caching (24 hours)
	CacheControl string `header:"Cache-Control"`
	Body         SubscriptionTiersResponse
}

// SubscriptionTiersResponse represents the API response for subscription tiers.
type SubscriptionTiersResponse struct {
	Tiers       map[domain.TierName]domain.SubscriptionTier `json:"tiers" doc:"Map of tier names to tier configurations"`
	DefaultTier domain.TierName                             `json:"default_tier" doc:"The default tier for new users"`
}

// SubscriptionTiersHandler handles subscription tier configuration requests.
type SubscriptionTiersHandler struct {
	tierConfig *config.TierConfig
}

// NewSubscriptionTiersHandler creates a new subscription tiers handler.
// Panics if tierConfig is nil (required dependency).
func NewSubscriptionTiersHandler(tierConfig *config.TierConfig) *SubscriptionTiersHandler {
	if tierConfig == nil {
		panic("tierConfig is required for SubscriptionTiersHandler")
	}

	return &SubscriptionTiersHandler{
		tierConfig: tierConfig,
	}
}

// GetSubscriptionTiers handles GET /api/v1/subscription/tiers requests.
// Returns all available subscription tiers with their limits, features, and pricing.
// This is a public endpoint (no authentication required) for displaying pricing information.
func (h *SubscriptionTiersHandler) GetSubscriptionTiers(ctx context.Context, input *GetSubscriptionTiersInput) (*GetSubscriptionTiersOutput, error) {
	return &GetSubscriptionTiersOutput{
		CacheControl: "public, max-age=86400",
		Body: SubscriptionTiersResponse{
			Tiers:       h.tierConfig.Tiers,
			DefaultTier: h.tierConfig.DefaultTier,
		},
	}, nil
}

// RegisterSubscriptionTiersRoutes registers subscription tiers routes with the Huma API.
func RegisterSubscriptionTiersRoutes(api huma.API, handler *SubscriptionTiersHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "getSubscriptionTiers",
		Method:      http.MethodGet,
		Path:        "/api/v1/subscription/tiers",
		Summary:     "Get subscription tiers",
		Description: "Returns all available subscription tiers with their limits, features, and pricing. This is a public endpoint for displaying pricing information.",
		Tags:        []string{"Subscription"},
	}, handler.GetSubscriptionTiers)
}
