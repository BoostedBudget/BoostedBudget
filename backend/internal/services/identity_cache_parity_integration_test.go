//go:build integration

// Package services — Issue #1531 Task 11c: cache parity for the `identity` namespace
// (cachedIdentityResolver.ResolveInternalUserID, identity_resolver_cached.go).
//
// Scope, per the task 11c brief: successful lookups only, plus the specific negative-cache
// hazard this namespace carries. Two properties, two tests:
//
//  1. TestIdentityCacheParity_SuccessfulLookup: a real clerk_user_id -> internal UUID mapping
//     round-trips through the cache (cold miss, warm hit). There is no Mutate/Bump case here —
//     unlike every other namespace in this file family, a clerk_user_id -> user_id mapping is
//     immutable for the life of that identity row (Task 10a's decorator has no write path at
//     all), so there is nothing analogous to "the mutation must move the ground truth" to prove.
//
//  2. TestIdentityCacheParity_NotFoundNeverCached: repository.ErrIdentityNotFound must NEVER be
//     written to the cache. cache.GetOrLoad's generic contract already guarantees this (the
//     loader's error propagates unencoded, and cache.set is only called on the nil-error path —
//     see cache/cache.go's loadAndSet) — the decorator itself has no special-case branch, and
//     this test's job is to prove that generic contract actually holds for THIS namespace's
//     wiring, not just assert it by reading the source. If it didn't, a fresh Clerk registration
//     would be invisible to the rest of the app for up to identity's 10-minute TTL: the FIRST
//     unauthenticated-looking request during onboarding would poison a negative cache entry that
//     every subsequent (now-valid) request keeps hitting.
//
// Run:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=redis://localhost:6397/0 \
//	  go test ./internal/services/... -tags integration -race -p 1 -run TestIdentityCacheParity -v
package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/cache/cachetest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// TestIdentityCacheParity_SuccessfulLookup proves cold-miss/warm-hit parity for a real identity.
func TestIdentityCacheParity_SuccessfulLookup(t *testing.T) {
	pool := connectIntegrationDB(t)
	redisClient := cachetest.ConnectTestRedis(t)
	c := cache.NewCache(cache.Options{Client: redisClient})
	ctx := context.Background()

	userID := uuid.New().String()
	clerkUserID := "user_" + uuid.New().String()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, clerkUserID, "int-identity-"+userID+"@example.com")
	require.NoError(t, err, "seed user")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID) //nolint:errcheck
	})

	enc := newTestFieldEncryptor(t)
	identityRepo := repository.NewUserIdentityRepository(pool, enc)
	identity := domain.NewUserIdentity(userID, clerkUserID, "int-identity-"+userID+"@example.com", true, true)
	require.NoError(t, identityRepo.Create(ctx, identity), "seed user_identities row")

	resolver := NewCachedIdentityResolver(identityRepo, c)

	// Ground truth.
	wantID, err := identityRepo.GetByClerkID(ctx, clerkUserID)
	require.NoError(t, err)

	// Cold read — exactly 1 miss.
	before := cache.Snapshot()
	gotCold, err := resolver.ResolveInternalUserID(ctx, clerkUserID)
	require.NoError(t, err)
	afterCold := cache.Snapshot()
	assert.Equal(t, wantID.UserID, gotCold.String(), "cold read must resolve to the seeded internal user id")
	assertDelta(t, afterCold.Delta(before), cache.NamespaceIdentity, 0, 1, "ResolveInternalUserID: cold read")

	// Warm read — exactly 1 hit, same value. This is the assertion the whole gate rests on: a
	// broken key builder that mints a fresh key every call would make this pass by permanently
	// missing.
	beforeWarm := cache.Snapshot()
	gotWarm, err := resolver.ResolveInternalUserID(ctx, clerkUserID)
	require.NoError(t, err)
	afterWarm := cache.Snapshot()
	assert.Equal(t, wantID.UserID, gotWarm.String(), "warm read must resolve to the same internal user id")
	assertDelta(t, afterWarm.Delta(beforeWarm), cache.NamespaceIdentity, 1, 0, "ResolveInternalUserID: warm read")
}

// TestIdentityCacheParity_NotFoundNeverCached proves an unknown clerk_user_id is never
// negative-cached: two consecutive lookups against the SAME unknown id must each independently
// miss (and each independently re-query Postgres), never serve a cached "not found".
func TestIdentityCacheParity_NotFoundNeverCached(t *testing.T) {
	pool := connectIntegrationDB(t)
	redisClient := cachetest.ConnectTestRedis(t)
	c := cache.NewCache(cache.Options{Client: redisClient})
	ctx := context.Background()

	enc := newTestFieldEncryptor(t)
	identityRepo := repository.NewUserIdentityRepository(pool, enc)
	resolver := NewCachedIdentityResolver(identityRepo, c)

	unknownClerkID := "user_" + uuid.New().String() + "-never-registered"

	// First lookup: must miss and must return ErrIdentityNotFound (not e.g. a zero-value success).
	before := cache.Snapshot()
	_, err := resolver.ResolveInternalUserID(ctx, unknownClerkID)
	after := cache.Snapshot()
	require.True(t, errors.Is(err, repository.ErrIdentityNotFound), "expected ErrIdentityNotFound, got %v", err)
	assertDelta(t, after.Delta(before), cache.NamespaceIdentity, 0, 1, "ResolveInternalUserID: first not-found lookup")

	// Second lookup, same unknown id: if the first call had (incorrectly) negative-cached the
	// miss, this would come back as a HIT with the same error baked into a cached value (or,
	// depending on implementation, simply never re-hit Postgres). Both are wrong: a fresh
	// registration between these two calls must be visible on the very next lookup, so this call
	// must ALSO be a miss, not a hit.
	beforeSecond := cache.Snapshot()
	_, err = resolver.ResolveInternalUserID(ctx, unknownClerkID)
	afterSecond := cache.Snapshot()
	require.True(t, errors.Is(err, repository.ErrIdentityNotFound), "expected ErrIdentityNotFound again, got %v", err)
	assertDelta(t, afterSecond.Delta(beforeSecond), cache.NamespaceIdentity, 0, 1, "ResolveInternalUserID: second not-found lookup must ALSO miss, proving nothing was negative-cached")
}
