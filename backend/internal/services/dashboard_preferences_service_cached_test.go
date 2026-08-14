package services

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingDashboardPreferencesService wraps a DashboardPreferencesService and counts calls to
// GetPreferences, so tests can assert a cache hit skipped the inner call entirely.
type countingDashboardPreferencesService struct {
	DashboardPreferencesService
	getPreferencesCalls int
}

func (s *countingDashboardPreferencesService) GetPreferences(ctx context.Context, userID string) (*domain.DashboardPreferencesResponse, error) {
	s.getPreferencesCalls++
	return s.DashboardPreferencesService.GetPreferences(ctx, userID)
}

func TestCachedDashboardPreferencesService_SecondCallIsACacheHit(t *testing.T) {
	layoutConfig := createTestLayoutConfig()
	require.NotNil(t, layoutConfig)
	repo := &mockDashboardPreferencesRepository{}
	inner := &countingDashboardPreferencesService{DashboardPreferencesService: NewDashboardPreferencesService(repo, layoutConfig)}
	c := newFakeCache()
	svc := NewCachedDashboardPreferencesService(inner, c)

	ctx := context.Background()
	_, err := svc.GetPreferences(ctx, "user-1")
	require.NoError(t, err)
	_, err = svc.GetPreferences(ctx, "user-1")
	require.NoError(t, err)

	assert.Equal(t, 1, inner.getPreferencesCalls, "second call should have been served from cache")
}

func TestCachedDashboardPreferencesService_DifferentUsersAreDifferentCacheEntries(t *testing.T) {
	layoutConfig := createTestLayoutConfig()
	require.NotNil(t, layoutConfig)
	repo := &mockDashboardPreferencesRepository{}
	inner := &countingDashboardPreferencesService{DashboardPreferencesService: NewDashboardPreferencesService(repo, layoutConfig)}
	c := newFakeCache()
	svc := NewCachedDashboardPreferencesService(inner, c)

	ctx := context.Background()
	_, err := svc.GetPreferences(ctx, "user-1")
	require.NoError(t, err)
	_, err = svc.GetPreferences(ctx, "user-2")
	require.NoError(t, err)

	assert.Equal(t, 2, inner.getPreferencesCalls)
}

// TestCachedDashboardPreferencesService_ZeroUpdatedAtRoundTrips proves the envelope faithfully
// round-trips the default-layout case: no saved row -> config-derived default with a zero
// UpdatedAt (dashboard_preferences_service.go's GetPreferences). A cache bug that serialized zero
// time.Time as, say, the Unix epoch would corrupt every never-customized user's response.
func TestCachedDashboardPreferencesService_ZeroUpdatedAtRoundTrips(t *testing.T) {
	layoutConfig := createTestLayoutConfig()
	require.NotNil(t, layoutConfig)
	repo := &mockDashboardPreferencesRepository{} // getByUserIDFn nil -> ErrDashboardPreferencesNotFound
	inner := NewDashboardPreferencesService(repo, layoutConfig)
	c := newFakeCache()
	svc := NewCachedDashboardPreferencesService(inner, c)

	ctx := context.Background()
	first, err := svc.GetPreferences(ctx, "user-1")
	require.NoError(t, err)
	require.True(t, first.UpdatedAt.IsZero())

	// Second call is served from the cache (JSON round-trip through the envelope codec).
	second, err := svc.GetPreferences(ctx, "user-1")
	require.NoError(t, err)
	assert.True(t, second.UpdatedAt.IsZero(), "zero UpdatedAt must round-trip through the cache as zero, not the Unix epoch or any other sentinel")
	assert.Equal(t, first.Layout.Version, second.Layout.Version)
}

func TestCachedDashboardPreferencesService_SavePassesThroughUncached(t *testing.T) {
	layoutConfig := createTestLayoutConfig()
	require.NotNil(t, layoutConfig)
	repo := &mockDashboardPreferencesRepository{}
	inner := NewDashboardPreferencesService(repo, layoutConfig)
	c := newFakeCache()
	svc := NewCachedDashboardPreferencesService(inner, c)

	ctx := context.Background()
	layout := layoutConfig.GetDefaultLayout()
	_, err := svc.SavePreferences(ctx, "user-1", *layout)
	require.NoError(t, err)
	// SavePreferences must not have been cached under GetPreferences's key or anything else —
	// the fake cache's store should remain untouched by a write.
	assert.Empty(t, c.store)
}

var _ DashboardPreferencesService = (*countingDashboardPreferencesService)(nil)
