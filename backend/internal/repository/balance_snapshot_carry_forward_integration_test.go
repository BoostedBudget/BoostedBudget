//go:build integration

package repository

// Integration tests for GetAggregatedHistory's END-DAY BOUND and PER-ACCOUNT CARRY-FORWARD
// (Issue #1626 Phase 3, steps 1f + 2a/2b).
//
// THE USER-REPORTED DEFECT these exist for. With a "this month" filter the balance-history chart
// showed ~$650,000 on one day, ~$0 the next, and a flat $0 for every day after. Two independent
// causes, both in this one read:
//
//  1. the range's upper bound was compared at INSTANT resolution (`bs.snapshot_at <= $3`) against a
//     MIDNIGHT endDate, so a snapshot written today at any hour fell outside a range ending
//     "today" and today's data was invisible; and
//  2. a day's total was the sum of only the accounts that happened to receive a snapshot THAT DAY.
//     Snapshots are written per-ITEM on webhook sync, so a day on which one item synced reported
//     only that item's accounts and the total collapsed toward zero — while every other account's
//     balance was merely ABSENT from that date, not zero.
//
// Every per-account balance is provider truth. The day-level AGGREGATE was not, and that
// distinction is the whole defect.
//
// ⚠ THESE MUST BE INTEGRATION TESTS. The pgxmock unit tests in balance_snapshot_repository_test.go
// build their ExpectQuery from `aggregatedHistorySQLBase` itself, so a regression in the template
// moves production and the expectation together and they all stay green — that file says so at
// TestAggregatedHistorySQLBase_PinsItsLoadBearingFragments. Only a real planner can tell us what
// the statement RETURNS.
//
// ⚠ NO TEST HERE PASSES AN endDate OF 23:59:59. TestGetAggregatedHistory_Integration does
// (balance_snapshot_repository_integration_test.go), and that is precisely what hid defect 1 for as
// long as it existed: hand-widening the bound in the fixture makes the instant comparison behave
// like the date comparison it should have been. Bounds here are midnight-UTC, the way ParseDate
// produces them and the way both dashboard services pass them on.
//
// Run with:
//
//	DATABASE_URL=postgres://devuser:devpass@localhost:5432/freshcheck?sslmode=disable \
//	CACHE_REDIS_TEST_URL=redis://localhost:6380 REDIS_TEST_URL=redis://localhost:6379 \
//	  go test ./internal/repository/... -run TestAggregatedHistoryCarry -tags integration -p 1 -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// carryFixture is one user, one linked item, and whatever accounts a test asks for.
type carryFixture struct {
	userID       uuid.UUID
	linkedItemID uuid.UUID
}

func seedCarryFixture(ctx context.Context, t *testing.T, db DB, prefix string) carryFixture {
	t.Helper()
	f := carryFixture{userID: uuid.New(), linkedItemID: uuid.New()}

	_, err := db.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')`,
		f.userID, "test|"+prefix+"-"+f.userID.String(), prefix+"-"+f.userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = db.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')`,
		f.linkedItemID, f.userID, prefix+"-item-"+f.linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	return f
}

// seedCarryAccount inserts one account and BACKDATES the 'active' interval that
// account_status_history_insert_trigger just opened at clock_timestamp().
//
// ⚠ THE BACKDATE IS LOAD-BEARING, NOT COSMETIC. The insert trigger stamps valid_from =
// clock_timestamp(), i.e. NOW, so an account created by a fixture is recorded as having become
// active today — and the carry-forward's removal bound (accountLiveOnDateSQL) would then refuse to
// carry ANY balance into a 2026-03 day, making a carry test pass for the wrong reason (or fail for
// one). Production has no such problem: an account's snapshots always postdate its own accounts
// row, and migration 000152 backfilled legacy accounts' 'active' interval from created_at. The
// fixture has to say out loud what production gets for free.
func seedCarryAccount(
	ctx context.Context, t *testing.T, db DB, f carryFixture,
	prefix, accountType string, activeFrom time.Time,
) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, $5, $6, 'active')`,
		id, f.linkedItemID, f.userID, prefix+"-"+id.String(), prefix, accountType)
	require.NoError(t, err, "seed account")

	_, err = db.Exec(ctx, `
		UPDATE account_status_history SET valid_from = $2
		WHERE account_id = $1 AND status = 'active'`, id, activeFrom)
	require.NoError(t, err, "backdate active interval")
	return id
}

// snap writes one balance snapshot through the production writer.
func snap(ctx context.Context, t *testing.T, repo BalanceSnapshotRepository,
	f carryFixture, accountID uuid.UUID, at time.Time, current, available *float64,
) {
	t.Helper()
	require.NoError(t, repo.Create(ctx, &domain.BalanceSnapshot{
		UserID:           f.userID.String(),
		AccountID:        accountID.String(),
		CurrentBalance:   current,
		AvailableBalance: available,
		CurrencyCode:     "USD",
		SnapshotAt:       at,
		Source:           domain.SnapshotSourcePlaidWebhook,
	}))
}

func amt(v float64) *float64 { return &v }

// pointOn returns the history point for the given UTC calendar day, or nil.
func pointOn(history []domain.BalanceHistoryPoint, day time.Time) *domain.BalanceHistoryPoint {
	for i := range history {
		if history[i].Date.UTC().Format("2006-01-02") == day.UTC().Format("2006-01-02") {
			return &history[i]
		}
	}
	return nil
}

func utcDay(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// ── Part 1: the end-day bound ─────────────────────────────────────────────────────────────────

// TestAggregatedHistoryCarry_EndDayBound_TodaysSnapshotIsVisible is the T-bound RED.
//
// A snapshot written today at 10:00 UTC must appear in a range whose endDate is today's MIDNIGHT —
// which is the only endDate the API can produce, because dateutil.ParseDate parses YYYY-MM-DD to
// midnight UTC and both dashboard services pass that instant straight through.
func TestAggregatedHistoryCarry_EndDayBound_TodaysSnapshotIsVisible(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "carry-bound")
	today := time.Now().UTC().Truncate(24 * time.Hour)
	acct := seedCarryAccount(ctx, t, tx, f, "carry-bound-dep", "depository", today.AddDate(0, 0, -30))

	repo := NewBalanceSnapshotRepository(tx)
	snap(ctx, t, repo, f, acct, today.Add(10*time.Hour), amt(650000), amt(650000))

	// Exactly what the endpoint passes: midnight UTC on both ends. NOT 23:59:59.
	history, err := repo.GetAggregatedHistory(ctx, f.userID.String(), today, today, nil)
	require.NoError(t, err)

	p := pointOn(history, today)
	require.NotNil(t, p, "a snapshot written today at 10:00 UTC must appear in a range ending today")
	assert.InDelta(t, 650000.0, p.TotalBalance, 0.01, "today's own balance")
}

// ── Part 2: the per-account carry-forward ─────────────────────────────────────────────────────

// TestAggregatedHistoryCarry_IncompleteCohort_CarriesTheAbsentAccountsLastKnownBalance is T15, and
// it is the user's reported symptom in miniature: on day 2 only one item synced.
func TestAggregatedHistoryCarry_IncompleteCohort_CarriesTheAbsentAccountsLastKnownBalance(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "carry-cohort")
	d1, d2 := utcDay(2026, 3, 1), utcDay(2026, 3, 2)
	activeFrom := utcDay(2026, 2, 1)

	brokerage := seedCarryAccount(ctx, t, tx, f, "carry-cohort-inv", "investment", activeFrom)
	checking := seedCarryAccount(ctx, t, tx, f, "carry-cohort-dep", "depository", activeFrom)

	repo := NewBalanceSnapshotRepository(tx)
	// Day 1: both items synced.
	snap(ctx, t, repo, f, brokerage, d1.Add(9*time.Hour), amt(640000), amt(0))
	snap(ctx, t, repo, f, checking, d1.Add(9*time.Hour), amt(10000), amt(10000))
	// Day 2: ONLY the checking item synced, and it holds almost nothing.
	snap(ctx, t, repo, f, checking, d2.Add(9*time.Hour), amt(9500), amt(9500))

	history, err := repo.GetAggregatedHistory(ctx, f.userID.String(), d1, d2, nil)
	require.NoError(t, err)

	p1 := pointOn(history, d1)
	require.NotNil(t, p1)
	assert.InDelta(t, 650000.0, p1.TotalBalance, 0.01, "day 1, both items synced")

	p2 := pointOn(history, d2)
	require.NotNil(t, p2, "day 2 must still be a point")
	assert.InDelta(t, 649500.0, p2.TotalBalance, 0.01,
		"day 2 must carry the brokerage's last known balance; without it the day collapses to 9500")
	assert.InDelta(t, 640000.0, p2.ByType["investment"], 0.01,
		"the carried balance must keep its own account's type, so net worth can be reconstructed")
	assert.InDelta(t, 9500.0, p2.ByType["depository"], 0.01, "the day's own observation still wins")
}

// TestAggregatedHistoryCarry_NoHistoryIsInventedBeforeAnAccountsFirstSnapshot is the step-2b
// decision, asserted rather than merely written down: history starts PER ACCOUNT, at that account's
// earliest snapshot. The total therefore GROWS as accounts join, and no day before an account's
// first snapshot is credited with a balance it did not have.
func TestAggregatedHistoryCarry_NoHistoryIsInventedBeforeAnAccountsFirstSnapshot(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "carry-2b")
	d1, d2 := utcDay(2026, 4, 1), utcDay(2026, 4, 2)
	activeFrom := utcDay(2026, 3, 1)

	first := seedCarryAccount(ctx, t, tx, f, "carry-2b-a", "depository", activeFrom)
	late := seedCarryAccount(ctx, t, tx, f, "carry-2b-b", "depository", activeFrom)

	repo := NewBalanceSnapshotRepository(tx)
	snap(ctx, t, repo, f, first, d1.Add(9*time.Hour), amt(100), amt(100))
	snap(ctx, t, repo, f, late, d2.Add(9*time.Hour), amt(400), amt(400))

	history, err := repo.GetAggregatedHistory(ctx, f.userID.String(), d1, d2, nil)
	require.NoError(t, err)

	p1 := pointOn(history, d1)
	require.NotNil(t, p1)
	assert.InDelta(t, 100.0, p1.TotalBalance, 0.01,
		"the late account had no balance on day 1 and must not be back-filled into it")

	p2 := pointOn(history, d2)
	require.NotNil(t, p2)
	assert.InDelta(t, 500.0, p2.TotalBalance, 0.01, "both accounts are known by day 2")
}

// TestAggregatedHistoryCarry_RemovedAccount_IsNotCarriedPastItsRemoval is T-removed.
//
// A closed account would otherwise contribute to net worth forever. CLAUDE.md's ruling is that a
// terminal account presents NULL rather than zero for a CURRENT balance while its history stays
// queryable — so the account's OWN snapshot days keep their value, and only the invented,
// carried-forward days stop.
func TestAggregatedHistoryCarry_RemovedAccount_IsNotCarriedPastItsRemoval(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "carry-removed")
	d1, d3 := utcDay(2026, 5, 1), utcDay(2026, 5, 3)
	removalAt := utcDay(2026, 5, 2)
	activeFrom := utcDay(2026, 4, 1)

	closing := seedCarryAccount(ctx, t, tx, f, "carry-removed-a", "depository", activeFrom)
	keeping := seedCarryAccount(ctx, t, tx, f, "carry-removed-b", "depository", activeFrom)

	repo := NewBalanceSnapshotRepository(tx)
	snap(ctx, t, repo, f, closing, d1.Add(9*time.Hour), amt(700), amt(700))
	snap(ctx, t, repo, f, keeping, d1.Add(9*time.Hour), amt(300), amt(300))
	snap(ctx, t, repo, f, keeping, d3.Add(9*time.Hour), amt(300), amt(300))

	// The user disconnects the closing account. Production status transition, so the trigger writes
	// the terminal interval; the fixture then dates both intervals to 2026-05-02.
	_, err = tx.Exec(ctx, `
		UPDATE accounts SET status = 'removed', removed_at = $2, removed_reason = 'user_disconnect'
		WHERE id = $1`, closing, removalAt)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		UPDATE account_status_history SET valid_to = $2
		WHERE account_id = $1 AND status = 'active'`, closing, removalAt)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		UPDATE account_status_history SET valid_from = $2
		WHERE account_id = $1 AND status = 'removed'`, closing, removalAt)
	require.NoError(t, err)

	history, err := repo.GetAggregatedHistory(ctx, f.userID.String(), d1, d3, nil)
	require.NoError(t, err)

	p1 := pointOn(history, d1)
	require.NotNil(t, p1)
	assert.InDelta(t, 1000.0, p1.TotalBalance, 0.01,
		"the closed account's own retained snapshot day keeps its balance")

	p3 := pointOn(history, d3)
	require.NotNil(t, p3)
	assert.InDelta(t, 300.0, p3.TotalBalance, 0.01,
		"a removed account's balance must NOT be carried into a day after its removal")
}

// TestAggregatedHistoryCarry_RelinkChain_CarryForwardDoesNotDoubleCount is T-supersession-guard.
//
// The carry-forward makes both members of a re-link chain present on every day after their own
// first snapshot, where before the fix each was present only on days it actually reported. That is
// exactly the input the recorded-succession collapse (accountChainSupersessionCTE) exists for, and
// getting it wrong double-counts the re-link. This test does NOT assert which balance wins — see
// the warning at the bottom of this file.
func TestAggregatedHistoryCarry_RelinkChain_CarryForwardDoesNotDoubleCount(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "carry-relink")
	d1, d2, d3 := utcDay(2026, 6, 1), utcDay(2026, 6, 2), utcDay(2026, 6, 3)
	activeFrom := utcDay(2026, 5, 1)
	flippedAt := d3.Add(12 * time.Hour)

	// One logical account, two physical rows: the classic Plaid re-link.
	userAccountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, mask, identity_type, identity_subtype)
		VALUES ($1, $2, 'ins_relink', '1111', 'depository', 'checking')`, userAccountID, f.userID)
	require.NoError(t, err)

	pred := seedCarryAccount(ctx, t, tx, f, "carry-relink-pred", "depository", activeFrom)
	succ := seedCarryAccount(ctx, t, tx, f, "carry-relink-succ", "depository", activeFrom)
	_, err = tx.Exec(ctx, `UPDATE accounts SET user_account_id = $2 WHERE id = ANY($1)`,
		[]uuid.UUID{pred, succ}, userAccountID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		UPDATE accounts SET replaced_by_id = $2, status = 'replaced', replaced_at = $3
		WHERE id = $1`, pred, succ, flippedAt)
	require.NoError(t, err)
	// The trigger closed the predecessor's 'active' interval at clock_timestamp() and opened a
	// 'replaced' one there; date both to the real flip instant.
	//
	// ⚠ THE OVERLAP IS THE POINT. The flip is dated LATE — midday on day 3 — so the predecessor is
	// recorded as still active across days 2 and 3, which is what a re-link genuinely looks like
	// while both rows exist. If it were dated at the takeover instead, the carry-forward's own
	// removal bound would drop the predecessor and this test would pass without the succession
	// collapse ever running, proving nothing about double-counting.
	_, err = tx.Exec(ctx, `
		UPDATE account_status_history SET valid_to = $2
		WHERE account_id = $1 AND status = 'active'`, pred, flippedAt)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		UPDATE account_status_history SET valid_from = $2
		WHERE account_id = $1 AND status = 'replaced'`, pred, flippedAt)
	require.NoError(t, err)

	repo := NewBalanceSnapshotRepository(tx)
	// The predecessor reported on day 1 only; the successor took over from day 2.
	snap(ctx, t, repo, f, pred, d1.Add(9*time.Hour), amt(5000), amt(5000))
	snap(ctx, t, repo, f, succ, d2.Add(9*time.Hour), amt(5000), amt(5000))
	// Day 3 has a row so it exists as a chart point; the predecessor has none after day 1.
	snap(ctx, t, repo, f, succ, d3.Add(9*time.Hour), amt(5100), amt(5100))

	history, err := repo.GetAggregatedHistory(ctx, f.userID.String(), d1, d3, nil)
	require.NoError(t, err)

	p1 := pointOn(history, d1)
	require.NotNil(t, p1)
	assert.InDelta(t, 5000.0, p1.TotalBalance, 0.01, "only the predecessor is known on day 1")

	p2 := pointOn(history, d2)
	require.NotNil(t, p2)
	assert.InDelta(t, 5000.0, p2.TotalBalance, 0.01,
		"the takeover day must count ONE account, not the successor plus the predecessor's carried balance")

	p3 := pointOn(history, d3)
	require.NotNil(t, p3)
	assert.InDelta(t, 5100.0, p3.TotalBalance, 0.01, "and it must not double-count on later days either")
}

// ── Part 3: two more paths to an apparent zero ────────────────────────────────────────────────

// TestAggregatedHistoryCarry_NullCurrentBalance_DoesNotDropTheAccount is T16.
//
// transactionSyncService accepts a snapshot whose current_balance is NULL while available_balance
// is present, and the aggregation sums only current_balance — so the account was silently dropped
// from the day's total. Worse than "dropped": on a day whose ONLY row was such a snapshot, SUM()
// returned SQL NULL and the row scan failed outright.
func TestAggregatedHistoryCarry_NullCurrentBalance_DoesNotDropTheAccount(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "carry-nullbal")
	d1, d2 := utcDay(2026, 7, 1), utcDay(2026, 7, 2)
	acct := seedCarryAccount(ctx, t, tx, f, "carry-nullbal-dep", "depository", utcDay(2026, 6, 1))

	repo := NewBalanceSnapshotRepository(tx)
	snap(ctx, t, repo, f, acct, d1.Add(9*time.Hour), amt(500), amt(500))
	// Plaid returned no current balance, only an available one.
	snap(ctx, t, repo, f, acct, d2.Add(9*time.Hour), nil, amt(400))

	history, err := repo.GetAggregatedHistory(ctx, f.userID.String(), d1, d2, nil)
	require.NoError(t, err, "a NULL current_balance must not make the whole read fail")

	p2 := pointOn(history, d2)
	require.NotNil(t, p2, "the day must survive a balance-less snapshot")
	assert.InDelta(t, 500.0, p2.TotalBalance, 0.01,
		"the account's last known CURRENT balance is carried; available_balance is a different "+
			"quantity and is not substituted for it")
}

// TestAggregatedHistoryCarry_OrphanDay_KeepsATypeSoNetWorthIsNotZero is T17.
//
// An orphaned snapshot (accounts row gone, ON DELETE SET NULL) counted toward TotalBalance but
// carried no ByType entry, and net-worth history is reconstructed EXCLUSIVELY from ByType — so a
// day whose only rows were orphans reported net worth of exactly 0 beside a correct balance.
func TestAggregatedHistoryCarry_OrphanDay_KeepsATypeSoNetWorthIsNotZero(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "carry-orphan")
	d1 := utcDay(2026, 8, 1)

	userAccountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, mask, identity_type, identity_subtype)
		VALUES ($1, $2, 'ins_orphan', '2222', 'credit', 'credit card')`, userAccountID, f.userID)
	require.NoError(t, err)

	acct := seedCarryAccount(ctx, t, tx, f, "carry-orphan-cred", "credit", utcDay(2026, 7, 1))
	_, err = tx.Exec(ctx, `UPDATE accounts SET user_account_id = $2 WHERE id = $1`, acct, userAccountID)
	require.NoError(t, err)

	repo := NewBalanceSnapshotRepository(tx)
	require.NoError(t, repo.Create(ctx, &domain.BalanceSnapshot{
		UserID:         f.userID.String(),
		AccountID:      acct.String(),
		UserAccountID:  ptrString(userAccountID.String()),
		CurrentBalance: amt(1200),
		CurrencyCode:   "USD",
		SnapshotAt:     d1.Add(9 * time.Hour),
		Source:         domain.SnapshotSourcePlaidWebhook,
	}))

	// Orphan the snapshot: the accounts row goes, account_id becomes NULL, user_account_id survives.
	_, err = tx.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, acct)
	require.NoError(t, err)

	history, err := repo.GetAggregatedHistory(ctx, f.userID.String(), d1, d1, nil)
	require.NoError(t, err)

	p1 := pointOn(history, d1)
	require.NotNil(t, p1)
	assert.InDelta(t, 1200.0, p1.TotalBalance, 0.01, "the orphaned balance still reaches the total")
	assert.InDelta(t, 1200.0, p1.ByType["credit"], 0.01,
		"an orphan's type is recoverable from its own chain row (user_accounts.identity_type), so "+
			"net-worth history must not read the day as exactly zero — and a credit card must not "+
			"be reconstructed as an ASSET, which is what an untyped bucket would do")
	assert.InDelta(t, 0.0, p1.UntypedBalance, 0.01,
		"nothing is left untyped while the chain row survives")

	// FINDING F5: the account-type PREDICATE must test the same expression the projection emits.
	// It used to be rendered against the raw `a.type`, which is NULL for an orphan — so this same
	// account appeared in ByType["credit"] for the unfiltered request above and VANISHED for
	// accountTypes=['credit']. One account, two answers, depending on a filter that is supposed to
	// select rather than to redefine.
	filtered, err := repo.GetAggregatedHistory(ctx, f.userID.String(), d1, d1, []string{"credit"})
	require.NoError(t, err)
	fp1 := pointOn(filtered, d1)
	require.NotNil(t, fp1,
		"the orphaned credit card must still be selected by a filter naming its recovered type")
	assert.InDelta(t, 1200.0, fp1.ByType["credit"], 0.01,
		"the filter and the projection must agree about what an orphan's type is")
	assert.InDelta(t, 1200.0, fp1.TotalBalance, 0.01, "and it is the whole of the filtered total")

	// The other direction, so the filter is still doing its job rather than admitting everything:
	// a filter naming a DIFFERENT type must not select it.
	other, err := repo.GetAggregatedHistory(ctx, f.userID.String(), d1, d1, []string{"depository"})
	require.NoError(t, err)
	assert.Nil(t, pointOn(other, d1),
		"a recovered type must be tested, not ignored — accountTypes=['depository'] selects no "+
			"credit card")
}

// TestAggregatedHistoryCarry_FullyDetachedOrphan_IsReportedAsUntypedRatherThanDiscarded covers the
// residue identity_type recovery cannot reach: the chain row is gone TOO, so nothing anywhere
// records what kind of account it was. The balance must still be reported, and reported as
// UNTYPED — the service turns that into assets or liabilities by its sign
// (dashboard_service.go GetNetWorthHistory). Before this change it reached TotalBalance and
// nothing else, and net-worth history read the day as exactly 0.
func TestAggregatedHistoryCarry_FullyDetachedOrphan_IsReportedAsUntypedRatherThanDiscarded(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "carry-detached")
	d1 := utcDay(2026, 8, 5)
	acct := seedCarryAccount(ctx, t, tx, f, "carry-detached-dep", "depository", utcDay(2026, 7, 1))

	repo := NewBalanceSnapshotRepository(tx)
	snap(ctx, t, repo, f, acct, d1.Add(9*time.Hour), amt(4200), amt(4200))

	// No user_account_id was ever set on this account, so orphaning it leaves the snapshot with
	// neither pointer — the shape identity_type recovery cannot help.
	_, err = tx.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, acct)
	require.NoError(t, err)

	history, err := repo.GetAggregatedHistory(ctx, f.userID.String(), d1, d1, nil)
	require.NoError(t, err)

	p1 := pointOn(history, d1)
	require.NotNil(t, p1)
	assert.InDelta(t, 4200.0, p1.TotalBalance, 0.01, "the balance still reaches the total")
	assert.Empty(t, p1.ByType, "there is no type to attribute it to, and none may be invented")
	assert.InDelta(t, 4200.0, p1.UntypedBalance, 0.01,
		"it must be RECORDED as untyped, not discarded: the net-worth reconstruction is driven by "+
			"ByType alone and would otherwise report this day as exactly zero")
}

// ── The UTC calendar-day convention, at the two extremes ──────────────────────────────────────

// TestAggregatedHistoryCarry_UTCDayBucketing_AtTheOffsetExtremes asserts the convention explicitly
// at UTC+13 and UTC-11: two instants whose LOCAL dates differ from their UTC date, and from each
// other, in opposite directions.
//
// UTC end-to-end is a controller ruling for this read and it is what the household twin already
// does (see GetAggregatedHistoryForHousehold), so both instants belong to the UTC day and to
// neither local one. ⚠ `users.timezone` DOES hold a validated IANA zone (see this task's report);
// it is deliberately not consulted here.
func TestAggregatedHistoryCarry_UTCDayBucketing_AtTheOffsetExtremes(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "carry-utc")
	day := utcDay(2026, 9, 10)
	acctA := seedCarryAccount(ctx, t, tx, f, "carry-utc-a", "depository", utcDay(2026, 8, 1))
	acctB := seedCarryAccount(ctx, t, tx, f, "carry-utc-b", "depository", utcDay(2026, 8, 1))

	// 23:30Z is 2026-09-11 12:30 at UTC+13 (Pacific/Auckland in DST).
	lateUTC := day.Add(23*time.Hour + 30*time.Minute)
	// 01:00Z is 2026-09-09 14:00 at UTC-11 (Pacific/Niue).
	earlyUTC := day.Add(1 * time.Hour)
	require.Equal(t, "2026-09-11", lateUTC.In(time.FixedZone("UTC+13", 13*3600)).Format("2006-01-02"))
	require.Equal(t, "2026-09-09", earlyUTC.In(time.FixedZone("UTC-11", -11*3600)).Format("2006-01-02"))

	repo := NewBalanceSnapshotRepository(tx)
	snap(ctx, t, repo, f, acctA, lateUTC, amt(70), amt(70))
	snap(ctx, t, repo, f, acctB, earlyUTC, amt(30), amt(30))

	// A single-day request for the UTC day must hold BOTH, at both bounds inclusive.
	history, err := repo.GetAggregatedHistory(ctx, f.userID.String(), day, day, nil)
	require.NoError(t, err)
	require.Len(t, history, 1, "both instants belong to the same UTC calendar day")
	assert.InDelta(t, 100.0, history[0].TotalBalance, 0.01,
		"a 23:30Z snapshot must not be excluded by a midnight endDate, and a 01:00Z one must not "+
			"be excluded by a midnight startDate")

	// And neither local day is a day of this series.
	for _, other := range []time.Time{utcDay(2026, 9, 9), utcDay(2026, 9, 11)} {
		assert.Nil(t, pointOn(history, other), "no local-date bucket may appear: %s", other.Format("2006-01-02"))
	}
}

// ⚠ DELIBERATELY ABSENT: a test asserting that the succession collapse picks the "real-looking"
// balance. TestHistoryRetentionRoundTrip_InstitutionChangeKeepsLastMonthWithoutDoubleCounting
// (history_retention_roundtrip_integration_test.go) already pins the re-link direction, and
// accountChainSupersessionCTE deliberately has NO opinion about which of two balances looks real.
// A test encoding "nonzero wins" would risk SUPPRESSING a legitimate zero balance — a worse bug
// than the one this file exists to fix.

func ptrString(s string) *string { return &s }

// TestAggregatedHistoryCarry_WindowStartInheritsFromBeforeTheWindow is the "this month" case the
// user actually filtered by, and it pins the two halves of the bound handling separately: the
// observed cohort is UNBOUNDED BELOW so a day at the start of the window can inherit a balance
// from before it, while the OUTPUT is still clipped to the window so no earlier day becomes a
// chart point.
//
// Without the first half a month-long window would under-count every account whose most recent
// sync landed in the previous month — which, with per-item syncs, is most of them.
func TestAggregatedHistoryCarry_WindowStartInheritsFromBeforeTheWindow(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "carry-seed")
	lastMonth := utcDay(2026, 10, 28)
	windowStart, inWindow := utcDay(2026, 11, 1), utcDay(2026, 11, 3)

	quiet := seedCarryAccount(ctx, t, tx, f, "carry-seed-quiet", "depository", utcDay(2026, 10, 1))
	chatty := seedCarryAccount(ctx, t, tx, f, "carry-seed-chatty", "depository", utcDay(2026, 10, 1))

	repo := NewBalanceSnapshotRepository(tx)
	// The quiet account last synced in October and has said nothing since.
	snap(ctx, t, repo, f, quiet, lastMonth.Add(9*time.Hour), amt(80000), amt(80000))
	snap(ctx, t, repo, f, chatty, lastMonth.Add(9*time.Hour), amt(500), amt(500))
	snap(ctx, t, repo, f, chatty, inWindow.Add(9*time.Hour), amt(600), amt(600))

	history, err := repo.GetAggregatedHistory(ctx, f.userID.String(), windowStart, utcDay(2026, 11, 30), nil)
	require.NoError(t, err)

	assert.Nil(t, pointOn(history, lastMonth), "a day outside the requested window is not a chart point")
	require.Len(t, history, 1, "only the one in-window day on which anything synced")

	p := pointOn(history, inWindow)
	require.NotNil(t, p)
	assert.InDelta(t, 80600.0, p.TotalBalance, 0.01,
		"the quiet account's last known balance must be inherited from BEFORE the window; "+
			"clipping the cohort at the start date instead reports 600")
}

// TestAggregatedHistoryCarry_AccountWithNoRecordedActiveInterval_StillCarries pins the first leg of
// the removal bound, which is the #1626 Phase 2 ruling applied here: absence of interval data is
// "no reason to hide", not evidence of removal.
//
// The shape is real, not hypothetical. Migration 000152's zero-length-interval guard skips the
// 'active' prefix when replaced_at/removed_at/updated_at is NULL or <= created_at, and an orphaned
// snapshot's history rows cascade away with its accounts row (fk_ash_account_id ON DELETE CASCADE).
// Keying the carry on a recorded interval ALONE would make this whole fix a silent no-op for
// exactly those accounts — hiding a user's own retained history because an interval was never
// written is the #1569 defect in miniature.
func TestAggregatedHistoryCarry_AccountWithNoRecordedActiveInterval_StillCarries(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "carry-nointerval")
	d1, d2 := utcDay(2026, 12, 1), utcDay(2026, 12, 2)

	bare := seedCarryAccount(ctx, t, tx, f, "carry-nointerval-a", "depository", utcDay(2026, 11, 1))
	other := seedCarryAccount(ctx, t, tx, f, "carry-nointerval-b", "depository", utcDay(2026, 11, 1))

	// Reproduce the backfill shape: no 'active' row whatsoever for this account.
	_, err = tx.Exec(ctx, `DELETE FROM account_status_history WHERE account_id = $1`, bare)
	require.NoError(t, err)
	var remaining int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT count(*) FROM account_status_history WHERE account_id = $1`, bare).Scan(&remaining))
	require.Zero(t, remaining, "the fixture must actually produce the no-interval shape")

	repo := NewBalanceSnapshotRepository(tx)
	snap(ctx, t, repo, f, bare, d1.Add(9*time.Hour), amt(2000), amt(2000))
	snap(ctx, t, repo, f, other, d1.Add(9*time.Hour), amt(100), amt(100))
	snap(ctx, t, repo, f, other, d2.Add(9*time.Hour), amt(100), amt(100))

	history, err := repo.GetAggregatedHistory(ctx, f.userID.String(), d1, d2, nil)
	require.NoError(t, err)

	p2 := pointOn(history, d2)
	require.NotNil(t, p2)
	assert.InDelta(t, 2100.0, p2.TotalBalance, 0.01,
		"an account with no recorded 'active' interval must still carry forward; keying the carry "+
			"on a recorded interval alone reports 100")
}

// seedCarryTerminalAccount inserts an account whose FIRST recorded status is already terminal, so
// account_status_history holds exactly ONE row and it is NOT 'active'.
//
// ⚠ THIS SHAPE IS NOT HYPOTHETICAL AND IT IS NOT THE SAME AS THE NO-HISTORY SHAPE ABOVE.
// record_account_status_change() opens an interval at the row's own status, so an account inserted
// terminal never gets an 'active' prefix; migration 000152's removed branch produces the same shape
// whenever removed_at is NULL or <= created_at
// (000152_backfill_account_status_history.up.sql, the 'removed accounts' INSERT pair). The
// distinction that matters for the carry-forward is that the accounts row still EXISTS and its
// status is terminal — so a predicate keyed only on "has no 'active' interval" reads TRUE here and
// carries a closed account's balance forever.
func seedCarryTerminalAccount(
	ctx context.Context, t *testing.T, db DB, f carryFixture, prefix, accountType string,
) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status,
		                      removed_at, removed_reason)
		VALUES ($1, $2, $3, $4, $5, $6, 'removed', now(), 'user_disconnect')`,
		id, f.linkedItemID, f.userID, prefix+"-"+id.String(), prefix, accountType)
	require.NoError(t, err, "seed terminal account")

	var active, total int
	require.NoError(t, db.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'active'), count(*)
		FROM account_status_history WHERE account_id = $1`, id).Scan(&active, &total))
	require.Zero(t, active, "the fixture must actually produce the TERMINAL-ONLY shape: no 'active' row")
	require.Positive(t, total, "and it must still have a terminal row, not no history at all")
	return id
}

// TestAggregatedHistoryCarry_TerminalOnlyAccount_DoesNotCarryPastItsOwnSnapshot is X1's RED.
//
// THE BUG IT PINS, and it is a wrong NUMBER rather than a missing row. The removal bound's first
// leg read `NOT EXISTS (… status='active')` as unconditional permission to carry, on the argument
// that absence of interval data is "no reason to hide". That argument is right for a row that
// HAPPENED (a transaction) and wrong for an INFERENCE that a balance persisted: a terminal-only
// account satisfies the NOT EXISTS forever, so its last retained balance was carried onto every
// later observed day, permanently overstating net worth.
//
// ⚠ RETENTION IS NOT WHAT IS BEING REMOVED. The closed account's OWN snapshot day keeps its full
// value below — that is provider truth about a day it really reported, and #1569 requires it to
// stay queryable. What stops is the carry.
func TestAggregatedHistoryCarry_TerminalOnlyAccount_DoesNotCarryPastItsOwnSnapshot(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "carry-terminal")
	d1, d3 := utcDay(2026, 4, 2), utcDay(2026, 4, 4)

	// Deliberately off every convenient boundary: the two balances are unequal, neither is a round
	// number, and their sum is not either of them — so a mutation that carries, drops, or doubles
	// one of them cannot land on the expected total by coincidence.
	closed := seedCarryTerminalAccount(ctx, t, tx, f, "carry-terminal-closed", "depository")
	live := seedCarryAccount(ctx, t, tx, f, "carry-terminal-live", "depository", utcDay(2026, 3, 1))

	repo := NewBalanceSnapshotRepository(tx)
	snap(ctx, t, repo, f, closed, d1.Add(9*time.Hour), amt(713), amt(713))
	snap(ctx, t, repo, f, live, d1.Add(9*time.Hour), amt(101), amt(101))
	snap(ctx, t, repo, f, live, d3.Add(9*time.Hour), amt(317), amt(317))

	history, err := repo.GetAggregatedHistory(ctx, f.userID.String(), d1, utcDay(2026, 4, 30), nil)
	require.NoError(t, err)

	p1 := pointOn(history, d1)
	require.NotNil(t, p1, "the closed account's own snapshot day must remain a chart point (#1569)")
	assert.InDelta(t, 814.0, p1.TotalBalance, 0.01,
		"the closed account's OWN day keeps its retained balance: 713 + 101")

	p3 := pointOn(history, d3)
	require.NotNil(t, p3)
	assert.InDelta(t, 317.0, p3.TotalBalance, 0.01,
		"a terminal-only account must not be carried onto a later day; carrying it reports 1030 "+
			"and does so on every later observed day, forever")
}

// TestAggregatedHistoryCarry_OrphanedSnapshot_DoesNotCarryPastItsOwnDay is X1's second RED, and it
// is a DIFFERENT shape from the one above rather than a variation of it: here the accounts row is
// GONE (balance_snapshots.account_id ON DELETE SET NULL, migration 000150) and its status history
// cascaded away with it (fk_ash_account_id ON DELETE CASCADE).
//
// So the NOT EXISTS was satisfied for the emptiest possible reason — there is no account to ask
// about — and, because live_all inner-joins accounts, no succession could ever suppress the carried
// row either. An orphan is the one shape where the carry can never be corrected by anything
// downstream.
func TestAggregatedHistoryCarry_OrphanedSnapshot_DoesNotCarryPastItsOwnDay(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "carry-orphan")
	d1, d3 := utcDay(2026, 5, 6), utcDay(2026, 5, 8)

	doomed := seedCarryAccount(ctx, t, tx, f, "carry-orphan-gone", "depository", utcDay(2026, 4, 1))
	live := seedCarryAccount(ctx, t, tx, f, "carry-orphan-live", "depository", utcDay(2026, 4, 1))

	repo := NewBalanceSnapshotRepository(tx)
	snap(ctx, t, repo, f, doomed, d1.Add(9*time.Hour), amt(907), amt(907))
	snap(ctx, t, repo, f, live, d1.Add(9*time.Hour), amt(101), amt(101))
	snap(ctx, t, repo, f, live, d3.Add(9*time.Hour), amt(317), amt(317))

	_, err = tx.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, doomed)
	require.NoError(t, err, "orphan the snapshot the way a hard delete does")

	var orphaned int
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT count(*) FROM balance_snapshots
		WHERE user_id = $1 AND account_id IS NULL`, f.userID).Scan(&orphaned))
	require.Equal(t, 1, orphaned, "the fixture must actually produce an account_id IS NULL snapshot")

	history, err := repo.GetAggregatedHistory(ctx, f.userID.String(), d1, utcDay(2026, 5, 31), nil)
	require.NoError(t, err)

	p1 := pointOn(history, d1)
	require.NotNil(t, p1, "an orphaned snapshot is still the user's own retained history (#1569)")
	assert.InDelta(t, 1008.0, p1.TotalBalance, 0.01,
		"the orphan's OWN day keeps its retained balance: 907 + 101")

	p3 := pointOn(history, d3)
	require.NotNil(t, p3)
	assert.InDelta(t, 317.0, p3.TotalBalance, 0.01,
		"an orphaned snapshot must not be carried onto a later day; carrying it reports 1224 and "+
			"nothing downstream can ever suppress it")
}
