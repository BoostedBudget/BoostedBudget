package operations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockPaddleWebhookRepository mocks PaddleWebhookRepository for admin endpoint tests.
type MockPaddleWebhookRepository struct {
	mock.Mock
}

func (m *MockPaddleWebhookRepository) Create(ctx context.Context, event *domain.PaddleWebhookEvent) error {
	return m.Called(ctx, event).Error(0)
}

func (m *MockPaddleWebhookRepository) GetByID(ctx context.Context, id string) (*domain.PaddleWebhookEvent, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.PaddleWebhookEvent), args.Error(1)
}

func (m *MockPaddleWebhookRepository) GetByEventID(ctx context.Context, eventID string) (*domain.PaddleWebhookEvent, error) {
	args := m.Called(ctx, eventID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.PaddleWebhookEvent), args.Error(1)
}

func (m *MockPaddleWebhookRepository) MarkProcessed(ctx context.Context, id string) error {
	return m.Called(ctx, id).Error(0)
}

func (m *MockPaddleWebhookRepository) MarkFailed(ctx context.Context, id string, errorMsg string) error {
	return m.Called(ctx, id, errorMsg).Error(0)
}

func (m *MockPaddleWebhookRepository) GetUnprocessed(ctx context.Context, limit int) ([]domain.PaddleWebhookEvent, error) {
	args := m.Called(ctx, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.PaddleWebhookEvent), args.Error(1)
}

func (m *MockPaddleWebhookRepository) GetUnprocessedByType(ctx context.Context, eventType string, limit int) ([]domain.PaddleWebhookEvent, error) {
	args := m.Called(ctx, eventType, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.PaddleWebhookEvent), args.Error(1)
}

func (m *MockPaddleWebhookRepository) Update(ctx context.Context, event *domain.PaddleWebhookEvent) error {
	return m.Called(ctx, event).Error(0)
}

func (m *MockPaddleWebhookRepository) GetRecentWebhooks(ctx context.Context, limit int) ([]domain.PaddleWebhookEvent, error) {
	args := m.Called(ctx, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.PaddleWebhookEvent), args.Error(1)
}

func (m *MockPaddleWebhookRepository) ListWebhookEvents(ctx context.Context, filters domain.WebhookEventFilters, limit, offset int) ([]domain.PaddleWebhookEvent, int, error) {
	args := m.Called(ctx, filters, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]domain.PaddleWebhookEvent), args.Int(1), args.Error(2)
}

func (m *MockPaddleWebhookRepository) GetWebhookStats(ctx context.Context, timeRangeHours int) (*domain.WebhookStatsResponse, error) {
	args := m.Called(ctx, timeRangeHours)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.WebhookStatsResponse), args.Error(1)
}

func (m *MockPaddleWebhookRepository) ResetProcessingStatus(ctx context.Context, id string) error {
	return m.Called(ctx, id).Error(0)
}

// MockAdminJobQueue mocks queue.JobQueue for admin webhook tests.
type MockAdminJobQueue struct {
	mock.Mock
}

func (m *MockAdminJobQueue) Enqueue(ctx context.Context, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	args := m.Called(ctx, job)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*queue.JobResult), args.Error(1)
}

func (m *MockAdminJobQueue) EnqueueTx(ctx context.Context, tx pgx.Tx, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	args := m.Called(ctx, tx, job)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*queue.JobResult), args.Error(1)
}

func (m *MockAdminJobQueue) EnqueueMany(ctx context.Context, jobs []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	args := m.Called(ctx, jobs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*queue.JobResult), args.Error(1)
}

func (m *MockAdminJobQueue) EnqueueManyTx(ctx context.Context, tx pgx.Tx, jobs []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	args := m.Called(ctx, tx, jobs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*queue.JobResult), args.Error(1)
}

// --- Test helpers ---

func newTestWebhookEvent(id string) *domain.PaddleWebhookEvent {
	return &domain.PaddleWebhookEvent{
		ID:                 id,
		EventID:            "evt_" + id,
		EventType:          "subscription.created",
		Payload:            json.RawMessage(`{}`),
		SignatureValid:     true,
		Processed:          false,
		ProcessingAttempts: 0,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
}

func setupAdminPaddleWebhooksAPI(t *testing.T) (humatest.TestAPI, *MockPaddleWebhookRepository, *MockAdminJobQueue, *MockAdminService) {
	t.Helper()
	_, api := humatest.New(t)
	mockWebhookRepo := new(MockPaddleWebhookRepository)
	mockJobQueue := new(MockAdminJobQueue)
	mockAdminSvc := new(MockAdminService)

	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, mockAdminSvc, nil, nil, nil, cfg).
		WithPaddleWebhookRepo(mockWebhookRepo).
		WithJobQueue(mockJobQueue)
	RegisterAdminPaddleWebhooksOperations(api, handler)
	return api, mockWebhookRepo, mockJobQueue, mockAdminSvc
}

func addAdminContext(req *http.Request, clerkUserID string) *http.Request {
	ctx := context.WithValue(req.Context(), inputs.ClerkUserIDKey, clerkUserID)
	claims := &clerk.SessionClaims{Custom: map[string]interface{}{"email": "admin@example.com"}}
	ctx = context.WithValue(ctx, inputs.ClerkClaimsKey, claims)
	return req.WithContext(ctx)
}

// --- Tests ---

func TestListPaddleWebhooks_OperationRegistered(t *testing.T) {
	api, _, _, mockAdminSvc := setupAdminPaddleWebhooksAPI(t)

	clerkUserID := "user_admin123"
	mockAdminSvc.On("IsHardcodedAdmin", mock.Anything).Return(false)
	mockAdminSvc.On("HasRole", mock.Anything, clerkUserID, domain.RoleAdmin).Return(true, nil)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/admin/webhooks/paddle", nil)
	req = addAdminContext(req, clerkUserID)
	res := httptest.NewRecorder()
	api.Adapter().ServeHTTP(res, req)

	assert.NotEqual(t, http.StatusNotFound, res.Code, "operation should be registered")
	assert.NotEqual(t, http.StatusMethodNotAllowed, res.Code, "GET method should be allowed")
}

func TestListPaddleWebhooks_ReturnsEvents(t *testing.T) {
	mockWebhookRepo := new(MockPaddleWebhookRepository)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg).
		WithPaddleWebhookRepo(mockWebhookRepo)

	evt := *newTestWebhookEvent("webhook-uuid-1")
	mockWebhookRepo.On("ListWebhookEvents", mock.Anything, domain.WebhookEventFilters{}, 20, 0).
		Return([]domain.PaddleWebhookEvent{evt}, 1, nil)

	input := &ListPaddleWebhooksInput{Limit: 20, Offset: 0}
	out, err := handler.ListPaddleWebhooks(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, 1, out.Body.Total)
	assert.Equal(t, 20, out.Body.PageSize)
	assert.Equal(t, 1, out.Body.Page)
	assert.Equal(t, 1, out.Body.TotalPages)
	assert.False(t, out.Body.HasMore)
	require.Len(t, out.Body.Events, 1)
	assert.Equal(t, "subscription.created", out.Body.Events[0].EventType)

	mockWebhookRepo.AssertExpectations(t)
}

func TestListPaddleWebhooks_WithFilters(t *testing.T) {
	mockWebhookRepo := new(MockPaddleWebhookRepository)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg).
		WithPaddleWebhookRepo(mockWebhookRepo)

	eventType := "subscription.updated"
	processed := false
	filters := domain.WebhookEventFilters{EventType: &eventType, Processed: &processed}
	mockWebhookRepo.On("ListWebhookEvents", mock.Anything, filters, 10, 0).
		Return([]domain.PaddleWebhookEvent{}, 0, nil)

	input := &ListPaddleWebhooksInput{Limit: 10, Offset: 0, EventType: eventType, Processed: "false"}
	out, err := handler.ListPaddleWebhooks(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, 0, out.Body.Total)
	assert.Empty(t, out.Body.Events)

	mockWebhookRepo.AssertExpectations(t)
}

func TestListPaddleWebhooks_EmptyResult(t *testing.T) {
	mockWebhookRepo := new(MockPaddleWebhookRepository)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg).
		WithPaddleWebhookRepo(mockWebhookRepo)

	mockWebhookRepo.On("ListWebhookEvents", mock.Anything, domain.WebhookEventFilters{}, 20, 0).
		Return(nil, 0, nil)

	input := &ListPaddleWebhooksInput{Limit: 20, Offset: 0}
	out, err := handler.ListPaddleWebhooks(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, out)
	assert.NotNil(t, out.Body.Events, "events slice should not be nil")
	assert.Empty(t, out.Body.Events)

	mockWebhookRepo.AssertExpectations(t)
}

func TestListPaddleWebhooks_NilRepo_Returns503(t *testing.T) {
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg)

	input := &ListPaddleWebhooksInput{Limit: 20, Offset: 0}
	out, err := handler.ListPaddleWebhooks(context.Background(), input)

	assert.Error(t, err)
	assert.Nil(t, out)
}

func TestGetPaddleWebhookDetail_Success(t *testing.T) {
	mockWebhookRepo := new(MockPaddleWebhookRepository)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg).
		WithPaddleWebhookRepo(mockWebhookRepo)

	evt := newTestWebhookEvent("wh-uuid-1")
	mockWebhookRepo.On("GetByID", mock.Anything, "wh-uuid-1").Return(evt, nil)

	input := &GetPaddleWebhookDetailInput{ID: "wh-uuid-1"}
	out, err := handler.GetPaddleWebhookDetail(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "wh-uuid-1", out.Body.ID)
	assert.Equal(t, "subscription.created", out.Body.EventType)

	mockWebhookRepo.AssertExpectations(t)
}

func TestGetPaddleWebhookDetail_NotFound(t *testing.T) {
	mockWebhookRepo := new(MockPaddleWebhookRepository)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg).
		WithPaddleWebhookRepo(mockWebhookRepo)

	mockWebhookRepo.On("GetByID", mock.Anything, "nonexistent").
		Return(nil, errors.New("paddle webhook not found"))

	input := &GetPaddleWebhookDetailInput{ID: "nonexistent"}
	out, err := handler.GetPaddleWebhookDetail(context.Background(), input)

	assert.Error(t, err)
	assert.Nil(t, out)

	mockWebhookRepo.AssertExpectations(t)
}

func TestGetPaddleWebhookDetail_NilRepo_Returns503(t *testing.T) {
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg)

	input := &GetPaddleWebhookDetailInput{ID: "any-id"}
	out, err := handler.GetPaddleWebhookDetail(context.Background(), input)

	assert.Error(t, err)
	assert.Nil(t, out)
}

func TestReplayPaddleWebhook_Success(t *testing.T) {
	mockWebhookRepo := new(MockPaddleWebhookRepository)
	mockJobQueue := new(MockAdminJobQueue)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg).
		WithPaddleWebhookRepo(mockWebhookRepo).
		WithJobQueue(mockJobQueue)

	evt := newTestWebhookEvent("wh-replay-1")
	mockWebhookRepo.On("GetByID", mock.Anything, "wh-replay-1").Return(evt, nil)
	mockWebhookRepo.On("ResetProcessingStatus", mock.Anything, "wh-replay-1").Return(nil)

	jobID := int64(42)
	mockJobQueue.On("Enqueue", mock.Anything, mock.Anything).
		Return(&queue.JobResult{ID: jobID}, nil)

	input := &ReplayPaddleWebhookInput{ID: "wh-replay-1"}
	out, err := handler.ReplayPaddleWebhook(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, out)
	assert.True(t, out.Body.Success)
	assert.Equal(t, "wh-replay-1", out.Body.WebhookID)
	assert.NotNil(t, out.Body.JobID)
	assert.Equal(t, jobID, *out.Body.JobID)

	mockWebhookRepo.AssertExpectations(t)
	mockJobQueue.AssertExpectations(t)
}

func TestReplayPaddleWebhook_NotFound(t *testing.T) {
	mockWebhookRepo := new(MockPaddleWebhookRepository)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg).
		WithPaddleWebhookRepo(mockWebhookRepo)

	mockWebhookRepo.On("GetByID", mock.Anything, "nonexistent").
		Return(nil, errors.New("paddle webhook not found"))

	input := &ReplayPaddleWebhookInput{ID: "nonexistent"}
	out, err := handler.ReplayPaddleWebhook(context.Background(), input)

	assert.Error(t, err)
	assert.Nil(t, out)

	mockWebhookRepo.AssertExpectations(t)
}

func TestReplayPaddleWebhook_NilRepo_Returns503(t *testing.T) {
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg)

	input := &ReplayPaddleWebhookInput{ID: "any-id"}
	out, err := handler.ReplayPaddleWebhook(context.Background(), input)

	assert.Error(t, err)
	assert.Nil(t, out)
}

func TestReplayPaddleWebhook_NilQueue_StillSucceeds(t *testing.T) {
	mockWebhookRepo := new(MockPaddleWebhookRepository)
	cfg := &config.Config{Environment: "qa"}
	// No job queue configured
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg).
		WithPaddleWebhookRepo(mockWebhookRepo)

	evt := newTestWebhookEvent("wh-noqueue-1")
	mockWebhookRepo.On("GetByID", mock.Anything, "wh-noqueue-1").Return(evt, nil)
	mockWebhookRepo.On("ResetProcessingStatus", mock.Anything, "wh-noqueue-1").Return(nil)

	input := &ReplayPaddleWebhookInput{ID: "wh-noqueue-1"}
	out, err := handler.ReplayPaddleWebhook(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, out)
	assert.True(t, out.Body.Success)
	assert.Nil(t, out.Body.JobID, "no job ID when queue is nil")

	mockWebhookRepo.AssertExpectations(t)
}

func TestGetPaddleWebhookStats_Success(t *testing.T) {
	mockWebhookRepo := new(MockPaddleWebhookRepository)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg).
		WithPaddleWebhookRepo(mockWebhookRepo)

	stats := &domain.WebhookStatsResponse{
		Total:               100,
		Processed:           90,
		Failed:              5,
		Unprocessed:         10,
		AvgProcessingTimeMs: 250,
		TimeRangeHours:      24,
	}
	mockWebhookRepo.On("GetWebhookStats", mock.Anything, 24).Return(stats, nil)

	input := &GetPaddleWebhookStatsInput{TimeRange: "24h"}
	out, err := handler.GetPaddleWebhookStats(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, int64(100), out.Body.Total)
	assert.Equal(t, int64(90), out.Body.Processed)
	assert.Equal(t, int64(5), out.Body.Failed)

	mockWebhookRepo.AssertExpectations(t)
}

func TestGetPaddleWebhookStats_TimeRangeConversion(t *testing.T) {
	tests := []struct {
		timeRange string
		wantHours int
	}{
		{"24h", 24},
		{"7d", 168},
		{"30d", 720},
		{"all", 0},
		{"unknown", 24}, // defaults to 24h
	}

	for _, tt := range tests {
		t.Run(tt.timeRange, func(t *testing.T) {
			hours := timeRangeToHours(tt.timeRange)
			assert.Equal(t, tt.wantHours, hours)
		})
	}
}

func TestGetPaddleWebhookStats_NilRepo_Returns503(t *testing.T) {
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg)

	input := &GetPaddleWebhookStatsInput{TimeRange: "24h"}
	out, err := handler.GetPaddleWebhookStats(context.Background(), input)

	assert.Error(t, err)
	assert.Nil(t, out)
}
