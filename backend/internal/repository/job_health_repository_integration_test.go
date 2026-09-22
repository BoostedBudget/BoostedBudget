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

// seedInvestmentsSkippedUser inserts a minimal users row with a FRESH gen_random_uuid() id and
// registers a cleanup that deletes it. The linked_items FK (fk_linked_items_user_id, ON DELETE
// CASCADE) removes every seeded linked_items row with it, so each run leaves no residue behind —
// which is what lets this test re-run against the same shared freshcheck scratch DB without a
// unique-key collision (a fixed item_id would collide on the UNIQUE (user_id, item_id) constraint
// only if the user_id repeated too, but a fresh user_id per run keeps both sides fresh).
//
// insertTestUser is NOT usable here: it lives in package repository_test, not package repository.
// This mirrors the in-package INSERT pattern already used across this package (e.g.
// merchant_repository_integration_test.go's seedMerchantFixture).
func seedInvestmentsSkippedUser(t *testing.T, pool *pgxpool.Pool, suffix string) string {
	t.Helper()
	var userID string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES (gen_random_uuid(), $1, $2, 'premium', 'active', 'iad')
		RETURNING id
	`, "clerk_isl_"+suffix, "isl-"+suffix+"@integration.example").Scan(&userID)
	require.NoError(t, err, "seed user")

	t.Cleanup(func() {
		// Best-effort: the FK cascade removes the linked_items rows; the test must still pass if
		// the pool is already closed, so the error is deliberately ignored.
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	})
	return userID
}

// seedInvestmentsSkippedLinkedItem inserts a linked_items row for userID with the given status and
// investments_skipped_at_link value. The column is written DIRECTLY in SQL — never through a
// production path — because this test is the authoritative home for the live-scoping predicate,
// which only a real database can evaluate (pgxmock has no query engine and returns a canned row).
//
// status = "replaced" also stamps replaced_at, satisfying the linked_items_replaced_at_consistency
// CHECK constraint (migration 000032) that requires replaced_at IS NOT NULL whenever status is
// 'replaced'. The other three shapes need no extra column: status defaults to 'active' and the
// three NOT NULL no-default columns (user_id, item_id, access_token_encrypted) are all supplied.
func seedInvestmentsSkippedLinkedItem(t *testing.T, pool *pgxpool.Pool, userID, itemID, status string, skipped bool) {
	t.Helper()
	var replacedAt any
	if status == "replaced" {
		replacedAt = time.Now()
	}
	_, err := pool.Exec(context.Background(), `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, status, replaced_at, investments_skipped_at_link)
		VALUES (gen_random_uuid(), $1, $2, 'enc-test-token', $3, $4, $5)
	`, userID, itemID, status, replacedAt, skipped)
	require.NoError(t, err, "seed linked_item")
}

// TestJobHealthRepository_Integration_InvestmentsSkippedAtLinkCount is the AUTHORITATIVE home for
// the live-scoping decision behind GetInvestmentsSkippedAtLinkCount (#1707). pgxmock
// (job_health_repository_test.go) can only pin the emitted SQL SHAPE — it has no query engine and
// returns a canned row — so the `status NOT IN ('replaced', 'disconnected')` predicate is
// deliberately NOT asserted there. It is asserted HERE, against real Postgres, by seeding all four
// shapes and proving only the live-TRUE row moves the count.
//
// ⚠ THE QUERY IS TABLE-WIDE and freshcheck is a shared scratch DB, so an absolute count is NOT
// assertable: the baseline is read BEFORE seeding and the assertion is on the DELTA. The sibling
// skipIfCompGrantBaselineNonZero guard is deliberately NOT used — this plan requires a
// NON-skipping run (a SKIP here would be a silent hole, not a pass) — and t.Cleanup removes the
// seeded rows instead.
func TestJobHealthRepository_Integration_InvestmentsSkippedAtLinkCount(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewJobHealthRepository(pool)
	ctx := context.Background()

	t.Run("counts_only_live_true_rows", func(t *testing.T) {
		baseline, err := repo.GetInvestmentsSkippedAtLinkCount(ctx)
		require.NoError(t, err, "baseline read must succeed")

		suffix := fmt.Sprintf("isl_count_%d", time.Now().UnixNano())
		userID := seedInvestmentsSkippedUser(t, pool, suffix)

		// All four shapes. Exactly ONE (the first) is live-TRUE and must be counted.
		seedInvestmentsSkippedLinkedItem(t, pool, userID, "item-isl-active-true-"+suffix, "active", true)
		seedInvestmentsSkippedLinkedItem(t, pool, userID, "item-isl-replaced-true-"+suffix, "replaced", true)
		seedInvestmentsSkippedLinkedItem(t, pool, userID, "item-isl-disconnected-true-"+suffix, "disconnected", true)
		seedInvestmentsSkippedLinkedItem(t, pool, userID, "item-isl-active-false-"+suffix, "active", false)

		count, err := repo.GetInvestmentsSkippedAtLinkCount(ctx)
		require.NoError(t, err)

		// NEGATIVE CONTROL for the live-scoping decision: THREE TRUE rows are seeded, so a query
		// that dropped `status NOT IN ('replaced','disconnected')` would report baseline+3 and
		// fail here. A query that dropped `= TRUE` would additionally count the FALSE row
		// (baseline+4). Only baseline+1 is correct.
		assert.Equal(t, baseline+1, count,
			"only the one live-TRUE row must be counted: the replaced TRUE and disconnected TRUE "+
				"rows are excluded by the live-scoping predicate and the active FALSE row by the "+
				"TRUE-only predicate")
	})

	t.Run("verified_zero_when_no_live_true_rows", func(t *testing.T) {
		baseline, err := repo.GetInvestmentsSkippedAtLinkCount(ctx)
		require.NoError(t, err, "baseline read must succeed")

		suffix := fmt.Sprintf("isl_zero_%d", time.Now().UnixNano())
		userID := seedInvestmentsSkippedUser(t, pool, suffix)

		// Only excluded shapes: no live-TRUE row at all.
		seedInvestmentsSkippedLinkedItem(t, pool, userID, "item-isl-zero-replaced-"+suffix, "replaced", true)
		seedInvestmentsSkippedLinkedItem(t, pool, userID, "item-isl-zero-disconnected-"+suffix, "disconnected", true)
		seedInvestmentsSkippedLinkedItem(t, pool, userID, "item-isl-zero-active-false-"+suffix, "active", false)

		count, err := repo.GetInvestmentsSkippedAtLinkCount(ctx)
		require.NoError(t, err, "a zero delta is a verified zero, not an error")
		assert.Equal(t, baseline, count,
			"with no live-TRUE row seeded the count must be unchanged — a verified zero delta "+
				"returned without an error")
	})
}