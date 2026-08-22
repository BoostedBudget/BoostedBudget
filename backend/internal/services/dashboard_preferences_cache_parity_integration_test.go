//go:build integration

// Package services — Issue #1531 Task 11c: cache parity for the `dashprefs` namespace
// (cachedDashboardPreferencesService.GetPreferences, dashboard_preferences_service_cached.go).
//
// This follows the same 7-step shape as the analytics parity harness (cold-miss, warm-hit,
// mutate + bump, stale-money-after-mutation) but is written standalone rather than through
// parityCase/runParityCase: DashboardPreferencesService is a different interface from
// AnalyticsService, and there's exactly one cached method here, so the table-driven machinery
// built for 19 (now 23) analytics rows would add indirection without buying anything.
//
// The case this file exists to prove: a user with NO saved preferences gets a config-derived
// default layout with a ZERO time.Time UpdatedAt (dashboard_preferences_service.go's
// GetPreferences) — that value must round-trip through the REAL cache envelope (JSON through real
// Redis, not the fake in-memory cache the existing unit test
// TestCachedDashboardPreferencesService_ZeroUpdatedAtRoundTrips uses) as zero, not the Unix epoch
// or some other sentinel a lossy codec could produce.
//
// Invalidation note: cachedDashboardPreferencesService.SavePreferences/ResetToDefault pass
// straight through to inner uncached (this file's own doc comment says so) — there is no
// decorator-level Bump. The REAL invalidation hook for dashprefs (ScopeUser, per
// cache/namespace.go's registry) lives one layer up, in
// internal/huma/middleware/cache_invalidation.go, which bumps cache.ScopeUser for the acting user
// after every mutating HTTP request succeeds.
//
// ⚠ This test does NOT invoke that hook. It calls cache.Generations().Bump itself, from test code,
// using the same scope/key shape the middleware uses. That is deliberate — the property under test
// is "the cached read equals uncached ground truth, and reflects a mutation once the generation
// moves" — but it means this test cannot detect a production path that forgets to bump. The
// HTTP-middleware side is covered separately by
// cmd/server/deletion_route_middleware_integration_test.go and the middleware's own tests.
//
// Run:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=redis://localhost:6397/0 \
//	  go test ./internal/services/... -tags integration -race -p 1 -run TestDashboardPreferencesCacheParity -v
package services

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/cache/cachetest"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

func TestDashboardPreferencesCacheParity(t *testing.T) {
	pool := connectIntegrationDB(t)
	redisClient := cachetest.ConnectTestRedis(t)
	c := cache.NewCache(cache.Options{Client: redisClient})
	ctx := context.Background()

	userID := uuid.New().String()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "int-dashprefs|"+userID, "int-dashprefs-"+userID+"@example.com")
	require.NoError(t, err, "seed user")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID) //nolint:errcheck
	})

	layoutConfig := createTestLayoutConfig()
	require.NotNil(t, layoutConfig, "sanity: config/default_dashboard_layout.yaml must load")
	prefsRepo := repository.NewDashboardPreferencesRepository(pool)
	inner := NewDashboardPreferencesService(prefsRepo, layoutConfig)
	cachedSvc := NewCachedDashboardPreferencesService(inner, c)

	// Step 2: ground truth #1 — no saved row yet, so GetPreferences returns the config-derived
	// default with a zero UpdatedAt.
	wantOff, err := inner.GetPreferences(ctx, userID)
	require.NoError(t, err)
	require.True(t, wantOff.UpdatedAt.IsZero(), "sanity: no saved prefs yet must mean zero UpdatedAt")

	// Step 3: cold read — must equal ground truth (including the zero UpdatedAt), exactly 1 miss.
	before := cache.Snapshot()
	gotCold, err := cachedSvc.GetPreferences(ctx, userID)
	require.NoError(t, err)
	afterCold := cache.Snapshot()
	assert.JSONEq(t, mustJSON(t, wantOff), mustJSON(t, gotCold), "cold read must equal ground truth")
	assertDelta(t, afterCold.Delta(before), cache.NamespaceDashPrefs, 0, 1, "GetPreferences: cold read")

	// Step 4: warm read — hit, and the zero UpdatedAt must survive a REAL round trip through
	// Redis (encode -> SET -> GET -> decode), not just the fake in-memory cache the existing unit
	// test exercises.
	beforeWarm := cache.Snapshot()
	gotWarm, err := cachedSvc.GetPreferences(ctx, userID)
	require.NoError(t, err)
	afterWarm := cache.Snapshot()
	assert.JSONEq(t, mustJSON(t, wantOff), mustJSON(t, gotWarm), "warm read must equal ground truth")
	assert.True(t, gotWarm.UpdatedAt.IsZero(), "zero UpdatedAt must round-trip through the real Redis envelope as zero, not the Unix epoch or any other sentinel")
	assertDelta(t, afterWarm.Delta(beforeWarm), cache.NamespaceDashPrefs, 1, 0, "GetPreferences: warm read")

	// Step 5: mutate — save a real, non-default layout (one fewer widget than the config
	// default; still passes DashboardLayout.Validate()), then bump the generation from test code
	// using the same scope the HTTP middleware bumps. This is NOT the production invalidation hook
	// (see the ⚠ in the file doc comment): it proves the cached read reflects the save once the
	// generation moves, not that the production path moves it.
	customLayout := *layoutConfig.GetDefaultLayout()
	require.NotEmpty(t, customLayout.Widgets, "sanity: default layout must have at least one widget to remove")
	customLayout.Widgets = customLayout.Widgets[:len(customLayout.Widgets)-1]
	_, err = cachedSvc.SavePreferences(ctx, userID, customLayout)
	require.NoError(t, err)
	c.Generations().Bump(ctx, cache.ScopeUser, userID, "test")

	// Step 6: the mutation must have genuinely moved the ground truth — a real saved row now
	// exists, so UpdatedAt must no longer be zero, and the widget count must have changed.
	wantOff2, err := inner.GetPreferences(ctx, userID)
	require.NoError(t, err)
	require.False(t, wantOff2.UpdatedAt.IsZero(), "Mutate must produce a real saved row with non-zero UpdatedAt")
	if mustJSON(t, wantOff) == mustJSON(t, wantOff2) {
		t.Fatal("Mutate did not change the ground-truth result — unable to test staleness honestly")
	}

	// Step 7: the stale-money assertion — after Bump, the cached read must reflect the save, not
	// the default warmed in step 4.
	gotAfter, err := cachedSvc.GetPreferences(ctx, userID)
	require.NoError(t, err)
	assert.JSONEq(t, mustJSON(t, wantOff2), mustJSON(t, gotAfter), "post-mutation cached read served stale preferences")
}
