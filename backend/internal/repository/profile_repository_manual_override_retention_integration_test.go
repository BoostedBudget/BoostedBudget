//go:build integration

package repository

// Integration tests for Issue #1413 Part A/B: excluding actively-comped users from the
// retention-notice/hard-delete queries, and proving the SQL exclusion predicate
// (noActiveManualOverridePredicate) agrees with domain.User.HasActiveManualOverride() row
// for row. pgxmock cannot validate SQL semantics (it only matches query text), so a wrong
// predicate, a wrong boundary operator, or a type error would pass silently without a real
// database — hence these tests require a live Postgres.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run 'TestGetUsersNeedingRetention|TestGetUsersWithExpiredRetention_Integration|TestNoActiveManualOverridePredicate' -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// retentionFixtureUsers holds the three canonical fixtures used by every retention-query
// exclusion test: a user with an active admin comp (must be excluded), a user whose comp has
// already expired (must still be selected — the negative case that catches an over-broad
// predicate that permanently exempts anyone ever comped), and an ordinary never-comped user
// (must still be selected — proves the query is not a no-op).
type retentionFixtureUsers struct {
	activeComp   *domain.User
	expiredComp  *domain.User
	ordinaryUser *domain.User
}

// createRetentionFixtureUser inserts a user row satisfying the shared retention-query
// preconditions (subscription_status IN ('inactive','cancelled'), is_deleted = FALSE,
// deletion_requested_at IS NULL, no notice sent yet) with the given retention_expires_at and
// manual-override columns, and registers cleanup.
func createRetentionFixtureUser(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repo ProfileRepository,
	suffix string,
	retentionExpiresAt time.Time,
	manualTierOverride *string,
	manualTierEndsAt *time.Time,
) *domain.User {
	t.Helper()

	user := &domain.User{
		ClerkUserID:        "clerk_retention_" + suffix,
		Email:              "retention-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck

	_, err = pool.Exec(ctx, `
		UPDATE users
		SET retention_expires_at = $1,
		    manual_tier_override = $2,
		    manual_tier_ends_at = $3
		WHERE id = $4`,
		retentionExpiresAt, manualTierOverride, manualTierEndsAt, user.ID,
	)
	require.NoError(t, err)

	user.RetentionExpiresAt = &retentionExpiresAt
	user.ManualTierOverride = manualTierOverride
	user.ManualTierEndsAt = manualTierEndsAt
	return user
}

// setupRetentionFixtures creates the three canonical fixtures (active comp, expired comp,
// ordinary) all sharing the same retention_expires_at, so a single call to the retention
// query under test can be asserted against all three at once.
func setupRetentionFixtures(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo ProfileRepository, namePrefix string, retentionExpiresAt time.Time) retentionFixtureUsers {
	t.Helper()
	suffix := fmt.Sprintf("%s_%d", namePrefix, time.Now().UnixNano())

	premiumTier := "premium"
	expiredAt := time.Now().Add(-1 * time.Hour)

	activeComp := createRetentionFixtureUser(t, ctx, pool, repo, suffix+"_active", retentionExpiresAt, &premiumTier, nil)
	expiredComp := createRetentionFixtureUser(t, ctx, pool, repo, suffix+"_expired", retentionExpiresAt, &premiumTier, &expiredAt)
	ordinaryUser := createRetentionFixtureUser(t, ctx, pool, repo, suffix+"_ordinary", retentionExpiresAt, nil, nil)

	return retentionFixtureUsers{activeComp: activeComp, expiredComp: expiredComp, ordinaryUser: ordinaryUser}
}

// assertRetentionSelection asserts the standard three-way outcome shared by all three
// retention queries: the actively-comped user must be excluded, while the expired-comp user
// and the ordinary user must both still be selected.
func assertRetentionSelection(t *testing.T, users []*domain.User, fixtures retentionFixtureUsers) {
	t.Helper()
	ids := make(map[string]bool, len(users))
	for _, u := range users {
		ids[u.ID] = true
	}

	if ids[fixtures.activeComp.ID] {
		t.Errorf("actively comped user %s must be EXCLUDED from retention selection, but was returned", fixtures.activeComp.ID)
	}
	if !ids[fixtures.expiredComp.ID] {
		t.Errorf("user %s with an EXPIRED override must still be selected (negative case for an over-broad predicate), but was excluded", fixtures.expiredComp.ID)
	}
	if !ids[fixtures.ordinaryUser.ID] {
		t.Errorf("ordinary never-comped user %s must still be selected — the query must not become a no-op", fixtures.ordinaryUser.ID)
	}
}

// TestGetUsersNeedingRetention30dNotice_Integration_ExcludesActiveComp verifies all three
// required outcomes for the 30-day notice query in one shot: exclude an actively comped user,
// still select a user whose override has expired, and still select an ordinary user.
func TestGetUsersNeedingRetention30dNotice_Integration_ExcludesActiveComp(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	// Within the 30-day notice window: NOW() < retention_expires_at <= NOW()+30d.
	retentionExpiresAt := time.Now().Add(15 * 24 * time.Hour)
	fixtures := setupRetentionFixtures(t, ctx, pool, repo, "r30", retentionExpiresAt)

	users, err := repo.GetUsersNeedingRetention30dNotice(ctx)
	require.NoError(t, err)

	assertRetentionSelection(t, users, fixtures)
}

// TestGetUsersNeedingRetention7dNotice_Integration_ExcludesActiveComp mirrors the 30-day test
// for the 7-day notice query.
func TestGetUsersNeedingRetention7dNotice_Integration_ExcludesActiveComp(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	// Within the 7-day notice window: NOW() < retention_expires_at <= NOW()+7d.
	retentionExpiresAt := time.Now().Add(3 * 24 * time.Hour)
	fixtures := setupRetentionFixtures(t, ctx, pool, repo, "r7", retentionExpiresAt)

	users, err := repo.GetUsersNeedingRetention7dNotice(ctx)
	require.NoError(t, err)

	assertRetentionSelection(t, users, fixtures)
}

// TestGetUsersWithExpiredRetention_Integration_ExcludesActiveComp mirrors the above for the
// hard-delete candidate query (retention_expires_at already in the past).
func TestGetUsersWithExpiredRetention_Integration_ExcludesActiveComp(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(-1 * time.Hour) // already expired
	fixtures := setupRetentionFixtures(t, ctx, pool, repo, "rexp", retentionExpiresAt)

	users, err := repo.GetUsersWithExpiredRetention(ctx)
	require.NoError(t, err)

	assertRetentionSelection(t, users, fixtures)
}

// TestNoActiveManualOverridePredicate_Integration_ParityWithGo is the point of Part B: the
// override predicate now has two independent expressions of the SAME rule — Go
// (domain.User.HasActiveManualOverride, used for row-level reads) and SQL
// (noActiveManualOverridePredicate, used for these set-based queries). This test inserts the
// full 3x3 fixture matrix from the plan (manual_tier_override: NULL/''/'premium' x
// manual_tier_ends_at: NULL/past/future) and asserts the SQL predicate's verdict equals the Go
// predicate's verdict for every one of the 9 rows. This is the guard against the two
// definitions drifting apart in a future edit.
func TestNoActiveManualOverridePredicate_Integration_ParityWithGo(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	past := time.Now().Add(-1 * time.Hour)
	future := time.Now().Add(1 * time.Hour)
	premium := "premium"
	empty := ""

	overrideValues := []*string{nil, &empty, &premium}
	endsAtValues := []*time.Time{nil, &past, &future}

	type row struct {
		id   string
		user *domain.User
	}
	var rows []row
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	for oi, override := range overrideValues {
		for ei, endsAt := range endsAtValues {
			id := fmt.Sprintf("parity_%s_o%d_e%d", suffix, oi, ei)
			user := &domain.User{
				ClerkUserID:        "clerk_" + id,
				Email:              id + "@example.com",
				SubscriptionTier:   "premium",
				SubscriptionStatus: "cancelled",
				PrimaryRegion:      "iad",
				IsActive:           true,
				ManualTierOverride: override,
				ManualTierEndsAt:   endsAt,
			}
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			require.NoError(t, repo.CreateUserTx(ctx, tx, user))
			require.NoError(t, tx.Commit(ctx))
			userID := user.ID
			t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID) }) //nolint:errcheck

			_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = $1, manual_tier_ends_at = $2 WHERE id = $3`,
				override, endsAt, user.ID)
			require.NoError(t, err)

			rows = append(rows, row{id: id, user: user})
		}
	}

	require.Len(t, rows, 9, "the plan specifies a 3x3 = 9 combination matrix")

	// Ask the SQL predicate, for every inserted row, which ids it considers "no active
	// override" (i.e. still eligible for retention selection). This runs the EXACT
	// production constant, not a hand-copied re-statement of it.
	ids := make([]string, len(rows))
	byID := make(map[string]row, len(rows))
	for i, r := range rows {
		ids[i] = r.user.ID
		byID[r.user.ID] = r
	}

	sqlRows, err := pool.Query(ctx, `SELECT id FROM users WHERE id = ANY($1) `+noActiveManualOverridePredicate, ids)
	require.NoError(t, err)
	sqlNoActiveOverride := make(map[string]bool, len(rows))
	for sqlRows.Next() {
		var id string
		require.NoError(t, sqlRows.Scan(&id))
		sqlNoActiveOverride[id] = true
	}
	require.NoError(t, sqlRows.Err())
	sqlRows.Close()

	for _, r := range rows {
		goSaysActive := r.user.HasActiveManualOverride()
		sqlSaysNoActiveOverride := sqlNoActiveOverride[r.user.ID]

		// SQL predicate is phrased as "NOT (active override)", so it agrees with Go's
		// HasActiveManualOverride when sqlSaysNoActiveOverride == !goSaysActive.
		if sqlSaysNoActiveOverride == goSaysActive {
			overrideDesc := "NULL"
			if r.user.ManualTierOverride != nil {
				overrideDesc = fmt.Sprintf("%q", *r.user.ManualTierOverride)
			}
			endsAtDesc := "NULL"
			if r.user.ManualTierEndsAt != nil {
				endsAtDesc = r.user.ManualTierEndsAt.String()
			}
			t.Errorf("parity mismatch for override=%s ends_at=%s: Go.HasActiveManualOverride()=%v, SQL noActiveManualOverridePredicate says no-active-override=%v (want !%v)",
				overrideDesc, endsAtDesc, goSaysActive, sqlSaysNoActiveOverride, goSaysActive)
		}
	}
}

// TestNoActiveManualOverridePredicate_Integration_ExactExpiryBoundary directly exercises the
// >= vs > boundary the plan calls out: manual_tier_ends_at exactly equal to the instant the
// predicate evaluates NOW() at. The 3x3 parity matrix above only uses +/-1 hour offsets, which
// can never disagree between >= and > (a value strictly in the past or strictly in the future
// evaluates identically under either operator) — only an exact tie can. To force a real tie
// deterministically (immune to clock drift), this test reads Postgres's own NOW() and re-uses
// that exact value as manual_tier_ends_at inside the SAME transaction, where NOW() is stable
// for the whole transaction, then evaluates the production predicate in that same transaction.
//
// Note this test deliberately does NOT also call domain.User.HasActiveManualOverride() here
// and assert agreement — that method always reads a fresh time.Now() internally with no way
// to pin it to txNow, so by the time it is called (after the DB round-trip) real wall-clock
// time has already moved past txNow and it would report "expired" regardless of which SQL
// operator is used. That would make this test meaningless as a parity check. The Go-side
// exact-boundary behavior (that `!ends_at.Before(now)` is true when ends_at == now) is instead
// proven directly against the unexported, now-injectable hasActiveManualOverrideAt(now) in
// domain/user_test.go's TestHasActiveManualOverrideAt_BoundarySemantics. This test's job is
// narrower: prove the SQL predicate, in isolation, treats an exact tie as an active override
// (i.e. behaves as `>=`, not `>`) — the same rule domain/user_test.go proves for Go.
func TestNoActiveManualOverridePredicate_Integration_ExactExpiryBoundary(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("boundary_%d", time.Now().UnixNano())
	premium := "premium"
	user := &domain.User{
		ClerkUserID:        "clerk_" + suffix,
		Email:              suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
		ManualTierOverride: &premium,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck

	boundaryTx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer boundaryTx.Rollback(ctx) //nolint:errcheck

	var txNow time.Time
	require.NoError(t, boundaryTx.QueryRow(ctx, "SELECT NOW()").Scan(&txNow))

	// CreateUserTx does not persist manual_tier_override/manual_tier_ends_at (they are set via
	// a dedicated UPDATE, same as every other fixture in this file), so both are set here.
	_, err = boundaryTx.Exec(ctx, `UPDATE users SET manual_tier_override = $1, manual_tier_ends_at = $2 WHERE id = $3`, premium, txNow, user.ID)
	require.NoError(t, err)

	// Evaluate the exact production predicate in the SAME transaction, so its NOW() call
	// returns the identical transaction-start timestamp as txNow above — a guaranteed tie.
	var excludedFromRetention bool
	err = boundaryTx.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM users WHERE id = $1 `+noActiveManualOverridePredicate+`)`, user.ID).Scan(&excludedFromRetention)
	require.NoError(t, err)

	require.True(t, excludedFromRetention, "SQL predicate must treat manual_tier_ends_at == NOW() as an active override and exclude the user (inclusive '>=' boundary, matching Go's !ends_at.Before(now)); a '>' operator would wrongly let this row through")
}
