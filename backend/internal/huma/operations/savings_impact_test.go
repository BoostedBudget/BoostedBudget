// Package operations provides tests for savings impact endpoints.
// Issue #972: Savings Transfer Automation — C1/C2 endpoint tests.
package operations

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock SavingsImpactRepository
// =============================================================================

type mockSavingsImpactRepository struct {
	latestSnapshot *domain.SavingsImpactSnapshot
	latestErr      error
	userImpact     *domain.UserSavingsImpact
	userImpactErr  error
}

func (m *mockSavingsImpactRepository) WriteSnapshot(ctx context.Context, snapshot *domain.SavingsImpactSnapshot) error {
	return nil
}

func (m *mockSavingsImpactRepository) GetLatestSnapshot(ctx context.Context) (*domain.SavingsImpactSnapshot, error) {
	return m.latestSnapshot, m.latestErr
}

func (m *mockSavingsImpactRepository) GetUserImpact(ctx context.Context, userID string) (*domain.UserSavingsImpact, error) {
	return m.userImpact, m.userImpactErr
}

func (m *mockSavingsImpactRepository) GetAggregateSavingsStats(ctx context.Context) (float64, int, int, error) {
	return 0, 0, 0, nil
}

// =============================================================================
// C1: GetSavingsImpactAggregate Tests
// =============================================================================

func TestGetSavingsImpactAggregate_NilSnapshot(t *testing.T) {
	repo := &mockSavingsImpactRepository{latestSnapshot: nil}
	handler := NewSavingsImpactHandler(repo)

	output, err := handler.GetAggregate(context.Background(), &GetSavingsImpactAggregateInput{})

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.False(t, output.Body.Published)
	assert.Equal(t, 0.0, output.Body.TotalAmount)
	assert.Equal(t, 0, output.Body.UserCount)
	assert.Equal(t, 0, output.Body.CompletedCount)
	assert.Nil(t, output.Body.ComputedAt)
	assert.Equal(t, "public, max-age=300", output.CacheControl)
}

func TestGetSavingsImpactAggregate_BelowUserThreshold(t *testing.T) {
	repo := &mockSavingsImpactRepository{
		latestSnapshot: &domain.SavingsImpactSnapshot{
			ID:             "snap-1",
			ComputedAt:     time.Now(),
			TotalAmount:    5000.0,
			UserCount:      5, // below MinAggregateUsers (10)
			CompletedCount: 20,
		},
	}
	handler := NewSavingsImpactHandler(repo)

	output, err := handler.GetAggregate(context.Background(), &GetSavingsImpactAggregateInput{})

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.False(t, output.Body.Published)
	assert.Equal(t, 0.0, output.Body.TotalAmount)
	assert.Equal(t, 0, output.Body.UserCount)
	assert.Equal(t, 0, output.Body.CompletedCount)
	assert.Nil(t, output.Body.ComputedAt)
}

func TestGetSavingsImpactAggregate_BelowAmountThreshold(t *testing.T) {
	repo := &mockSavingsImpactRepository{
		latestSnapshot: &domain.SavingsImpactSnapshot{
			ID:             "snap-1",
			ComputedAt:     time.Now(),
			TotalAmount:    500.0, // below MinAggregateAmount (1000)
			UserCount:      15,
			CompletedCount: 20,
		},
	}
	handler := NewSavingsImpactHandler(repo)

	output, err := handler.GetAggregate(context.Background(), &GetSavingsImpactAggregateInput{})

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.False(t, output.Body.Published)
}

func TestGetSavingsImpactAggregate_AboveThreshold(t *testing.T) {
	computedAt := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	repo := &mockSavingsImpactRepository{
		latestSnapshot: &domain.SavingsImpactSnapshot{
			ID:             "snap-1",
			ComputedAt:     computedAt,
			TotalAmount:    12345.67, // will round to 12000
			UserCount:      20,
			CompletedCount: 50,
		},
	}
	handler := NewSavingsImpactHandler(repo)

	output, err := handler.GetAggregate(context.Background(), &GetSavingsImpactAggregateInput{})

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.True(t, output.Body.Published)
	assert.Equal(t, 12000.0, output.Body.TotalAmount) // rounded to nearest $1000
	assert.Equal(t, 20, output.Body.UserCount)
	assert.Equal(t, 50, output.Body.CompletedCount)
	require.NotNil(t, output.Body.ComputedAt)
	assert.Equal(t, "2026-06-05T12:00:00Z", *output.Body.ComputedAt)
	assert.Equal(t, "public, max-age=300", output.CacheControl)
}

func TestGetSavingsImpactAggregate_RoundingEdgeCases(t *testing.T) {
	t.Run("rounds up at midpoint", func(t *testing.T) {
		repo := &mockSavingsImpactRepository{
			latestSnapshot: &domain.SavingsImpactSnapshot{
				TotalAmount:    1500.0,
				UserCount:      15,
				CompletedCount: 10,
				ComputedAt:     time.Now(),
			},
		}
		handler := NewSavingsImpactHandler(repo)
		output, err := handler.GetAggregate(context.Background(), &GetSavingsImpactAggregateInput{})
		require.NoError(t, err)
		assert.True(t, output.Body.Published)
		assert.Equal(t, 2000.0, output.Body.TotalAmount) // 1500 rounds to 2000
	})

	t.Run("rounds down below midpoint", func(t *testing.T) {
		repo := &mockSavingsImpactRepository{
			latestSnapshot: &domain.SavingsImpactSnapshot{
				TotalAmount:    1400.0,
				UserCount:      15,
				CompletedCount: 10,
				ComputedAt:     time.Now(),
			},
		}
		handler := NewSavingsImpactHandler(repo)
		output, err := handler.GetAggregate(context.Background(), &GetSavingsImpactAggregateInput{})
		require.NoError(t, err)
		assert.True(t, output.Body.Published)
		assert.Equal(t, 1000.0, output.Body.TotalAmount) // 1400 rounds to 1000
	})
}

func TestGetSavingsImpactAggregate_RepoError(t *testing.T) {
	repo := &mockSavingsImpactRepository{latestErr: errors.New("database error")}
	handler := NewSavingsImpactHandler(repo)

	output, err := handler.GetAggregate(context.Background(), &GetSavingsImpactAggregateInput{})

	assert.Nil(t, output)
	require.Error(t, err)
}

func TestGetSavingsImpactAggregate_CacheControlHeader(t *testing.T) {
	repo := &mockSavingsImpactRepository{latestSnapshot: nil}
	handler := NewSavingsImpactHandler(repo)

	output, err := handler.GetAggregate(context.Background(), &GetSavingsImpactAggregateInput{})

	require.NoError(t, err)
	assert.Equal(t, "public, max-age=300", output.CacheControl)
}

// =============================================================================
// C1: Route Registration Test (public — no auth required)
// =============================================================================

func TestSavingsImpactAggregateRouteRegistration(t *testing.T) {
	repo := &mockSavingsImpactRepository{latestSnapshot: nil}
	handler := NewSavingsImpactHandler(repo)

	_, api := humatest.New(t)
	RegisterSavingsImpactRoutes(api, handler)

	// Public endpoint — should return 200 (not 401).
	resp := api.Get("/api/v1/savings-impact/aggregate")
	assert.Equal(t, http.StatusOK, resp.Code)
}

// =============================================================================
// C2: GetSavingsImpactMe Tests
// =============================================================================

func TestGetSavingsImpactMe_Success(t *testing.T) {
	now := time.Now()
	impact := &domain.UserSavingsImpact{
		TotalAmount:     2500.00,
		CompletedCount:  8,
		MonthAmount:     300.00,
		PrevMonthAmount: 400.00,
		RecentItems: []*domain.CompletedSuggestionSummary{
			{
				ID:           "s-1",
				Amount:       100.00,
				ActualAmount: 100.00,
				CompletedAt:  &now,
			},
		},
	}
	repo := &mockSavingsImpactRepository{userImpact: impact}
	handler := NewSavingsImpactHandler(repo)

	input := &GetSavingsImpactMeInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetMe(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 2500.00, output.Body.TotalAmount)
	assert.Equal(t, 8, output.Body.CompletedCount)
	assert.Equal(t, 300.00, output.Body.MonthAmount)
	assert.Equal(t, 400.00, output.Body.PrevMonthAmount)
	assert.Len(t, output.Body.RecentItems, 1)
}

func TestGetSavingsImpactMe_EmptyState(t *testing.T) {
	impact := &domain.UserSavingsImpact{
		TotalAmount:     0,
		CompletedCount:  0,
		MonthAmount:     0,
		PrevMonthAmount: 0,
		RecentItems:     []*domain.CompletedSuggestionSummary{},
	}
	repo := &mockSavingsImpactRepository{userImpact: impact}
	handler := NewSavingsImpactHandler(repo)

	input := &GetSavingsImpactMeInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-new"},
	}

	output, err := handler.GetMe(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 0.0, output.Body.TotalAmount)
	assert.Equal(t, 0, output.Body.CompletedCount)
	assert.NotNil(t, output.Body.RecentItems)
	assert.Len(t, output.Body.RecentItems, 0)
}

func TestGetSavingsImpactMe_RepoError(t *testing.T) {
	repo := &mockSavingsImpactRepository{userImpactErr: errors.New("database error")}
	handler := NewSavingsImpactHandler(repo)

	input := &GetSavingsImpactMeInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetMe(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// =============================================================================
// C2: Route Registration Test (authed — returns 401 without auth)
// =============================================================================

func TestSavingsImpactMeRouteRegistration(t *testing.T) {
	repo := &mockSavingsImpactRepository{}
	handler := NewSavingsImpactHandler(repo)

	_, api := humatest.New(t)
	RegisterSavingsImpactRoutes(api, handler)

	// Authed endpoint — should return 401 without auth.
	resp := api.Get("/api/v1/savings-impact/me")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}
