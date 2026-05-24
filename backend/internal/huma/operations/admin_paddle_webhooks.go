package operations

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/dateutil"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/queue/jobs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// ListPaddleWebhooksInput represents the query parameters for listing Paddle webhook events.
type ListPaddleWebhooksInput struct {
	inputs.AdminAuthenticatedInput
	Limit     int    `query:"limit" default:"20" minimum:"1" maximum:"100"`
	Offset    int    `query:"offset" default:"0" minimum:"0"`
	EventType string `query:"event_type" doc:"Filter by event type (empty = all)"`
	Processed string `query:"processed" doc:"Filter by processed status: 'true', 'false', or empty for all"`
	StartDate string `query:"start_date" doc:"ISO 8601 date string"`
	EndDate   string `query:"end_date" doc:"ISO 8601 date string"`
	SortBy    string `query:"sort_by"  enum:"created_at,attempts,status" default:"created_at"  doc:"Column to sort by"`
	SortDir   string `query:"sort_dir" enum:"asc,desc"                   default:"desc"         doc:"Sort direction"`
}

// ListPaddleWebhooksOutput represents the response for listing Paddle webhook events.
type ListPaddleWebhooksOutput struct {
	Body *domain.WebhookEventListResponse
}

// GetPaddleWebhookDetailInput represents the input for getting a specific webhook event.
type GetPaddleWebhookDetailInput struct {
	inputs.AdminAuthenticatedInput
	ID string `path:"id"`
}

// GetPaddleWebhookDetailOutput represents the response for a specific webhook event.
type GetPaddleWebhookDetailOutput struct {
	Body *domain.PaddleWebhookEvent
}

// ReplayPaddleWebhookInput represents the input for replaying a webhook event.
type ReplayPaddleWebhookInput struct {
	inputs.AdminAuthenticatedInput
	ID string `path:"id"`
}

// ReplayPaddleWebhookOutput represents the response for replaying a webhook event.
type ReplayPaddleWebhookOutput struct {
	Body *domain.WebhookReplayResponse
}

// GetPaddleWebhookStatsInput represents the query parameters for webhook statistics.
type GetPaddleWebhookStatsInput struct {
	inputs.AdminAuthenticatedInput
	TimeRange string `query:"time_range" default:"24h" enum:"24h,7d,30d,all"`
}

// GetPaddleWebhookStatsOutput represents the response for webhook statistics.
type GetPaddleWebhookStatsOutput struct {
	Body *domain.WebhookStatsResponse
}

// ListPaddleWebhooks returns a paginated list of Paddle webhook events.
//
// GET /api/v1/admin/webhooks/paddle
func (h *AdminHandler) ListPaddleWebhooks(ctx context.Context, input *ListPaddleWebhooksInput) (*ListPaddleWebhooksOutput, error) {
	if h.paddleWebhookRepo == nil {
		return nil, huma.Error503ServiceUnavailable("paddle webhook repository not configured")
	}
	filters := h.buildWebhookEventFilters(input)

	events, total, err := h.paddleWebhookRepo.ListWebhookEvents(ctx, filters, input.Limit, input.Offset)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to list webhook events: %w", err))
	}
	if events == nil {
		events = []domain.PaddleWebhookEvent{}
	}

	page := 1
	if input.Offset > 0 && input.Limit > 0 {
		page = (input.Offset / input.Limit) + 1
	}
	totalPages := 1
	if input.Limit > 0 && total > 0 {
		totalPages = (total + input.Limit - 1) / input.Limit
	}

	return &ListPaddleWebhooksOutput{
		Body: &domain.WebhookEventListResponse{
			Events:     events,
			Total:      total,
			Page:       page,
			PageSize:   input.Limit,
			TotalPages: totalPages,
			HasMore:    input.Offset+input.Limit < total,
		},
	}, nil
}

// buildWebhookEventFilters constructs filter criteria from request input
func (h *AdminHandler) buildWebhookEventFilters(input *ListPaddleWebhooksInput) domain.WebhookEventFilters {
	filters := domain.WebhookEventFilters{}
	if input.EventType != "" {
		et := input.EventType
		filters.EventType = &et
	}
	if input.Processed != "" {
		if b, err := strconv.ParseBool(input.Processed); err == nil {
			filters.Processed = &b
		}
	}
	if t, _ := dateutil.ParseOptionalDate(input.StartDate, "start_date"); t != nil {
		filters.StartDate = t
	}
	if t, _ := dateutil.ParseOptionalDate(input.EndDate, "end_date"); t != nil {
		filters.EndDate = t
	}
	filters.SortBy = input.SortBy
	filters.SortDir = input.SortDir
	return filters
}

// GetPaddleWebhookDetail returns a single Paddle webhook event by ID.
//
// GET /api/v1/admin/webhooks/paddle/{id}
func (h *AdminHandler) GetPaddleWebhookDetail(ctx context.Context, input *GetPaddleWebhookDetailInput) (*GetPaddleWebhookDetailOutput, error) {
	if h.paddleWebhookRepo == nil {
		return nil, huma.Error503ServiceUnavailable("paddle webhook repository not configured")
	}
	event, err := h.paddleWebhookRepo.GetByID(ctx, input.ID)
	if err != nil {
		if err == repository.ErrPaddleWebhookNotFound {
			return nil, huma.Error404NotFound("webhook not found")
		}
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to get webhook: %w", err))
	}

	return &GetPaddleWebhookDetailOutput{Body: event}, nil
}

// ReplayPaddleWebhook resets a webhook's processing status and re-enqueues it for processing.
//
// POST /api/v1/admin/webhooks/paddle/{id}/replay
func (h *AdminHandler) ReplayPaddleWebhook(ctx context.Context, input *ReplayPaddleWebhookInput) (*ReplayPaddleWebhookOutput, error) {
	if h.paddleWebhookRepo == nil {
		return nil, huma.Error503ServiceUnavailable("paddle webhook repository not configured")
	}
	event, err := h.paddleWebhookRepo.GetByID(ctx, input.ID)
	if err != nil {
		if err == repository.ErrPaddleWebhookNotFound {
			return nil, huma.Error404NotFound("webhook not found")
		}
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to get webhook: %w", err))
	}

	if err := h.paddleWebhookRepo.ResetProcessingStatus(ctx, input.ID); err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to reset webhook status: %w", err))
	}

	resp := &domain.WebhookReplayResponse{
		Success:   true,
		Message:   "Webhook queued for replay",
		WebhookID: input.ID,
	}

	if h.jobQueue != nil {
		result, enqErr := h.jobQueue.Enqueue(ctx, jobs.SubscriptionWebhookArgs{
			WebhookID: event.ID,
			EventID:   event.EventID,
			EventType: event.EventType,
		}, queue.WithQueue("critical"))
		if enqErr != nil {
			resp.Message = "Webhook status reset, but failed to enqueue job"
		} else {
			resp.JobID = &result.ID
		}
	}

	return &ReplayPaddleWebhookOutput{Body: resp}, nil
}

// GetPaddleWebhookStats returns aggregate statistics for Paddle webhook events.
//
// GET /api/v1/admin/webhooks/paddle/stats
func (h *AdminHandler) GetPaddleWebhookStats(ctx context.Context, input *GetPaddleWebhookStatsInput) (*GetPaddleWebhookStatsOutput, error) {
	if h.paddleWebhookRepo == nil {
		return nil, huma.Error503ServiceUnavailable("paddle webhook repository not configured")
	}
	hours := timeRangeToHours(input.TimeRange)
	stats, err := h.paddleWebhookRepo.GetWebhookStats(ctx, hours)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to get webhook stats: %w", err))
	}

	return &GetPaddleWebhookStatsOutput{Body: stats}, nil
}

// timeRangeToHours converts a time_range query parameter string to an integer number of hours.
// Returns 0 for "all" (meaning no time restriction).
func timeRangeToHours(timeRange string) int {
	switch timeRange {
	case "7d":
		return 168
	case "30d":
		return 720
	case "all":
		return 0
	default: // "24h"
		return 24
	}
}

// RegisterAdminPaddleWebhooksOperations registers the Paddle webhook admin endpoints.
// The stats route MUST be registered before /{id} to avoid route collision.
func RegisterAdminPaddleWebhooksOperations(api huma.API, handler *AdminHandler) {
	middleware := huma.Middlewares{
		humamiddleware.NewAdminMiddleware(handler.config, handler.adminService),
	}

	// Stats MUST be registered before /{id} to avoid route collision
	huma.Register(api, huma.Operation{
		OperationID: "getAdminPaddleWebhookStats",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/webhooks/paddle/stats",
		Summary:     "Get Paddle webhook statistics (admin)",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
		Middlewares: middleware,
	}, handler.GetPaddleWebhookStats)

	huma.Register(api, huma.Operation{
		OperationID: "listAdminPaddleWebhooks",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/webhooks/paddle",
		Summary:     "List Paddle webhook events (admin)",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
		Middlewares: middleware,
	}, handler.ListPaddleWebhooks)

	huma.Register(api, huma.Operation{
		OperationID: "replayAdminPaddleWebhook",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/webhooks/paddle/{id}/replay",
		Summary:     "Replay a Paddle webhook (admin)",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
		Middlewares: middleware,
	}, handler.ReplayPaddleWebhook)

	huma.Register(api, huma.Operation{
		OperationID: "getAdminPaddleWebhookDetail",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/webhooks/paddle/{id}",
		Summary:     "Get Paddle webhook detail (admin)",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
		Middlewares: middleware,
	}, handler.GetPaddleWebhookDetail)
}
