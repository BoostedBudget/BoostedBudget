//go:build integration

package repository

// Batch S2 Task 2 regression tests: EntitlementRepository.GetActiveEntitlement and
// SubscriptionOverrideRepository.GetActiveOverrides were widened to take an explicit `asOf
// time.Time` parameter instead of comparing against the database's own NOW(), so every
// expiry/liveness predicate evaluated for a single admin subscription-detail request agrees on
// the same instant.
//
// pgxmock cannot catch either of these two regressions:
//   - It only matches query text/arg SHAPE, never SQL semantics — it would happily "pass" a query
//     that silently ignored the asOf argument and used a hardcoded NOW() instead, as long as the
//     WithArgs() call lists the right number of arguments.
//   - It has no session/time-zone concept at all, so it cannot exercise a comparison that (if the
//     column, or the comparison, were zone-naked) could disagree across sessions.
//
// Follows the pattern in migration_000130_subscription_overrides_tz_integration_test.go
// (internal/database) and TestDbAddMonths_Integration_IndependentOfSessionTimeZone
// (admin_user_repository_remove_tier_override_integration_test.go, this package): acquire a
// single *pgxpool.Conn, force its session TimeZone away from UTC, and prove the result is
// unchanged.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestAsOfThreading -tags integration -v -p 1

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── EntitlementRepository.GetActiveEntitlement ──────────────────────────────────────────────

// TestAsOfThreading_GetActiveEntitlement_FiltersByPassedInstant_NotDatabaseNow proves (a): the
// query's cutoff moves with the asOf PARAMETER, not with the database's real wall-clock NOW().
// effective_from is seeded far in the past (2024) — relative to the real NOW() this test runs
// under, EVERY asOf value used below is also in the past, so a query that (bug-for-bug) still
// used now() internally would find the row for BOTH assertions. Only a query that genuinely binds
// asOf as $2 can tell "before effective_from" apart from "after effective_from".
func TestAsOfThreading_GetActiveEntitlement_FiltersByPassedInstant_NotDatabaseNow(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	profileRepo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createClaimForDeletionFixtureUser(t, ctx, pool, profileRepo, suffix, false, nil, nil, nil)

	entRepo := NewEntitlementRepository(pool)

	effectiveFrom := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	_, err := pool.Exec(ctx, `
		INSERT INTO subscription_entitlements (user_id, source, tier, status, effective_from, effective_until)
		VALUES ($1, 'own_subscription', 'premium', 'active', $2, NULL)
	`, user.ID, effectiveFrom)
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM subscription_entitlements WHERE user_id = $1`, user.ID) //nolint:errcheck
	})

	before := effectiveFrom.Add(-1 * time.Hour)
	_, err = entRepo.GetActiveEntitlement(ctx, user.ID, before)
	assert.ErrorIs(t, err, ErrEntitlementNotFound,
		"asOf BEFORE effective_from must NOT find the row — finding it means the query is still "+
			"filtering by the database's own NOW() instead of the passed asOf parameter")

	after := effectiveFrom.Add(1 * time.Hour)
	ent, err := entRepo.GetActiveEntitlement(ctx, user.ID, after)
	require.NoError(t, err, "asOf AFTER effective_from must find the row")
	assert.Equal(t, "premium", ent.Tier)
}

// TestAsOfThreading_GetActiveEntitlement_ConsistentAcrossSessionTimeZones proves (b): binding
// asOf as a query parameter (rather than relying on a session-zone-dependent comparison) makes
// the result identical regardless of which time zone the reading connection happens to be in.
func TestAsOfThreading_GetActiveEntitlement_ConsistentAcrossSessionTimeZones(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	profileRepo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createClaimForDeletionFixtureUser(t, ctx, pool, profileRepo, suffix, false, nil, nil, nil)

	effectiveFrom := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	_, err := pool.Exec(ctx, `
		INSERT INTO subscription_entitlements (user_id, source, tier, status, effective_from, effective_until)
		VALUES ($1, 'own_subscription', 'premium', 'active', $2, NULL)
	`, user.ID, effectiveFrom)
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM subscription_entitlements WHERE user_id = $1`, user.ID) //nolint:errcheck
	})

	asOf := effectiveFrom.Add(1 * time.Hour)

	found := func(tz string) bool {
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()
		_, err = conn.Exec(ctx, "SET TIME ZONE '"+tz+"'")
		require.NoError(t, err)

		repo := NewEntitlementRepository(conn)
		_, err = repo.GetActiveEntitlement(ctx, user.ID, asOf)
		if errors.Is(err, ErrEntitlementNotFound) {
			return false
		}
		require.NoError(t, err)
		return true
	}

	utcFound := found("UTC")
	nyFound := found("America/New_York")

	assert.True(t, utcFound, "sanity: the row should be found under a UTC session for this asOf")
	assert.Equal(t, utcFound, nyFound,
		"GetActiveEntitlement's result must not depend on the reading session's time zone "+
			"(UTC found=%v vs America/New_York found=%v)", utcFound, nyFound)
}

// ─── SubscriptionOverrideRepository.GetActiveOverrides ──────────────────────────────────────

// TestAsOfThreading_GetActiveOverrides_FiltersByPassedInstant_NotDatabaseNow mirrors the
// entitlement test above for the override repository's expires_at predicate.
func TestAsOfThreading_GetActiveOverrides_FiltersByPassedInstant_NotDatabaseNow(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	profileRepo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createClaimForDeletionFixtureUser(t, ctx, pool, profileRepo, suffix, false, nil, nil, nil)

	overrideRepo := NewSubscriptionOverrideRepository(pool)

	expiresAt := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	reason := "asof-threading-test"
	_, err := pool.Exec(ctx, `
		INSERT INTO user_subscription_overrides (user_id, resource_type, limit_value, reason, expires_at)
		VALUES ($1, 'transaction_history_days', 999, $2, $3)
	`, user.ID, reason, expiresAt)
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM user_subscription_overrides WHERE user_id = $1`, user.ID) //nolint:errcheck
	})

	// asOf BEFORE expires_at: override is still active (expires_at > asOf).
	before := expiresAt.Add(-1 * time.Hour)
	activeBefore, err := overrideRepo.GetActiveOverrides(ctx, user.ID, before)
	require.NoError(t, err)
	assert.Len(t, activeBefore, 1,
		"asOf BEFORE expires_at must find the override as active — a query still using the "+
			"database's real NOW() (always after 2024 for this test) would wrongly exclude it")

	// asOf AFTER expires_at: override has lapsed (expires_at <= asOf).
	after := expiresAt.Add(1 * time.Hour)
	activeAfter, err := overrideRepo.GetActiveOverrides(ctx, user.ID, after)
	require.NoError(t, err)
	assert.Empty(t, activeAfter, "asOf AFTER expires_at must NOT find the override as active")
}

// TestAsOfThreading_GetActiveOverrides_ConsistentAcrossSessionTimeZones mirrors the entitlement
// zone-independence test for the override repository.
func TestAsOfThreading_GetActiveOverrides_ConsistentAcrossSessionTimeZones(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	profileRepo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createClaimForDeletionFixtureUser(t, ctx, pool, profileRepo, suffix, false, nil, nil, nil)

	expiresAt := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	reason := "asof-threading-tz-test"
	_, err := pool.Exec(ctx, `
		INSERT INTO user_subscription_overrides (user_id, resource_type, limit_value, reason, expires_at)
		VALUES ($1, 'transaction_history_days', 999, $2, $3)
	`, user.ID, reason, expiresAt)
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM user_subscription_overrides WHERE user_id = $1`, user.ID) //nolint:errcheck
	})

	asOf := expiresAt.Add(-1 * time.Hour) // still active regardless of session zone

	activeCount := func(tz string) int {
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()
		_, err = conn.Exec(ctx, "SET TIME ZONE '"+tz+"'")
		require.NoError(t, err)

		repo := NewSubscriptionOverrideRepository(conn)
		overrides, err := repo.GetActiveOverrides(ctx, user.ID, asOf)
		require.NoError(t, err)
		return len(overrides)
	}

	utcCount := activeCount("UTC")
	nyCount := activeCount("America/New_York")

	assert.Equal(t, 1, utcCount, "sanity: the override should be found active under a UTC session for this asOf")
	assert.Equal(t, utcCount, nyCount,
		"GetActiveOverrides's result must not depend on the reading session's time zone "+
			"(UTC count=%d vs America/New_York count=%d)", utcCount, nyCount)
}
