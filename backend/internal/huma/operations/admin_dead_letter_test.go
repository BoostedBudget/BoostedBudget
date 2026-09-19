package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// assertHTTPStatus extracts the HTTP status code from a Huma StatusError.
func assertHTTPStatus(t *testing.T, err error, expectedStatus int) {
	t.Helper()
	var se huma.StatusError
	if errors.As(err, &se) {
		assert.Equal(t, expectedStatus, se.GetStatus())
	} else {
		t.Fatalf("expected huma.StatusError but got: %v (%T)", err, err)
	}
}

// MockDeadLetterService mocks services.DeadLetterService for handler tests.
type MockDeadLetterService struct {
	mock.Mock
}

func (m *MockDeadLetterService) List(ctx context.Context, filters domain.DeadLetterJobFilters, page, pageSize int) (*domain.DeadLetterJobListResponse, error) {
	args := m.Called(ctx, filters, page, pageSize)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.DeadLetterJobListResponse), args.Error(1)
}

func (m *MockDeadLetterService) GetByID(ctx context.Context, id int64) (*domain.DeadLetterJob, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.DeadLetterJob), args.Error(1)
}

func (m *MockDeadLetterService) GetRawArgs(ctx context.Context, id int64) (json.RawMessage, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(json.RawMessage), args.Error(1)
}

func (m *MockDeadLetterService) Retry(ctx context.Context, id int64) (*domain.DeadLetterRetryResponse, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.DeadLetterRetryResponse), args.Error(1)
}

func (m *MockDeadLetterService) AddNotes(ctx context.Context, id int64, notes string) error {
	return m.Called(ctx, id, notes).Error(0)
}

func (m *MockDeadLetterService) Delete(ctx context.Context, id int64) error {
	return m.Called(ctx, id).Error(0)
}

func (m *MockDeadLetterService) DeleteByFilters(ctx context.Context, filters domain.DeadLetterJobFilters, expected int64) (int64, error) {
	args := m.Called(ctx, filters, expected)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockDeadLetterService) CountByFilters(ctx context.Context, filters domain.DeadLetterJobFilters) (int64, error) {
	args := m.Called(ctx, filters)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockDeadLetterService) GetStats(ctx context.Context) (*domain.DeadLetterStatsResponse, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.DeadLetterStatsResponse), args.Error(1)
}

func (m *MockDeadLetterService) Cleanup(ctx context.Context, retentionDays int) (int64, error) {
	args := m.Called(ctx, retentionDays)
	return args.Get(0).(int64), args.Error(1)
}

var _ services.DeadLetterService = (*MockDeadLetterService)(nil)

// newDLQHandler creates an AdminHandler with mock DLQ service for direct handler tests.
func newDLQHandler(svc services.DeadLetterService) *AdminHandler {
	cfg := &config.Config{Environment: "qa"}
	return NewAdminHandler(nil, nil, nil, nil, nil, nil, cfg).
		WithDeadLetterService(svc)
}

func makeDLQTestJob(id int64) *domain.DeadLetterJob {
	return &domain.DeadLetterJob{
		ID:          id,
		Kind:        "plaid_sync",
		Queue:       "default",
		Args:        json.RawMessage(`{"user_id":"u1"}`),
		Errors:      json.RawMessage(`[]`),
		MaxAttempts: 5,
		CreatedAt:   time.Now(),
		FailedAt:    time.Now(),
	}
}

// --- ListDeadLetterJobs ---

func TestAdminDLQ_List_ReturnsJobs(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	jobs := []*domain.DeadLetterJob{makeDLQTestJob(1)}
	resp := &domain.DeadLetterJobListResponse{Jobs: jobs, Total: 1, Page: 1, PageSize: 20, TotalPages: 1}
	mockSvc.On("List", mock.Anything, domain.DeadLetterJobFilters{}, 1, 20).Return(resp, nil)

	input := &ListDeadLetterJobsInput{Page: 1, PageSize: 20}
	out, err := h.ListDeadLetterJobs(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, 1, out.Body.Total)
	assert.Len(t, out.Body.Jobs, 1)
	mockSvc.AssertExpectations(t)
}

func TestAdminDLQ_List_ServiceError(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	mockSvc.On("List", mock.Anything, domain.DeadLetterJobFilters{}, 1, 20).Return(nil, errors.New("db error"))

	input := &ListDeadLetterJobsInput{Page: 1, PageSize: 20}
	_, err := h.ListDeadLetterJobs(context.Background(), input)

	require.Error(t, err)
	mockSvc.AssertExpectations(t)
}

func TestAdminDLQ_List_WithKindFilter(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	kind := "plaid_sync"
	expectedFilters := domain.DeadLetterJobFilters{Kind: &kind}
	resp := &domain.DeadLetterJobListResponse{Jobs: []*domain.DeadLetterJob{}, Total: 0, Page: 1, PageSize: 20, TotalPages: 1}
	mockSvc.On("List", mock.Anything, expectedFilters, 1, 20).Return(resp, nil)

	input := &ListDeadLetterJobsInput{Page: 1, PageSize: 20, Kind: "plaid_sync"}
	out, err := h.ListDeadLetterJobs(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, 0, out.Body.Total)
	mockSvc.AssertExpectations(t)
}

// --- GetDLQStats ---

func TestAdminDLQ_GetStats_ReturnsStats(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	stats := &domain.DeadLetterStatsResponse{Total: 5, Unnotified: 3, Retried: 1, Last24Hours: 2, ByKind: map[string]int64{"plaid_sync": 3}}
	mockSvc.On("GetStats", mock.Anything).Return(stats, nil)

	input := &GetDLQStatsInput{}
	out, err := h.GetDLQStats(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, int64(5), out.Body.Total)
	assert.Equal(t, int64(3), out.Body.Unnotified)
	mockSvc.AssertExpectations(t)
}

func TestAdminDLQ_GetStats_Error(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	mockSvc.On("GetStats", mock.Anything).Return(nil, errors.New("db error"))

	input := &GetDLQStatsInput{}
	_, err := h.GetDLQStats(context.Background(), input)
	require.Error(t, err)
}

// --- GetDLQDetail ---

func TestAdminDLQ_GetDetail_ReturnsJob(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	job := makeDLQTestJob(1)
	mockSvc.On("GetByID", mock.Anything, int64(1)).Return(job, nil)

	input := &GetDLQDetailInput{ID: 1}
	out, err := h.GetDLQDetail(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, int64(1), out.Body.ID)
	mockSvc.AssertExpectations(t)
}

func TestAdminDLQ_GetDetail_NotFound(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	mockSvc.On("GetByID", mock.Anything, int64(99)).Return(nil, services.ErrDeadLetterJobNotFound)

	input := &GetDLQDetailInput{ID: 99}
	_, err := h.GetDLQDetail(context.Background(), input)

	require.Error(t, err)
	assertHTTPStatus(t, err, http.StatusNotFound)
	mockSvc.AssertExpectations(t)
}

// --- GetDLQArgs ---

func TestAdminDLQ_GetArgs_ReturnsArgs(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	rawArgs := json.RawMessage(`{"user_id":"u1","access_token":"tok_abc"}`)
	mockSvc.On("GetRawArgs", mock.Anything, int64(1)).Return(rawArgs, nil)

	input := &GetDLQArgsInput{ID: 1}
	out, err := h.GetDLQArgs(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, out.Body)
	assert.Equal(t, "u1", out.Body["user_id"])
	mockSvc.AssertExpectations(t)
}

func TestAdminDLQ_GetArgs_NotFound(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	mockSvc.On("GetRawArgs", mock.Anything, int64(99)).Return(nil, services.ErrDeadLetterJobNotFound)

	input := &GetDLQArgsInput{ID: 99}
	_, err := h.GetDLQArgs(context.Background(), input)

	require.Error(t, err)
	assertHTTPStatus(t, err, http.StatusNotFound)
}

// --- RetryDLQJob ---

func TestAdminDLQ_Retry_Success(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	newJobID := int64(42)
	retryResp := &domain.DeadLetterRetryResponse{Success: true, Message: "Job re-enqueued successfully", NewJobID: &newJobID, DeadLetterID: 1}
	mockSvc.On("Retry", mock.Anything, int64(1)).Return(retryResp, nil)

	input := &RetryDLQJobInput{ID: 1}
	out, err := h.RetryDLQJob(context.Background(), input)

	require.NoError(t, err)
	assert.True(t, out.Body.Success)
	assert.Equal(t, int64(42), *out.Body.NewJobID)
	mockSvc.AssertExpectations(t)
}

func TestAdminDLQ_Retry_AlreadyRetried(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	mockSvc.On("Retry", mock.Anything, int64(1)).Return(nil, services.ErrDeadLetterJobAlreadyRetried)

	input := &RetryDLQJobInput{ID: 1}
	_, err := h.RetryDLQJob(context.Background(), input)

	require.Error(t, err)
	assertHTTPStatus(t, err, http.StatusConflict)
	mockSvc.AssertExpectations(t)
}

func TestAdminDLQ_Retry_NotFound(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	mockSvc.On("Retry", mock.Anything, int64(99)).Return(nil, services.ErrDeadLetterJobNotFound)

	input := &RetryDLQJobInput{ID: 99}
	_, err := h.RetryDLQJob(context.Background(), input)

	require.Error(t, err)
	assertHTTPStatus(t, err, http.StatusNotFound)
}

// TestAdminDLQ_Retry_Deduplicated: a replay River skipped as a duplicate enqueued NOTHING, so it must
// not reach the client as a 200 — the admin UI fires its ADMIN_DLQ_RETRY analytics event on HTTP
// success alone and never inspects the response's `success` field, so a 200 here would be reported to
// the operator as a retry that is running.
func TestAdminDLQ_Retry_Deduplicated(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	mockSvc.On("Retry", mock.Anything, int64(1)).Return(nil, services.ErrDeadLetterRetryDeduplicated)

	_, err := h.RetryDLQJob(context.Background(), &RetryDLQJobInput{ID: 1})

	require.Error(t, err)
	assertHTTPStatus(t, err, http.StatusConflict)
	mockSvc.AssertExpectations(t)
}

// TestAdminDLQ_Retry_KindUnknown: the fail-closed refusal is a server-side gap (a kind missing from
// jobs.uniqueOptsByKind), not anything the operator can fix by changing the request. 422 rather than
// 400 (the request is well-formed) or 409 (nothing conflicts — the policy is simply absent).
func TestAdminDLQ_Retry_KindUnknown(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	mockSvc.On("Retry", mock.Anything, int64(1)).
		Return(nil, fmt.Errorf("%w: some_retired_kind", services.ErrDeadLetterRetryKindUnknown))

	_, err := h.RetryDLQJob(context.Background(), &RetryDLQJobInput{ID: 1})

	require.Error(t, err)
	assertHTTPStatus(t, err, http.StatusUnprocessableEntity)
	mockSvc.AssertExpectations(t)
}

// --- AddDLQNotes ---

func TestAdminDLQ_AddNotes_Success(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	mockSvc.On("AddNotes", mock.Anything, int64(1), "investigating timeout").Return(nil)

	input := &AddDLQNotesInput{ID: 1, Body: AddDLQNotesRequest{Notes: "investigating timeout"}}
	_, err := h.AddDLQNotes(context.Background(), input)

	require.NoError(t, err)
	mockSvc.AssertExpectations(t)
}

// --- DeleteDLQJob ---

func TestAdminDLQ_Delete_Success(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	mockSvc.On("Delete", mock.Anything, int64(1)).Return(nil)

	input := &DeleteDLQJobInput{ID: 1}
	_, err := h.DeleteDLQJob(context.Background(), input)

	require.NoError(t, err)
	mockSvc.AssertExpectations(t)
}

// --- BulkDeleteDLQJobs ---

func TestAdminDLQ_BulkDelete_Success(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	kind := "plaid_sync"
	mockSvc.On("CountByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters")).Return(int64(3), nil)
	mockSvc.On("DeleteByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters"), mock.AnythingOfType("int64")).Return(int64(3), nil)

	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{Kind: &kind}}
	out, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, int64(3), out.Body.Deleted)
	mockSvc.AssertExpectations(t)
}

func TestAdminDLQ_BulkDelete_EmptyFilters_Returns400(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	// Empty request body — should be rejected before calling the service
	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{}}
	_, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.Error(t, err)
	assertHTTPStatus(t, err, http.StatusBadRequest)
	// Arity must match MockDeadLetterService.DeleteByFilters' m.Called(ctx, filters, expected): the zero-argument
	// form this replaced could never fail, because testify silently passes an AssertNotCalled whose
	// argument count does not match the recorded call. Pre-existing (not part of #1492), fixed here
	// because this file is already open and the same trap was measured in internal/services this round.
	mockSvc.AssertNotCalled(t, "DeleteByFilters", mock.Anything, mock.Anything, mock.Anything)
}

func TestAdminDLQ_BulkDelete_ServiceError(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	kind := "plaid_sync"
	mockSvc.On("CountByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters")).Return(int64(3), nil)
	mockSvc.On("DeleteByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters"), mock.AnythingOfType("int64")).Return(int64(0), errors.New("db error"))

	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{Kind: &kind}}
	_, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.Error(t, err)
	mockSvc.AssertExpectations(t)
}

// --- BulkDeleteDLQJobs: input validation (B1) ---
//
// B1: the pre-EA guard tested only the NIL-ness of the five filter pointers, and
// buildDLQBulkFilters discarded every date parse error. Both of these bodies therefore
// reached the repository as a filter set with zero conditions, and
// DeadLetterJobRepository.DeleteByFilters interpolates an empty WHERE clause into
// `DELETE FROM dead_letter_jobs` — a full-table wipe reported to the caller as success.
// Restoration is Postgres PITR only, and it destroys precisely the operator-triaged rows
// that DLQ retention keeps forever (DeleteOlderThan's skipWithNotes leg).

func TestAdminDLQ_BulkDelete_EmptyStringDate_Returns400(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	empty := ""
	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{StartDate: &empty}}
	_, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.Error(t, err)
	assertHTTPStatus(t, err, http.StatusBadRequest)
	mockSvc.AssertNotCalled(t, "DeleteByFilters", mock.Anything, mock.Anything, mock.Anything)
}

// A malformed date must be reported AS a malformed date. The assertion on the message is
// load-bearing: with the parse error discarded the filter simply vanishes, and a request
// carrying only a bad date still ends in a 400 — from the empty-filter guard, for a
// different reason. Only the message distinguishes "rejected" from "silently widened".
func TestAdminDLQ_BulkDelete_MalformedDate_Returns400(t *testing.T) {
	for _, tc := range []struct {
		name  string
		start string
	}{
		{name: "wrong layout", start: "01/15/2026"},
		{name: "out of range", start: "2026-13-45"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := new(MockDeadLetterService)
			h := newDLQHandler(mockSvc)

			start := tc.start
			input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{StartDate: &start}}
			_, err := h.BulkDeleteDLQJobs(context.Background(), input)

			require.Error(t, err)
			assertHTTPStatus(t, err, http.StatusBadRequest)
			assert.Contains(t, err.Error(), "start_date")
			mockSvc.AssertNotCalled(t, "CountByFilters", mock.Anything, mock.Anything)
			mockSvc.AssertNotCalled(t, "DeleteByFilters", mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

func TestAdminDLQ_BulkDelete_MalformedEndDate_Returns400(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	end := "01/15/2026"
	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{EndDate: &end}}
	_, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.Error(t, err)
	assertHTTPStatus(t, err, http.StatusBadRequest)
	assert.Contains(t, err.Error(), "end_date")
	mockSvc.AssertNotCalled(t, "CountByFilters", mock.Anything, mock.Anything)
	mockSvc.AssertNotCalled(t, "DeleteByFilters", mock.Anything, mock.Anything, mock.Anything)
}

// The sharp case for the discarded parse error: paired with a valid filter, a swallowed
// bad date does not produce an empty filter set — it produces a WIDER one. The request
// asked for one month of one kind; dropping the dates asks for every row of that kind.
func TestAdminDLQ_BulkDelete_MalformedDateWithValidKind_Returns400(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	kind := "plaid_sync"
	start := "2026-13-45"
	end := "2026-01-31"
	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{Kind: &kind, StartDate: &start, EndDate: &end}}
	_, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.Error(t, err)
	assertHTTPStatus(t, err, http.StatusBadRequest)
	mockSvc.AssertNotCalled(t, "CountByFilters", mock.Anything, mock.Anything)
	mockSvc.AssertNotCalled(t, "DeleteByFilters", mock.Anything, mock.Anything, mock.Anything)
}

// --- BulkDeleteDLQJobs: blast radius (B2-2) ---
//
// B2-2: validation alone does NOT close B1. `{"start_date":"0001-01-01"}` is
// schema-valid and parses cleanly; it builds a real, non-empty WHERE clause
// (`failed_at >= year 1`) that matches every ordinary row, because failed_at defaults to
// NOW() (000030_add_river_queue.up.sql:177). `{"retried":false}` does the same whenever
// no row has been replayed. The bound has to be on the row count, not on the input.

func TestAdminDLQ_BulkDelete_BroadValidFilter_Returns409WithoutConfirmation(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	start := "0001-01-01"
	mockSvc.On("CountByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters")).Return(int64(5000), nil)

	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{StartDate: &start}}
	_, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.Error(t, err)
	assertHTTPStatus(t, err, http.StatusConflict)
	assert.Contains(t, err.Error(), "5000", "the refusal must disclose the count the caller has to confirm")
	mockSvc.AssertNotCalled(t, "DeleteByFilters", mock.Anything, mock.Anything, mock.Anything)
}

func TestAdminDLQ_BulkDelete_BroadRetriedFilter_Returns409WithoutConfirmation(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	retried := false
	mockSvc.On("CountByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters")).Return(int64(1200), nil)

	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{Retried: &retried}}
	_, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.Error(t, err)
	assertHTTPStatus(t, err, http.StatusConflict)
	mockSvc.AssertNotCalled(t, "DeleteByFilters", mock.Anything, mock.Anything, mock.Anything)
}

func TestAdminDLQ_BulkDelete_WrongExpectedCount_Returns409(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	start := "0001-01-01"
	wrong := int64(4999)
	mockSvc.On("CountByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters")).Return(int64(5000), nil)

	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{StartDate: &start, ExpectedCount: &wrong}}
	_, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.Error(t, err)
	assertHTTPStatus(t, err, http.StatusConflict)
	mockSvc.AssertNotCalled(t, "DeleteByFilters", mock.Anything, mock.Anything, mock.Anything)
}

func TestAdminDLQ_BulkDelete_ExactExpectedCount_Proceeds(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	start := "0001-01-01"
	confirmed := int64(5000)
	mockSvc.On("CountByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters")).Return(int64(5000), nil)
	mockSvc.On("DeleteByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters"), mock.AnythingOfType("int64")).Return(int64(5000), nil)

	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{StartDate: &start, ExpectedCount: &confirmed}}
	out, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, int64(5000), out.Body.Deleted)
	mockSvc.AssertExpectations(t)
}

// TestAdminDLQ_BulkDelete_WrongExpectedCountBelowCeiling_Returns409 closes the EE re-review's
// finding: the equality was only consulted ABOVE the ceiling, so a caller who said
// expected_count=1 and whose filters matched 100 rows had all 100 deleted — while
// api/openapi.yaml documents the field as the exact number the caller expects. A supplied
// confirmation is now honoured wherever it appears; it can only ever narrow the outcome,
// never widen it, because omitting it still leaves the ceiling in force.
func TestAdminDLQ_BulkDelete_WrongExpectedCountBelowCeiling_Returns409(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	retried := false
	wrong := int64(1)
	mockSvc.On("CountByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters")).Return(int64(100), nil)

	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{Retried: &retried, ExpectedCount: &wrong}}
	_, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.Error(t, err)
	assertHTTPStatus(t, err, http.StatusConflict)
	assert.Contains(t, err.Error(), "100", "the refusal must disclose the count the server actually computed")
	mockSvc.AssertNotCalled(t, "DeleteByFilters", mock.Anything, mock.Anything, mock.Anything)
}

// The sibling: a correct confirmation below the ceiling still proceeds, so the new equality
// cannot be passing above merely by refusing everything.
func TestAdminDLQ_BulkDelete_CorrectExpectedCountBelowCeiling_Proceeds(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	retried := false
	confirmed := int64(12)
	mockSvc.On("CountByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters")).Return(int64(12), nil)
	mockSvc.On("DeleteByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters"), int64(12)).Return(int64(12), nil)

	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{Retried: &retried, ExpectedCount: &confirmed}}
	out, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, int64(12), out.Body.Deleted)
	mockSvc.AssertExpectations(t)
}

// TestAdminDLQ_BulkDelete_WrongExpectedCountWithZeroMatches_Returns409 closes the second
// re-review Low: the zero-match success return sat BEFORE the confirmation check, so
// expected_count=1 against zero matching rows answered 200/deleted=0 — a caller who believed
// a row existed was told the delete succeeded. Nothing was destroyed, but the field's whole
// purpose is to make the server and the caller agree about what they are looking at, and here
// they disagreed silently.
func TestAdminDLQ_BulkDelete_WrongExpectedCountWithZeroMatches_Returns409(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	kind := "plaid_sync"
	wrong := int64(1)
	mockSvc.On("CountByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters")).Return(int64(0), nil)

	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{Kind: &kind, ExpectedCount: &wrong}}
	_, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.Error(t, err)
	assertHTTPStatus(t, err, http.StatusConflict)
	mockSvc.AssertNotCalled(t, "DeleteByFilters", mock.Anything, mock.Anything, mock.Anything)
}

// The sibling: zero matches with NO confirmation supplied is still an ordinary success, so the
// check above cannot be passing by turning every empty result into an error.
func TestAdminDLQ_BulkDelete_ZeroMatchesWithoutConfirmation_Succeeds(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	kind := "plaid_sync"
	mockSvc.On("CountByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters")).Return(int64(0), nil)

	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{Kind: &kind}}
	out, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, int64(0), out.Body.Deleted)
	mockSvc.AssertNotCalled(t, "DeleteByFilters", mock.Anything, mock.Anything, mock.Anything)
}

// TestAdminDLQ_BulkDelete_PassesServerComputedCountToTheDelete pins the other half of the
// EE re-review's finding: COUNT and DELETE ran as two independent statements against two
// PostgreSQL snapshots, so a row committed in between was deleted without ever having been
// counted or confirmed. The handler must hand the count it authorised down to the delete,
// which is where the atomic check lives.
func TestAdminDLQ_BulkDelete_PassesServerComputedCountToTheDelete(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	kind := "plaid_sync"
	mockSvc.On("CountByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters")).Return(int64(9), nil)
	mockSvc.On("DeleteByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters"), int64(9)).Return(int64(9), nil)

	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{Kind: &kind}}
	out, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, int64(9), out.Body.Deleted)
	mockSvc.AssertExpectations(t)
}

// A narrow delete under the unconfirmed ceiling must still work with no extra ceremony —
// the bound exists to stop table-scale deletions, not routine triage cleanup.
func TestAdminDLQ_BulkDelete_NarrowFilter_NeedsNoConfirmation(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	kind := "plaid_sync"
	start := "2026-01-01"
	end := "2026-01-31"
	mockSvc.On("CountByFilters", mock.Anything, mock.MatchedBy(func(f domain.DeadLetterJobFilters) bool {
		return f.Kind != nil && *f.Kind == kind && f.StartDate != nil && f.EndDate != nil
	})).Return(int64(7), nil)
	mockSvc.On("DeleteByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters"), mock.AnythingOfType("int64")).Return(int64(7), nil)

	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{Kind: &kind, StartDate: &start, EndDate: &end}}
	out, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, int64(7), out.Body.Deleted)
	mockSvc.AssertExpectations(t)
}

// Nothing matches: report zero without asking for a confirmation nobody can give.
func TestAdminDLQ_BulkDelete_ZeroMatches_ReturnsZero(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	kind := "plaid_sync"
	mockSvc.On("CountByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters")).Return(int64(0), nil)

	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{Kind: &kind}}
	out, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, int64(0), out.Body.Deleted)
	mockSvc.AssertNotCalled(t, "DeleteByFilters", mock.Anything, mock.Anything, mock.Anything)
}

// An expected_count on its own is not a filter: it must not satisfy the "at least one
// filter" guard, or it becomes the parameter that raises the bound.
func TestAdminDLQ_BulkDelete_ExpectedCountOnly_Returns400(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	confirmed := int64(5000)
	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{ExpectedCount: &confirmed}}
	_, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.Error(t, err)
	assertHTTPStatus(t, err, http.StatusBadRequest)
	mockSvc.AssertNotCalled(t, "CountByFilters", mock.Anything, mock.Anything)
	mockSvc.AssertNotCalled(t, "DeleteByFilters", mock.Anything, mock.Anything, mock.Anything)
}

func TestAdminDLQ_BulkDelete_CountError_Propagates(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	kind := "plaid_sync"
	mockSvc.On("CountByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters")).Return(int64(0), errors.New("db error"))

	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{Kind: &kind}}
	_, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.Error(t, err)
	mockSvc.AssertNotCalled(t, "DeleteByFilters", mock.Anything, mock.Anything, mock.Anything)
}
