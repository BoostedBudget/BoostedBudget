package operations

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDeletionService is a mock implementation of services.DeletionService
type mockDeletionService struct {
	cancellationToken string
	scheduledAt       time.Time
	requestErr        error
	cancelErr         error
}

func (m *mockDeletionService) RequestDeletion(ctx context.Context, userID, email string) (string, time.Time, error) {
	if m.requestErr != nil {
		return "", time.Time{}, m.requestErr
	}
	return m.cancellationToken, m.scheduledAt, nil
}

func (m *mockDeletionService) CancelDeletion(ctx context.Context, token string) error {
	return m.cancelErr
}

func (m *mockDeletionService) ExecuteDeletion(ctx context.Context, userID string, kind repository.DeletionClaimKind) error {
	return nil
}

// Issue #668: Plaid Item Management for Cost Optimization
func (m *mockDeletionService) SetPlaidCleanupService(svc services.PlaidItemCleanupService) {
	// No-op for mock
}

func (m *mockDeletionService) SetPaddleCleanupService(svc services.PaddleDeletionCleaner) {}

// SetHouseholdReader/SetHouseholdInviteRevoker (#1367 — Phase 3) are not exercised by
// these deletion-operation tests; stubbed to satisfy the expanded interface.
func (m *mockDeletionService) SetHouseholdReader(r services.HouseholdReader)               {}
func (m *mockDeletionService) SetHouseholdInviteRevoker(r services.HouseholdInviteRevoker) {}
func (m *mockDeletionService) SetDB(db repository.DB)                                      {}

// SetWorkClaimsRepo (Issue #1413 Critical finding C2) is not exercised by these
// deletion-operation tests; stubbed to satisfy the expanded interface.
func (m *mockDeletionService) SetWorkClaimsRepo(repo repository.WorkClaimsRepository)         {}
func (m *mockDeletionService) SetSubscriptionCleanupGuard(services.SubscriptionCleanupGuard)  {}
func (m *mockDeletionService) SetRevenueCatCleanupService(services.RevenueCatDeletionCleaner) {}

func (m *mockDeletionService) SetAnalytics(a phclient.Analytics) {
	// No-op for mock
}

// SetCache (Issue #1531 Task 10b) is not exercised by these deletion-operation tests; stubbed to
// satisfy the expanded interface.
func (m *mockDeletionService) SetCache(purger cache.Purger, gens *cache.Generations, jq queue.JobQueue) {
}

// TestRequestDeletion_Success tests successful deletion request.
func TestRequestDeletion_Success(t *testing.T) {
	scheduledAt := time.Now().Add(30 * 24 * time.Hour)
	mockService := &mockDeletionService{
		cancellationToken: "test-token-123",
		scheduledAt:       scheduledAt,
	}

	handler := NewDeletionHandler(mockService)

	input := &RequestDeletionInput{
		AuthenticatedWithEmailInput: inputs.AuthenticatedWithEmailInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			Email:              "test@example.com",
		},
	}

	output, err := handler.RequestDeletion(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Account deletion scheduled", output.Body.Message)
	assert.Equal(t, "test-token-123", output.Body.CancellationToken)
	assert.Contains(t, output.Body.ScheduledAt, scheduledAt.Format("2006-01-02"))
}

// TestRequestDeletion_ServiceError tests error handling for deletion request.
func TestRequestDeletion_ServiceError(t *testing.T) {
	mockService := &mockDeletionService{
		requestErr: errors.New("service error"),
	}

	handler := NewDeletionHandler(mockService)

	input := &RequestDeletionInput{
		AuthenticatedWithEmailInput: inputs.AuthenticatedWithEmailInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			Email:              "test@example.com",
		},
	}

	output, err := handler.RequestDeletion(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	// #1367: handler now routes through LoggingMapError so domain errors (e.g. the
	// household owner guard) map to their correct status; an unmapped generic error
	// falls through to the mapper's default 500 message.
	statusErr, ok := err.(huma.StatusError)
	require.True(t, ok, "expected huma.StatusError")
	assert.Equal(t, http.StatusInternalServerError, statusErr.GetStatus())
}

// TestRequestDeletion_HouseholdOwnerWithActiveMembers_Returns409 tests that the
// #1367 household owner-deletion guard surfaces as 409, not a generic 500 (the
// handler previously hard-coded Error500InternalServerError for every error).
func TestRequestDeletion_HouseholdOwnerWithActiveMembers_Returns409(t *testing.T) {
	mockService := &mockDeletionService{
		requestErr: domain.ErrHouseholdHasActiveMembers,
	}

	handler := NewDeletionHandler(mockService)

	input := &RequestDeletionInput{
		AuthenticatedWithEmailInput: inputs.AuthenticatedWithEmailInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "owner-1"},
			Email:              "owner@example.com",
		},
	}

	output, err := handler.RequestDeletion(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	statusErr, ok := err.(huma.StatusError)
	require.True(t, ok, "expected huma.StatusError")
	assert.Equal(t, http.StatusConflict, statusErr.GetStatus())
}

// TestCancelDeletion_Success tests successful deletion cancellation.
func TestCancelDeletion_Success(t *testing.T) {
	mockService := &mockDeletionService{}

	handler := NewDeletionHandler(mockService)

	input := &CancelDeletionInput{
		Token: "valid-token-123",
	}

	output, err := handler.CancelDeletion(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Account deletion cancelled", output.Body.Message)
}

// TestCancelDeletion_EmptyToken tests error when token is empty.
func TestCancelDeletion_EmptyToken(t *testing.T) {
	mockService := &mockDeletionService{}

	handler := NewDeletionHandler(mockService)

	input := &CancelDeletionInput{
		Token: "",
	}

	output, err := handler.CancelDeletion(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token required")
}

// TestCancelDeletion_InvalidToken tests error when token is invalid.
func TestCancelDeletion_InvalidToken(t *testing.T) {
	mockService := &mockDeletionService{
		cancelErr: errors.New("invalid token"),
	}

	handler := NewDeletionHandler(mockService)

	input := &CancelDeletionInput{
		Token: "invalid-token",
	}

	output, err := handler.CancelDeletion(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid or expired")
}

// TestDeletionRouteRegistration tests that routes are properly registered.
func TestDeletionRouteRegistration(t *testing.T) {
	mockService := &mockDeletionService{
		cancellationToken: "test-token",
		scheduledAt:       time.Now().Add(30 * 24 * time.Hour),
	}

	_, api := humatest.New(t)
	handler := NewDeletionHandler(mockService)
	RegisterDeletionRoutes(api, handler)

	// Test POST /api/v1/user/delete (without auth, should fail with 401)
	resp := api.Post("/api/v1/user/delete", "application/json", map[string]string{})
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Test GET /api/v1/user/delete/cancel/{token} (public endpoint, should succeed)
	resp = api.Get("/api/v1/user/delete/cancel/test-token")
	assert.Equal(t, http.StatusOK, resp.Code)
}
