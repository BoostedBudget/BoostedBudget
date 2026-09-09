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
	"github.com/maxnussbaum/BoostedBudget/internal/services"
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

// mintUserAccountIdentity sets a user_accounts row's FROZEN identity, which the fixture helper
// insertTestUserAccount deliberately leaves NULL.
//
// The UPDATE is legitimate here for two independently-checked reasons. Migration 000179 relaxed
// trg_user_accounts_identity_immutable to permit exactly one transition — NULL -> value, the
// first-time claim — and rejects value -> different value; and the AST guard
// TestNoUpdateOfUserAccountsIdentityColumns skips `_test.go` files (its walk returns early on the
// `_test.go` suffix), so it governs application code rather than a fixture.
func mintUserAccountIdentity(t *testing.T, pool *pgxpool.Pool, userAccountID, identityType, identitySubtype string) {
	t.Helper()
	tag, err := pool.Exec(context.Background(), `
		UPDATE user_accounts SET identity_type = $2, identity_subtype = $3 WHERE id = $1`,
		userAccountID, identityType, identitySubtype)
	require.NoError(t, err, "mint user_accounts identity")
	require.Equal(t, int64(1), tag.RowsAffected(),
		"expected to mint exactly one user_accounts row — a fixture that silently minted nothing "+
			"would leave the orphan untyped and this test would assert the pre-fix answer")
}

// pointOnDay returns the chart point for a UTC calendar day, failing the test when the day is
// absent — the assertions below are about a point's TYPE BREAKDOWN, so "no such day" and "a day
// whose breakdown is empty" must not be conflated.
func pointOnDay(t *testing.T, points []domain.BalanceHistoryPoint, day time.Time) *domain.BalanceHistoryPoint {
	t.Helper()
	want := day.UTC().Format("2006-01-02")
	for i := range points {
		if points[i].Date.UTC().Format("2006-01-02") == want {
			return &points[i]
		}
	}
	t.Fatalf("no chart point for %s", want)
	return nil
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
//
// ⚠ RE-AIMED BY THE FINAL WHOLE-BRANCH REVIEW (finding F1). This test used to assert that the
// orphan's balance lands in UntypedBalance — and that assertion was PINNING A LIVE WRONG NUMBER.
// The personal read recovers an orphan's type from its own chain row
// (COALESCE(a.type, ua.identity_type)) precisely so a credit card is not reconstructed as an asset;
// its test says so in as many words. The household read did not, `identity_type` appeared exactly
// once in balance_snapshot_repository.go, and GetNetWorthHistory routes a POSITIVE UntypedBalance
// straight into totalAssets — while Plaid reports a credit card's current_balance as a POSITIVE
// debt. So a household's orphaned credit card ADDED its balance to assets instead of subtracting
// it: net worth overstated by twice the debt. Phase 3 item 5 was half-applied in household scope,
// and this test defended the missing half.
//
// It therefore asserts the ARITHMETIC, through the real reconstruction in
// services.GetNetWorthHistory, not merely that a field is populated: 42.42 of debt must land in
// TotalLiabilities and must be absent from TotalAssets.
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

	// The orphan's logical account is a CREDIT CARD, recorded on its chain row. That is the whole
	// point of the fixture: an untyped bucket routes a positive balance to assets, so the wrong
	// answer and the right one differ by 2 x 42.42 in net worth.
	mintUserAccountIdentity(t, pool, memberAcct.UserAccountID, "credit", "credit card")
	_, err := pool.Exec(ctx, `UPDATE accounts SET type = 'credit' WHERE id = $1`, memberAcct.AccountID)
	require.NoError(t, err, "keep the fixture coherent: the chain row's identity is its account's type")

	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, nil, domain.ShareVisibilityShared, c.at(-5, 8, 0))
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, c.at(-5, 8, 0))

	// The orphan: account_id IS NULL, user_account_id intact — which is exactly what the chain
	// pointer is for, and it is what keeps the row inside the household's universe.
	_, err = pool.Exec(ctx, `
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

	day0 := pointOnDay(t, points, c.day(0))
	assert.InDelta(t, 42.42, day0.ByType["credit"], 0.001,
		"an orphan's type is recoverable from its own chain row (user_accounts.identity_type), and "+
			"a credit card must not be reconstructed as an ASSET, which is what an untyped bucket "+
			"would do — the personal read has said so since Phase 3 item 5")
	assert.InDelta(t, 0.0, day0.UntypedBalance, 0.001,
		"nothing is left untyped while the chain row survives")
	assert.InDelta(t, 1000.11, day0.ByType["depository"], 0.001,
		"and the owner's asset is still its own type, not merged into the recovered one")

	// THE ARITHMETIC, through the real reconstruction rather than a restatement of it. A field
	// being populated is not the defect; the defect is which side of the balance sheet the money
	// reaches. Pre-fix this read TotalAssets 1042.53 / TotalLiabilities 0 / NetWorth 1042.53.
	start, end := c.day(-1), c.day(10)
	nw, err := services.NewDashboardService(repo, nil).GetNetWorthHistory(ctx, owner,
		&domain.DashboardFilters{StartDate: &start, EndDate: &end, HouseholdScope: scope})
	require.NoError(t, err)

	var day0NW *domain.NetWorthHistorySnapshot
	for i := range nw.Snapshots {
		if nw.Snapshots[i].Date.UTC().Format("2006-01-02") == c.day(0).UTC().Format("2006-01-02") {
			day0NW = &nw.Snapshots[i]
		}
	}
	require.NotNil(t, day0NW, "expected a net-worth snapshot for the orphan's own day")
	assert.InDelta(t, 42.42, day0NW.TotalLiabilities, 0.001,
		"the orphaned credit card's balance is a DEBT and must reach liabilities")
	assert.InDelta(t, 1000.11, day0NW.TotalAssets, 0.001,
		"and it must NOT be added to assets: an untyped positive balance is routed to assets, "+
			"which overstated household net worth by twice the debt")
	assert.InDelta(t, 957.69, day0NW.NetWorth, 0.001,
		"net worth is assets minus the debt, not assets plus it")

	// FINDING F5, the same expression seen from the filter's side. The type predicate and the
	// projection have to agree about what an orphan's type IS, or the same account gets two answers
	// — present in ByType["credit"] for an unfiltered request and absent for accountTypes=['credit'].
	filtered, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, c.day(-1), c.day(10),
		[]string{"credit"})
	require.NoError(t, err)
	filteredDay0 := pointOnDay(t, filtered, c.day(0))
	assert.InDelta(t, 42.42, filteredDay0.ByType["credit"], 0.001,
		"the recovered type must be the one the accountTypes filter tests, so an orphaned credit "+
			"card cannot appear for an unfiltered request and vanish for accountTypes=['credit']")
	assert.InDelta(t, 42.42, filteredDay0.TotalBalance, 0.001,
		"and the filtered total is the credit card alone")
}

// TestHouseholdAggregatedHistoryCarry_UnmintedChainRow_IsReportedAsUntypedRatherThanDiscarded is the
// household twin of TestAggregatedHistoryCarry_FullyDetachedOrphan_IsReportedAsUntypedRatherThanDiscarded.
//
// ⚠ IT IS NOT THE SAME FIXTURE, and it cannot be. The personal residue is a snapshot with NEITHER
// pointer — account_id and user_account_id both NULL. Such a row can never reach household scope at
// all: the household `observed` CTE reaches it through `JOIN household_accounts ha ON
// ha.user_account_id = bs.user_account_id`, so a NULL chain pointer means no grant matches and the
// row is outside the household's universe by construction, not merely untyped in it.
//
// The reachable household residue is therefore an orphan whose chain row SURVIVES but was never
// minted: user_accounts.identity_type IS NULL, which migration 000177's header calls an orphan
// user_accounts row and migration 000179 documents as a shape no application code claims. The
// balance must still be REPORTED — the total is provider truth — and reported as UNTYPED, because
// nothing anywhere records what kind of account it was and none may be invented.
func TestHouseholdAggregatedHistoryCarry_UnmintedChainRow_IsReportedAsUntypedRatherThanDiscarded(t *testing.T) {
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

	// Deliberately NOT minted: insertTestUserAccount leaves identity_type NULL, and that is the
	// shape this test exists for. Asserted rather than assumed, because the whole test turns on it.
	var identityType *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT identity_type FROM user_accounts WHERE id = $1`, memberAcct.UserAccountID).Scan(&identityType))
	require.Nil(t, identityType,
		"the fixture must really leave the chain row unminted — otherwise this test asserts the "+
			"RECOVERY path and says nothing about the residue")

	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, nil, domain.ShareVisibilityShared, c.at(-5, 8, 0))
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, c.at(-5, 8, 0))

	_, err := pool.Exec(ctx, `
		INSERT INTO balance_snapshots (account_id, user_account_id, user_id, current_balance,
		                               currency_code, snapshot_at, source)
		VALUES (NULL, $1, $2, 63.37, 'USD', $3, 'manual')`,
		memberAcct.UserAccountID, member, c.at(0, 10, 33))
	require.NoError(t, err, "seed orphaned snapshot with an unminted chain row")

	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1000.11, c.at(0, 9, 17))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, c.day(-1), c.day(10), nil)
	require.NoError(t, err)

	requireTotalOn(t, points, c.day(0), 1063.48, "the balance still reaches the total")
	day0 := pointOnDay(t, points, c.day(0))
	assert.InDelta(t, 63.37, day0.UntypedBalance, 0.001,
		"it must be RECORDED as untyped, not discarded: the net-worth reconstruction is driven by "+
			"ByType plus UntypedBalance and would otherwise lose it entirely")
	assert.Equal(t, map[string]float64{"depository": 1000.11}, day0.ByType,
		"the owner's asset is the ONLY typed entry: the empty type must never become a ByType key, "+
			"because domain.AccountTypeCategory(\"\") defaults to assets and that is the sign guess "+
			"UntypedBalance exists to avoid")
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

// TestHouseholdAggregatedHistoryCarry_RelinkChain_CarriedPredecessorIsStillSuperseded is why
// live_all is built from the FILLED series rather than from the observed rows.
//
// ⚠ ADDED BY MUTATION. Pointing live_all at `observed` was killed only by the structural pin test.
// The succession rule emits a (predecessor, successor) pair only when BOTH members are live on that
// day, and it takes "live on that day" from live_all — so if live_all sees only the OBSERVED rows,
// a predecessor that is present on a later day ONLY because the carry put it there is never marked
// superseded, and the same physical account is counted twice on exactly the days the carry adds.
//
// The predecessor here is genuinely carriable on day 3: its recorded 'active' interval runs until
// 06:00 that day (accountLiveOnDateClause is an interval OVERLAP test), even though accounts.status
// already reads 'replaced'. That combination is what makes the day-3 assertion the one that
// separates the two spellings of live_all.
func TestHouseholdAggregatedHistoryCarry_RelinkChain_CarriedPredecessorIsStillSuperseded(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)
	c := newHouseholdCarryClock()

	owner := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ua := insertTestUserAccount(t, pool, owner)
	li := insertTestLinkedItem(t, pool, owner)

	// Both chain members hang off ONE user_account, which is what a Plaid re-link produces.
	oldID := insertTestAccountNamed(t, pool, li, owner, ua, "Old (relinked)", "depository")
	newID := insertTestAccountNamed(t, pool, li, owner, ua, "New (relinked)", "depository")
	backdateHouseholdAccountActive(t, pool, oldID, c.day(-20))
	backdateHouseholdAccountActive(t, pool, newID, c.at(2, 0, 0))

	// The recorded succession, plus the terminal status the re-link handler writes.
	_, err := pool.Exec(ctx, `
		UPDATE accounts SET status = 'replaced', replaced_at = $2, replaced_by_id = $3::uuid
		WHERE id = $1`, oldID, c.at(3, 6, 0), newID)
	require.NoError(t, err)
	tag, err := pool.Exec(ctx, `
		UPDATE account_status_history SET valid_to = $2
		WHERE account_id = $1 AND status = 'active'`, oldID, c.at(3, 6, 0))
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.RowsAffected(), "expected one 'active' interval to close")
	tag, err = pool.Exec(ctx, `
		UPDATE account_status_history SET valid_from = $2
		WHERE account_id = $1 AND status = 'replaced'`, oldID, c.at(3, 6, 0))
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.RowsAffected(), "expected one 'replaced' interval to backdate")

	insertHouseholdShare(t, pool, hh, ua, nil, domain.ShareVisibilityShared, c.at(-5, 8, 0))

	// Day 2: the overlap day, both members report. Day 3: only the successor does.
	insertBalanceSnapshot(t, pool, owner, oldID, ua, 300.33, c.at(2, 9, 17))
	insertBalanceSnapshot(t, pool, owner, newID, ua, 500.55, c.at(2, 10, 41))
	insertBalanceSnapshot(t, pool, owner, newID, ua, 520.66, c.at(3, 9, 17))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, c.day(-1), c.day(10), nil)
	require.NoError(t, err)

	requireTotalOn(t, points, c.day(2), 500.55,
		"the overlap day was already collapsed before the carry existed — 800.88 here means the "+
			"recorded-succession rule stopped being applied at all")
	requireTotalOn(t, points, c.day(3), 520.66,
		"and a predecessor present only BECAUSE of the carry must still be superseded — 820.99 "+
			"here is one physical account counted twice on a day the carry itself added")
}
