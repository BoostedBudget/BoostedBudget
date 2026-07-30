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
// already expired (Issue #1413 Critical finding C1, Codex review — must ALSO be excluded: a
// lapsed comp still delays retention until the daily expiry job clears the column and re-arms
// the clock, otherwise a user could be hard-deleted with no warning in the window between the
// retention-cleanup job and the override-expiry job — see
// TestRetentionQueries_Integration_ExpiredOverride_ExcludedUntilCleared for proof this is a
// delay, not a permanent exemption), and an ordinary never-comped user (must still be selected
// — proves the query is not a no-op).
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
// retention queries: BOTH the actively-comped user and the lapsed-comp user must be excluded
// (C1 fix — any non-empty override, live or expired, delays retention), while the ordinary user
// must still be selected.
func assertRetentionSelection(t *testing.T, users []*domain.User, fixtures retentionFixtureUsers) {
	t.Helper()
	ids := make(map[string]bool, len(users))
	for _, u := range users {
		ids[u.ID] = true
	}

	if ids[fixtures.activeComp.ID] {
		t.Errorf("actively comped user %s must be EXCLUDED from retention selection, but was returned", fixtures.activeComp.ID)
	}
	if ids[fixtures.expiredComp.ID] {
		t.Errorf("user %s with a LAPSED override must ALSO be EXCLUDED (Issue #1413 C1 fix) — an override, live or expired, always delays retention until the column is explicitly cleared, but was returned", fixtures.expiredComp.ID)
	}
	if !ids[fixtures.ordinaryUser.ID] {
		t.Errorf("ordinary never-comped user %s must still be selected — the query must not become a no-op", fixtures.ordinaryUser.ID)
	}
}

// TestGetUsersNeedingRetention30dNotice_Integration_ExcludesActiveComp verifies all three
// required outcomes for the 30-day notice query in one shot: exclude an actively comped user,
// ALSO exclude a user whose override has already lapsed but hasn't been cleared (Issue #1413 C1
// fix — noManualOverrideAtAllPredicate excludes on any override, live or expired), and still
// select an ordinary user.
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

// TestRetentionQueries_Integration_ExpiredOverride_ExcludedUntilCleared is the C1 negative-
// direction proof: exclusion of a lapsed comp must be a DELAY, not a permanent exemption. For
// each of the three retention queries, a user with an already-expired
// manual_tier_override/manual_tier_ends_at is first confirmed excluded, then the override
// columns are cleared (simulating the manual_tier_override_expiry_check job re-arming the
// clock), and the SAME query is run again — the user must now be selected. An implementation
// that accidentally excluded on retention_expires_at or some other sticky flag (rather than the
// override columns themselves) would fail this second assertion.
func TestRetentionQueries_Integration_ExpiredOverride_ExcludedUntilCleared(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name               string
		retentionExpiresAt time.Time
		query              func(context.Context) ([]*domain.User, error)
	}{
		{"30d_notice", time.Now().Add(15 * 24 * time.Hour), repo.GetUsersNeedingRetention30dNotice},
		{"7d_notice", time.Now().Add(3 * 24 * time.Hour), repo.GetUsersNeedingRetention7dNotice},
		{"expired_retention", time.Now().Add(-1 * time.Hour), repo.GetUsersWithExpiredRetention},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("c1delay_%s_%d", tt.name, time.Now().UnixNano())
			premium := "premium"
			expiredAt := time.Now().Add(-1 * time.Hour)
			user := createRetentionFixtureUser(t, ctx, pool, repo, suffix, tt.retentionExpiresAt, &premium, &expiredAt)

			before, err := tt.query(ctx)
			require.NoError(t, err)
			beforeIDs := make(map[string]bool, len(before))
			for _, u := range before {
				beforeIDs[u.ID] = true
			}
			if beforeIDs[user.ID] {
				t.Fatalf("user %s with a LAPSED override must be excluded before the column is cleared", user.ID)
			}

			// Simulate the expiry job clearing the lapsed override.
			_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = NULL, manual_tier_ends_at = NULL WHERE id = $1`, user.ID)
			require.NoError(t, err)

			after, err := tt.query(ctx)
			require.NoError(t, err)
			afterIDs := make(map[string]bool, len(after))
			for _, u := range after {
				afterIDs[u.ID] = true
			}
			if !afterIDs[user.ID] {
				t.Errorf("user %s must be selected once the override column is cleared — exclusion must be a delay, not a permanent exemption", user.ID)
			}
		})
	}
}

// TestRetentionQueries_Integration_MalformedOverride_StillExcluded is the "opposite pull" half
// of the Issue #1413 G2 required test pair (see profile_repository_test.go's
// TestGetEffectiveSubscriptionStatusByUserID_MalformedOverride_DoesNotGrantActive for the other
// half, which proves the SAME override value denies access): a user whose manual_tier_override
// names NO configured tier at all ('not_a_tier' — garbage, not a real tier name) must still be
// EXCLUDED from every retention/deletion query, exactly as a validly-tiered override would be.
// G2 only changes the ACCESS axis (GetEffectiveSubscriptionStatusByUserID) —
// noManualOverrideAtAllPredicate must keep treating ANY non-empty override, garbage or not, as
// "hands off". If a future change made this predicate tier-aware too, a malformed value in this
// single unconstrained VARCHAR(50) column would silently flip from "denied write access" (safe)
// to "eligible for hard deletion" (data loss) — the exact over-correction this test guards
// against.
func TestRetentionQueries_Integration_MalformedOverride_StillExcluded(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name               string
		retentionExpiresAt time.Time
		query              func(context.Context) ([]*domain.User, error)
	}{
		{"30d_notice", time.Now().Add(15 * 24 * time.Hour), repo.GetUsersNeedingRetention30dNotice},
		{"7d_notice", time.Now().Add(3 * 24 * time.Hour), repo.GetUsersNeedingRetention7dNotice},
		{"expired_retention", time.Now().Add(-1 * time.Hour), repo.GetUsersWithExpiredRetention},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("malformed_%s_%d", tt.name, time.Now().UnixNano())
			garbage := "not_a_tier"
			user := createRetentionFixtureUser(t, ctx, pool, repo, suffix, tt.retentionExpiresAt, &garbage, nil)

			users, err := tt.query(ctx)
			require.NoError(t, err)

			for _, u := range users {
				if u.ID == user.ID {
					t.Fatalf("user %s with a malformed manual_tier_override (%q, names no configured "+
						"tier) must be EXCLUDED from retention/deletion selection — the retention "+
						"predicate must treat ANY non-empty override as hands-off, valid tier name or "+
						"not (Issue #1413 G2)", user.ID, garbage)
				}
			}
		})
	}
}

// TestNoActiveManualOverridePredicate_Integration_ParityWithGo is the point of Part B: the
// override predicate now has two independent expressions of the SAME rule — Go
// (domain.User.HasActiveManualOverride, used for row-level reads) and SQL
// (noActiveManualOverridePredicate, used for these set-based queries). This test inserts the
// full 3x3 fixture matrix from the plan (manual_tier_override: NULL/”/'premium' x
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

// Why there is no companion test here that writes a nanosecond-precision manual_tier_ends_at
// through AdminUserRepository.UpdateUserTier, reads it back, and asserts
// domain.User.hasActiveManualOverrideAt(now) agrees with noActiveManualOverridePredicate at that
// SAME instant (Issue #1413 follow-up, Task JE — "close the expiry-tie gap"): it was attempted
// and found NOT to discriminate, by both proof and experiment, so it was not added. Documenting
// this so a future reader does not mistake the absence for an oversight and try to re-add it.
//
// The clock-fencing rule this kind of test must follow (learned the hard way — comparing a
// Go-side time.Now() against a timestamp Postgres wrote failed CI once, on ff2cc4bb, because CI
// runs Postgres in a separate container whose clock can lag the runner's) is: the comparison
// instant fed to BOTH predicates must come from a single `SELECT NOW()` against the same pool,
// exactly as txNow is used above.
//
// Any instant sourced that way is necessarily microsecond-precision — that is what timestamptz
// stores, and it is all `SELECT NOW()` can return. Experimentally, Postgres/pgx TRUNCATE
// (floor), not round, when writing a nanosecond-precision Go time.Time into a timestamptz column
// (verified directly against this database for offsets of -999ns..+999ns around a
// Postgres-sourced instant N: the stored, floored value and N agreed on `>=` in every single
// case). Flooring is monotonic non-increasing (floor(E) <= E for all E), so for any
// microsecond-aligned N: E >= N implies floor(E) >= N, and E < N implies floor(E) < N — the
// ordering relative to N can never flip. So for any nanosecond-precision expiresAt, comparing
// the RAW (untruncated) value against N and comparing its Postgres-truncated persisted form
// against that SAME N are mathematically guaranteed to agree, with or without the
// AdminUserService.UpdateUserTier truncation fix (admin_user_service.go).
//
// The gap the truncation fix actually closes is not "these two comparisons can disagree against
// a shared instant" (they provably cannot — see above) — it is that, absent the fix,
// UpdateUserTier's Clerk-sync scratch user and the row it persists silently carried two
// DIFFERENT time values (nanosecond vs microsecond) for what is supposed to be the exact same
// instant. That is worth fixing on its own (and trivial to fix), but no single-shared-instant
// agreement test — the only kind that is not itself flaky — can observe it, so none is included
// here. See TestAdminUserService_UpdateUserTier_TruncatesExpiresAtToMicrosecond
// (admin_user_service_test.go) for the regression test that actually exercises this fix, by
// asserting on the exact value handed to the repository rather than on predicate agreement.
