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

	// THE RE-LINK, in the shape production writes it (account_service step 5b): the OLD LINKED
	// ITEM is marked replaced and points at the new one — that item-level link is the only durable
	// record that these two accounts are a chain rather than two accounts that happen to share a
	// user_account_id. The account row is marked replaced with a NULL replaced_by_id, exactly as
	// UpdateStatus(..., AccountStatusReplaced, nil) leaves it.
	_, err = db.Exec(ctx, `
		UPDATE linked_items SET status = 'replaced', replaced_at = NOW(), replaced_by_id = $2
		WHERE id = (SELECT linked_item_id FROM accounts WHERE id = $1)`, oldAccountID, newItemID)
	require.NoError(t, err, "mark the predecessor's linked item replaced")
	_, err = db.Exec(ctx, `
		UPDATE accounts SET status = 'replaced', replaced_at = NOW(), updated_at = NOW()
		WHERE id = $1`, oldAccountID)
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
		"9000 here is the double-count: both members of the chain contributed on the overlap day, "+
			"so a $5,000 portfolio reads as $9,000 the day the user changed brokers. Only the "+
			"SUPERSEDED member may be dropped, and only because the successor's linked item is "+
			"recorded as replacing its own.")
	require.InDelta(t, 3000.0, snap.TotalCostBasis, 0.001,
		"cost basis must be excluded by the same rule, or gain/loss is computed against a doubled basis")
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

// TestPortfolioSnapshot_Upsert_TwoLiveAccountsSharingAChainKey_BOTHCount is the test that refuted
// this fix's first version, and it is kept as the guard against ever writing that version again.
//
// The first fix collapsed each user_account_id to one row, copying the balance side. It rested on
// an invariant stated as fact in account_liveness_sql.go's household comment — that two 'active'
// accounts never share a user_account_id — and that invariant is FALSE. MatchOrCreate keys on
// (user, institution, MASK), and a NULL mask matches every other NULL mask at that institution, so
// a brokerage and an IRA at one broker (neither of which Plaid gives a mask) get the SAME
// user_accounts row while both are live. Probed against real Postgres before this test was written.
//
// One-row-per-chain therefore HALVES such a portfolio: a larger error than the doubling it was
// meant to fix, in the opposite direction, and invisible because the number stays plausible.
func TestPortfolioSnapshot_Upsert_TwoLiveAccountsSharingAChainKey_BOTHCount(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, brokerage, secID := seedInvestmentFixture(ctx, t, tx, "psnap-maskless")

	// Both accounts get the SAME user_account_id, exactly as MatchOrCreate would hand them out for
	// two maskless accounts at one institution, and BOTH stay active — they coexist, they are not
	// a chain.
	userAccountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, $3, 'investment')`, userAccountID, userID, "psnap-maskless-ins")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE accounts SET user_account_id = $2 WHERE id = $1`, brokerage, userAccountID)
	require.NoError(t, err)

	ira := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Roth IRA', 'investment', 'active', $5)`,
		ira, linkedItemID, userID, "psnap-maskless-ira-"+ira.String(), userAccountID)
	require.NoError(t, err)

	for _, acct := range []uuid.UUID{brokerage, ira} {
		_, err = tx.Exec(ctx, `
			INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value)
			VALUES ($1, $2, $3, $4, 10, 2000.00, 4000.00)`, uuid.New(), userID, acct, secID)
		require.NoError(t, err)
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), today)
	require.NoError(t, err)
	require.InDelta(t, 8000.0, snap.TotalValue, 0.001,
		"4000 means one of two genuinely live accounts was dropped as a duplicate of the other. "+
			"They overlap in time; a re-link chain abuts. Only succession may exclude a row.")
}

// TestPortfolioSnapshot_Upsert_LegacyNullUserAccountIDs_DoNotCollapseIntoOneChain guards the NULL
// half of the sibling test. `accounts.user_account_id` is NULLABLE and pre-dates the chain design,
// so two entirely unrelated legacy accounts both carry NULL. Plain SQL equality already refuses to
// match NULL to NULL, which is exactly right here — but "fixing" that with IS NOT DISTINCT FROM,
// or with a COALESCE onto a shared literal, would make every identity-less account a sibling of
// every other and silently delete whole accounts from the portfolio total.
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

// TestPortfolioSnapshot_Upsert_RelinkDoesNotSuppressAnUNRELATEDAccountOnTheOldItem pins the
// same-chain leg of the exclusion, which two mutation rounds showed nothing else was holding.
//
// The shape is ordinary: a user with two accounts at one institution closes one of them at the
// bank, then re-links. The new item reports only the survivor. On the changeover day the old
// item's OTHER account was still live and still held money — it has no successor at all — so it
// must count. Drop the `s.user_account_id = l.user_account_id` leg and the survivor's successor
// suppresses it too, silently deleting a real account's holdings from that day's total.
func TestPortfolioSnapshot_Upsert_RelinkDoesNotSuppressAnUNRELATEDAccountOnTheOldItem(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, oldAccountID, newAccountID, secID := seedRelinkedInvestmentChain(ctx, t, tx, "psnap-unrelated")

	// A SECOND account on the OLD item, with its own stable identity and no successor.
	var oldItemID uuid.UUID
	require.NoError(t, tx.QueryRow(ctx, `SELECT linked_item_id FROM accounts WHERE id = $1`, oldAccountID).
		Scan(&oldItemID))
	otherUA, otherAccount := uuid.New(), uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, 'psnap-unrelated-ins2', 'investment')`, otherUA, userID)
	require.NoError(t, err)
	// Inserted ACTIVE and then closed, so the 000152 trigger records a real active interval — an
	// account inserted straight into a terminal status has no 'active' history and is excluded by
	// liveness before this rule ever sees it, which would make the test pass for the wrong reason.
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Closed At The Bank', 'investment', 'active', $5)`,
		otherAccount, oldItemID, userID, "psnap-unrelated-other-"+otherAccount.String(), otherUA)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		UPDATE accounts SET status = 'replaced', replaced_at = NOW() WHERE id = $1`, otherAccount)
	require.NoError(t, err)

	for acct, value := range map[uuid.UUID]float64{oldAccountID: 4000, newAccountID: 5000, otherAccount: 1500} {
		_, err = tx.Exec(ctx, `
			INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value)
			VALUES ($1, $2, $3, $4, 10, 1000.00, $5)`, uuid.New(), userID, acct, secID, value)
		require.NoError(t, err)
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), today)
	require.NoError(t, err)
	require.InDelta(t, 6500.0, snap.TotalValue, 0.001,
		"expected the successor (5000) plus the unrelated old-item account (1500). 5000 means the "+
			"exclusion reached across chains and deleted an account nothing replaced; 9000+ means "+
			"the predecessor was not excluded at all")
}

// TestPortfolioSnapshot_Upsert_NullChainKeysAcrossARelink_BothCount pins the NULL half of the same
// leg, which is a separate mutation: `IS NOT DISTINCT FROM` makes NULL match NULL.
//
// Two identity-less accounts, one on a replaced item and one on its successor, may or may not be
// the same logical account — the chain key that would say is exactly what is missing. Under NULL
// matching, the older one is deleted from the total. Under plain equality it is retained and the
// day may double-count. #1569's bias decides the tie: retain and possibly over-report a legacy
// shape rather than silently destroy a real account's holdings, and the doubling is at least
// visible while the deletion is not.
func TestPortfolioSnapshot_Upsert_NullChainKeysAcrossARelink_BothCount(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, oldAccountID, newAccountID, secID := seedRelinkedInvestmentChain(ctx, t, tx, "psnap-nullrelink")
	_, err = tx.Exec(ctx, `UPDATE accounts SET user_account_id = NULL WHERE id = ANY($1)`,
		[]uuid.UUID{oldAccountID, newAccountID})
	require.NoError(t, err, "strip the chain key from BOTH members — the legacy shape")

	for acct, value := range map[uuid.UUID]float64{oldAccountID: 4000, newAccountID: 5000} {
		_, err = tx.Exec(ctx, `
			INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value)
			VALUES ($1, $2, $3, $4, 10, 1000.00, $5)`, uuid.New(), userID, acct, secID, value)
		require.NoError(t, err)
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), today)
	require.NoError(t, err)
	require.InDelta(t, 9000.0, snap.TotalValue, 0.001,
		"5000 means NULL was matched to NULL and an account with no recorded relationship to the "+
			"successor was deleted from the total")
}

// ── #1569 task 9-FIX: three residuals inside the re-link exclusion, all money-visible ─────────

// TestPortfolioSnapshot_Upsert_TwoMasklessPredecessorsOneSuccessor_TheUNREPLACEDOneStillCounts is
// item 1: the same NULL-mask collision that killed this fix's FIRST version, now biting the
// replacement rule from the other side.
//
// One old item holds TWO maskless accounts that MatchOrCreate handed the SAME user_account_id. The
// successor item reports only ONE of them. The naive rule asks "does SOME sibling under the
// replacing item share my chain key and have an observation?" — and the answer is yes for BOTH old
// accounts, so both are excluded and the one nothing replaced vanishes from the total.
//
// The existing unrelated-account test cannot see this: its second old account has a DISTINCT
// user_account_id, so the chain-key leg already protects it.
func TestPortfolioSnapshot_Upsert_TwoMasklessPredecessorsOneSuccessor_TheUNREPLACEDOneStillCounts(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, oldAccountID, newAccountID, secID := seedRelinkedInvestmentChain(ctx, t, tx, "psnap-maskless-pair")

	var oldItemID, chainKey uuid.UUID
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT linked_item_id, user_account_id FROM accounts WHERE id = $1`, oldAccountID).
		Scan(&oldItemID, &chainKey))

	// The SECOND maskless account on the OLD item, sharing the SAME user_account_id — the shape
	// MatchOrCreate really produces for a brokerage and an IRA at one broker. Inserted active and
	// then replaced, so the 000152 trigger records a real live interval for today.
	sibling := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Roth IRA', 'investment', 'active', $5)`,
		sibling, oldItemID, userID, "psnap-maskless-pair-ira-"+sibling.String(), chainKey)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE accounts SET status = 'replaced', replaced_at = NOW() WHERE id = $1`, sibling)
	require.NoError(t, err)

	// Both predecessors carry the SAME retained value, so the assertion does not depend on WHICH
	// of the two the rule keeps — only on the fact that exactly one of them survives.
	for acct, value := range map[uuid.UUID]float64{oldAccountID: 1500, sibling: 1500, newAccountID: 5000} {
		_, err = tx.Exec(ctx, `
			INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value)
			VALUES ($1, $2, $3, $4, 10, 500.00, $5)`, uuid.New(), userID, acct, secID, value)
		require.NoError(t, err)
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), today)
	require.NoError(t, err)
	require.InDelta(t, 6500.0, snap.TotalValue, 0.001,
		"5000 means ONE successor suppressed TWO predecessors and a real account's money was "+
			"deleted from the day; 8000 means neither predecessor was excluded")
}

// TestPortfolioSnapshot_Upsert_ThreeLinkChainOnOneDay_CountsOnce is item 3: A → B → C, all live on
// one day, B never observed. A checks only its DIRECT successor B, finds no observation, and stays
// in; B is excluded because C is observed. A and C are both summed — the doubling, one link
// further along.
func TestPortfolioSnapshot_Upsert_ThreeLinkChainOnOneDay_CountsOnce(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// A (old item) → B (middle item), from the shared helper.
	userID, accountA, accountB, secID := seedRelinkedInvestmentChain(ctx, t, tx, "psnap-3link")

	var middleItemID, chainKey uuid.UUID
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT linked_item_id, user_account_id FROM accounts WHERE id = $1`, accountB).
		Scan(&middleItemID, &chainKey))

	// B → C: a SECOND re-link of the same chain on the same day.
	newItemID, accountC := uuid.New(), uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')`, newItemID, userID, "psnap-3link-item3-"+newItemID.String())
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Newest Brokerage', 'investment', 'active', $5)`,
		accountC, newItemID, userID, "psnap-3link-acct3-"+accountC.String(), chainKey)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		UPDATE linked_items SET status = 'replaced', replaced_at = NOW(), replaced_by_id = $2 WHERE id = $1`,
		middleItemID, newItemID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE accounts SET status = 'replaced', replaced_at = NOW() WHERE id = $1`, accountB)
	require.NoError(t, err)

	// B is the link that was never synced: it has NO holdings at all. A carries the stale value.
	for acct, value := range map[uuid.UUID]float64{accountA: 4000, accountC: 5000} {
		_, err = tx.Exec(ctx, `
			INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value)
			VALUES ($1, $2, $3, $4, 10, 1000.00, $5)`, uuid.New(), userID, acct, secID, value)
		require.NoError(t, err)
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), today)
	require.NoError(t, err)
	require.InDelta(t, 5000.0, snap.TotalValue, 0.001,
		"9000 is the three-link double count: the rule looked only ONE edge ahead, saw B "+
			"unobserved, and kept A alongside the C that already superseded both")
}

// TestPortfolioSnapshot_Upsert_ConfirmedEmptySuccessor_DropsTheStalePredecessor is item 2, and it
// turns on a distinction this branch already made once for the holdings day-picker: "we observed
// it and it held nothing" is NOT "we never observed it".
//
// A successful /investments/holdings/get can list the successor with ZERO holdings — the user sold
// everything before re-linking. ReplaceForAccount still runs, but the capture inserts no row when
// there is nothing to insert, so the successor looks unobserved, the predecessor is retained, and
// the portfolio reports the predecessor's STALE value instead of the zero that is true.
func TestPortfolioSnapshot_Upsert_ConfirmedEmptySuccessor_DropsTheStalePredecessor(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, oldAccountID, newAccountID, secID := seedRelinkedInvestmentChain(ctx, t, tx, "psnap-emptysucc")

	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value)
		VALUES ($1, $2, $3, $4, 10, 3000.00, 4000.00)`, uuid.New(), userID, oldAccountID, secID)
	require.NoError(t, err)

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	// The successor holds NOTHING, and that is a CONFIRMED fact: the capture ran for it today and
	// recorded the observation even though there was no position to record.
	_, err = tx.Exec(ctx, `
		INSERT INTO holding_observations (user_id, account_id, as_of_date)
		VALUES ($1, $2, $3::date)`, userID, newAccountID, today)
	require.NoError(t, err, "record the confirmed-empty observation")

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), today)
	require.NoError(t, err)
	require.InDelta(t, 0.0, snap.TotalValue, 0.001,
		"4000 means a CONFIRMED-EMPTY successor was read as an unobserved one, so the retained "+
			"predecessor's stale valuation was reported as the user's portfolio")
}

// TestPortfolioSnapshot_Upsert_ItemPointerLost_TheAccountPointerStillIdentifiesTheSuccessor is
// item 4, the last leg of the money fix.
//
// account_service.go step 5b writes linked_items.replaced_by_id FAIL-OPEN: it logs and continues.
// The backstop that catches that failure — accountRepository.CreateBatch's
// supersedeStrandedChainSiblings — repairs the ACCOUNT row (status 'replaced' plus
// accounts.replaced_by_id) inside the transaction that inserts the successor. It does NOT repair
// the item row. So on that path the item-level succession test finds nothing at all, and the
// predecessor is summed beside its own successor: the doubling, back again, for that user.
//
// The fix reads EITHER recorded pointer. Both are written only by a re-link, and the same-chain
// leg still gates them.
func TestPortfolioSnapshot_Upsert_ItemPointerLost_TheAccountPointerStillIdentifiesTheSuccessor(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, oldAccountID, newAccountID, secID := seedRelinkedInvestmentChain(ctx, t, tx, "psnap-itemptr")

	// The fail-open write never landed: the old item still looks active and points at nothing.
	ct, err := tx.Exec(ctx, `
		UPDATE linked_items SET status = 'active', replaced_at = NULL, replaced_by_id = NULL
		WHERE id = (SELECT linked_item_id FROM accounts WHERE id = $1)`, oldAccountID)
	require.NoError(t, err)
	require.EqualValues(t, 1, ct.RowsAffected(), "the fixture must have written an item pointer to lose")

	// ...and CreateBatch's backstop repaired the ACCOUNT row instead, which is all it can reach.
	_, err = tx.Exec(ctx, `UPDATE accounts SET replaced_by_id = $2 WHERE id = $1`,
		oldAccountID, newAccountID)
	require.NoError(t, err)

	for acct, value := range map[uuid.UUID]float64{oldAccountID: 4000, newAccountID: 5000} {
		_, err = tx.Exec(ctx, `
			INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value)
			VALUES ($1, $2, $3, $4, 10, 1000.00, $5)`, uuid.New(), userID, acct, secID, value)
		require.NoError(t, err)
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), today)
	require.NoError(t, err)
	require.InDelta(t, 5000.0, snap.TotalValue, 0.001,
		"9000 means the only surviving record of the succession — accounts.replaced_by_id, written "+
			"atomically by CreateBatch's backstop — was ignored, so the fail-open item pointer "+
			"reintroduces the doubling it was supposed to have stopped")
}

// TestPortfolioSnapshot_Upsert_MasklessPairThroughAThreeLinkChain_StillKeepsTheUNREPLACEDOne is
// the interaction between items 1 and 3, and it is here because the first draft of this fix
// SURVIVED the mutant that removes the `surviving` CTE. Rationing and transitivity are each
// correct alone and wrong together without it.
//
// Two maskless predecessors A1/A2 under one old item, whose chain was then re-linked TWICE:
// A1/A2 → B → C, with B and C both observed. Counting every transitive successor gives the old
// item TWO successors (B and C) for TWO predecessors, so both are excluded — even though B is
// itself superseded by C and contributes nothing to the total. The account nothing replaced
// disappears again, by a different route.
//
// `surviving` is the rule that fixes it: a successor that has an observed successor of its own is
// not a survivor and may not be counted as one.
func TestPortfolioSnapshot_Upsert_MasklessPairThroughAThreeLinkChain_StillKeepsTheUNREPLACEDOne(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// A1 (old item) → B (middle item).
	userID, a1, accountB, secID := seedRelinkedInvestmentChain(ctx, t, tx, "psnap-pair3link")

	var oldItemID, middleItemID, chainKey uuid.UUID
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT linked_item_id, user_account_id FROM accounts WHERE id = $1`, a1).Scan(&oldItemID, &chainKey))
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT linked_item_id FROM accounts WHERE id = $1`, accountB).Scan(&middleItemID))

	// A2: the maskless sibling on the OLD item, same chain key, no successor of its own.
	a2 := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Roth IRA', 'investment', 'active', $5)`,
		a2, oldItemID, userID, "psnap-pair3link-ira-"+a2.String(), chainKey)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE accounts SET status = 'replaced', replaced_at = NOW() WHERE id = $1`, a2)
	require.NoError(t, err)

	// B → C: the second re-link, same day.
	newItemID, accountC := uuid.New(), uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')`, newItemID, userID, "psnap-pair3link-item3-"+newItemID.String())
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Newest Brokerage', 'investment', 'active', $5)`,
		accountC, newItemID, userID, "psnap-pair3link-acct3-"+accountC.String(), chainKey)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		UPDATE linked_items SET status = 'replaced', replaced_at = NOW(), replaced_by_id = $2 WHERE id = $1`,
		middleItemID, newItemID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE accounts SET status = 'replaced', replaced_at = NOW() WHERE id = $1`, accountB)
	require.NoError(t, err)

	// Both predecessors carry the same retained value, so the assertion does not depend on WHICH
	// one the deterministic ordering keeps.
	for acct, value := range map[uuid.UUID]float64{a1: 1500, a2: 1500, accountB: 2000, accountC: 5000} {
		_, err = tx.Exec(ctx, `
			INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value)
			VALUES ($1, $2, $3, $4, 10, 500.00, $5)`, uuid.New(), userID, acct, secID, value)
		require.NoError(t, err)
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), today)
	require.NoError(t, err)
	require.InDelta(t, 6500.0, snap.TotalValue, 0.001,
		"5000 means the superseded middle link B was counted as a surviving successor, so ONE "+
			"real successor rationed away TWO predecessors and the unreplaced account vanished")
}
