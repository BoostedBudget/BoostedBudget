// household_share_asof_integration_test.go — DB-backed behaviour for #1569 Task 15 (T7):
// the household share grant as a HALF-OPEN INTERVAL [shared_at, revoked_at) and the
// point-in-time household read that reads it.
//
// The product decision this pins: a member leaves; the remaining members keep what was
// shared — FOR THE SHARED PERIOD ONLY. They must not gain visibility of anything after the
// member left, and must not lose visibility of the period when sharing was live.
//
// Run:
//
//	DATABASE_URL="postgres://..." go test -tags integration -p 1 ./internal/repository/ -run TestHouseholdShareAsOf -v
package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// revokeShareAt closes the currently-open grant for (household, user_account) at exactly
// `at`, the way member removal does (RevokeSharesForUser sets revoked_at = now()) but with
// a deterministic timestamp so the interval boundary can be asserted.
func revokeShareAt(t *testing.T, pool *pgxpool.Pool, householdID, userAccountID string, at time.Time) {
	t.Helper()
	ct, err := pool.Exec(context.Background(), `
		UPDATE household_account_shares SET revoked_at = $1, updated_at = $1
		WHERE household_id = $2 AND user_account_id = $3 AND revoked_at IS NULL
	`, at, householdID, userAccountID)
	require.NoError(t, err, "revoke share at fixed instant")
	require.EqualValues(t, 1, ct.RowsAffected(), "exactly one open grant must have been closed")
}

// departedMemberFixture builds: owner + member in household hh, member's account shared
// into hh at sharedAt and revoked (member left) at revokedAt, with one balance snapshot for
// the member's account at snapshotAt. The returned scope carries ONLY the owner in
// MemberUserIDs — which is what the service layer supplies once the member has left.
func departedMemberFixture(t *testing.T, pool *pgxpool.Pool, sharedAt, revokedAt, snapshotAt time.Time, balance float64) (owner string, hh string, scope *domain.HouseholdScope) {
	t.Helper()
	owner = insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh = insertTestHousehold(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, sharedAt)
	revokeShareAt(t, pool, hh, memberAcct.UserAccountID, revokedAt)
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, balance, snapshotAt)

	return owner, hh, &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}}
}

// TestHouseholdShareAsOf_InsideSharedWindow_DepartedMemberStillVisible is direction 1 of the
// product decision: an as-of read for an instant INSIDE [shared_at, revoked_at) must still
// see what was shared, even though the member has since left and the grant is revoked today.
func TestHouseholdShareAsOf_InsideSharedWindow_DepartedMemberStillVisible(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	now := time.Now().UTC()
	sharedAt := now.AddDate(0, 0, -60)
	revokedAt := now.AddDate(0, 0, -10)
	owner, _, scope := departedMemberFixture(t, pool, sharedAt, revokedAt, now.AddDate(0, 0, -30), 777)

	snapshots, err := repo.GetSnapshotsAsOfForHousehold(ctx, owner, scope, now.AddDate(0, 0, -20))
	require.NoError(t, err)
	require.Len(t, snapshots, 1, "an as-of read INSIDE the shared window must still see the departed member's shared account")
	require.NotNil(t, snapshots[0].CurrentBalance)
	assert.InDelta(t, 777, *snapshots[0].CurrentBalance, 0.01)
}

// TestHouseholdShareAsOf_AfterDeparture_NotVisible is direction 2: an as-of read for an
// instant AT OR AFTER revoked_at must see nothing. Retaining the grant row must not hand the
// remaining members visibility of a period they were never entitled to.
func TestHouseholdShareAsOf_AfterDeparture_NotVisible(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	now := time.Now().UTC()
	sharedAt := now.AddDate(0, 0, -60)
	revokedAt := now.AddDate(0, 0, -10)
	owner, _, scope := departedMemberFixture(t, pool, sharedAt, revokedAt, now.AddDate(0, 0, -30), 777)

	snapshots, err := repo.GetSnapshotsAsOfForHousehold(ctx, owner, scope, now)
	require.NoError(t, err)
	assert.Empty(t, snapshots, "an as-of read AFTER the departure must not see the departed member's account")
}

// TestHouseholdShareAsOf_IntervalIsHalfOpen pins the boundary semantics that an earlier task
// on this branch got wrong once already (`valid_to >` vs `>=`): the grant covers
// [shared_at, revoked_at) — inclusive at shared_at, EXCLUSIVE at revoked_at.
func TestHouseholdShareAsOf_IntervalIsHalfOpen(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	now := time.Now().UTC()
	sharedAt := now.AddDate(0, 0, -60)
	revokedAt := now.AddDate(0, 0, -10)
	// The snapshot predates shared_at so BOTH boundary reads have a snapshot available and
	// the only thing that can differ between them is the grant-interval predicate itself.
	owner, _, scope := departedMemberFixture(t, pool, sharedAt, revokedAt, now.AddDate(0, 0, -70), 42)

	atStart, err := repo.GetSnapshotsAsOfForHousehold(ctx, owner, scope, sharedAt)
	require.NoError(t, err)
	assert.Len(t, atStart, 1, "asOf == shared_at is INSIDE the half-open interval")

	atEnd, err := repo.GetSnapshotsAsOfForHousehold(ctx, owner, scope, revokedAt)
	require.NoError(t, err)
	assert.Empty(t, atEnd, "asOf == revoked_at is OUTSIDE the half-open interval")

	justBeforeEnd, err := repo.GetSnapshotsAsOfForHousehold(ctx, owner, scope, revokedAt.Add(-time.Microsecond))
	require.NoError(t, err)
	assert.Len(t, justBeforeEnd, 1, "one microsecond before revoked_at is still inside")
}

// TestHouseholdShareAsOf_OverlappingHistoricalRowsAreDeterministic covers the hazard carried
// from earlier tasks: uq_household_account_shares_active only constrains OPEN rows, so once
// closed rows are retained an as-of instant can match SEVERAL rows for one logical grant.
//
// The two rows here carry DIFFERENT physical fingerprints (the account was re-fingerprinted
// between grants), which is what makes the hazard observable: the joint-account dedup keys on
// the fingerprint, so two matching rows for ONE user_account fall into two different dedup
// buckets and the account is emitted TWICE. The as-of read must collapse a logical grant to
// exactly one row, and must pick the interval actually in force at asOf (latest shared_at),
// not an arbitrary one.
func TestHouseholdShareAsOf_OverlappingHistoricalRowsAreDeterministic(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	// Two CLOSED, deliberately OVERLAPPING grant rows for the same (household, user_account),
	// with different fingerprints. Neither is open, so the partial unique index permits both.
	oldFP := "fp-old-" + hh
	newFP := "fp-new-" + hh
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, &oldFP, domain.ShareVisibilityShared, now.AddDate(0, 0, -90))
	revokeShareAt(t, pool, hh, memberAcct.UserAccountID, now.AddDate(0, 0, -10))
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, &newFP, domain.ShareVisibilityShared, now.AddDate(0, 0, -60))
	revokeShareAt(t, pool, hh, memberAcct.UserAccountID, now.AddDate(0, 0, -5))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}}
	asOf := now.AddDate(0, 0, -30) // covered by BOTH intervals

	acctRepo := repository.NewAccountRepository(pool, nil)
	for i := 0; i < 5; i++ {
		got, err := acctRepo.GetForHouseholdScope(ctx, owner, scope, asOf)
		require.NoError(t, err)
		require.Len(t, got, 1, "one logical grant matching asOf twice must still yield exactly ONE account row")
		require.Equal(t, memberAcct.AccountID, got[0].ID)
	}

	// The interval in force at asOf is the later-starting one, so its fingerprint is the one
	// the dedup must have used. Prove it by adding a SECOND account that collides with the
	// OLD fingerprint only: if the stale interval had won, the two would collapse to one row.
	other := setupHouseholdMemberAccount(t, pool, member)
	insertHouseholdShare(t, pool, hh, other.UserAccountID, &oldFP, domain.ShareVisibilityShared, now.AddDate(0, 0, -60))
	revokeShareAt(t, pool, hh, other.UserAccountID, now.AddDate(0, 0, -5))

	got, err := acctRepo.GetForHouseholdScope(ctx, owner, scope, asOf)
	require.NoError(t, err)
	assert.Len(t, got, 2,
		"the grant in force at asOf carries the NEW fingerprint, so it must not collide with the "+
			"other account's OLD fingerprint — a length of 1 means the stale overlapping interval won")
}

// TestHouseholdShareAsOf_OpenGrantStillRequiresActiveMembership pins the half of the
// membership guard that #1569 does NOT relax. Retaining history widens the as-of read for
// CLOSED grants only; an OPEN grant whose owner is not a currently-active member stays
// excluded exactly as before, so the present-day (asOf = now) result set is byte-for-byte
// the one the pre-#1569 `revoked_at IS NULL AND ua.user_id = ANY($3)` predicate produced.
func TestHouseholdShareAsOf_OpenGrantStillRequiresActiveMembership(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	now := time.Now().UTC()
	owner := insertTestUser(t, pool)
	stranger := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	strangerAcct := setupHouseholdMemberAccount(t, pool, stranger)

	// An OPEN grant (never revoked) for an account whose owner is absent from the scope.
	insertHouseholdShare(t, pool, hh, strangerAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, 0, -60))
	insertBalanceSnapshot(t, pool, stranger, strangerAcct.AccountID, strangerAcct.UserAccountID, 1234, now.AddDate(0, 0, -30))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}}

	nowRead, err := repo.GetSnapshotsAsOfForHousehold(ctx, owner, scope, now)
	require.NoError(t, err)
	assert.Empty(t, nowRead, "an OPEN grant for a non-active member must stay excluded today")

	pastRead, err := repo.GetSnapshotsAsOfForHousehold(ctx, owner, scope, now.AddDate(0, 0, -20))
	require.NoError(t, err)
	assert.Empty(t, pastRead, "and in the past too — only a CLOSED (historical) grant is admitted without active membership")
}
