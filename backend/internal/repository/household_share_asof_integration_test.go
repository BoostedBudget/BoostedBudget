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

	// The LOWER bound's refusal direction. Without this probe the whole "window opened too
	// wide" direction is unpinned: widening shared_at's comparison shows the household data
	// from BEFORE the grant existed, which is a privacy leak rather than a missing row, and
	// every other assertion in this file stays green through it.
	beforeStart, err := repo.GetSnapshotsAsOfForHousehold(ctx, owner, scope, sharedAt.Add(-time.Microsecond))
	require.NoError(t, err)
	assert.Empty(t, beforeStart, "one microsecond BEFORE shared_at the grant did not exist yet")

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
		got, err := acctRepo.GetForHouseholdScopeActiveNow(ctx, owner, scope, asOf)
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

	got, err := acctRepo.GetForHouseholdScopeActiveNow(ctx, owner, scope, asOf)
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

// TestHouseholdShareAsOf_VisibilityChangeDoesNotRewriteThePast is scope item 2 of #1569 T7.
// SetShareVisibility overwrote `visibility` in place on the single open grant row, so the
// row's ONE interval carried whatever the member last chose. Hiding an account therefore
// rewrote the whole history of that account: an as-of read for a date when it demonstrably
// WAS in the household aggregate reported that it never had been. A visibility change is a
// new state starting now, not a correction of the past.
func TestHouseholdShareAsOf_VisibilityChangeDoesNotRewriteThePast(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdShareRepository(pool)
	acctRepo := repository.NewAccountRepository(pool, nil)

	now := time.Now().UTC()
	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM household_account_shares WHERE household_id = $1`, hh) //nolint:errcheck
	})

	shared, err := repo.SetShareVisibility(ctx, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE household_account_shares SET shared_at = $1 WHERE id = $2`, now.AddDate(0, 0, -60), shared.ID)
	require.NoError(t, err)

	// The member hides the account TODAY.
	hidden, err := repo.SetShareVisibility(ctx, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityHidden)
	require.NoError(t, err)
	require.Equal(t, domain.ShareVisibilityHidden, hidden.Visibility)
	require.Nil(t, hidden.RevokedAt, "the CURRENT state must stay an open row so a later RevokeSharesForUser finds it")

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}

	past, err := acctRepo.GetForHouseholdScopeActiveNow(ctx, owner, scope, now.AddDate(0, 0, -30))
	require.NoError(t, err)
	require.Len(t, past, 1, "30 days ago this account WAS in the household aggregate; hiding it today must not rewrite that")
	require.Equal(t, memberAcct.AccountID, past[0].ID)

	present, err := acctRepo.GetForHouseholdScopeActiveNow(ctx, owner, scope, now.Add(time.Minute))
	require.NoError(t, err)
	require.Empty(t, present, "and it must be hidden from TODAY's read — retaining the past must not resurrect it")

	// The shape that makes the above possible: the shared period survives as its own CLOSED
	// interval, and the hidden period is the open one.
	rows, err := pool.Query(ctx, `
		SELECT visibility, revoked_at IS NULL FROM household_account_shares
		WHERE household_id = $1 AND user_account_id = $2 ORDER BY shared_at
	`, hh, memberAcct.UserAccountID)
	require.NoError(t, err)
	defer rows.Close()
	type state struct {
		visibility string
		open       bool
	}
	var states []state
	for rows.Next() {
		var st state
		require.NoError(t, rows.Scan(&st.visibility, &st.open))
		states = append(states, st)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []state{{"shared", false}, {"hidden", true}}, states,
		"a visibility change must CLOSE the outgoing state's interval and OPEN a new one, not overwrite in place")
}

// TestHouseholdShareAsOf_RepeatingTheCurrentVisibilityDoesNotChurnIntervals guards the other
// direction: an idempotent PATCH (setting the visibility it already has) must not manufacture
// a zero-length interval and a new row on every call. Only a genuine state CHANGE closes.
func TestHouseholdShareAsOf_RepeatingTheCurrentVisibilityDoesNotChurnIntervals(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdShareRepository(pool)

	owner := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ua := insertTestUserAccount(t, pool, owner)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM household_account_shares WHERE household_id = $1`, hh) //nolint:errcheck
	})

	first, err := repo.SetShareVisibility(ctx, hh, ua, nil, domain.ShareVisibilityShared)
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		again, err := repo.SetShareVisibility(ctx, hh, ua, nil, domain.ShareVisibilityShared)
		require.NoError(t, err)
		require.Equal(t, first.ID, again.ID, "re-asserting the same visibility must return the SAME open row")
	}

	var total int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2`,
		hh, ua).Scan(&total))
	assert.Equal(t, 1, total, "no-op PATCHes must not accumulate zero-length intervals")
}

// TestHouseholdShareAsOf_EqualSharedAtTieBreaksOnLowestID pins the DIRECTION of the grant
// dedup's final tiebreak. Every other fixture in this file has distinct shared_at values, so
// `has.id` never actually decides anything there and flipping it to `has.id DESC` leaves the
// suite green. Either direction is deterministic, so this is about pinning the choice, not
// about correctness — but an unpinned tiebreak is one refactor away from being no tiebreak.
//
// Observability trick, same as the overlapping-rows test: the winner is invisible directly (the
// as-of reads project no grant id), so a SECOND account is given the winner's fingerprint. If
// the intended row won, the two collide in the joint-account dedup and one account comes back.
func TestHouseholdShareAsOf_EqualSharedAtTieBreaksOnLowestID(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	acctRepo := repository.NewAccountRepository(pool, nil)

	now := time.Now().UTC()
	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	target := setupHouseholdMemberAccount(t, pool, member)
	sibling := setupHouseholdMemberAccount(t, pool, member)

	fpA := "fp-a-" + hh
	fpB := "fp-b-" + hh
	idA := insertHouseholdShare(t, pool, hh, target.UserAccountID, &fpA, domain.ShareVisibilityShared, now.AddDate(0, 0, -60))
	revokeShareAt(t, pool, hh, target.UserAccountID, now.AddDate(0, 0, -5))
	idB := insertHouseholdShare(t, pool, hh, target.UserAccountID, &fpB, domain.ShareVisibilityShared, now.AddDate(0, 0, -60))
	revokeShareAt(t, pool, hh, target.UserAccountID, now.AddDate(0, 0, -5))

	// Force the tie the rest of the suite never produces: identical shared_at AND revoked_at,
	// so `has.id` is the only remaining discriminator.
	_, err := pool.Exec(ctx, `
		UPDATE household_account_shares SET shared_at = $1, revoked_at = $2
		WHERE household_id = $3 AND user_account_id = $4
	`, now.AddDate(0, 0, -60), now.AddDate(0, 0, -5), hh, target.UserAccountID)
	require.NoError(t, err)

	// ASC means the lowest id wins; give the sibling account that row's fingerprint.
	winnerFP := fpA
	if idB < idA {
		winnerFP = fpB
	}
	insertHouseholdShare(t, pool, hh, sibling.UserAccountID, &winnerFP, domain.ShareVisibilityShared, now.AddDate(0, 0, -60))
	revokeShareAt(t, pool, hh, sibling.UserAccountID, now.AddDate(0, 0, -5))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}}
	got, err := acctRepo.GetForHouseholdScopeActiveNow(ctx, owner, scope, now.AddDate(0, 0, -30))
	require.NoError(t, err)
	assert.Len(t, got, 1,
		"with shared_at tied, the LOWEST-id interval must win: its fingerprint collides with the "+
			"sibling account's, so the two collapse. A length of 2 means the tiebreak runs the other way")
}

// ─── The RANGE read (#1569 T7, F1) ───────────────────────────────────────────
//
// GetAggregatedHistoryForHousehold is the household net-worth CHART. It is the most visible
// instance of the defect this task exists to fix: its CTE filtered `revoked_at IS NULL AND
// ua.user_id = ANY($3)`, two independent reasons a departed member's contribution disappeared
// from the WHOLE series — not clipped at the departure, erased from every day before it too.

// rangeHistoryTotals collapses a BalanceHistoryPoint slice to (date -> total) for assertions
// that care about which DAYS survive rather than the per-type breakdown.
func rangeHistoryTotals(points []domain.BalanceHistoryPoint) map[string]float64 {
	totals := make(map[string]float64, len(points))
	for _, p := range points {
		totals[p.Date.Format("2006-01-02")] += p.TotalBalance
	}
	return totals
}

// departedMemberRangeFixture: the member shares 60 days ago, has snapshots at -50/-40/-30/-20,
// and departs at -10. Every snapshot therefore falls strictly inside the shared window.
func departedMemberRangeFixture(t *testing.T, pool *pgxpool.Pool) (owner, hh string, days []time.Time) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()

	owner = insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh = insertTestHousehold(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, 0, -60))
	for i, d := range []int{-50, -40, -30, -20} {
		at := now.AddDate(0, 0, d)
		insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, float64(100*(i+1)), at)
		days = append(days, at)
	}
	revokeShareAt(t, pool, hh, memberAcct.UserAccountID, now.AddDate(0, 0, -10))
	_ = ctx
	return owner, hh, days
}

// TestHouseholdShareRange_DepartedMemberKeepsTheSharedPeriod is F1's headline: the four days
// the member was genuinely shared must survive their departure. Before this fix the series
// went to ZERO points.
func TestHouseholdShareRange_DepartedMemberKeepsTheSharedPeriod(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	now := time.Now().UTC()
	owner, hh, days := departedMemberRangeFixture(t, pool)
	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}}

	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, now.AddDate(0, 0, -60), now, nil)
	require.NoError(t, err)

	totals := rangeHistoryTotals(points)
	require.Len(t, totals, 4,
		"all four days inside [shared_at, revoked_at) must survive the departure — zero means the "+
			"whole series was erased, which is the defect")
	for i, d := range days {
		key := d.Format("2006-01-02")
		require.Contains(t, totals, key)
		assert.InDelta(t, float64(100*(i+1)), totals[key], 0.01, "day %s", key)
	}
}

// TestHouseholdShareRange_StopsAtTheDeparture is the other direction: retaining the shared
// period must not hand the household anything from after the member left.
func TestHouseholdShareRange_StopsAtTheDeparture(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	now := time.Now().UTC()
	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, 0, -60))
	inside := now.AddDate(0, 0, -30)
	after := now.AddDate(0, 0, -3)
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 111, inside)
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 999, after)
	revokeShareAt(t, pool, hh, memberAcct.UserAccountID, now.AddDate(0, 0, -10))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, now.AddDate(0, 0, -60), now, nil)
	require.NoError(t, err)

	totals := rangeHistoryTotals(points)
	assert.Contains(t, totals, inside.Format("2006-01-02"), "the shared day stays")
	assert.NotContains(t, totals, after.Format("2006-01-02"),
		"a snapshot from AFTER the departure must never enter the household series")
}

// TestHouseholdShareRange_RevocationDayKeepsOnlyPreRevocationInstants pins the CLOSING bound at
// INSTANT resolution, not date resolution.
//
// The first version of this rule excluded the revocation day whole, on a false binary: either drop
// the member's last partial day or admit a post-departure instant. Clipping on bs.snapshot_at
// against share_ended_at directly does neither — the household keeps every instant it was
// genuinely entitled to, including ones earlier on the departure day, and nothing after.
func TestHouseholdShareRange_RevocationDayKeepsOnlyPreRevocationInstants(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	now := time.Now().UTC()
	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	// Revoke at midday on day -10, with a snapshot on either side of that instant ON THAT DAY.
	revokedAt := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.UTC).AddDate(0, 0, -10)
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, 0, -60))
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 50, revokedAt.AddDate(0, 0, -1))
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 60, revokedAt.Add(-time.Hour))
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 70, revokedAt.Add(time.Hour))
	revokeShareAt(t, pool, hh, memberAcct.UserAccountID, revokedAt)

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, now.AddDate(0, 0, -60), now, nil)
	require.NoError(t, err)

	totals := rangeHistoryTotals(points)
	assert.Contains(t, totals, revokedAt.AddDate(0, 0, -1).Format("2006-01-02"), "the last full day stays")

	departureDay := revokedAt.Format("2006-01-02")
	require.Contains(t, totals, departureDay,
		"the departure day must NOT be dropped whole — the household legitimately saw this account "+
			"until midday, and an instant-resolution clip keeps that without admitting anything later")
	assert.InDelta(t, 60, totals[departureDay], 0.01,
		"70 is the snapshot taken an hour AFTER the revocation and must never enter the series; a "+
			"missing day means the clip is back at date resolution and threw away legitimate history")
}

// TestHouseholdShareRange_ShareStartDayExcludesPreGrantInstants is the mirror of the above, and
// the direction that was leaking: the lower bound was GREATEST($4::date, share_started_at::date),
// which admits the WHOLE share-start day — including instants BEFORE the grant existed. The
// "cannot leak" principle the closing bound was argued from has to apply here too.
func TestHouseholdShareRange_ShareStartDayExcludesPreGrantInstants(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	now := time.Now().UTC()
	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	// The grant opens at midday on day -10; the ONLY snapshot that day is three hours earlier.
	sharedAt := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.UTC).AddDate(0, 0, -10)
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, sharedAt)
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 777, sharedAt.Add(-3*time.Hour))
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 888, sharedAt.AddDate(0, 0, 1))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, now.AddDate(0, 0, -60), now, nil)
	require.NoError(t, err)

	totals := rangeHistoryTotals(points)
	assert.NotContains(t, totals, sharedAt.Format("2006-01-02"),
		"an instant three hours BEFORE the grant existed must not appear in the household series — "+
			"a date-resolution lower bound admits the whole share-start day and leaks exactly that")
	assert.Contains(t, totals, sharedAt.AddDate(0, 0, 1).Format("2006-01-02"), "the day after is genuinely shared")
}

// TestHouseholdShareRange_OverlappingRefingerprintedIntervalsDoNotDoubleCount is the gap the
// interval relaxation opens and the reviewer's prototype did not cover. Once closed rows are
// admitted, one user_account can match SEVERAL grant rows on the same day; the joint-account
// dedup keys on the fingerprint, so two rows with different fingerprints put ONE account in two
// buckets and its balance is summed TWICE into that day's total.
func TestHouseholdShareRange_OverlappingRefingerprintedIntervalsDoNotDoubleCount(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	now := time.Now().UTC()
	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	oldFP := "fp-old-" + hh
	newFP := "fp-new-" + hh
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, &oldFP, domain.ShareVisibilityShared, now.AddDate(0, 0, -90))
	revokeShareAt(t, pool, hh, memberAcct.UserAccountID, now.AddDate(0, 0, -10))
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, &newFP, domain.ShareVisibilityShared, now.AddDate(0, 0, -60))
	revokeShareAt(t, pool, hh, memberAcct.UserAccountID, now.AddDate(0, 0, -5))

	day := now.AddDate(0, 0, -30) // covered by BOTH intervals
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 400, day)

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, now.AddDate(0, 0, -60), now, nil)
	require.NoError(t, err)

	totals := rangeHistoryTotals(points)
	require.Contains(t, totals, day.Format("2006-01-02"))
	assert.InDelta(t, 400, totals[day.Format("2006-01-02")], 0.01,
		"800 means the account was counted once per matching grant interval — the per-(day, user_account) "+
			"collapse must run BEFORE the joint-account fingerprint dedup")
}

// TestHouseholdShareRange_OpenGrantStillRequiresActiveMembership mirrors the as-of read's
// guard: the relaxation is for CLOSED intervals only.
func TestHouseholdShareRange_OpenGrantStillRequiresActiveMembership(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	now := time.Now().UTC()
	owner := insertTestUser(t, pool)
	stranger := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	strangerAcct := setupHouseholdMemberAccount(t, pool, stranger)

	insertHouseholdShare(t, pool, hh, strangerAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, 0, -60))
	insertBalanceSnapshot(t, pool, stranger, strangerAcct.AccountID, strangerAcct.UserAccountID, 1234, now.AddDate(0, 0, -30))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, now.AddDate(0, 0, -60), now, nil)
	require.NoError(t, err)
	assert.Empty(t, rangeHistoryTotals(points), "an OPEN grant for a non-active member stays excluded")
}
