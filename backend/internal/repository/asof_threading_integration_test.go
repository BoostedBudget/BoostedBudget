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

// TestAsOfThreading_GetActiveEntitlement_ResidualWindow_ClosedAfterAsOfIsNotReturned pins the
// residual-window semantics documented on GetActiveEntitlement's doc comment (G5-4): the
// `effective_until IS NULL` half of the WHERE clause is a CURRENT-STATE predicate, evaluated at
// query time, not at asOf. An entitlement that was open at asOf but has since been closed
// (effective_until set strictly after asOf, but before this query runs) must NOT be returned —
// there is no asOf-consistent "as it existed then" read of a closed epoch.
//
// This test would FAIL if the predicate were relaxed to
// `(effective_until IS NULL OR effective_until > $2)` — that relaxed form would match the closed
// row below (effective_until is after asOf), flipping assertion 1 from ErrEntitlementNotFound to
// a found row. Relaxing the predicate that way would also require revisiting the LIMIT 1's
// determinism: migration 000112's partial unique index
// (uq_subscription_entitlements_active_user, WHERE effective_until IS NULL) only guarantees
// uniqueness for the OPEN row: with the relaxed predicate, an asOf that falls inside more than
// one closed epoch's [effective_from, effective_until) window would make LIMIT 1 pick an
// arbitrary one among them. Do NOT relax the predicate without addressing that.
func TestAsOfThreading_GetActiveEntitlement_ResidualWindow_ClosedAfterAsOfIsNotReturned(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	profileRepo := NewProfileRepository(pool, enc)
	ctx := context.Background()
	entRepo := NewEntitlementRepository(pool)

	asOf := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	effectiveFrom := asOf.Add(-30 * 24 * time.Hour) // well before asOf
	effectiveUntil := asOf.Add(1 * time.Hour)       // closed strictly AFTER asOf

	// Row 1: was open at asOf, closed afterwards. Must NOT be returned for this asOf.
	closedUser := createClaimForDeletionFixtureUser(t, ctx, pool, profileRepo,
		fmt.Sprintf("residual-closed-%d", time.Now().UnixNano()), false, nil, nil, nil)
	_, err := pool.Exec(ctx, `
		INSERT INTO subscription_entitlements (user_id, source, tier, status, effective_from, effective_until)
		VALUES ($1, 'own_subscription', 'premium', 'active', $2, $3)
	`, closedUser.ID, effectiveFrom, effectiveUntil)
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM subscription_entitlements WHERE user_id = $1`, closedUser.ID) //nolint:errcheck
	})

	// Row 2 (sibling, mandatory sanity check): same effective_from/asOf shape, but still open
	// (effective_until IS NULL). MUST be returned for the same asOf. Without this positive
	// assertion, assertion 1 passing could mean the fixture was never created, the user id was
	// wrong, or setup silently failed — a both-false pass that proves nothing.
	openUser := createClaimForDeletionFixtureUser(t, ctx, pool, profileRepo,
		fmt.Sprintf("residual-open-%d", time.Now().UnixNano()), false, nil, nil, nil)
	_, err = pool.Exec(ctx, `
		INSERT INTO subscription_entitlements (user_id, source, tier, status, effective_from, effective_until)
		VALUES ($1, 'own_subscription', 'premium', 'active', $2, NULL)
	`, openUser.ID, effectiveFrom)
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM subscription_entitlements WHERE user_id = $1`, openUser.ID) //nolint:errcheck
	})

	// Assertion 1: closed-after-asOf row is NOT returned.
	_, err = entRepo.GetActiveEntitlement(ctx, closedUser.ID, asOf)
	assert.ErrorIs(t, err, ErrEntitlementNotFound,
		"an entitlement open at asOf but closed strictly after asOf must NOT be returned — "+
			"effective_until IS NULL is a current-state predicate, not an asOf-relative one")

	// Assertion 2 (sanity): still-open row for the same asOf IS returned.
	ent, err := entRepo.GetActiveEntitlement(ctx, openUser.ID, asOf)
	require.NoError(t, err, "sanity: the still-open sibling row must be found for the same asOf")
	assert.True(t, ent != nil && ent.Tier == "premium", "sanity: found row must be the seeded fixture")
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
