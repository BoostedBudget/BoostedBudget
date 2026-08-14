package services

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// methodGetDashboardPreferences is the cache method constant for
// cachedDashboardPreferencesService.GetPreferences.
const methodGetDashboardPreferences = "GetPreferences"

// cachedDashboardPreferencesService decorates DashboardPreferencesService, caching ONLY
// GetPreferences (namespace dashprefs, 300s TTL). SavePreferences and ResetToDefault are writes
// and pass straight through — Task 10b is responsible for bumping this user's generation after
// either succeeds; until then, a cached GetPreferences read can serve a stale layout for up to
// the namespace TTL after a save. That is this task's known, deliberate gap.
type cachedDashboardPreferencesService struct {
	inner DashboardPreferencesService
	c     cache.Cache
}

// NewCachedDashboardPreferencesService returns a DashboardPreferencesService whose GetPreferences
// reads through c; SavePreferences/ResetToDefault delegate to inner unchanged.
func NewCachedDashboardPreferencesService(inner DashboardPreferencesService, c cache.Cache) DashboardPreferencesService {
	return &cachedDashboardPreferencesService{inner: inner, c: c}
}

// GetPreferences reads through the cache. No arguments beyond userID (the scope) to encode.
//
// When the repo has no saved row, the inner service returns a config-derived default with a zero
// UpdatedAt (dashboard_preferences_service.go's GetPreferences) — that is cacheable and correct,
// and the codec's envelope round-trips a zero time.Time faithfully (see
// TestCachedDashboardPreferencesService_ZeroUpdatedAtRoundTrips).
func (s *cachedDashboardPreferencesService) GetPreferences(ctx context.Context, userID string) (*domain.DashboardPreferencesResponse, error) {
	k := cache.NewKey(cache.NamespaceDashPrefs, cache.ScopeFor(userID, nil), methodGetDashboardPreferences).
		Enc(func(e *cache.ArgEncoder) {})
	return cache.GetOrLoad(ctx, s.c, k, func(ctx context.Context) (*domain.DashboardPreferencesResponse, error) {
		return s.inner.GetPreferences(ctx, userID)
	})
}

func (s *cachedDashboardPreferencesService) SavePreferences(ctx context.Context, userID string, layout domain.DashboardLayout) (*domain.DashboardPreferencesResponse, error) {
	return s.inner.SavePreferences(ctx, userID, layout)
}

func (s *cachedDashboardPreferencesService) ResetToDefault(ctx context.Context, userID string) (*domain.DashboardPreferencesResponse, error) {
	return s.inner.ResetToDefault(ctx, userID)
}

var _ DashboardPreferencesService = (*cachedDashboardPreferencesService)(nil)
