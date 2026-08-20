//go:build integration

package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/cache/cachetest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/require"
)

// #1537 — regression test for a CONFIRMED cross-user cache bug.
//
// ⚠ WHAT THIS DOES NOT PROVE, corrected after review: this is NOT an invalidation harness, and an
// earlier version of this comment implied it was. It calls the repository method directly rather
// than driving transactionSyncService.SyncTransactions, and it issues the bump itself below — so
// deleting bumpCacheForSync from transaction_sync_service.go entirely would leave this test GREEN.
// It cannot detect a missing production bump, which is precisely the criticism this comment levels
// at the parity harness a few lines down.
//
// What it DOES prove, and what its load-bearing assertion is: an existing merchants row's
// normalized_name is not rewritten by another user's sync (require.Nil on the stored value). That
// makes the staleness class unreachable at the source, which is stronger than invalidating after
// the fact. For a test that observes a genuinely production-issued bump, see
// cmd/server/deletion_route_middleware_integration_test.go.
//
// The existing parity harness cannot detect a missing production bump: its `Bump` field is
// documented as "the production invalidation hook" but resolves to bumpPersonal(), i.e.
// `gens.Bump(ctx, ScopeUser, f.userID, "test")` — a test-authored call — and its `Mutate`
// writes raw SQL straight to the pool. It proves "if something bumps, the cached read
// updates"; it can never prove "the production path bumps".
//
// This pilot proves the opposite shape: a real production write whose blast radius escapes the
// bump leaves ANOTHER user's cached read stale. Target: MerchantRepo.GetOrCreateMerchantsWithNormalize,
// one of #1537's 127 unclassified entries.
//
// Why it is a cross-user write: `merchants` is a GLOBAL table (UNIQUE(name), no user column), so
// any two users transacting at the same merchant share one row via transactions.merchant_id.
// GetSpendingByMerchant renders
//
//	COALESCE(mno_t.display_name, NULLIF(m2.normalized_name,''), m2.name, NULLIF(t.merchant_name,''), 'Unknown')
//
// preferring m2.normalized_name over m2.name, and that read is cached under NamespaceAnalytics /
// ScopeUser. Filling a previously-NULL normalized_name therefore changes the rendered merchant
// name for EVERY user sharing the row, while the Plaid sync bumps only the user being synced
// (transaction_sync_service.go bumpCacheForSync).
func TestMerchantNormalize_DoesNotStaleOtherUsersCachedMerchantNames(t *testing.T) {
	ctx := context.Background()
	pool := connectIntegrationDB(t)
	c := cache.NewCache(cache.Options{Client: cachetest.ConnectTestRedis(t)})

	// One shared global merchant row with normalized_name NULL, as Plaid-synced rows start.
	merchantName := "PILOT SHARED MERCHANT " + uuid.NewString()[:8]
	merchantID := uuid.NewString()
	_, err := pool.Exec(ctx,
		`INSERT INTO merchants (id, name, normalized_name, created_at) VALUES ($1, $2, NULL, now())`,
		merchantID, merchantName)
	require.NoError(t, err, "seed shared merchant")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM merchants WHERE id = $1`, merchantID) //nolint:errcheck
	})

	// Two unrelated users, each with a transaction pointing at that same merchant row.
	userA := newParityFixture(t, ctx, pool)
	userB := newParityFixture(t, ctx, pool)
	day := time.Now().UTC().AddDate(0, 0, -3)
	for _, f := range []*parityFixture{userA, userB} {
		f.seedTransaction(t, ctx, 25.00, day, "coffee", merchantName, "FOOD_AND_DRINK", "FOOD_AND_DRINK_COFFEE")
		_, err := pool.Exec(ctx,
			`UPDATE transactions SET merchant_id = $1 WHERE user_id = $2`, merchantID, f.userID)
		require.NoError(t, err, "link transaction to shared merchant")
	}

	analyticsRepo := repository.NewAnalyticsRepository(pool)
	inner := NewAnalyticsService(analyticsRepo)
	cachedSvc := NewCachedAnalyticsService(inner, c)
	filters := domain.SpendingFilters{
		StartDate: day.AddDate(0, 0, -1),
		EndDate:   day.AddDate(0, 0, 1),
	}

	readB := func(t *testing.T, svc AnalyticsService) string {
		t.Helper()
		resp, err := svc.GetSpendingByMerchant(ctx, userB.userID, filters, 10)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Merchants, "user B must see the shared merchant")
		return resp.Merchants[0].MerchantName
	}

	// Warm user B's cache. Nothing has normalized the merchant yet, so B sees m2.name.
	warm := readB(t, cachedSvc)
	require.Equal(t, merchantName, warm, "before normalization B should see the raw merchant name")

	// THE PRODUCTION WRITE, exactly as the Plaid sync issues it for user A only.
	merchantRepo := repository.NewMerchantRepository(pool)
	normalized := "Pilot Shared Merchant (Normalized)"
	_, err = merchantRepo.GetOrCreateMerchantsWithNormalize(ctx,
		[]string{merchantName},
		func(string) string { return normalized })
	require.NoError(t, err, "GetOrCreateMerchantsWithNormalize")

	// The pre-existing row's normalized_name must be untouched. This is the invariant the fix
	// establishes, asserted directly so the test cannot pass merely because nothing ran.
	var stored *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT normalized_name FROM merchants WHERE id = $1`, merchantID).Scan(&stored))
	require.Nil(t, stored, "an EXISTING merchants row's normalized_name must not be backfilled "+
		"by another user's sync — that write is cross-user and only the syncing user is bumped")

	// ...but a BRAND-NEW merchant must still be normalized, or the fix has broken the feature.
	newName := "PILOT FRESH MERCHANT " + uuid.NewString()[:8]
	ids, err := merchantRepo.GetOrCreateMerchantsWithNormalize(ctx, []string{newName},
		func(string) string { return normalized })
	require.NoError(t, err)
	require.Len(t, ids, 1)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM merchants WHERE name = $1`, newName) //nolint:errcheck
	})
	var freshStored *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT normalized_name FROM merchants WHERE name = $1`, newName).Scan(&freshStored))
	require.NotNil(t, freshStored, "a newly created merchant must still get normalized_name set")
	require.Equal(t, normalized, *freshStored)

	// What production does after a sync: bump ONLY the synced user.
	c.Generations().Bump(ctx, cache.ScopeUser, userA.userID, "plaid_sync")

	// B's UNCACHED read is ground truth. With the fix it still renders the raw name, because the
	// shared row's normalized_name was left alone.
	fresh := readB(t, inner)
	require.Equal(t, warm, fresh,
		"ground truth for B changed after user A's sync — a SHARED global merchants row was "+
			"mutated. merchants has no user column, so this is a cross-user write.")

	// The staleness assertion: B's cached read must match ground truth. Deliberately compared
	// against `fresh`, NOT against `warm` — comparing to `warm` is what a STALE cache returns, so
	// that assertion would PASS under the very bug it claims to guard (caught by mutation-probing
	// this line). Under the pre-fix code `fresh` moves to the normalized name while `got` stays at
	// `warm`, and both this and the assertion above fail.
	got := readB(t, cachedSvc)
	require.Equal(t, fresh, got,
		"user B serves a STALE merchant name: cached=%q ground-truth=%q. Only user A was bumped "+
			"(transaction_sync_service.go bumpCacheForSync), so B stays stale for up to the 120s "+
			"NamespaceAnalytics TTL. Keep normalized_name insert-only in "+
			"GetOrCreateMerchantsWithNormalize.", got, fresh)
}
