// balance_snapshot_repository_household_integration_test.go — DB-backed behavior for the
// household-scope balance/net-worth queries added by #1369 (Phase 5).
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/repository/ -run TestBalanceSnapshotRepository_.*Household -v
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

func insertBalanceSnapshot(t *testing.T, pool *pgxpool.Pool, userID, accountID, userAccountID string, balance float64, snapshotAt time.Time) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO balance_snapshots (account_id, user_account_id, user_id, current_balance, currency_code, snapshot_at, source)
		VALUES ($1, $2, $3, $4, 'USD', $5, 'manual')
	`, accountID, userAccountID, userID, balance, snapshotAt)
	require.NoError(t, err, "seed balance snapshot")
}

func TestBalanceSnapshotRepository_GetSnapshotsAsOfForHousehold_IncludesSharedMemberAccount(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	owner, member, hh, ownerAcct, memberAcct := setupTwoMemberSharedHousehold(t, pool)
	now := time.Now().UTC()
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1000, now.AddDate(0, 0, -1))
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 500, now.AddDate(0, 0, -1))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	snapshots, err := repo.GetSnapshotsAsOfForHousehold(ctx, owner, scope, now)
	require.NoError(t, err)

	var total float64
	for _, s := range snapshots {
		if s.CurrentBalance != nil {
			total += *s.CurrentBalance
		}
	}
	assert.InDelta(t, 1500, total, 0.01, "owner's $1000 + member's $500 must both be included")
}

func TestBalanceSnapshotRepository_GetSnapshotsAsOfForHousehold_ExcludesHiddenVisibilityShare(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)
	shareRepo := repository.NewHouseholdShareRepository(pool)
	require.NoError(t, shareRepo.InsertShare(ctx, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityHidden))

	now := time.Now().UTC()
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 999, now.AddDate(0, 0, -1))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	snapshots, err := repo.GetSnapshotsAsOfForHousehold(ctx, owner, scope, now)
	require.NoError(t, err)
	assert.Empty(t, snapshots, "hidden-visibility share must be excluded")
}

func TestBalanceSnapshotRepository_GetSnapshotsAsOfForHousehold_JointAccountUsesFreshestSnapshot(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	owner, member, hh, ownerAcct, memberAcct := setupTwoMemberSharedHousehold(t, pool)
	now := time.Now().UTC()
	fingerprint := "joint-fp-" + hh

	// Re-share both with the SAME fingerprint (simulating a joint account).
	shareRepo := repository.NewHouseholdShareRepository(pool)
	require.NoError(t, shareRepo.RevokeSharesForUser(ctx, hh, owner))
	require.NoError(t, shareRepo.RevokeSharesForUser(ctx, hh, member))
	require.NoError(t, shareRepo.InsertShare(ctx, hh, ownerAcct.UserAccountID, &fingerprint, domain.ShareVisibilityShared))
	require.NoError(t, shareRepo.InsertShare(ctx, hh, memberAcct.UserAccountID, &fingerprint, domain.ShareVisibilityShared))

	// Owner's copy is STALE (synced 10 days ago); member's copy is FRESH (synced today).
	// Balance dedup must pick the freshest, not the earliest-shared (Codex P1-8).
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 100, now.AddDate(0, 0, -10))
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 250, now)

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	snapshots, err := repo.GetSnapshotsAsOfForHousehold(ctx, owner, scope, now)
	require.NoError(t, err)
	require.Len(t, snapshots, 1, "joint account must collapse to a single snapshot")
	require.NotNil(t, snapshots[0].CurrentBalance)
	assert.InDelta(t, 250, *snapshots[0].CurrentBalance, 0.01, "freshest-synced copy ($250) must win, not the earliest-shared stale copy ($100)")
}

// TestBalanceSnapshotRepository_GetSnapshotsAsOfForHousehold_JointAccountWinnerRespectsAsOfCutoff
// guards against Codex adversarial review finding P1-2: the joint-account fingerprint
// winner must be selected using "latest snapshot AT OR BEFORE asOf", not "latest snapshot
// ever". Otherwise a previous-period (asOf=30d ago) query could pick the globally-freshest
// member's copy even when that copy has no snapshot before the cutoff, silently returning
// zero data for the joint account instead of the OTHER member's genuinely earlier balance.
func TestBalanceSnapshotRepository_GetSnapshotsAsOfForHousehold_JointAccountWinnerRespectsAsOfCutoff(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	owner, member, hh, ownerAcct, memberAcct := setupTwoMemberSharedHousehold(t, pool)
	now := time.Now().UTC()
	fingerprint := "joint-fp-" + hh

	shareRepo := repository.NewHouseholdShareRepository(pool)
	require.NoError(t, shareRepo.RevokeSharesForUser(ctx, hh, owner))
	require.NoError(t, shareRepo.RevokeSharesForUser(ctx, hh, member))
	require.NoError(t, shareRepo.InsertShare(ctx, hh, ownerAcct.UserAccountID, &fingerprint, domain.ShareVisibilityShared))
	require.NoError(t, shareRepo.InsertShare(ctx, hh, memberAcct.UserAccountID, &fingerprint, domain.ShareVisibilityShared))

	// Owner's copy: only has an OLD snapshot (40 days ago) — no recent activity.
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 100, now.AddDate(0, 0, -40))
	// Member's copy: globally freshest (synced TODAY), but has NO snapshot before 35 days ago.
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 250, now)

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}

	// Querying "as of 35 days ago": member's copy has no snapshot before this cutoff, but
	// owner's copy does ($100 from 40 days ago). The winner selection must respect the
	// asOf cutoff and resolve to owner's copy, not silently drop the joint account.
	snapshots, err := repo.GetSnapshotsAsOfForHousehold(ctx, owner, scope, now.AddDate(0, 0, -35))
	require.NoError(t, err)
	require.Len(t, snapshots, 1, "joint account must still resolve using the pre-cutoff copy, not disappear")
	require.NotNil(t, snapshots[0].CurrentBalance)
	assert.InDelta(t, 100, *snapshots[0].CurrentBalance, 0.01, "must use owner's pre-cutoff snapshot, not silently return nothing")

	// Querying "as of today": member's copy IS eligible now and is the freshest overall.
	current, err := repo.GetSnapshotsAsOfForHousehold(ctx, owner, scope, now)
	require.NoError(t, err)
	require.Len(t, current, 1)
	require.NotNil(t, current[0].CurrentBalance)
	assert.InDelta(t, 250, *current[0].CurrentBalance, 0.01, "as of today, member's freshest copy must win")
}

func TestBalanceSnapshotRepository_GetAggregatedHistoryForHousehold_ClipsToSharedAt(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	now := time.Now().UTC()
	sharedAt := now.AddDate(0, 0, -5)
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, sharedAt)

	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 40, sharedAt.AddDate(0, 0, -1))
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 60, sharedAt.AddDate(0, 0, 1))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, now.AddDate(0, -1, 0), now, nil)
	require.NoError(t, err)

	foundBefore, foundAfter := false, false
	for _, p := range points {
		if p.TotalBalance == 40 {
			foundBefore = true
		}
		if p.TotalBalance == 60 {
			foundAfter = true
		}
	}
	assert.False(t, foundBefore, "snapshot dated before shared_at must be excluded")
	assert.True(t, foundAfter, "snapshot dated after shared_at must be included")
}

// TestBalanceSnapshotRepository_GetAggregatedHistoryForHousehold_JointAccountPerDayDedup guards
// against #1378: GetAggregatedHistoryForHousehold used to resolve a joint account's fingerprint
// collision to ONE globally-freshest winner for the WHOLE query (householdAccountsCTEForBalances),
// so a member who started syncing mid-range would erase the OTHER member's genuinely-earlier
// history from every day of the series, not merely clip it at share_started_at. The fix must pick
// a winner PER CALENDAR DAY instead.
func TestBalanceSnapshotRepository_GetAggregatedHistoryForHousehold_JointAccountPerDayDedup(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	// Anchor all "day" arithmetic at noon UTC so intra-day hour offsets below never cross a
	// calendar-day boundary regardless of what wall-clock time the test actually runs at.
	today := time.Date(time.Now().UTC().Year(), time.Now().UTC().Month(), time.Now().UTC().Day(), 12, 0, 0, 0, time.UTC)
	fingerprint := "joint-fp-" + hh
	sharedAt := today.AddDate(0, -2, 0) // well before the queried range — no clipping in play here
	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, &fingerprint, domain.ShareVisibilityShared, sharedAt)
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, &fingerprint, domain.ShareVisibilityShared, sharedAt)

	// Owner's copy: EARLY snapshots, then stops syncing.
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 100, today.AddDate(0, 0, -20))
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 100, today.AddDate(0, 0, -16))
	// Day -2: BOTH copies sync on the SAME calendar day — owner earlier in the day (stale
	// within that day), member later (fresh within that day). Proves the per-day winner is
	// resolved WITHIN a day of genuine overlap, not merely across non-overlapping days.
	overlapDay := today.AddDate(0, 0, -2)
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 100, overlapDay.Add(-2*time.Hour))
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 250, overlapDay.Add(2*time.Hour))
	// Member's copy: starts syncing LATE in the range — globally freshest overall.
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 250, today)

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, today.AddDate(0, -1, 0), today, nil)
	require.NoError(t, err)

	foundEarly, foundLate := false, false
	for _, p := range points {
		assert.NotEqual(t, float64(350), p.TotalBalance, "joint account must never double-count both copies on a single day, including a day both copies genuinely overlap on")
		if p.TotalBalance == 100 {
			foundEarly = true
		}
		if p.TotalBalance == 250 {
			foundLate = true
		}
	}
	assert.True(t, foundEarly, "owner's earlier data for the joint account must survive per-day, not be erased by member's globally-freshest copy")
	assert.True(t, foundLate, "member's later data for the joint account must be included")
}

// TestBalanceSnapshotRepository_GetAggregatedHistoryForHousehold_NonJointAccountAggregatesEveryDay
// is a regression guard: a normal (non-joint, nil-fingerprint) household account must still
// aggregate every day it has a snapshot, proving the per-day fingerprint dedup falls back
// correctly to user_account_id when there's no physical fingerprint collision to resolve.
func TestBalanceSnapshotRepository_GetAggregatedHistoryForHousehold_NonJointAccountAggregatesEveryDay(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))

	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 10, now.AddDate(0, 0, -2))
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 20, now.AddDate(0, 0, -1))
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 30, now)

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, now.AddDate(0, -1, 0), now, nil)
	require.NoError(t, err)

	var totals []float64
	for _, p := range points {
		totals = append(totals, p.TotalBalance)
	}
	assert.Contains(t, totals, float64(10), "day -2 balance must be present")
	assert.Contains(t, totals, float64(20), "day -1 balance must be present")
	assert.Contains(t, totals, float64(30), "day 0 balance must be present")
}

// TestBalanceSnapshotRepository_GetAggregatedHistoryForHousehold_JointAccountTypeFilterAppliesToWinner
// guards against a fix regression flagged in Codex adversarial review (finding H1): the
// accountTypes filter must be applied to the PER-DAY WINNER's type, not used to filter the
// DISTINCT ON candidate set before freshness is resolved. The two joint-account copies are
// given DIFFERENT account types (owner=depository, member=credit) and made to overlap on the
// SAME calendar day, so the two filtering strategies produce different, distinguishable
// results: filtering candidates before ranking would let the stale depository copy leak
// through on the overlap day even though the fresher credit copy should have won (and then
// been excluded by the filter); filtering the winner's type afterward correctly excludes that
// day entirely.
func TestBalanceSnapshotRepository_GetAggregatedHistoryForHousehold_JointAccountTypeFilterAppliesToWinner(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	// insertTestAccount seeds both accounts as type 'depository' by default; give the member's
	// copy a different type so the two copies are distinguishable by type.
	_, err := pool.Exec(ctx, `UPDATE accounts SET type = 'credit' WHERE id = $1`, memberAcct.AccountID)
	require.NoError(t, err, "set member account type to credit")

	today := time.Date(time.Now().UTC().Year(), time.Now().UTC().Month(), time.Now().UTC().Day(), 12, 0, 0, 0, time.UTC)
	fingerprint := "joint-fp-type-" + hh
	sharedAt := today.AddDate(0, -2, 0)
	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, &fingerprint, domain.ShareVisibilityShared, sharedAt)
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, &fingerprint, domain.ShareVisibilityShared, sharedAt)

	// Day -20: owner (depository) only — an unambiguous depository-only day.
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 100, today.AddDate(0, 0, -20))
	// Day -5: BOTH copies overlap — owner (depository, stale within the day) at 10:00, member
	// (credit, fresh within the day) at 14:00. The day's true winner is the credit copy.
	overlapDay := today.AddDate(0, 0, -5)
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 999, overlapDay.Add(-2*time.Hour))
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 250, overlapDay.Add(2*time.Hour))
	// Day 0: member (credit) only — an unambiguous credit-only day.
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 300, today)

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}

	depositoryOnly, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, today.AddDate(0, -1, 0), today, []string{"depository"})
	require.NoError(t, err)
	var depositoryTotals []float64
	for _, p := range depositoryOnly {
		depositoryTotals = append(depositoryTotals, p.TotalBalance)
	}
	assert.Contains(t, depositoryTotals, float64(100), "the unambiguous depository-only day must still surface")
	assert.NotContains(t, depositoryTotals, float64(999),
		"the stale depository copy on the overlap day must NOT leak through — the fresher credit copy won that day and doesn't match the filter")
	assert.NotContains(t, depositoryTotals, float64(250), "the credit copy must never be labeled depository")
	assert.NotContains(t, depositoryTotals, float64(300), "the credit-only day must not match a depository filter")

	creditOnly, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, today.AddDate(0, -1, 0), today, []string{"credit"})
	require.NoError(t, err)
	var creditTotals []float64
	for _, p := range creditOnly {
		creditTotals = append(creditTotals, p.TotalBalance)
	}
	assert.Contains(t, creditTotals, float64(250), "the overlap day's true (credit) winner must surface under a credit filter")
	assert.Contains(t, creditTotals, float64(300), "the unambiguous credit-only day must still surface")
	assert.NotContains(t, creditTotals, float64(100), "the depository-only day must not match a credit filter")
	assert.NotContains(t, creditTotals, float64(999), "the stale depository copy must never surface under any filter")
}

// TestBalanceSnapshotRepository_GetAggregatedHistoryForHousehold_JointAccountCopiesClipIndependently
// is a regression guard for the fix in #1378: when two copies of the same joint-account
// fingerprint have DIFFERENT share_started_at values, each copy must be clipped against ITS OWN
// share_started_at, not some shared/collapsed value. Owner's copy was shared 2 months ago;
// member's copy was shared only 8 days ago — a much more recent share date. Owner's snapshot at
// day -20 falls after owner's own share date but before member's, and must still be INCLUDED
// because clipping is per-row (per household_accounts copy), not per-fingerprint-collapsed.
// Member's snapshot at day -15 falls before member's own share date and must be EXCLUDED, even
// though owner's data from that same era survives — proving the two copies clip independently.
func TestBalanceSnapshotRepository_GetAggregatedHistoryForHousehold_JointAccountCopiesClipIndependently(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	// Anchor all "day" arithmetic at noon UTC so intra-day hour offsets never cross a calendar-day
	// boundary regardless of what wall-clock time the test actually runs at.
	today := time.Date(time.Now().UTC().Year(), time.Now().UTC().Month(), time.Now().UTC().Day(), 12, 0, 0, 0, time.UTC)
	fingerprint := "joint-fp-clip-independent-" + hh

	ownerSharedAt := today.AddDate(0, -2, 0)  // 2 months ago — well before the queried range
	memberSharedAt := today.AddDate(0, 0, -8) // only 8 days ago — much more recent than owner's
	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, &fingerprint, domain.ShareVisibilityShared, ownerSharedAt)
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, &fingerprint, domain.ShareVisibilityShared, memberSharedAt)

	// Owner snapshot at day -20: after owner's OWN share date (2 months ago) but before member's
	// (8 days ago). Must be INCLUDED — clipping uses owner's own share date, not member's.
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 111, today.AddDate(0, 0, -20))
	// Member snapshot at day -15: before member's OWN share date (8 days ago). Must be EXCLUDED,
	// even though the same calendar-day window is otherwise in range and owner's data from that
	// era is included — this is the crux of the "independent" claim.
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 222, today.AddDate(0, 0, -15))
	// Member snapshot at day -3: after member's own share date (8 days ago). Must be INCLUDED.
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 333, today.AddDate(0, 0, -3))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, today.AddDate(0, -1, 0), today, nil)
	require.NoError(t, err)

	found111, found222, found333 := false, false, false
	for _, p := range points {
		if p.TotalBalance == 111 {
			found111 = true
		}
		if p.TotalBalance == 222 {
			found222 = true
		}
		if p.TotalBalance == 333 {
			found333 = true
		}
	}
	assert.True(t, found111, "owner's early data must survive despite member's later share date — clipping uses owner's own share_started_at")
	assert.False(t, found222, "member's pre-share data must be excluded even though owner's data from the same era is included")
	assert.True(t, found333, "member's post-share data must be included")
}

// TestGetAggregatedHistoryForHousehold_RelinkChain_UsesActiveAccountType is the regression test for a
// defect introduced while fixing #1561, and caught by adversarial review — not by any existing test.
//
// #1561 removed `AND a.status = 'active'` from the household analytics joins, correctly: there the
// filter was dropping pre-relink transaction history that personal scope deliberately keeps.
//
// Applying the same removal to THIS query was wrong, because status plays a different role here.
// Balance snapshots are already selected by `bs.user_account_id`, so every member of a re-link chain
// contributes its snapshots regardless of status — no history is being dropped. The accounts join
// exists solely to supply `a.type` for the per-type grouping, and it matches EVERY chain member
// sharing that `user_account_id`. With nothing expressing a preference, `DISTINCT ON` broke the tie
// on `a.id` — an arbitrary UUID — so the balance could be attributed to a REPLACED account's type,
// and with an accountTypes filter applied it could be dropped from the result entirely.
//
// WHAT THE FIX IS, stated exactly, because this comment previously argued for the wrong instrument.
// It reads as though the fix restored the JOIN PREDICATE. It did not, and restoring it would have
// reintroduced a second bug: when NO active row shares the user_account_id — what
// ExpireAccountsByFingerprint leaves behind for a whole fingerprint — a filtered join yields a NULL
// `a.type`, which NULL never satisfies `= ANY`, so the accountTypes filter drops those snapshots
// outright while personal scope keeps them. The join is therefore UNFILTERED
// (`LEFT JOIN accounts a ON a.user_account_id = bs.user_account_id`,
// balance_snapshot_repository.go:320) and the preference lives in the ORDER BY:
// `(a.status = 'active') DESC NULLS LAST, a.id` (:313) — active wins, any chain member's type is the
// fallback, `a.id` only breaks ties within a status class. Both halves are pinned: this test (active
// wins) and TestGetAggregatedHistoryForHousehold_ExpiredOnlyChain_KeepsTypeFilteredHistory
// (fallback).
//
// The active account's type is the authoritative one. This test seeds an active `depository` and a
// replaced `credit` account on the SAME user_account_id, with the replaced account given a
// deliberately LOWER uuid so the `a.id` tiebreak would choose it — making the pre-fix failure
// deterministic rather than a coin flip.
func TestGetAggregatedHistoryForHousehold_RelinkChain_UsesActiveAccountType(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	owner := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ua := insertTestUserAccount(t, pool, owner)
	li := insertTestLinkedItem(t, pool, owner)

	// Replaced chain member: type 'credit', uuid forced to sort FIRST so the a.id tiebreak prefers it.
	const lowUUID = "00000000-0000-4000-8000-000000000001"
	_, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, user_account_id,
		                      status, replaced_at)
		VALUES ($1::uuid, $2, $3, 'plaid_'||gen_random_uuid()::text, 'Old (relinked)', 'credit', $4,
		        'replaced', now())`, lowUUID, li, owner, ua)
	require.NoError(t, err, "seed the replaced chain member")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id = $1::uuid`, lowUUID) //nolint:errcheck
	})

	// Active chain member: type 'depository' (the authoritative type).
	activeID := insertTestAccount(t, pool, li, owner, ua)

	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, ua, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))

	_, err = pool.Exec(ctx, `
		INSERT INTO balance_snapshots (account_id, user_id, user_account_id, current_balance, snapshot_at,
		                               currency_code, source)
		VALUES ($1, $2, $3, 500.00, $4, 'USD', 'plaid')`, activeID, owner, ua, now.AddDate(0, 0, -1))
	require.NoError(t, err, "seed the balance snapshot")

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}}
	rows, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, now.AddDate(0, 0, -7), now, nil)
	require.NoError(t, err)
	require.NotEmpty(t, rows, "the shared account's snapshot must appear in household history")

	for _, r := range rows {
		assert.Contains(t, r.ByType, "depository",
			"the balance must be attributed to the ACTIVE account's type. A 'credit' bucket here means "+
				"the accounts join is picking a REPLACED chain member via the arbitrary a.id tiebreak — "+
				"balances land in the wrong type bucket, and an accountTypes filter would drop them "+
				"entirely. Got ByType=%v", r.ByType)
		assert.NotContains(t, r.ByType, "credit",
			"the replaced chain member's type must not appear at all: it is the same physical account, "+
				"and only the active row's type is authoritative. Got ByType=%v", r.ByType)
	}
}

// TestGetAggregatedHistoryForHousehold_ExpiredOnlyChain_KeepsTypeFilteredHistory encodes the
// adversarial-review claim about the counterpart of the 58920ae3 case: when NO active row shares
// the snapshot's user_account_id — exactly the state AccountRepository.ExpireAccountsByFingerprint
// leaves behind for a whole fingerprint — the `AND a.status = 'active'` join predicate yields a
// NULL account_type, and `WHERE account_type = ANY($6)` then discards the row, because NULL never
// satisfies `= ANY`. The snapshots exist, personal scope still reports them (it takes the type from
// the snapshot's own account row), but type-filtered household history silently loses them.
func TestGetAggregatedHistoryForHousehold_ExpiredOnlyChain_KeepsTypeFilteredHistory(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	owner := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ua := insertTestUserAccount(t, pool, owner)
	li := insertTestLinkedItem(t, pool, owner)

	// The only accounts row for this user_account_id is 'depository' and has been EXPIRED, as
	// ExpireAccountsByFingerprint does to every active row sharing a fingerprint.
	acctID := insertTestAccount(t, pool, li, owner, ua)
	// accounts_expired_at_consistency (migration 000155, Issue #1569 Task 6) requires expired_at
	// whenever status = 'expired'.
	_, err := pool.Exec(ctx, `UPDATE accounts SET status = 'expired', expired_at = NOW() WHERE id = $1`, acctID)
	require.NoError(t, err, "expire the only chain member")

	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, ua, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))
	insertBalanceSnapshot(t, pool, owner, acctID, ua, 700, now.AddDate(0, 0, -1))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}}

	unfiltered, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, now.AddDate(0, 0, -7), now, nil)
	require.NoError(t, err)
	require.NotEmpty(t, unfiltered, "the expired chain's snapshots must still appear with no type filter")
	foundTyped := false
	for _, p := range unfiltered {
		if p.ByType["depository"] == 700 {
			foundTyped = true
		}
	}
	assert.True(t, foundTyped,
		"with no active row on the chain, the type must still be resolved from the expired member — "+
			"an empty/absent type bucket means account_type came back NULL. Got %v", unfiltered)

	filtered, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, now.AddDate(0, 0, -7), now, []string{"depository"})
	require.NoError(t, err)
	var total float64
	for _, p := range filtered {
		total += p.TotalBalance
	}
	assert.InDelta(t, 700, total, 0.01,
		"a depository filter must still match the expired-only chain; NULL account_type fails "+
			"`= ANY` and drops the history entirely. Got %v", filtered)
}
