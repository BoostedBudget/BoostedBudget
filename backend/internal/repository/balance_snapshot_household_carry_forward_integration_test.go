//go:build integration

// balance_snapshot_household_carry_forward_integration_test.go — the HOUSEHOLD half of the
// per-account carry-forward (#1626 E2 Part 1).
//
// Task E gave the PERSONAL balance-history read a per-account LOCF, because a day's total was the
// sum of only the accounts that happened to receive a snapshot that day: snapshots are written
// per-ITEM on webhook sync, so a day on which one item synced reported only that item's accounts
// and the total collapsed toward zero, while every other account's balance was merely ABSENT from
// that date rather than gone. GetAggregatedHistoryForHousehold had the identical defect and did not
// get the fix, so a household user still saw the collapse. It does now.
//
// ⚠ THE HOUSEHOLD PATH IS NOT A COPY OF THE PERSONAL ONE, and the difference these tests exist for
// is the GRANT CLIP. Household membership and account sharing are half-open intervals, evaluated
// for BALANCES at INSTANT resolution (docs/decisions/2026-08-26-household-grants-cte-performance.md
// and ADR Decision 7). A carried row is not an observation, so it has no instant — only a calendar
// day. The ruling implemented and asserted here is DAY INTERSECTION: a carried value is admitted on
// a day the grant covers even PARTIALLY, and refused on a day the grant does not touch at all. See
// aggregatedHistoryHouseholdSQLBase's grant_on_day CTE for the full reasoning, and
// TestHouseholdAggregatedHistoryCarry_DepartedMember_CarriesThroughThePartialRevocationDayOnly,
// which asserts BOTH directions of that decision rather than only the safe one.
//
// ⚠ EVERY FIXTURE BALANCE HERE IS OFF THE CONVENIENT BOUNDARY — odd cents, and grant/liveness
// boundaries at 07:13 rather than midnight or noon. A mutation on this branch survived because a
// fixture sat exactly on a boundary the code would have generated anyway.
//
// Run:
//
//	DATABASE_URL=postgres://devuser:devpass@localhost:5432/freshcheck?sslmode=disable \
//	CACHE_REDIS_TEST_URL=redis://localhost:6380 REDIS_TEST_URL=redis://localhost:6379 \
//	  go test ./internal/repository/ -run TestHouseholdAggregatedHistoryCarry -tags integration -p 1 -v
package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// householdCarryClock fixes the calendar the whole file reasons about: day 0 is midnight UTC thirty
// days ago, so every fixture instant is comfortably in the past (the carry's removal bound consults
// account_status_history, and "today" would put the fixture on the wrong side of it) and no
// hour-offset arithmetic can cross a day boundary by accident.
type householdCarryClock struct{ origin time.Time }

func newHouseholdCarryClock() householdCarryClock {
	return householdCarryClock{origin: time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -30)}
}

func (c householdCarryClock) day(n int) time.Time { return c.origin.AddDate(0, 0, n) }

func (c householdCarryClock) at(n, hour, minute int) time.Time {
	return c.day(n).Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
}

// backdateHouseholdAccountActive backdates the 'active' interval that
// account_status_history_insert_trigger opened at clock_timestamp() when the fixture inserted the
// accounts row.
//
// ⚠ LOAD-BEARING, NOT COSMETIC — the same note seedCarryAccount carries for personal scope. The
// trigger stamps valid_from = NOW, so a fixture account is recorded as having become active today,
// and the carry's removal bound (accountLiveOnDateSQL) would then refuse to carry ANY balance into a
// day thirty days ago — making a carry test fail, or pass, for entirely the wrong reason. Production
// has no such problem: an account's snapshots always postdate its own accounts row.
func backdateHouseholdAccountActive(t *testing.T, pool *pgxpool.Pool, accountID string, from time.Time) {
	t.Helper()
	tag, err := pool.Exec(context.Background(), `
		UPDATE account_status_history SET valid_from = $2
		WHERE account_id = $1 AND status = 'active'`, accountID, from)
	require.NoError(t, err, "backdate active interval")
	require.Equal(t, int64(1), tag.RowsAffected(),
		"expected exactly one open 'active' interval to backdate — if the insert trigger stopped "+
			"writing one, this helper is silently doing nothing and every carry assertion below "+
			"becomes untrustworthy")
}

// totalOn returns the aggregated total for a UTC calendar day, and whether the day is in the series
// at all. The two are separate answers: "no point for that day" and "a point worth 0" mean
// different things here, because DAYS ARE NOT INVENTED by this read — only VALUES are.
func totalOn(points []domain.BalanceHistoryPoint, day time.Time) (float64, bool) {
	want := day.UTC().Format("2006-01-02")
	for i := range points {
		if points[i].Date.UTC().Format("2006-01-02") == want {
			return points[i].TotalBalance, true
		}
	}
	return 0, false
}

func requireTotalOn(t *testing.T, points []domain.BalanceHistoryPoint, day time.Time, want float64, msg string) {
	t.Helper()
	got, ok := totalOn(points, day)
	require.True(t, ok, "expected a chart point for %s — %s", day.UTC().Format("2006-01-02"), msg)
	assert.InDelta(t, want, got, 0.001, msg)
}

// TestHouseholdAggregatedHistoryCarry_IncompleteCohort_CarriesTheOtherMembersLastKnownBalance is the
// user-reported symptom in household miniature: two members, and on a later day only one member's
// Plaid item synced.
func TestHouseholdAggregatedHistoryCarry_IncompleteCohort_CarriesTheOtherMembersLastKnownBalance(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)
	c := newHouseholdCarryClock()

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)
	backdateHouseholdAccountActive(t, pool, ownerAcct.AccountID, c.day(-20))
	backdateHouseholdAccountActive(t, pool, memberAcct.AccountID, c.day(-20))

	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, nil, domain.ShareVisibilityShared, c.at(-5, 8, 0))
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, c.at(-5, 8, 0))

	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1234.56, c.at(0, 9, 17))
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 307.89, c.at(0, 10, 33))
	// Day 2: only the OWNER's item syncs. The member's account did not report and is not gone.
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1300.11, c.at(2, 9, 17))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, c.day(-1), c.day(10), nil)
	require.NoError(t, err)

	requireTotalOn(t, points, c.day(0), 1542.45, "both members reported on day 0")
	requireTotalOn(t, points, c.day(2), 1608.00,
		"day 2 must carry the member's last known 307.89 alongside the owner's fresh 1300.11 — "+
			"1300.11 alone is the collapse this fix exists to remove")

	_, hasDay1 := totalOn(points, c.day(1))
	assert.False(t, hasDay1,
		"DAYS ARE NOT INVENTED, only VALUES: nobody synced on day 1, so it stays absent from the "+
			"series and the frontend still bridges it. Generating a calendar grid would also "+
			"generate FUTURE days, endDate being unvalidated against today")
}

// TestHouseholdAggregatedHistoryCarry_DepartedMember_CarriesThroughThePartialRevocationDayOnly is
// THE PARTIAL-GRANT-DAY DECISION, asserted in both directions.
//
// The grant is revoked at 07:13 on day 2, so day 2 is covered for its first seven hours and day 3
// is not covered at all. The ruling implemented is DAY INTERSECTION: the carried value is admitted
// on day 2 and refused from day 3 onward.
//
// The alternative — demand FULL coverage of the carried day — would drop the departing account's
// balance on the revocation day while other members still report, which is precisely the
// collapse-toward-zero defect this whole CTE chain exists to remove, reintroduced on the single
// most visible day in a household's life. Nothing leaks the other way, and that is what the day-3
// and day-4 assertions are for.
func TestHouseholdAggregatedHistoryCarry_DepartedMember_CarriesThroughThePartialRevocationDayOnly(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)
	c := newHouseholdCarryClock()

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)
	backdateHouseholdAccountActive(t, pool, ownerAcct.AccountID, c.day(-20))
	backdateHouseholdAccountActive(t, pool, memberAcct.AccountID, c.day(-20))

	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, nil, domain.ShareVisibilityShared, c.at(-5, 8, 0))
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, c.at(-5, 8, 0))

	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 307.89, c.at(0, 10, 33))
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1000.11, c.at(0, 9, 17))
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1100.22, c.at(2, 9, 17))
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1200.33, c.at(3, 9, 17))
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1300.44, c.at(4, 9, 17))

	// The member leaves at 07:13 on day 2 — deliberately not midnight and not noon.
	revokeHouseholdShareAt(t, pool, hh, member, memberAcct.UserAccountID, c.at(2, 7, 13))

	// The remaining household is the owner alone. The departed member's CLOSED grant is still
	// readable (householdAccountsCTEForBalancesPerDay relaxes the membership guard for closed
	// grants only), which is what keeps the shared period from vanishing retroactively.
	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, c.day(-1), c.day(10), nil)
	require.NoError(t, err)

	requireTotalOn(t, points, c.day(0), 1308.00,
		"the shared period must survive the departure — this is #1569's whole point")
	requireTotalOn(t, points, c.day(2), 1408.11,
		"day 2 is covered by the grant until 07:13, so the carried 307.89 describes instants the "+
			"household was genuinely entitled to. THIS IS THE PARTIAL-GRANT-DAY DECISION")
	requireTotalOn(t, points, c.day(3), 1200.33,
		"day 3 is not touched by the grant at all — carrying here would leak a departed member's "+
			"balance past their departure")
	requireTotalOn(t, points, c.day(4), 1300.44,
		"and it must not come back on any later day either — an unbounded carry would put the "+
			"departed member in the household's net worth forever")
}

// TestHouseholdAggregatedHistoryCarry_ShareStartDay_DoesNotCarryAPreGrantInstant is the other end of
// the same interval, and the direction that leaks rather than drops.
//
// The grant opens at 07:13 on day 3. A snapshot taken at 05:00 that same day is BEFORE the grant and
// is already excluded from the observed set at instant resolution (pinned independently by
// TestHouseholdShareRange_ShareStartDayExcludesPreGrantInstants). The carry must not smuggle it back
// in through the day: because it was never observed, there is nothing to carry, so the household
// sees the account only from its first IN-GRANT snapshot onward — the same "history starts per
// account" ruling personal scope records at per_account_per_day.
func TestHouseholdAggregatedHistoryCarry_ShareStartDay_DoesNotCarryAPreGrantInstant(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)
	c := newHouseholdCarryClock()

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)
	backdateHouseholdAccountActive(t, pool, ownerAcct.AccountID, c.day(-20))
	backdateHouseholdAccountActive(t, pool, memberAcct.AccountID, c.day(-20))

	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, nil, domain.ShareVisibilityShared, c.at(-5, 8, 0))
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, c.at(3, 7, 13))

	// The member's account has a long personal history the household is not entitled to.
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 999.99, c.at(0, 10, 33))
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 888.88, c.at(3, 5, 0))
	// ... and its first in-grant snapshot, later on the share-start day.
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 250.25, c.at(3, 9, 0))

	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1000.11, c.at(0, 9, 17))
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1100.22, c.at(2, 9, 17))
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1200.33, c.at(4, 9, 17))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, c.day(-1), c.day(10), nil)
	require.NoError(t, err)

	requireTotalOn(t, points, c.day(0), 1000.11,
		"a pre-grant snapshot is not in the household's universe, so there is nothing to carry")
	requireTotalOn(t, points, c.day(2), 1100.22,
		"and no carry may invent one on a day still before the grant opens")
	requireTotalOn(t, points, c.day(3), 1350.47,
		"on the share-start day the household sees the 09:00 in-grant snapshot (250.25) and NOT "+
			"the 05:00 pre-grant one (888.88), which the instant clip already excluded")
	requireTotalOn(t, points, c.day(4), 1450.58,
		"day 4 carries the member's first in-grant balance forward — 250.25, never 888.88")
}

// TestHouseholdAggregatedHistoryCarry_RemovedAccount_IsNotCarriedPastItsRemoval is the household
// mirror of the personal removal bound, and of the live wrong number that bound was added to correct
// (#1626 Phase 2, X1): shaped only as "no recorded 'active' interval", the carry leg reads as
// unconditional permission and a closed account contributes to household net worth forever.
//
// CLAUDE.md's ruling is that a terminal account presents NULL rather than zero for a CURRENT balance
// while its history stays queryable — so the account's OWN snapshot days keep their value, the day
// it was live for part of counts (accountLiveOnDateClause is an interval OVERLAP, not "active at
// midnight"), and only the inferred, carried days after that stop.
func TestHouseholdAggregatedHistoryCarry_RemovedAccount_IsNotCarriedPastItsRemoval(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)
	c := newHouseholdCarryClock()

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)
	backdateHouseholdAccountActive(t, pool, ownerAcct.AccountID, c.day(-20))
	backdateHouseholdAccountActive(t, pool, memberAcct.AccountID, c.day(-20))

	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, nil, domain.ShareVisibilityShared, c.at(-5, 8, 0))
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, c.at(-5, 8, 0))

	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 307.89, c.at(0, 10, 33))
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1000.11, c.at(0, 9, 17))
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1050.22, c.at(1, 9, 17))
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1100.33, c.at(2, 9, 17))
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1200.44, c.at(3, 9, 17))

	// The member's account is REMOVED at 06:00 on day 1 — the sharing grant is untouched, so the
	// only thing that can stop the carry is the account's own recorded liveness.
	// removed_at is set alongside status because accounts_removed_at_consistency requires them to
	// agree — the schema will not let a test fake a terminal account by status alone.
	_, err := pool.Exec(ctx, `
		UPDATE accounts SET status = 'removed', removed_at = $2, removed_reason = 'user_disconnect'
		WHERE id = $1`, memberAcct.AccountID, c.at(1, 6, 0))
	require.NoError(t, err)
	// The status UPDATE fires account_status_history's trigger, which has ALREADY closed the
	// 'active' interval and opened a 'removed' one — both stamped clock_timestamp(), i.e. now. Both
	// ends are backdated to the fixture's removal instant so the recorded history says the account
	// stopped being live on day 1 at 06:00 rather than today.
	tag, err := pool.Exec(ctx, `
		UPDATE account_status_history SET valid_to = $2
		WHERE account_id = $1 AND status = 'active'`, memberAcct.AccountID, c.at(1, 6, 0))
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.RowsAffected(), "expected one 'active' interval to close")
	tag, err = pool.Exec(ctx, `
		UPDATE account_status_history SET valid_from = $2
		WHERE account_id = $1 AND status = 'removed'`, memberAcct.AccountID, c.at(1, 6, 0))
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.RowsAffected(), "expected one 'removed' interval to backdate")

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, c.day(-1), c.day(10), nil)
	require.NoError(t, err)

	requireTotalOn(t, points, c.day(0), 1308.00,
		"the account's OWN snapshot day is provider truth and is retained (#1569)")
	requireTotalOn(t, points, c.day(1), 1358.11,
		"the account was genuinely live for the first six hours of day 1, and "+
			"accountLiveOnDateClause is an interval OVERLAP test rather than 'active at midnight'")
	requireTotalOn(t, points, c.day(2), 1100.33,
		"a closed account must not be carried onto a day nobody was confirming it existed")
	requireTotalOn(t, points, c.day(3), 1200.44,
		"and it must never come back — an unbounded carry overstates household net worth forever")
}

// TestHouseholdAggregatedHistoryCarry_JointAccount_CarriedCopyDoesNotDoubleCount is the collision
// the personal path cannot have: the SAME physical account linked and shared by TWO members must be
// counted ONCE, and the carry-forward makes both copies present on every later day instead of only
// the days each actually reported. If a carried copy escaped the per-day fingerprint collapse, a
// joint account would be summed twice on exactly the days this fix adds.
func TestHouseholdAggregatedHistoryCarry_JointAccount_CarriedCopyDoesNotDoubleCount(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)
	c := newHouseholdCarryClock()

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)
	backdateHouseholdAccountActive(t, pool, ownerAcct.AccountID, c.day(-20))
	backdateHouseholdAccountActive(t, pool, memberAcct.AccountID, c.day(-20))

	fingerprint := "joint-carry-fp-" + hh
	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, &fingerprint, domain.ShareVisibilityShared, c.at(-5, 8, 0))
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, &fingerprint, domain.ShareVisibilityShared, c.at(-5, 8, 0))

	// Day 0: both copies report, the member's later in the day and therefore fresher.
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 500.55, c.at(0, 9, 17))
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 512.34, c.at(0, 11, 41))
	// Day 2: only the owner's copy reports. The member's copy is carried from day 0.
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 520.66, c.at(2, 9, 17))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, c.day(-1), c.day(10), nil)
	require.NoError(t, err)

	// Stated as "no point may be within a cent of either doubled sum" rather than as an equality,
	// so a rounding difference cannot make a double-count slip past.
	for _, p := range points {
		for want, why := range map[float64]string{
			1032.89: "a carried copy of a joint account must not escape the per-day fingerprint collapse",
			1012.89: "nor may both copies be summed on the day they both genuinely reported",
		} {
			assert.False(t, p.TotalBalance > want-0.01 && p.TotalBalance < want+0.01,
				"%s (day %s totalled %.2f)", why, p.Date.UTC().Format("2006-01-02"), p.TotalBalance)
		}
	}
	requireTotalOn(t, points, c.day(0), 512.34,
		"$512.34 shared by two people is still $512.34, and the freshest copy of the day wins")
	requireTotalOn(t, points, c.day(2), 520.66,
		"on day 2 the owner's real observation outranks the member's carried inference, and the "+
			"joint account is still counted exactly once")
}

// TestHouseholdAggregatedHistoryCarry_WindowStart_InheritsABalanceFromBeforeTheWindow is why the
// observed set is UNBOUNDED BELOW and the caller's start date is applied to the FILLED series
// instead.
//
// ⚠ ADDED BY MUTATION. The first version of this file passed with the lower bound restored to the
// observed CTE, because every fixture window opened before every fixture snapshot — so the
// difference between "bound the observations" and "bound the filled series" was unobservable, and
// the comment claiming the distinction mattered was unfalsifiable. Here the window opens on day 2
// and the member's most recent sync is on day 0: bounding the observations would erase that account
// from the chart entirely, which is the "this month chart under-counts every account whose last
// sync was last month" defect stated as a test.
func TestHouseholdAggregatedHistoryCarry_WindowStart_InheritsABalanceFromBeforeTheWindow(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)
	c := newHouseholdCarryClock()

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)
	backdateHouseholdAccountActive(t, pool, ownerAcct.AccountID, c.day(-20))
	backdateHouseholdAccountActive(t, pool, memberAcct.AccountID, c.day(-20))

	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, nil, domain.ShareVisibilityShared, c.at(-5, 8, 0))
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, c.at(-5, 8, 0))

	// The member's last sync is BEFORE the requested window opens.
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 307.89, c.at(0, 10, 33))
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1000.11, c.at(0, 9, 17))
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1100.22, c.at(3, 9, 17))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, c.day(2), c.day(10), nil)
	require.NoError(t, err)

	_, hasDay0 := totalOn(points, c.day(0))
	assert.False(t, hasDay0, "the window's own lower bound still applies to what is RETURNED")
	requireTotalOn(t, points, c.day(3), 1408.11,
		"day 3 must inherit the member's day-0 balance even though day 0 is outside the window — "+
			"bounding the OBSERVATIONS below would erase that account from the whole chart")
}

// TestHouseholdAggregatedHistoryCarry_TerminalOnlyHistory_DoesNotCarryPastItsOwnSnapshot is the
// exact shape the personal read's carry leg was WRONG about before #1626 Phase 2's X1 fix, stated
// in household scope so the household half cannot regress into it independently.
//
// ⚠ ADDED BY MUTATION. Deleting `AND a_carry.status = 'active'` from the missing-history leg was
// killed only by the structural pin test, never behaviourally, because
// TestHouseholdAggregatedHistoryCarry_RemovedAccount_IsNotCarriedPastItsRemoval CLOSES the account's
// 'active' interval rather than removing it — so `NOT EXISTS(active interval)` is false there and
// the leg never fires at all, with or without the status term. Migration 000152's removed branch
// DELIBERATELY writes a terminal-only history when removed_at is NULL or <= created_at, and that is
// the shape in which the leg reads as unconditional permission to carry: without the status term a
// closed account's last retained balance lands on every later observed day, forever.
func TestHouseholdAggregatedHistoryCarry_TerminalOnlyHistory_DoesNotCarryPastItsOwnSnapshot(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)
	c := newHouseholdCarryClock()

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)
	backdateHouseholdAccountActive(t, pool, ownerAcct.AccountID, c.day(-20))

	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, nil, domain.ShareVisibilityShared, c.at(-5, 8, 0))
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, c.at(-5, 8, 0))

	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 307.89, c.at(0, 10, 33))
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1000.11, c.at(0, 9, 17))
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1100.22, c.at(2, 9, 17))

	_, err := pool.Exec(ctx, `
		UPDATE accounts SET status = 'removed', removed_at = $2, removed_reason = 'user_disconnect'
		WHERE id = $1`, memberAcct.AccountID, c.at(1, 6, 0))
	require.NoError(t, err)
	// THE SHAPE: a terminal-only history, exactly as migration 000152's removed branch writes it.
	tag, err := pool.Exec(ctx, `
		DELETE FROM account_status_history WHERE account_id = $1 AND status = 'active'`,
		memberAcct.AccountID)
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.RowsAffected(),
		"the fixture must actually leave a terminal-only history — if no 'active' row was there to "+
			"delete, this test is asserting nothing about the leg it exists for")

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, c.day(-1), c.day(10), nil)
	require.NoError(t, err)

	requireTotalOn(t, points, c.day(0), 1308.00,
		"the account's OWN snapshot day is provider truth and stays retained (#1569)")
	requireTotalOn(t, points, c.day(2), 1100.22,
		"'this account has no recorded active interval' is NOT permission to carry — it is true "+
			"FOREVER for migration 000152's terminal-only shape, which is how a closed account came "+
			"to be counted in net worth on every later day")
}

// TestHouseholdAggregatedHistoryCarry_OrphanedSnapshot_KeepsItsOwnDay is why carry_key is a rendered
// text key rather than the (user_account_id, account_id) pair.
//
// ⚠ ADDED BY MUTATION. Replacing the NULL-safe key with a bare `bs.account_id::text` was killed only
// by the structural pin test. An ORPHANED snapshot (account_id IS NULL after migration 000150's ON
// DELETE SET NULL) then has a NULL carry_key, the grid's equi-join never matches it, and it loses
// even its OWN observed row on the way back out — so the fill would have DELETED retained history
// rather than merely failing to extend it. That is a worse failure than the one the fill fixes.
//
// It must also not be CARRIED: an orphan has no accounts row for either liveness leg to find, so
// nobody is confirming it exists, which is the same ruling the personal statement records.
func TestHouseholdAggregatedHistoryCarry_OrphanedSnapshot_KeepsItsOwnDay(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)
	c := newHouseholdCarryClock()

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)
	backdateHouseholdAccountActive(t, pool, ownerAcct.AccountID, c.day(-20))

	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, nil, domain.ShareVisibilityShared, c.at(-5, 8, 0))
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, c.at(-5, 8, 0))

	// The orphan: account_id IS NULL, user_account_id intact — which is exactly what the chain
	// pointer is for, and it is what keeps the row inside the household's universe.
	_, err := pool.Exec(ctx, `
		INSERT INTO balance_snapshots (account_id, user_account_id, user_id, current_balance,
		                               currency_code, snapshot_at, source)
		VALUES (NULL, $1, $2, 42.42, 'USD', $3, 'manual')`,
		memberAcct.UserAccountID, member, c.at(0, 10, 33))
	require.NoError(t, err, "seed orphaned snapshot")

	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1000.11, c.at(0, 9, 17))
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1100.22, c.at(2, 9, 17))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, c.day(-1), c.day(10), nil)
	require.NoError(t, err)

	requireTotalOn(t, points, c.day(0), 1042.53,
		"an orphaned snapshot must survive the grid round-trip and still reach its own day's total")
	requireTotalOn(t, points, c.day(2), 1100.22,
		"but it must not be carried: an orphan has no accounts row, so nobody is confirming it exists")

	day0, ok := totalOn(points, c.day(0))
	require.True(t, ok)
	assert.InDelta(t, 1042.53, day0, 0.001)
	for i := range points {
		if points[i].Date.UTC().Format("2006-01-02") == c.day(0).UTC().Format("2006-01-02") {
			assert.InDelta(t, 42.42, points[i].UntypedBalance, 0.001,
				"and it lands in the UNTYPED bucket, not silently in a typed one — net-worth "+
					"history is reconstructed from ByType plus UntypedBalance")
		}
	}
}

// TestHouseholdAggregatedHistoryCarry_RefingerprintedJointAccount_CarriedRowUsesTheDaysFingerprint
// is why a carried row takes the fingerprint in force on the CARRIED day rather than the one its
// source day recorded.
//
// ⚠ ADDED BY MUTATION. Replacing the CASE with a plain `o.fingerprint` was killed only by the
// structural pin test, because no fixture re-fingerprinted anything. household_account_shares
// records the physical identity per GRANT (physical_account_blind_idx), so a joint account
// re-identified mid-history has one fingerprint before the re-share and another after — and the
// joint-account collapse keys on the fingerprint. A stale one travelling forward across the
// re-share stops colliding with the other member's copy, and the SAME physical account is summed
// twice on precisely the days the carry-forward adds.
//
// The fixture: the member's grant is closed at 07:13 on day 1 and re-opened with the owner's
// fingerprint, so day 0 legitimately counts both copies (the household had no record they were the
// same account yet) while day 2 must count one.
func TestHouseholdAggregatedHistoryCarry_RefingerprintedJointAccount_CarriedRowUsesTheDaysFingerprint(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)
	c := newHouseholdCarryClock()

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)
	backdateHouseholdAccountActive(t, pool, ownerAcct.AccountID, c.day(-20))
	backdateHouseholdAccountActive(t, pool, memberAcct.AccountID, c.day(-20))

	fpJoint := "joint-refp-" + hh
	fpOld := "stale-refp-" + hh
	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, &fpJoint, domain.ShareVisibilityShared, c.at(-5, 8, 0))
	// The member's FIRST grant carries the pre-re-identification fingerprint …
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, &fpOld, domain.ShareVisibilityShared, c.at(-5, 8, 0))

	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 300.33, c.at(0, 10, 33))
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 500.55, c.at(0, 9, 17))

	// … and is replaced at 07:13 on day 1 by one that records the joint identity.
	revokeHouseholdShareAt(t, pool, hh, member, memberAcct.UserAccountID, c.at(1, 7, 13))
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, &fpJoint, domain.ShareVisibilityShared, c.at(1, 7, 13))

	// Day 2: only the owner's copy reports; the member's is carried from day 0.
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 520.66, c.at(2, 9, 17))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, c.day(-1), c.day(10), nil)
	require.NoError(t, err)

	requireTotalOn(t, points, c.day(0), 800.88,
		"on day 0 the household had no record the two copies were one account, so both count — "+
			"the fingerprint in force on a day is the one that was actually recorded then")
	requireTotalOn(t, points, c.day(2), 520.66,
		"by day 2 the re-share records the joint identity, so the CARRIED copy must collapse into "+
			"the owner's real observation. 820.99 here is the same physical account counted twice")
}
