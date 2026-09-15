//go:build integration

package repository

// Integration tests for JobHealthRepository.GetOldestExpiredCompGrantAge (#1652 todo 4, Part C).
//
// pgxmock (job_health_repository_test.go) cannot validate the aggregate SQL itself — MIN,
// COALESCE, EXTRACT(EPOCH FROM ...), the jsonb_array_elements lateral join, or the NOW()-minus-
// timestamp arithmetic — a real database is required to prove those. This is the RevenueCat
// analogue of the retired legacy-column gauge: it must climb only for comp_* grants that have
// actually lapsed and are still present in RC's stored projection, and must never count live
// grants, permanent (no-expiry) grants, or paid_*/trial_* grants.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestJobHealthRepository_Integration -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipIfCompGrantBaselineNonZero guards the "must be zero" assertions below against a shared dev DB
// that already has an unrelated expired comp grant sitting in it — GetOldestExpiredCompGrantAge
// intentionally scans the WHOLE revenuecat_customers table (it's a real health gauge, not scoped to
// one test's fixtures), so it is NOT isolated from other rows the way an ID-scoped assertion would
// be. Skips (does not fail) when the precondition doesn't hold.
func skipIfCompGrantBaselineNonZero(t *testing.T, repo JobHealthRepository, ctx context.Context) {
	t.Helper()
	if baseline, err := repo.GetOldestExpiredCompGrantAge(ctx); err == nil && baseline != 0 {
		t.Skipf("baseline age is already %s — another expired comp grant exists in this shared dev DB outside this test's control", baseline)
	}
}

// seedCompGrant inserts a revenuecat_customers row for userID carrying a single grant with the
// given key and expiry (nil = permanent). The user row must already exist (FK).
func seedCompGrant(t *testing.T, pool *pgxpool.Pool, userID, key string, expiresAt *time.Time) {
	t.Helper()
	grants := fmt.Sprintf(`[{"key":%q,"expires_at":%s}]`, key, jsonTime(expiresAt))
	_, err := pool.Exec(context.Background(),
		`INSERT INTO revenuecat_customers (user_id, grants, access_updated_at) VALUES ($1, $2::jsonb, clock_timestamp())`,
		userID, grants)
	require.NoError(t, err)
}

func jsonTime(t *time.Time) string {
	if t == nil {
		return "null"
	}
	return fmt.Sprintf("%q", t.UTC().Format(time.RFC3339))
}

// TestJobHealthRepository_Integration_NoExpiredCompGrants_ReturnsZero proves a healthy system (no
// expired comp grants at all) reports age zero — the gauge's steady state.
func TestJobHealthRepository_Integration_NoExpiredCompGrants_ReturnsZero(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewJobHealthRepository(pool)
	ctx := context.Background()

	skipIfCompGrantBaselineNonZero(t, repo, ctx)

	suffix := fmt.Sprintf("jh_rc_none_%d", time.Now().UnixNano())
	user := createRemoveTierOverrideUser(t, pool, enc, suffix, "premium", "active")
	future := time.Now().Add(30 * 24 * time.Hour)
	seedCompGrant(t, pool, user.ID, "comp_premium", &future)

	age, err := repo.GetOldestExpiredCompGrantAge(ctx)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), age, "a live comp grant must not be counted")
}

// TestJobHealthRepository_Integration_ExpiredCompGrant_ReportsApproxAge proves the gauge reports the
// actual elapsed time since the comp grant lapsed — the direct measure of how long the refresh path
// has been failing to drop it.
func TestJobHealthRepository_Integration_ExpiredCompGrant_ReportsApproxAge(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewJobHealthRepository(pool)
	ctx := context.Background()

	skipIfCompGrantBaselineNonZero(t, repo, ctx)

	suffix := fmt.Sprintf("jh_rc_5d_%d", time.Now().UnixNano())
	user := createRemoveTierOverrideUser(t, pool, enc, suffix, "premium", "inactive")
	expired := time.Now().Add(-5 * 24 * time.Hour)
	seedCompGrant(t, pool, user.ID, "comp_premium", &expired)

	age, err := repo.GetOldestExpiredCompGrantAge(ctx)
	require.NoError(t, err)

	assert.InDelta(t, (5 * 24 * time.Hour).Seconds(), age.Seconds(), 30,
		"age must reflect the actual time since the comp grant lapsed, within a 30s test-execution tolerance")
}

// TestJobHealthRepository_Integration_PermanentCompGrant_NotCounted proves a comp grant with no
// expiry (a permanent comp) must NOT contribute to the gauge — only LAPSED grants are the hazard.
func TestJobHealthRepository_Integration_PermanentCompGrant_NotCounted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewJobHealthRepository(pool)
	ctx := context.Background()
	skipIfCompGrantBaselineNonZero(t, repo, ctx)

	suffix := fmt.Sprintf("jh_rc_perm_%d", time.Now().UnixNano())
	user := createRemoveTierOverrideUser(t, pool, enc, suffix, "premium", "active")
	seedCompGrant(t, pool, user.ID, "comp_household_2", nil)

	age, err := repo.GetOldestExpiredCompGrantAge(ctx)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), age, "a permanent (no-expiry) comp grant must not be counted")
}

// TestJobHealthRepository_Integration_PaidAndTrialGrants_NotCounted proves the gauge is scoped to
// comp_* grants: a lapsed paid_* or trial_* grant is a normal subscription event, not this hazard.
func TestJobHealthRepository_Integration_PaidAndTrialGrants_NotCounted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewJobHealthRepository(pool)
	ctx := context.Background()
	skipIfCompGrantBaselineNonZero(t, repo, ctx)

	suffix := fmt.Sprintf("jh_rc_paid_%d", time.Now().UnixNano())
	user := createRemoveTierOverrideUser(t, pool, enc, suffix, "premium", "inactive")
	expired := time.Now().Add(-10 * 24 * time.Hour)
	seedCompGrant(t, pool, user.ID, "paid_premium", &expired)

	age, err := repo.GetOldestExpiredCompGrantAge(ctx)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), age, "a lapsed paid_* grant must not be counted")
}

// TestJobHealthRepository_Integration_OldestOfMultiple_PicksTheOldest proves that with several
// expired comp grants outstanding, the gauge reports the OLDEST one's age — the worst-case signal
// an alert should fire on — not an average or the newest.
func TestJobHealthRepository_Integration_OldestOfMultiple_PicksTheOldest(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewJobHealthRepository(pool)
	ctx := context.Background()

	suffix := fmt.Sprintf("jh_rc_multi_%d", time.Now().UnixNano())

	recentUser := createRemoveTierOverrideUser(t, pool, enc, suffix+"_recent", "premium", "inactive")
	recentEndsAt := time.Now().Add(-1 * time.Hour)
	seedCompGrant(t, pool, recentUser.ID, "comp_premium", &recentEndsAt)

	oldestUser := createRemoveTierOverrideUser(t, pool, enc, suffix+"_oldest", "premium", "inactive")
	oldestEndsAt := time.Now().Add(-10 * 24 * time.Hour)
	seedCompGrant(t, pool, oldestUser.ID, "comp_household_3", &oldestEndsAt)

	age, err := repo.GetOldestExpiredCompGrantAge(ctx)
	require.NoError(t, err)
	assert.InDelta(t, (10 * 24 * time.Hour).Seconds(), age.Seconds(), 30,
		"must report the OLDEST outstanding expired comp grant's age, not the most recent")
}