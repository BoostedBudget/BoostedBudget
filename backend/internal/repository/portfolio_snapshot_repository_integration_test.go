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
