package operations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/dateutil"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// --- Input/Output types ---

// ListDeadLetterJobsInput represents query parameters for listing DLQ entries.
type ListDeadLetterJobsInput struct {
	inputs.AdminAuthenticatedInput
	Page     int    `query:"page" default:"1" minimum:"1"`
	PageSize int    `query:"page_size" default:"20" minimum:"1" maximum:"100"`
	Kind     string `query:"kind" doc:"Filter by job kind"`
	UserID   string `query:"user_id" doc:"Filter by user ID extracted from args"`
	Retried  string `query:"retried" doc:"Filter by retried status: 'true', 'false', or empty for all"`
	Start    string `query:"start_date" doc:"ISO 8601 start date filter"`
	End      string `query:"end_date" doc:"ISO 8601 end date filter"`
	SortBy   string `query:"sort_by"  enum:"failed_at,kind,retried_at" default:"failed_at" doc:"Column to sort by"`
	SortDir  string `query:"sort_dir" enum:"asc,desc"                  default:"desc"      doc:"Sort direction"`
}

// ListDeadLetterJobsOutput is the response for listing DLQ entries.
type ListDeadLetterJobsOutput struct {
	Body *domain.DeadLetterJobListResponse
}

// GetDLQStatsInput has no extra query params beyond admin auth.
type GetDLQStatsInput struct {
	inputs.AdminAuthenticatedInput
}

// GetDLQStatsOutput is the response for DLQ statistics.
type GetDLQStatsOutput struct {
	Body *domain.DeadLetterStatsResponse
}

// GetDLQDetailInput identifies a single DLQ entry by ID.
type GetDLQDetailInput struct {
	inputs.AdminAuthenticatedInput
	ID int64 `path:"id"`
}

// GetDLQDetailOutput is the response for a single DLQ entry.
type GetDLQDetailOutput struct {
	Body *domain.DeadLetterJob
}

// GetDLQArgsInput identifies a DLQ entry for raw-args retrieval.
type GetDLQArgsInput struct {
	inputs.AdminAuthenticatedInput
	ID int64 `path:"id"`
}

// GetDLQArgsOutput wraps the raw args JSON.
type GetDLQArgsOutput struct {
	Body map[string]interface{}
}

// RetryDLQJobInput identifies a DLQ entry to retry.
type RetryDLQJobInput struct {
	inputs.AdminAuthenticatedInput
	ID int64 `path:"id"`
}

// RetryDLQJobOutput is the response for a retry operation.
type RetryDLQJobOutput struct {
	Body *domain.DeadLetterRetryResponse
}

// AddDLQNotesInput carries the notes body and entry ID.
type AddDLQNotesInput struct {
	inputs.AdminAuthenticatedInput
	ID   int64              `path:"id"`
	Body AddDLQNotesRequest `required:"true"`
}

// AddDLQNotesRequest is the request body for adding notes.
type AddDLQNotesRequest struct {
	Notes string `json:"notes" doc:"Admin notes for this dead letter job"`
}

// AddDLQNotesOutput is empty (204 No Content).
type AddDLQNotesOutput struct{}

// DeleteDLQJobInput identifies a single DLQ entry to delete.
type DeleteDLQJobInput struct {
	inputs.AdminAuthenticatedInput
	ID int64 `path:"id"`
}

// DeleteDLQJobOutput is empty (204 No Content).
type DeleteDLQJobOutput struct{}

// BulkDeleteDLQJobsInput carries filter criteria for bulk deletion.
type BulkDeleteDLQJobsInput struct {
	inputs.AdminAuthenticatedInput
	Body domain.DeadLetterBulkDeleteRequest
}

// BulkDeleteDLQJobsOutput is the response for a bulk delete operation.
type BulkDeleteDLQJobsOutput struct {
	Body *domain.DeadLetterBulkDeleteResponse
}

// --- Handler methods ---

// ListDeadLetterJobs returns a paginated list of dead letter jobs.
//
// GET /api/v1/admin/dead-letter-jobs
func (h *AdminHandler) ListDeadLetterJobs(ctx context.Context, input *ListDeadLetterJobsInput) (*ListDeadLetterJobsOutput, error) {
	if h.deadLetterService == nil {
		return nil, huma.Error503ServiceUnavailable("dead letter service not configured")
	}
	filters := h.buildDLQFilters(input)
	resp, err := h.deadLetterService.List(ctx, filters, input.Page, input.PageSize)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &ListDeadLetterJobsOutput{Body: resp}, nil
}

// buildDLQFilters constructs DeadLetterJobFilters from list request input.
func (h *AdminHandler) buildDLQFilters(input *ListDeadLetterJobsInput) domain.DeadLetterJobFilters {
	filters := domain.DeadLetterJobFilters{}
	if input.Kind != "" {
		k := input.Kind
		filters.Kind = &k
	}
	if input.UserID != "" {
		u := input.UserID
		filters.UserID = &u
	}
	if input.Retried != "" {
		if b, err := strconv.ParseBool(input.Retried); err == nil {
			filters.Retried = &b
		}
	}
	if t, _ := dateutil.ParseOptionalDate(input.Start, "start_date"); t != nil {
		filters.StartDate = t
	}
	if t, _ := dateutil.ParseOptionalDate(input.End, "end_date"); t != nil {
		filters.EndDate = t
	}
	filters.SortBy = input.SortBy
	filters.SortDir = input.SortDir
	return filters
}

// GetDLQStats returns aggregate statistics about dead letter jobs.
//
// GET /api/v1/admin/dead-letter-jobs/stats
func (h *AdminHandler) GetDLQStats(ctx context.Context, input *GetDLQStatsInput) (*GetDLQStatsOutput, error) {
	if h.deadLetterService == nil {
		return nil, huma.Error503ServiceUnavailable("dead letter service not configured")
	}
	stats, err := h.deadLetterService.GetStats(ctx)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &GetDLQStatsOutput{Body: stats}, nil
}

// GetDLQDetail returns a single dead letter job (args and errors redacted).
//
// GET /api/v1/admin/dead-letter-jobs/{id}
func (h *AdminHandler) GetDLQDetail(ctx context.Context, input *GetDLQDetailInput) (*GetDLQDetailOutput, error) {
	if h.deadLetterService == nil {
		return nil, huma.Error503ServiceUnavailable("dead letter service not configured")
	}
	job, err := h.deadLetterService.GetByID(ctx, input.ID)
	if err != nil {
		if errors.Is(err, services.ErrDeadLetterJobNotFound) {
			return nil, huma.Error404NotFound("dead letter job not found")
		}
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &GetDLQDetailOutput{Body: job}, nil
}

// GetDLQArgs returns the raw (unredacted) job args for admin inspection.
//
// GET /api/v1/admin/dead-letter-jobs/{id}/args
func (h *AdminHandler) GetDLQArgs(ctx context.Context, input *GetDLQArgsInput) (*GetDLQArgsOutput, error) {
	if h.deadLetterService == nil {
		return nil, huma.Error503ServiceUnavailable("dead letter service not configured")
	}
	raw, err := h.deadLetterService.GetRawArgs(ctx, input.ID)
	if err != nil {
		if errors.Is(err, services.ErrDeadLetterJobNotFound) {
			return nil, huma.Error404NotFound("dead letter job not found")
		}
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(raw, &parsed); jsonErr != nil {
		parsed = map[string]interface{}{"_raw": string(raw)}
	}
	return &GetDLQArgsOutput{Body: parsed}, nil
}

// RetryDLQJob re-enqueues a dead letter job.
//
// POST /api/v1/admin/dead-letter-jobs/{id}/retry
func (h *AdminHandler) RetryDLQJob(ctx context.Context, input *RetryDLQJobInput) (*RetryDLQJobOutput, error) {
	if h.deadLetterService == nil {
		return nil, huma.Error503ServiceUnavailable("dead letter service not configured")
	}
	resp, err := h.deadLetterService.Retry(ctx, input.ID)
	if err != nil {
		if errors.Is(err, services.ErrDeadLetterJobNotFound) {
			return nil, huma.Error404NotFound("dead letter job not found")
		}
		if errors.Is(err, services.ErrDeadLetterJobAlreadyRetried) {
			return nil, huma.Error409Conflict("dead letter job has already been retried")
		}
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &RetryDLQJobOutput{Body: resp}, nil
}

// AddDLQNotes sets admin investigation notes on a dead letter job.
//
// POST /api/v1/admin/dead-letter-jobs/{id}/notes
func (h *AdminHandler) AddDLQNotes(ctx context.Context, input *AddDLQNotesInput) (*AddDLQNotesOutput, error) {
	if h.deadLetterService == nil {
		return nil, huma.Error503ServiceUnavailable("dead letter service not configured")
	}
	if err := h.deadLetterService.AddNotes(ctx, input.ID, input.Body.Notes); err != nil {
		if errors.Is(err, services.ErrDeadLetterJobNotFound) {
			return nil, huma.Error404NotFound("dead letter job not found")
		}
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &AddDLQNotesOutput{}, nil
}

// DeleteDLQJob permanently removes a single dead letter job.
//
// DELETE /api/v1/admin/dead-letter-jobs/{id}
func (h *AdminHandler) DeleteDLQJob(ctx context.Context, input *DeleteDLQJobInput) (*DeleteDLQJobOutput, error) {
	if h.deadLetterService == nil {
		return nil, huma.Error503ServiceUnavailable("dead letter service not configured")
	}
	if err := h.deadLetterService.Delete(ctx, input.ID); err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &DeleteDLQJobOutput{}, nil
}

// BulkDeleteDLQJobs deletes all dead letter jobs matching the given filters.
//
// POST /api/v1/admin/dead-letter-jobs/bulk-delete
func (h *AdminHandler) BulkDeleteDLQJobs(ctx context.Context, input *BulkDeleteDLQJobsInput) (*BulkDeleteDLQJobsOutput, error) {
	if h.deadLetterService == nil {
		return nil, huma.Error503ServiceUnavailable("dead letter service not configured")
	}
	// Require at least one filter to prevent accidental full-table delete.
	req := input.Body
	if req.Kind == nil && req.UserID == nil && req.Retried == nil && req.StartDate == nil && req.EndDate == nil {
		return nil, huma.Error400BadRequest("at least one filter is required for bulk delete")
	}
	filters := buildDLQBulkFilters(req)
	count, err := h.deadLetterService.DeleteByFilters(ctx, filters)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &BulkDeleteDLQJobsOutput{Body: &domain.DeadLetterBulkDeleteResponse{Deleted: count}}, nil
}

// buildDLQBulkFilters converts a BulkDeleteRequest into DeadLetterJobFilters.
func buildDLQBulkFilters(req domain.DeadLetterBulkDeleteRequest) domain.DeadLetterJobFilters {
	filters := domain.DeadLetterJobFilters{}
	if req.Kind != nil {
		filters.Kind = req.Kind
	}
	if req.UserID != nil {
		filters.UserID = req.UserID
	}
	if req.Retried != nil {
		filters.Retried = req.Retried
	}
	if req.StartDate != nil {
		if t, _ := dateutil.ParseOptionalDate(*req.StartDate, "start_date"); t != nil {
			filters.StartDate = t
		}
	}
	if req.EndDate != nil {
		if t, _ := dateutil.ParseOptionalDate(*req.EndDate, "end_date"); t != nil {
			filters.EndDate = t
		}
	}
	return filters
}

// RegisterAdminDeadLetterOperations registers all DLQ admin endpoints.
// Static paths (/stats, /bulk-delete) MUST be registered before /{id} to avoid route collision.
func RegisterAdminDeadLetterOperations(api huma.API, handler *AdminHandler) {
	middleware := huma.Middlewares{
		humamiddleware.NewAdminMiddleware(handler.config, handler.adminService),
	}

	// /stats must come before /{id}
	huma.Register(api, huma.Operation{
		OperationID: "getAdminDeadLetterJobStats",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/dead-letter-jobs/stats",
		Summary:     "Get dead letter job statistics (admin)",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
		Middlewares: middleware,
	}, handler.GetDLQStats)

	// /bulk-delete must come before /{id}
	huma.Register(api, huma.Operation{
		OperationID: "bulkDeleteAdminDeadLetterJobs",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/dead-letter-jobs/bulk-delete",
		Summary:     "Bulk delete dead letter jobs (admin)",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
		Middlewares: middleware,
	}, handler.BulkDeleteDLQJobs)

	huma.Register(api, huma.Operation{
		OperationID: "listAdminDeadLetterJobs",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/dead-letter-jobs",
		Summary:     "List dead letter jobs (admin)",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
		Middlewares: middleware,
	}, handler.ListDeadLetterJobs)

	// /{id}/args and /{id}/retry and /{id}/notes before /{id}
	huma.Register(api, huma.Operation{
		OperationID: "getAdminDeadLetterJobArgs",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/dead-letter-jobs/{id}/args",
		Summary:     "Get raw (unredacted) job args (admin)",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
		Middlewares: middleware,
	}, handler.GetDLQArgs)

	huma.Register(api, huma.Operation{
		OperationID: "retryAdminDeadLetterJob",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/dead-letter-jobs/{id}/retry",
		Summary:     "Retry a dead letter job (admin)",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
		Middlewares: middleware,
	}, handler.RetryDLQJob)

	huma.Register(api, huma.Operation{
		OperationID: "addAdminDeadLetterJobNotes",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/dead-letter-jobs/{id}/notes",
		Summary:     "Add admin notes to a dead letter job (admin)",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
		Middlewares: middleware,
	}, handler.AddDLQNotes)

	huma.Register(api, huma.Operation{
		OperationID: "getAdminDeadLetterJobDetail",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/dead-letter-jobs/{id}",
		Summary:     "Get dead letter job detail (admin)",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
		Middlewares: middleware,
	}, handler.GetDLQDetail)

	huma.Register(api, huma.Operation{
		OperationID: "deleteAdminDeadLetterJob",
		Method:      http.MethodDelete,
		Path:        "/api/v1/admin/dead-letter-jobs/{id}",
		Summary:     "Delete a dead letter job (admin)",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
		Middlewares: middleware,
		DefaultStatus: http.StatusNoContent,
	}, handler.DeleteDLQJob)
}
