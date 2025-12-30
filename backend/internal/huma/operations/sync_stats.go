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

// SyncStatsHandler handles sync statistics HTTP requests.
// Issue #242: Monthly sync limits - expose usage stats to frontend
type SyncStatsHandler struct {
	service services.SyncLimitService
}

// NewSyncStatsHandler creates a new SyncStatsHandler instance.
func NewSyncStatsHandler(service services.SyncLimitService) *SyncStatsHandler {
	return &SyncStatsHandler{service: service}
}

// --- Input/Output Types ---

// GetSyncStatsInput represents the input for getting sync statistics.
type GetSyncStatsInput struct {
	inputs.AuthenticatedInput
}

// GetSyncStatsOutput represents the response for getting sync statistics.
type GetSyncStatsOutput struct {
	Body *domain.SyncStats
}

// --- Handler Methods ---

// GetSyncStats handles GET /api/v1/sync-stats requests.
// Returns monthly sync usage statistics for the authenticated user.
func (h *SyncStatsHandler) GetSyncStats(ctx context.Context, input *GetSyncStatsInput) (*GetSyncStatsOutput, error) {
	stats, err := h.service.GetSyncStats(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &GetSyncStatsOutput{Body: stats}, nil
}

// RegisterSyncStatsRoutes registers sync stats routes with the Huma API.
// Note: These routes require the Clerk middleware to be applied.
func RegisterSyncStatsRoutes(api huma.API, handler *SyncStatsHandler) {
	// GET /api/v1/sync-stats - Get sync statistics
	huma.Register(api, huma.Operation{
		OperationID: "getSyncStats",
		Method:      http.MethodGet,
		Path:        "/api/v1/sync-stats",
		Summary:     "Get sync statistics",
		Description: "Returns monthly sync usage statistics for the authenticated user.",
		Tags:        []string{"Sync"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetSyncStats)
}
