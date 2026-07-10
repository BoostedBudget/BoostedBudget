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
