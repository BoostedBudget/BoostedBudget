// Package operations provides HTTP handler implementations for savings impact endpoints.
// Issue #972: Savings Transfer Automation — C1 (public aggregate) + C2 (per-user impact).
package operations

import (
	"context"
	"math"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// K-anonymity thresholds: aggregate is only published when BOTH are met.
// This prevents individual user data from being inferred from small samples.
const (
	MinAggregateUsers  = 10     // minimum user count to publish
	MinAggregateAmount = 1000.0 // minimum total amount to publish ($1000)
)

// SavingsImpactHandler handles savings impact HTTP requests.
type SavingsImpactHandler struct {
	savingsImpactRepo repository.SavingsImpactRepository
}

// NewSavingsImpactHandler creates a new SavingsImpactHandler.
func NewSavingsImpactHandler(repo repository.SavingsImpactRepository) *SavingsImpactHandler {
	return &SavingsImpactHandler{savingsImpactRepo: repo}
}

// --- Input / Output types ---

// GetSavingsImpactAggregateInput is the input for the public aggregate endpoint (no auth).
type GetSavingsImpactAggregateInput struct{}

// SavingsImpactAggregateBody is the response body for GET /api/v1/savings-impact/aggregate.
type SavingsImpactAggregateBody struct {
	Published      bool    `json:"published" doc:"False when k-anonymity threshold not met"`
	TotalAmount    float64 `json:"total_amount" doc:"Total moved to savings, rounded to nearest $1000 (0 when not published)"`
	UserCount      int     `json:"user_count" doc:"Number of users (0 when not published)"`
	CompletedCount int     `json:"completed_count" doc:"Number of completed transfers (0 when not published)"`
	ComputedAt     *string `json:"computed_at,omitempty" doc:"ISO8601 timestamp of last snapshot (omitted when not published)"`
}

// GetSavingsImpactAggregateOutput is the response for GET /api/v1/savings-impact/aggregate.
type GetSavingsImpactAggregateOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         SavingsImpactAggregateBody
}

// GetSavingsImpactMeInput is the input for the per-user savings impact endpoint.
type GetSavingsImpactMeInput struct {
	inputs.AuthenticatedInput
}

// GetSavingsImpactMeOutput is the response for GET /api/v1/savings-impact/me.
type GetSavingsImpactMeOutput struct {
	Body *domain.UserSavingsImpact
}

// --- Handler Methods ---

// GetAggregate handles GET /api/v1/savings-impact/aggregate.
// Public endpoint; returns zeroed body when k-anonymity thresholds are not met.
func (h *SavingsImpactHandler) GetAggregate(ctx context.Context, _ *GetSavingsImpactAggregateInput) (*GetSavingsImpactAggregateOutput, error) {
	snapshot, err := h.savingsImpactRepo.GetLatestSnapshot(ctx)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// K-anonymity guard: publish only when both thresholds are met.
	if snapshot == nil || snapshot.UserCount < MinAggregateUsers || snapshot.TotalAmount < MinAggregateAmount {
		return &GetSavingsImpactAggregateOutput{
			CacheControl: "public, max-age=300",
			Body:         SavingsImpactAggregateBody{Published: false},
		}, nil
	}

	// Round total to nearest $1000 to prevent fingerprinting.
	rounded := math.Round(snapshot.TotalAmount/1000) * 1000
	computedAtStr := snapshot.ComputedAt.UTC().Format(time.RFC3339)
	return &GetSavingsImpactAggregateOutput{
		CacheControl: "public, max-age=300",
		Body: SavingsImpactAggregateBody{
			Published:      true,
			TotalAmount:    rounded,
			UserCount:      snapshot.UserCount,
			CompletedCount: snapshot.CompletedCount,
			ComputedAt:     &computedAtStr,
		},
	}, nil
}

// GetMe handles GET /api/v1/savings-impact/me.
// Returns per-user savings impact totals and recent activity.
func (h *SavingsImpactHandler) GetMe(ctx context.Context, input *GetSavingsImpactMeInput) (*GetSavingsImpactMeOutput, error) {
	impact, err := h.savingsImpactRepo.GetUserImpact(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetSavingsImpactMeOutput{Body: impact}, nil
}

// RegisterSavingsImpactRoutes registers savings impact routes with the Huma API.
func RegisterSavingsImpactRoutes(api huma.API, handler *SavingsImpactHandler) {
	// GET /api/v1/savings-impact/aggregate — public, k-anonymity guarded, CDN-cacheable
	huma.Register(api, huma.Operation{
		OperationID: "getSavingsImpactAggregate",
		Method:      http.MethodGet,
		Path:        "/api/v1/savings-impact/aggregate",
		Summary:     "Get aggregate savings impact",
		Description: "Returns platform-wide savings transfer automation impact. Published only when k-anonymity thresholds are met (≥10 users, ≥$1000 total). Public endpoint — no authentication required.",
		Tags:        []string{"SavingsImpact"},
		// No Security field — public endpoint.
	}, handler.GetAggregate)

	// GET /api/v1/savings-impact/me — authenticated, per-user totals
	huma.Register(api, huma.Operation{
		OperationID: "getSavingsImpactMe",
		Method:      http.MethodGet,
		Path:        "/api/v1/savings-impact/me",
		Summary:     "Get my savings impact",
		Description: "Returns savings transfer automation impact totals for the authenticated user.",
		Tags:        []string{"SavingsImpact"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetMe)
}
