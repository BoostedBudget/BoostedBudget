//go:build integration

// maskless_pair_balance_integration_test.go — #1569 task 10.2.
//
// THE DEFECT. Two accounts at one institution that Plaid reports with NO mask — a brokerage and
// an IRA at the same broker, an entirely ordinary shape — are given the SAME user_accounts row by
// userAccountService.MatchOrCreate, and the schema ENFORCES that: migration 000100 creates
// `UNIQUE (user_id, institution_id_blind_idx, mask_blind_idx) NULLS NOT DISTINCT`, so a second
// maskless row cannot even be created. Both accounts then stay 'active' forever.
//
// Every balance read used to dedup snapshots on COALESCE(user_account_id, account_id) — one row
// per CHAIN — so the pair collapsed to ONE row and the user's reported total was ONE account's
// balance. Nobody noticed because ChangePercent stays roughly right: both the current figure and
// the 30-day baseline halve together, so the trend looks correct while the level is wrong.
//
// These are DB-backed on purpose. pgxmock returns canned rows and cannot tell a correct DISTINCT
// ON from a wrong one, and the sibling defect on the portfolio side was measured, not reasoned:
// see account_liveness_sql.go's comment for the two wrong versions that came first.
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

// insertTestAccountNamed inserts one accounts row under an existing linked item and user account.
// insertTestAccount cannot be reused because the maskless case needs TWO accounts under ONE
// (linked_item, user_account) pair, and needs to control the type.
func insertTestAccountNamed(t *testing.T, pool *pgxpool.Pool, linkedItemID, userID, userAccountID, name, acctType string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type, user_account_id, status)
		VALUES ($1, $2, 'plaid_'||gen_random_uuid()::text, $3, $4, $5, 'active')
		RETURNING id::text
	`, linkedItemID, userID, name, acctType, userAccountID).Scan(&id)
	require.NoError(t, err, "insertTestAccountNamed")
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM accounts WHERE id = $1`, id) //nolint:errcheck
	})
	return id
}

// masklessPair is the fixture the whole file turns on: ONE user_accounts row with TWO live
// accounts hanging off ONE linked item, exactly what MatchOrCreate produces for two maskless
// accounts at one institution.
type masklessPair struct {
	UserID        string
	UserAccountID string
	LinkedItemID  string
	BrokerageID   string
	IRAID         string
}

func seedMasklessPair(t *testing.T, pool *pgxpool.Pool, userID string) masklessPair {
	t.Helper()
	ua := insertTestUserAccount(t, pool, userID)
	li := insertTestLinkedItem(t, pool, userID)
	return masklessPair{
		UserID:        userID,
		UserAccountID: ua,
		LinkedItemID:  li,
		BrokerageID:   insertTestAccountNamed(t, pool, li, userID, ua, "Brokerage", "investment"),
		IRAID:         insertTestAccountNamed(t, pool, li, userID, ua, "IRA", "investment"),
	}
}

func sumSnapshots(snaps []domain.BalanceSnapshot) float64 {
	var total float64
	for _, s := range snaps {
		if s.CurrentBalance != nil {
			total += *s.CurrentBalance
		}
	}
	return total
}

func TestMasklessPair_PersonalBalanceReads_CountBothAccounts(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	user := insertTestUser(t, pool)
	p := seedMasklessPair(t, pool, user)

	now := time.Now().UTC()
	at := now.Add(-2 * time.Hour)
	insertBalanceSnapshot(t, pool, user, p.BrokerageID, p.UserAccountID, 100, at)
	insertBalanceSnapshot(t, pool, user, p.IRAID, p.UserAccountID, 400, at.Add(time.Minute))

	latest, err := repo.GetLatestByUserID(ctx, user)
	require.NoError(t, err)
	assert.Len(t, latest, 2, "GetLatestByUserID must return BOTH maskless accounts, not one")
	assert.InDelta(t, 500.0, sumSnapshots(latest), 0.01,
		"400 here is the halving: one of two coexisting accounts was dropped by the chain dedup")

	asOf, err := repo.GetSnapshotsAsOfForUser(ctx, user, now)
	require.NoError(t, err)
	assert.Len(t, asOf, 2, "GetSnapshotsAsOfForUser must return BOTH maskless accounts")
	assert.InDelta(t, 500.0, sumSnapshots(asOf), 0.01, "as-of read halved the same way")

	points, err := repo.GetAggregatedHistory(ctx, user, at.Add(-24*time.Hour), now, nil)
	require.NoError(t, err)
	byDay := map[string]float64{}
	for _, pt := range points {
		byDay[pt.Date.Format("2006-01-02")] = pt.TotalBalance
	}
	day := at.Format("2006-01-02")
	require.Contains(t, byDay, day)
	assert.InDelta(t, 500.0, byDay[day], 0.01, "the history chart halved the same way")
}

func TestMasklessPair_HouseholdBalanceReads_CountBothAccounts(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	p := seedMasklessPair(t, pool, member)

	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, p.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))

	at := now.Add(-2 * time.Hour)
	insertBalanceSnapshot(t, pool, member, p.BrokerageID, p.UserAccountID, 100, at)
	insertBalanceSnapshot(t, pool, member, p.IRAID, p.UserAccountID, 400, at.Add(time.Minute))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}

	snaps, err := repo.GetSnapshotsAsOfForHousehold(ctx, owner, scope, now)
	require.NoError(t, err)
	assert.InDelta(t, 500.0, sumSnapshots(snaps), 0.01,
		"sharing the ONE user_account shares BOTH accounts behind it; 400 is the halving")

	points, err := repo.GetAggregatedHistoryForHousehold(ctx, owner, scope, at.Add(-24*time.Hour), now, nil)
	require.NoError(t, err)
	byDay := map[string]float64{}
	for _, pt := range points {
		byDay[pt.Date.Format("2006-01-02")] = pt.TotalBalance
	}
	day := at.Format("2006-01-02")
	require.Contains(t, byDay, day)
	assert.InDelta(t, 500.0, byDay[day], 0.01, "the household history chart halved the same way")
}

// TestMasklessPair_RelinkChainStillCollapses is the OTHER direction, and it is the reason the fix
// cannot simply be "dedup on account_id". A genuine re-link produces byte-identical row shapes to
// a maskless pair — two live accounts under one user_account — and must still count ONCE.
//
// The discriminator is a RECORDED succession fact. This fixture records it the way PRODUCTION
// does: account_service stamps linked_items.replaced_by_id and passes nil for the account-level
// pointer, so only the item pointer exists here. (The account-level pointer's direction is
// covered by TestHistoryRetentionRoundTrip_InstitutionChangeKeepsLastMonthWithoutDoubleCounting,
// whose fixture goes through accountRepository.UpdateStatus with a non-nil replacedByID.)
func TestMasklessPair_RelinkChainStillCollapses(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewBalanceSnapshotRepository(pool)

	user := insertTestUser(t, pool)
	ua := insertTestUserAccount(t, pool, user)
	oldItem := insertTestLinkedItem(t, pool, user)
	newItem := insertTestLinkedItem(t, pool, user)
	oldAcct := insertTestAccountNamed(t, pool, oldItem, user, ua, "Old Checking", "depository")
	newAcct := insertTestAccountNamed(t, pool, newItem, user, ua, "New Checking", "depository")

	_, err := pool.Exec(ctx, `UPDATE linked_items SET replaced_by_id = $1 WHERE id = $2`, newItem, oldItem)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE accounts SET status = 'replaced', replaced_at = now() WHERE id = $1`, oldAcct)
	require.NoError(t, err)

	now := time.Now().UTC()
	at := now.Add(-6 * time.Hour)
	insertBalanceSnapshot(t, pool, user, oldAcct, ua, 800, at)
	insertBalanceSnapshot(t, pool, user, newAcct, ua, 800, at.Add(time.Hour))

	latest, err := repo.GetLatestByUserID(ctx, user)
	require.NoError(t, err)
	assert.InDelta(t, 800.0, sumSnapshots(latest), 0.01,
		"1600 means the overlap was double-counted: a re-link is ONE logical account")

	asOf, err := repo.GetSnapshotsAsOfForUser(ctx, user, now)
	require.NoError(t, err)
	assert.InDelta(t, 800.0, sumSnapshots(asOf), 0.01, "same, for the as-of read")

	points, err := repo.GetAggregatedHistory(ctx, user, at.Add(-24*time.Hour), now, nil)
	require.NoError(t, err)
	byDay := map[string]float64{}
	for _, pt := range points {
		byDay[pt.Date.Format("2006-01-02")] = pt.TotalBalance
	}
	assert.InDelta(t, 800.0, byDay[at.Format("2006-01-02")], 0.01, "same, for the history chart")
}
