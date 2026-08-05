//go:build integration

package repository

// Integration tests for JobHealthRepository.GetOldestUnclearedExpiredManualTierOverrideAge
// (Issue #1413 Task P4).
//
// pgxmock (job_health_repository_test.go) cannot validate the aggregate SQL itself — MIN,
// COALESCE, EXTRACT(EPOCH FROM ...), or the NOW()-minus-timestamp arithmetic — a real database is
// required to prove those. This is the direct measure of the C1 hazard: the gauge must climb only
// for overrides that are actually expired-and-uncleared, and must never count live overrides or
// NULL/empty ones.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestJobHealthRepository_Integration -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipIfBaselineNonZero guards the "must be zero" assertions below against a shared dev DB that
// already has an unrelated expired-uncleared override sitting in it (a stray leftover fixture, or
// another test/process touching the same `users` table concurrently) — GetOldestUnclearedExpired
// ManualTierOverrideAge intentionally scans the WHOLE table (it's a real health gauge, not scoped
// to one test's fixtures), so it is NOT isolated from other rows the way an ID-scoped assertion
// would be. Skips (does not fail) when the precondition doesn't hold; the query's actual
// correctness is still proven by the non-zero-expectation tests in this file regardless.
func skipIfBaselineNonZero(t *testing.T, repo JobHealthRepository, ctx context.Context) {
	t.Helper()
	if baseline, err := repo.GetOldestUnclearedExpiredManualTierOverrideAge(ctx); err == nil && baseline != 0 {
		t.Skipf("baseline age is already %s — another expired-uncleared override exists in this shared dev DB outside this test's control", baseline)
	}
}

// TestJobHealthRepository_Integration_NoExpiredOverrides_ReturnsZero proves a healthy system (no
// expired-uncleared overrides at all) reports age zero — the gauge's steady state.
func TestJobHealthRepository_Integration_NoExpiredOverrides_ReturnsZero(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewJobHealthRepository(pool)
	ctx := context.Background()

	skipIfBaselineNonZero(t, repo, ctx)

	suffix := fmt.Sprintf("jh_none_%d", time.Now().UnixNano())
	createRemoveTierOverrideUser(t, pool, enc, suffix, "premium", "active")
	// No override set at all — the baseline "nothing to see" case.

	age, err := repo.GetOldestUnclearedExpiredManualTierOverrideAge(ctx)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), age, "no expired-uncleared overrides anywhere must report age zero")
}

// TestJobHealthRepository_Integration_FiveDayOldExpiredOverride_ReportsApproxFiveDays proves the
// gauge reports the actual elapsed time since the override lapsed — the direct measure of how
// long manual_tier_override_expiry_check has been failing to clear it.
func TestJobHealthRepository_Integration_FiveDayOldExpiredOverride_ReportsApproxFiveDays(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewJobHealthRepository(pool)
	ctx := context.Background()

	suffix := fmt.Sprintf("jh_5d_%d", time.Now().UnixNano())
	user := createRemoveTierOverrideUser(t, pool, enc, suffix, "premium", "cancelled")

	override := "household_2"
	endsAt := time.Now().Add(-5 * 24 * time.Hour)
	setManualOverride(t, pool, user.ID, &override, &endsAt)

	age, err := repo.GetOldestUnclearedExpiredManualTierOverrideAge(ctx)
	require.NoError(t, err)

	assert.InDelta(t, (5 * 24 * time.Hour).Seconds(), age.Seconds(), 30,
		"age must reflect the actual time since manual_tier_ends_at lapsed, within a 30s test-execution tolerance")
}

// TestJobHealthRepository_Integration_LiveOverride_NotCounted proves a future manual_tier_ends_at
// (a comp that is still active) must NOT contribute to the gauge — only LAPSED overrides are the
// hazard this metric tracks; an active comp is working as designed.
//
// Falsification note: this test alone cannot fully isolate the predicate's `manual_tier_ends_at <
// NOW()` clause specifically — with only one (live) row in play, removing that clause makes MIN()
// return a FUTURE timestamp, so NOW() - MIN(...) goes negative and the repository's own defensive
// `if ageSeconds < 0 { ageSeconds = 0 }` clamp (job_health_repository.go) coincidentally still
// yields 0, masking the regression at this black-box level. The `< NOW()` clause itself is
// covered independently and more directly (row-selection, not aggregate math) by
// TestGetUsersWithExpiredManualTierOverride_Integration_SelectsExpiredExcludesLiveAndDeleted in
// profile_repository_manual_tier_expiry_integration_test.go — this repository calls the exact
// same expiredManualTierOverridePredicate constant, not a copy, so that coverage transfers here.
// This test still catches other regressions (e.g. dropping the `manual_tier_ends_at IS NOT NULL`
// guard for permanent overrides) that don't share the clamp's blind spot.
func TestJobHealthRepository_Integration_LiveOverride_NotCounted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewJobHealthRepository(pool)
	ctx := context.Background()
	skipIfBaselineNonZero(t, repo, ctx)

	suffix := fmt.Sprintf("jh_live_%d", time.Now().UnixNano())
	user := createRemoveTierOverrideUser(t, pool, enc, suffix, "premium", "active")

	override := "household_2"
	endsAt := time.Now().Add(30 * 24 * time.Hour) // still live
	setManualOverride(t, pool, user.ID, &override, &endsAt)

	age, err := repo.GetOldestUnclearedExpiredManualTierOverrideAge(ctx)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), age, "a live (not-yet-expired) override must not be counted")
}

// TestJobHealthRepository_Integration_NullAndEmptyOverrides_NotCounted proves NULL
// manual_tier_override (the common case — most users never had a comp) and an empty-string
// override are both excluded, matching expiredManualTierOverridePredicate's own
// `IS NOT NULL AND <> ''` guard.
func TestJobHealthRepository_Integration_NullAndEmptyOverrides_NotCounted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewJobHealthRepository(pool)
	ctx := context.Background()
	skipIfBaselineNonZero(t, repo, ctx)

	suffix := fmt.Sprintf("jh_nullempty_%d", time.Now().UnixNano())

	// NULL override, NULL ends_at (never had a comp).
	createRemoveTierOverrideUser(t, pool, enc, suffix+"_null", "premium", "active")

	// Empty-string override with a PAST ends_at — must still be excluded by `<> ''`.
	emptyUser := createRemoveTierOverrideUser(t, pool, enc, suffix+"_empty", "premium", "active")
	empty := ""
	pastEndsAt := time.Now().Add(-1 * time.Hour)
	setManualOverride(t, pool, emptyUser.ID, &empty, &pastEndsAt)

	age, err := repo.GetOldestUnclearedExpiredManualTierOverrideAge(ctx)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), age, "NULL and empty-string overrides must never be counted, even with a past ends_at")
}

// TestJobHealthRepository_Integration_OldestOfMultiple_PicksTheOldest proves that with several
// expired-uncleared overrides outstanding, the gauge reports the OLDEST one's age — the worst-case
// signal an alert should fire on — not an average or the newest.
func TestJobHealthRepository_Integration_OldestOfMultiple_PicksTheOldest(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewJobHealthRepository(pool)
	ctx := context.Background()

	suffix := fmt.Sprintf("jh_multi_%d", time.Now().UnixNano())
	override := "household_2"

	recentUser := createRemoveTierOverrideUser(t, pool, enc, suffix+"_recent", "premium", "cancelled")
	recentEndsAt := time.Now().Add(-1 * time.Hour)
	setManualOverride(t, pool, recentUser.ID, &override, &recentEndsAt)

	oldestUser := createRemoveTierOverrideUser(t, pool, enc, suffix+"_oldest", "premium", "cancelled")
	oldestEndsAt := time.Now().Add(-10 * 24 * time.Hour)
	setManualOverride(t, pool, oldestUser.ID, &override, &oldestEndsAt)

	age, err := repo.GetOldestUnclearedExpiredManualTierOverrideAge(ctx)
	require.NoError(t, err)
	assert.InDelta(t, (10 * 24 * time.Hour).Seconds(), age.Seconds(), 30,
		"must report the OLDEST outstanding expired override's age, not the most recent")
}
