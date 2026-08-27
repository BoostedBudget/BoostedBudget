//go:build integration

package repository

// Integration test for Fix A (Issue #1569, Task 6): portfolio_snapshots' source query used to join
// accounts on the account's CURRENT status (a.status = 'active'). Upsert recomputes and re-writes a
// PAST day's total every time it runs (it is an upsert keyed on (user_id, snapshot_date)), so once
// an account was later removed, re-upserting that same past day with a current-status predicate
// dropped its holdings and silently rewrote a portfolio total the user had already seen — for a day
// that did not actually change. Under retention the holdings rows still exist; the fix reads
// recorded liveness AT THE SNAPSHOT'S OWN DATE from account_status_history instead of the account's
// current status. A pgxmock test would only assert the string we wrote is the string we wrote — this
// needs the real half-open [valid_from, valid_to) rows the account_status_history trigger produces.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestPortfolioSnapshot_ -tags integration -v -p 1

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestPortfolioSnapshot_Upsert_RecomputingPastDayAfterAccountRemoved_KeepsSameTotal is the Fix A
// regression: an account with holdings that was live on day D, and is removed (retained, not
// deleted) sometime AFTER D. Re-upserting D's snapshot must still include that account's holdings
// and produce the SAME total it produced before the removal — the whole point of retention is that
// history that already happened does not get rewritten by a later, unrelated status change.
func TestPortfolioSnapshot_Upsert_RecomputingPastDayAfterAccountRemoved_KeepsSameTotal(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "psnap-fixa")

	holdingID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value)
		VALUES ($1, $2, $3, $4, 10, 4000.00, 5000.00)
	`, holdingID, userID, accountID, secID)
	require.NoError(t, err, "seed holding")

	now := time.Now().UTC()
	snapshotDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -5)
	backdateFrom := now.AddDate(0, 0, -30) // well before snapshotDate

	// Backdate the account's open 'active' interval so it has been live since before snapshotDate.
	ct, err := tx.Exec(ctx, `
		UPDATE account_status_history
		SET valid_from = $2
		WHERE account_id = $1 AND status = 'active' AND valid_to IS NULL`, accountID, backdateFrom)
	require.NoError(t, err, "backdate account_status_history")
	require.EqualValues(t, 1, ct.RowsAffected(),
		"the INSERT trigger from migration 000152 must have opened exactly one active interval")

	repo := NewPortfolioSnapshotRepository(tx)

	before, err := repo.Upsert(ctx, userID.String(), snapshotDate)
	require.NoError(t, err)
	require.InDelta(t, 5000.0, before.TotalValue, 0.001,
		"sanity: the holding is counted while the account is still active")

	// The account is removed TODAY — well after snapshotDate. It is retained, not deleted: its
	// holdings row and its account_status_history rows still exist.
	ct, err = tx.Exec(ctx, `
		UPDATE accounts SET status = 'removed', removed_at = NOW(), updated_at = NOW()
		WHERE id = $1`, accountID)
	require.NoError(t, err, "soft-remove account")
	require.EqualValues(t, 1, ct.RowsAffected())

	// Recompute snapshotDate again (this is exactly what a re-sync or backfill job does: the same
	// (user_id, snapshot_date) upsert key, run again later).
	after, err := repo.Upsert(ctx, userID.String(), snapshotDate)
	require.NoError(t, err)

	require.InDelta(t, before.TotalValue, after.TotalValue, 0.001,
		"recomputing a PAST day must not change once the account is removed — the account was "+
			"live ON snapshotDate, and that fact does not change later. A regression here means "+
			"the portfolio curve is retroactively rewriting itself downward for history that did "+
			"not change, which is the exact bug #1561 partially caused and had to be reverted for.")
	require.InDelta(t, 5000.0, after.TotalValue, 0.001,
		"the holding must still be counted — dropping to 0 is the bug this test guards against")
}

// TestPortfolioSnapshot_Upsert_OverlapBoundaryIsUTC_RegardlessOfSessionTimeZone pins the #1569 F4
// fix: the day-overlap comparison must mean UTC, not "whatever the session's TimeZone happens to
// be".
//
// "$2::date + INTERVAL '1 day'" produces a timestamp WITHOUT time zone, and "$2::date" compared to
// a timestamptz is cast using the SESSION zone — so before the fix this query was correct only
// because database.NewPool pins TimeZone=UTC (the #1461 lesson). That is an invisible dependency:
// a psql session, or this SQL moved into a migration, does not inherit the pin.
//
// The probe is a chain whose 'active' interval closes EXACTLY at snapshotDate 00:00:00 UTC. Under
// the half-open rule the account was NOT live on snapshotDate, so its holding must be excluded. Run
// under a UTC+14 session, the pre-fix expression reads the lower bound as snapshotDate 00:00+14
// (i.e. 14 hours EARLIER in UTC), the interval then appears to extend past it, and the holding is
// wrongly counted. Verified: reverting either AT TIME ZONE 'UTC' makes this fail.
func TestPortfolioSnapshot_Upsert_OverlapBoundaryIsUTC_RegardlessOfSessionTimeZone(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// A session zone as far from UTC as Postgres offers, so a session-relative reading of the
	// boundary lands on the other side of it.
	_, err = tx.Exec(ctx, `SET LOCAL TimeZone = 'Pacific/Kiritimati'`) // UTC+14
	require.NoError(t, err, "set a non-UTC session zone")

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "psnap-tz")

	holdingID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value)
		VALUES ($1, $2, $3, $4, 10, 4000.00, 5000.00)
	`, holdingID, userID, accountID, secID)
	require.NoError(t, err, "seed holding")

	now := time.Now().UTC()
	snapshotDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -5)

	// The account was live from 30 days ago until EXACTLY snapshotDate 00:00:00 UTC — the instant
	// the day begins. [valid_from, valid_to) is half-open, so it was not live on that day at all.
	ct, err := tx.Exec(ctx, `
		UPDATE account_status_history
		SET valid_from = $2, valid_to = $3
		WHERE account_id = $1 AND status = 'active' AND valid_to IS NULL`,
		accountID, now.AddDate(0, 0, -30), snapshotDate)
	require.NoError(t, err, "close the active interval on the day boundary")
	require.EqualValues(t, 1, ct.RowsAffected())

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), snapshotDate)
	require.NoError(t, err)
	require.InDelta(t, 0.0, snap.TotalValue, 0.001,
		"the active interval ends at the first instant of snapshotDate, so the holding must be "+
			"excluded — a non-zero total here means the boundary was resolved in the session's "+
			"zone rather than UTC")
}

// ── #1569 task 9.1, item A: a same-day RE-LINK must not double the portfolio ──────────────────
//
// seedRelinkedInvestmentChain seeds one LOGICAL investment account that has been re-linked today:
// two physical `accounts` rows sharing one `user_account_id`, each carrying the same position.
// The old row is 'replaced' as of a moment ago; both are therefore live at SOME point during
// today, which is what accountLiveOnDateClause asks. Returns (userID, oldAccountID, newAccountID,
// securityID).
func seedRelinkedInvestmentChain(ctx context.Context, t *testing.T, db DB, prefix string) (
	userID, oldAccountID, newAccountID, secID uuid.UUID,
) {
	t.Helper()
	userID, _, oldAccountID, secID = seedInvestmentFixture(ctx, t, db, prefix)

	userAccountID := uuid.New()
	_, err := db.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, $3, 'investment')`, userAccountID, userID, prefix+"-inst")
	require.NoError(t, err, "seed user_account (the chain key)")

	_, err = db.Exec(ctx, `UPDATE accounts SET user_account_id = $2 WHERE id = $1`,
		oldAccountID, userAccountID)
	require.NoError(t, err, "attach the old account to the chain")

	newItemID := uuid.New()
	newAccountID = uuid.New()
	_, err = db.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')`, newItemID, userID, prefix+"-item2-"+newItemID.String())
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'New Brokerage', 'investment', 'active', $5)`,
		newAccountID, newItemID, userID, prefix+"-acct2-"+newAccountID.String(), userAccountID)
	require.NoError(t, err, "seed the successor account")

	// THE RE-LINK: the predecessor is retained, not deleted, and closes its active interval today.
	_, err = db.Exec(ctx, `
		UPDATE accounts SET status = 'replaced', replaced_by_id = $2, replaced_at = NOW(), updated_at = NOW()
		WHERE id = $1`, oldAccountID, newAccountID)
	require.NoError(t, err, "mark the predecessor replaced")

	return userID, oldAccountID, newAccountID, secID
}

// TestPortfolioSnapshot_Upsert_SameDayRelink_CountsTheChainOnce is the item-A regression.
//
// Before the fix, `holdings h JOIN accounts a` summed EVERY account live at any point during the
// snapshot date, and on a re-link day BOTH chain members qualify — so one logical $5,000 position
// produced a $9,000 snapshot (the retained predecessor's stale $4,000 plus the successor's
// $5,000). The liveness predicate is right and must not be narrowed; what was missing is that the
// portfolio side had never been taught about re-link chains at all (the balance side dedups on
// COALESCE(user_account_id, account_id) in over thirty places; the portfolio side mentioned
// user_account_id nowhere).
func TestPortfolioSnapshot_Upsert_SameDayRelink_CountsTheChainOnce(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, oldAccountID, newAccountID, secID := seedRelinkedInvestmentChain(ctx, t, tx, "psnap-relink")

	// The predecessor's retained holdings, last observed an hour ago: a STALE valuation.
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value, updated_at)
		VALUES ($1, $2, $3, $4, 10, 3000.00, 4000.00, NOW() - INTERVAL '1 hour')
	`, uuid.New(), userID, oldAccountID, secID)
	require.NoError(t, err, "seed the predecessor's retained holding")

	// The successor reports the SAME logical position, freshly.
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value, updated_at)
		VALUES ($1, $2, $3, $4, 10, 3000.00, 5000.00, NOW())
	`, uuid.New(), userID, newAccountID, secID)
	require.NoError(t, err, "seed the successor's holding")

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), today)
	require.NoError(t, err)

	require.InDelta(t, 5000.0, snap.TotalValue, 0.001,
		"9000 here is the double-count: the re-linked chain contributed twice on the overlap day, "+
			"so a $5,000 portfolio reads as $9,000 the day the user changed brokers. The winner "+
			"must be the chain's FRESHEST observation (the successor's 5000), matching the "+
			"balance side's DedupKeepsLatestInChain rule.")
	require.InDelta(t, 3000.0, snap.TotalCostBasis, 0.001,
		"cost basis must be deduped by the same rule, or gain/loss is computed against a doubled basis")
}

// TestPortfolioSnapshot_Upsert_RelinkBeforeTheSuccessorHasSynced_KeepsThePredecessor pins the OTHER
// direction, and it is why the winner is the freshest OBSERVATION rather than the newest ACCOUNT.
//
// A re-link creates the successor row before any investment sync has written a single holding for
// it. If dedup simply preferred the newest/active chain member, the daily upsert that runs in that
// window would write ZERO for the day — a cliff in the portfolio curve on a day the user's money
// did not move, which is exactly the retroactive-rewrite failure #1569 exists to prevent.
func TestPortfolioSnapshot_Upsert_RelinkBeforeTheSuccessorHasSynced_KeepsThePredecessor(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, oldAccountID, _, secID := seedRelinkedInvestmentChain(ctx, t, tx, "psnap-relink-nosync")

	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value)
		VALUES ($1, $2, $3, $4, 10, 3000.00, 4000.00)
	`, uuid.New(), userID, oldAccountID, secID)
	require.NoError(t, err)

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), today)
	require.NoError(t, err)

	require.InDelta(t, 4000.0, snap.TotalValue, 0.001,
		"0 here means dedup picked the successor purely for being newer, even though nothing has "+
			"observed it yet — the curve would fall off a cliff on re-link day and recover the "+
			"next sync")
}

// TestPortfolioSnapshot_Upsert_BothSiblingsLeftActive_StillCountsTheChainOnce is the case leg 2 of
// the chain-winner order cannot separate, and it is not hypothetical: account_service's re-link
// LOGS AND CONTINUES when its UpdateStatus of the predecessor fails (item B of #1569 task 9.1), so
// a transient error leaves both siblings 'active' with nothing to say so. Chain dedup must hold the
// portfolio number correct across that window, and the winner must be the successor — the sibling
// the user's new bank connection actually feeds.
func TestPortfolioSnapshot_Upsert_BothSiblingsLeftActive_StillCountsTheChainOnce(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, oldAccountID, newAccountID, secID := seedRelinkedInvestmentChain(ctx, t, tx, "psnap-bothactive")

	// Undo the status transition: this is the fail-open state, where the re-link created the
	// successor and never managed to mark the predecessor.
	_, err = tx.Exec(ctx, `
		UPDATE accounts SET status = 'active', replaced_by_id = NULL, replaced_at = NULL
		WHERE id = $1`, oldAccountID)
	require.NoError(t, err)
	// created_at defaults to the TRANSACTION timestamp, so both rows tie on it inside one test
	// transaction. Production never does — the predecessor predates the re-link by weeks.
	_, err = tx.Exec(ctx, `UPDATE accounts SET created_at = NOW() - INTERVAL '30 days' WHERE id = $1`,
		oldAccountID)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value)
		VALUES ($1, $2, $3, $4, 10, 3000.00, 4000.00)`, uuid.New(), userID, oldAccountID, secID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value)
		VALUES ($1, $2, $3, $4, 10, 3000.00, 5000.00)`, uuid.New(), userID, newAccountID, secID)
	require.NoError(t, err)

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), today)
	require.NoError(t, err)
	require.InDelta(t, 5000.0, snap.TotalValue, 0.001,
		"9000 means the both-active window doubles the portfolio; 4000 means the winner was "+
			"decided by uuid rather than by chain recency")
}

// TestPortfolioSnapshot_Upsert_LegacyNullUserAccountIDs_DoNotCollapseIntoOneChain guards the half
// of the dedup key that is easy to drop as noise. `accounts.user_account_id` is NULLABLE and
// pre-dates the chain design, so two entirely unrelated legacy accounts both key on NULL. Deduping
// on the raw column would put them in ONE bucket and silently delete a whole account's holdings
// from every portfolio total — the opposite failure to the one the dedup was added for, and a much
// quieter one. COALESCE(user_account_id, id) is why they stay separate; it is the same key
// balance_snapshot_repository.go uses, for the same reason.
func TestPortfolioSnapshot_Upsert_LegacyNullUserAccountIDs_DoNotCollapseIntoOneChain(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountA, secID := seedInvestmentFixture(ctx, t, tx, "psnap-nullchain")
	accountB := seedAccount(ctx, t, tx, userID, linkedItemID, "psnap-nullchain-b")

	var nulls int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT count(*) FROM accounts WHERE id = ANY($1) AND user_account_id IS NULL`,
		[]uuid.UUID{accountA, accountB}).Scan(&nulls))
	require.Equal(t, 2, nulls, "the fixture must actually produce the legacy NULL shape")

	for _, acct := range []uuid.UUID{accountA, accountB} {
		_, err = tx.Exec(ctx, `
			INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value)
			VALUES ($1, $2, $3, $4, 10, 1000.00, 2500.00)`, uuid.New(), userID, acct, secID)
		require.NoError(t, err)
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), today)
	require.NoError(t, err)
	require.InDelta(t, 5000.0, snap.TotalValue, 0.001,
		"2500 means the two NULL-keyed accounts collapsed into one bucket and half the user's "+
			"portfolio vanished")
}
