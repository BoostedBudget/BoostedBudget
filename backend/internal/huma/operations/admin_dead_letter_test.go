package operations

import (
	"context"
	"encoding/json"
	"errors"
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

func (m *MockDeadLetterService) DeleteByFilters(ctx context.Context, filters domain.DeadLetterJobFilters) (int64, error) {
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
	return NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg).
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
	mockSvc.On("DeleteByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters")).Return(int64(3), nil)

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
	mockSvc.AssertNotCalled(t, "DeleteByFilters")
}

func TestAdminDLQ_BulkDelete_ServiceError(t *testing.T) {
	mockSvc := new(MockDeadLetterService)
	h := newDLQHandler(mockSvc)

	kind := "plaid_sync"
	mockSvc.On("DeleteByFilters", mock.Anything, mock.AnythingOfType("domain.DeadLetterJobFilters")).Return(int64(0), errors.New("db error"))

	input := &BulkDeleteDLQJobsInput{Body: domain.DeadLetterBulkDeleteRequest{Kind: &kind}}
	_, err := h.BulkDeleteDLQJobs(context.Background(), input)

	require.Error(t, err)
	mockSvc.AssertExpectations(t)
}
