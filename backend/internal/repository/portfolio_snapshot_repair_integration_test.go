//go:build integration

package repository

// Integration tests for #1569 Task 11 (S6/E4): the coverage-limited repair of portfolio_snapshots.
//
// Every claim here is about what Postgres actually computes — that the reconstruction reads
// holding_snapshots and NEVER current holdings, that FILTER semantics match Upsert's, that a day
// with no coverage is left exactly as it is, and that a second run writes nothing. pgxmock asserts
// query TEXT and never executes it, so it can confirm none of that.
//
// Run with:
//
//	DATABASE_URL=postgres://devuser:devpass@localhost:5434/freshcheck?sslmode=disable \
//	  go test ./internal/repository/... -run TestPortfolioRepair_ -tags integration -p 1 -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ── fixture helpers ───────────────────────────────────────────────────────────────────────────

// dayUTC returns the SERVER's UTC date, n days back. Read from the server rather than the Go
// clock for the same reason todayUTC is: a test running near midnight UTC on a host in another
// zone would otherwise disagree with the repair about which day it means.
func dayUTC(ctx context.Context, t *testing.T, db DB, back int) time.Time {
	t.Helper()
	var d time.Time
	require.NoError(t, db.QueryRow(ctx,
		`SELECT (now() AT TIME ZONE 'UTC')::date - $1::int`, back).Scan(&d))
	return d
}

// backdateLiveness rewrites the account's trigger-created open 'active' interval so it starts
// `back` days ago. Without this every account is live only from the instant the fixture inserted
// it, and no PAST day would have any live account at all.
func backdateLiveness(ctx context.Context, t *testing.T, db DB, accountID uuid.UUID, back int) {
	t.Helper()
	ct, err := db.Exec(ctx, `
		UPDATE account_status_history ash
		SET valid_from = (((now() AT TIME ZONE 'UTC')::date)::timestamp AT TIME ZONE 'UTC')
		                 - make_interval(days => $2::int)
		WHERE ash.account_id = $1 AND ash.status = 'active' AND ash.valid_to IS NULL`,
		accountID, back)
	require.NoError(t, err)
	require.EqualValues(t, 1, ct.RowsAffected(), "the trigger-created 'active' interval must exist to rewrite")
}

func seedHoldingSnapshot(
	ctx context.Context, t *testing.T, db DB,
	userID, accountID, secID uuid.UUID, day time.Time, present bool, costBasis, instValue *float64,
) {
	t.Helper()
	_, err := db.Exec(ctx, `
		INSERT INTO holding_snapshots
			(user_id, account_id, security_id, as_of_date, present, quantity, cost_basis, institution_value)
		VALUES ($1, $2, $3, $4::date, $5, 1, $6, $7)`,
		userID, accountID, secID, day, present, costBasis, instValue)
	require.NoError(t, err)
}

func seedPortfolioSnapshot(
	ctx context.Context, t *testing.T, db DB,
	userID uuid.UUID, day time.Time, value, basis, gain float64, pct *float64,
) {
	t.Helper()
	_, err := db.Exec(ctx, `
		INSERT INTO portfolio_snapshots
			(user_id, snapshot_date, total_value, total_cost_basis, total_gain_loss, total_gain_loss_pct)
		VALUES ($1, $2::date, $3, $4, $5, $6)`,
		userID, day, value, basis, gain, pct)
	require.NoError(t, err)
}

// seedSecurity adds one more security to a fixture that needs several shapes on one day.
func seedSecurity(ctx context.Context, t *testing.T, db DB, label string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(ctx,
		`INSERT INTO securities (id, plaid_security_id, name, ticker_symbol) VALUES ($1, $2, $3, 'TSEC')`,
		id, label+"-"+id.String(), label)
	require.NoError(t, err)
	return id
}

func seedAccount(ctx context.Context, t *testing.T, db DB, userID, linkedItemID uuid.UUID, label string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, $5, 'investment', 'active')`,
		id, linkedItemID, userID, label+"-"+id.String(), label)
	require.NoError(t, err)
	return id
}

func seedCurrentHolding(
	ctx context.Context, t *testing.T, db DB, userID, accountID, secID uuid.UUID, costBasis, instValue float64,
) {
	t.Helper()
	_, err := db.Exec(ctx, `
		INSERT INTO holdings (user_id, account_id, security_id, quantity, cost_basis, institution_value)
		VALUES ($1, $2, $3, 1, $4, $5)`, userID, accountID, secID, costBasis, instValue)
	require.NoError(t, err)
}

type storedSnapshot struct {
	totalValue, totalCostBasis, totalGainLoss float64
	totalGainLossPct                          *float64
	updatedAt                                 time.Time
}

func readPortfolioSnapshot(ctx context.Context, t *testing.T, db DB, userID uuid.UUID, day time.Time) storedSnapshot {
	t.Helper()
	var s storedSnapshot
	require.NoError(t, db.QueryRow(ctx, `
		SELECT total_value, total_cost_basis, total_gain_loss, total_gain_loss_pct, updated_at
		FROM portfolio_snapshots WHERE user_id = $1 AND snapshot_date = $2::date`, userID, day).
		Scan(&s.totalValue, &s.totalCostBasis, &s.totalGainLoss, &s.totalGainLossPct, &s.updatedAt))
	return s
}

type ledgerRow struct {
	outcome                string
	reason                 string
	expectedCount          int
	observedCount          int
	previousTotalValue     float64
	repairedTotalValue     *float64
	repairedTotalCostBasis *float64
	repairedTotalGainLoss  *float64
	repairedTotalGainPct   *float64
}

func readLedger(ctx context.Context, t *testing.T, db DB, userID uuid.UUID, day time.Time) (ledgerRow, bool) {
	t.Helper()
	rows, err := db.Query(ctx, `
		SELECT outcome, reason, expected_account_count, observed_account_count,
		       previous_total_value, repaired_total_value, repaired_total_cost_basis,
		       repaired_total_gain_loss, repaired_total_gain_loss_pct
		FROM portfolio_snapshot_repairs WHERE user_id = $1 AND snapshot_date = $2::date`, userID, day)
	require.NoError(t, err)
	defer rows.Close()
	if !rows.Next() {
		require.NoError(t, rows.Err())
		return ledgerRow{}, false
	}
	var l ledgerRow
	require.NoError(t, rows.Scan(&l.outcome, &l.reason, &l.expectedCount, &l.observedCount,
		&l.previousTotalValue, &l.repairedTotalValue, &l.repairedTotalCostBasis,
		&l.repairedTotalGainLoss, &l.repairedTotalGainPct))
	return l, true
}

func ledgerCount(ctx context.Context, t *testing.T, db DB, userID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(ctx,
		`SELECT count(*) FROM portfolio_snapshot_repairs WHERE user_id = $1`, userID).Scan(&n))
	return n
}

// ── Test 1: a fully covered day is rebuilt from HISTORY, all four columns, never from today's
// prices ──────────────────────────────────────────────────────────────────────────────────────
//
// The trap this test exists for: Upsert computes SUM(h.institution_value) from CURRENT holdings,
// and there is no price-history table. Both of Upsert's callers pass today, so that is harmless in
// normal operation — but a repair IS a past-day recompute, so the obvious implementation would
// silently overwrite recorded history with today's prices. The current holdings here are seeded at
// 999999 against a history of 6000 precisely so that a repair reading them cannot pass.
//
// The three row shapes reproduce task 10's ReconstructsAllFourPortfolioSnapshotColumns fixture:
// value 6000, basis 6000, gain 1000, pct 25 — with filtered basis (4000) deliberately DIFFERENT
// from total basis (6000), so a rebuild that divides by the wrong denominator cannot pass either.
func TestPortfolioRepair_FullCoverageDay_RebuiltFromHistoryNotCurrentPrices(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secA := seedInvestmentFixture(ctx, t, tx, "repair-full")
	backdateLiveness(ctx, t, tx, accountID, 30)
	secB := seedSecurity(ctx, t, tx, "repair-full-b")
	secC := seedSecurity(ctx, t, tx, "repair-full-c")

	day := dayUTC(ctx, t, tx, 3)
	seedHoldingSnapshot(ctx, t, tx, userID, accountID, secA, day, true, f64(4000), f64(5000))
	seedHoldingSnapshot(ctx, t, tx, userID, accountID, secB, day, true, nil, f64(1000))
	seedHoldingSnapshot(ctx, t, tx, userID, accountID, secC, day, true, f64(2000), nil)

	// Today's prices, wildly different from that day's. Nothing in the repair may read these.
	seedCurrentHolding(ctx, t, tx, userID, accountID, secA, 1, 999999)

	seedPortfolioSnapshot(ctx, t, tx, userID, day, 1500, 1500, 0, nil)

	repo := NewPortfolioSnapshotRepository(tx)
	sum, err := repo.RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	assert.Equal(t, 1, sum.Examined)
	assert.Equal(t, 1, sum.Repaired)

	got := readPortfolioSnapshot(ctx, t, tx, userID, day)
	assert.InDelta(t, 6000.0, got.totalValue, 0.001,
		"total_value must be the HISTORY sum (6000), not the current-price sum (999999)")
	assert.InDelta(t, 6000.0, got.totalCostBasis, 0.001)
	assert.InDelta(t, 1000.0, got.totalGainLoss, 0.001,
		"gain is SUM(iv)-SUM(cb) over only rows where BOTH are non-NULL: 5000-4000")
	require.NotNil(t, got.totalGainLossPct)
	assert.InDelta(t, 25.0, *got.totalGainLossPct, 0.001,
		"pct denominator is the FILTERED basis (4000), not total_cost_basis (6000)")

	l, ok := readLedger(ctx, t, tx, userID, day)
	require.True(t, ok, "a repaired day must be recorded in the ledger")
	assert.Equal(t, domain.PortfolioRepairOutcomeRepaired, l.outcome)
	assert.Equal(t, 1, l.expectedCount)
	assert.Equal(t, 1, l.observedCount)
	assert.InDelta(t, 1500.0, l.previousTotalValue, 0.001, "the pre-repair value survives only here")
	require.NotNil(t, l.repairedTotalValue)
	assert.InDelta(t, 6000.0, *l.repairedTotalValue, 0.001)
}

// ── Test 2: a day with NO history rows is left exactly as it is, and no zero is written ────────
//
// This is the single binding constraint (task 10, ruling R36). A day can have no rows because it
// predates the history tables, or because a partial sync skipped the account and the daily sweep
// never ran. "No rows" is indistinguishable from "held nothing", and writing a zero there is the
// same corruption this repair exists to remove.
func TestPortfolioRepair_DayWithNoHistoryRows_IsLeftAloneAndNeverZeroed(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secA := seedInvestmentFixture(ctx, t, tx, "repair-nocov")
	backdateLiveness(ctx, t, tx, accountID, 30)
	seedCurrentHolding(ctx, t, tx, userID, accountID, secA, 100, 200)

	day := dayUTC(ctx, t, tx, 4)
	seedPortfolioSnapshot(ctx, t, tx, userID, day, 777, 700, 77, f64(11))
	before := readPortfolioSnapshot(ctx, t, tx, userID, day)

	repo := NewPortfolioSnapshotRepository(tx)
	sum, err := repo.RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	assert.Equal(t, 1, sum.Examined)
	assert.Equal(t, 1, sum.RefusedNoCoverage)
	assert.Equal(t, 0, sum.Repaired)

	after := readPortfolioSnapshot(ctx, t, tx, userID, day)
	assert.InDelta(t, 777.0, after.totalValue, 0.001, "a day with no coverage must never be zeroed")
	assert.InDelta(t, 700.0, after.totalCostBasis, 0.001)
	assert.InDelta(t, 77.0, after.totalGainLoss, 0.001)
	require.NotNil(t, after.totalGainLossPct)
	assert.InDelta(t, 11.0, *after.totalGainLossPct, 0.001)
	assert.True(t, before.updatedAt.Equal(after.updatedAt),
		"a refusal must not even touch updated_at — an untouched row must read as untouched")

	l, ok := readLedger(ctx, t, tx, userID, day)
	require.True(t, ok, "a refusal must be recorded, or 'left alone' is indistinguishable from 'never examined'")
	assert.Equal(t, domain.PortfolioRepairOutcomeRefusedNoCoverage, l.outcome)
	assert.Equal(t, 0, l.observedCount)
	assert.Nil(t, l.repairedTotalValue, "a refusal writes nothing, and NULL here says so")
	assert.NotEmpty(t, l.reason)
}

// ── Test 3 (the ruling): a PARTIALLY covered day is refused, not half-rebuilt ──────────────────
//
// Two accounts are live and known to bear holdings; only one has rows for the day. Rebuilding from
// that produces a number that is wrong in a PLAUSIBLE way — a portfolio curve that dips on a day
// nothing happened — which is worse than an obvious gap, because nothing downstream can tell it is
// wrong. The day is refused and recorded.
func TestPortfolioRepair_PartiallyCoveredDay_IsRefusedNotHalfRebuilt(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountA, secA := seedInvestmentFixture(ctx, t, tx, "repair-partial")
	backdateLiveness(ctx, t, tx, accountA, 30)
	accountB := seedAccount(ctx, t, tx, userID, linkedItemID, "repair-partial-b")
	backdateLiveness(ctx, t, tx, accountB, 30)
	// Account B is holdings-bearing: it holds something right now. It has NO row for the day.
	seedCurrentHolding(ctx, t, tx, userID, accountB, secA, 3000, 3500)

	day := dayUTC(ctx, t, tx, 5)
	seedHoldingSnapshot(ctx, t, tx, userID, accountA, secA, day, true, f64(1000), f64(1200))
	seedPortfolioSnapshot(ctx, t, tx, userID, day, 4700, 4000, 700, f64(17.5))

	repo := NewPortfolioSnapshotRepository(tx)
	sum, err := repo.RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	assert.Equal(t, 1, sum.RefusedPartialCoverage)
	assert.Equal(t, 0, sum.Repaired)

	after := readPortfolioSnapshot(ctx, t, tx, userID, day)
	assert.InDelta(t, 4700.0, after.totalValue, 0.001,
		"a partially covered day must keep its stored value — 1200 would be a plausible-looking lie")

	l, ok := readLedger(ctx, t, tx, userID, day)
	require.True(t, ok)
	assert.Equal(t, domain.PortfolioRepairOutcomeRefusedPartialCoverage, l.outcome)
	assert.Equal(t, 2, l.expectedCount)
	assert.Equal(t, 1, l.observedCount)
	assert.Nil(t, l.repairedTotalValue)
}

// ── Test 4: the repair is idempotent ──────────────────────────────────────────────────────────
func TestPortfolioRepair_SecondRunChangesNothing(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountA, secA := seedInvestmentFixture(ctx, t, tx, "repair-idem")
	backdateLiveness(ctx, t, tx, accountA, 30)
	accountB := seedAccount(ctx, t, tx, userID, linkedItemID, "repair-idem-b")
	backdateLiveness(ctx, t, tx, accountB, 30)
	seedCurrentHolding(ctx, t, tx, userID, accountB, secA, 10, 20)

	// All three coverage states in one fixture: full, none, partial.
	full := dayUTC(ctx, t, tx, 6)
	none := dayUTC(ctx, t, tx, 7)
	partial := dayUTC(ctx, t, tx, 8)
	seedHoldingSnapshot(ctx, t, tx, userID, accountA, secA, full, true, f64(100), f64(150))
	seedHoldingSnapshot(ctx, t, tx, userID, accountB, secA, full, true, f64(10), f64(25))
	seedHoldingSnapshot(ctx, t, tx, userID, accountA, secA, partial, true, f64(100), f64(150))
	for _, d := range []time.Time{full, none, partial} {
		seedPortfolioSnapshot(ctx, t, tx, userID, d, 1, 1, 0, nil)
	}

	repo := NewPortfolioSnapshotRepository(tx)
	first, err := repo.RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	assert.Equal(t, 3, first.Examined)
	assert.Equal(t, 1, first.Repaired, "only the fully covered day is repairable")
	assert.Equal(t, 1, first.RefusedNoCoverage)
	assert.Equal(t, 1, first.RefusedPartialCoverage)

	afterFirst := map[string]storedSnapshot{}
	for _, d := range []time.Time{full, none, partial} {
		afterFirst[d.Format("2006-01-02")] = readPortfolioSnapshot(ctx, t, tx, userID, d)
	}
	require.Equal(t, 3, ledgerCount(ctx, t, tx, userID))

	second, err := repo.RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	assert.Equal(t, 0, second.Examined, "a second run must find nothing left to examine")

	assert.Equal(t, 3, ledgerCount(ctx, t, tx, userID), "a second run must not append a second verdict")
	for _, d := range []time.Time{full, none, partial} {
		got := readPortfolioSnapshot(ctx, t, tx, userID, d)
		want := afterFirst[d.Format("2006-01-02")]
		assert.InDelta(t, want.totalValue, got.totalValue, 0.001)
		assert.True(t, want.updatedAt.Equal(got.updatedAt),
			"a second run must not touch updated_at on %s", d.Format("2006-01-02"))
	}
}

// ── Test 5: a CONFIRMED-empty day is repairable to zero ───────────────────────────────────────
//
// The distinction that makes the "never write a zero" rule workable. A day whose rows all read
// present = false is positive evidence that the capture ran and observed nothing — a liquidation.
// That is not the same as a day with no rows at all, and conflating the two would either zero real
// history or refuse to record a real liquidation. Coverage is therefore counted WITHOUT filtering
// on present, while the values are summed from present rows only.
func TestPortfolioRepair_ConfirmedEmptyDay_IsRepairedToZero(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secA := seedInvestmentFixture(ctx, t, tx, "repair-empty")
	backdateLiveness(ctx, t, tx, accountID, 30)

	day := dayUTC(ctx, t, tx, 9)
	seedHoldingSnapshot(ctx, t, tx, userID, accountID, secA, day, false, f64(4000), f64(5000))
	seedPortfolioSnapshot(ctx, t, tx, userID, day, 5000, 4000, 1000, f64(25))

	repo := NewPortfolioSnapshotRepository(tx)
	sum, err := repo.RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	assert.Equal(t, 1, sum.Repaired)

	after := readPortfolioSnapshot(ctx, t, tx, userID, day)
	assert.InDelta(t, 0.0, after.totalValue, 0.001,
		"an observed-and-empty day is a liquidation, and zero is the recorded answer — not a guess")
	assert.InDelta(t, 0.0, after.totalCostBasis, 0.001)
	assert.InDelta(t, 0.0, after.totalGainLoss, 0.001)
	assert.Nil(t, after.totalGainLossPct, "pct is NULL when the filtered basis is 0")
}

// ── Test 6: TODAY is never examined ───────────────────────────────────────────────────────────
//
// Today's history is still being written — a sync later today will add rows, and the daily sweep
// will rewrite the aggregate anyway. Pinning a verdict on today would freeze a mid-day partial as
// if it were settled history.
func TestPortfolioRepair_TodayIsNeverExamined(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secA := seedInvestmentFixture(ctx, t, tx, "repair-today")
	backdateLiveness(ctx, t, tx, accountID, 30)

	today := dayUTC(ctx, t, tx, 0)
	seedHoldingSnapshot(ctx, t, tx, userID, accountID, secA, today, true, f64(4000), f64(5000))
	seedPortfolioSnapshot(ctx, t, tx, userID, today, 1, 1, 0, nil)

	repo := NewPortfolioSnapshotRepository(tx)
	sum, err := repo.RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	assert.Equal(t, 0, sum.Examined)

	after := readPortfolioSnapshot(ctx, t, tx, userID, today)
	assert.InDelta(t, 1.0, after.totalValue, 0.001, "today is the daily job's to write, not the repair's")
	_, ok := readLedger(ctx, t, tx, userID, today)
	assert.False(t, ok)
}

// ── Test 7: the repair never widens its own scope ─────────────────────────────────────────────
//
// A day with history coverage but no portfolio_snapshots row must NOT gain one. The repair fixes
// rows that exist; inventing aggregates for days the product never recorded is a different
// operation with a different risk profile, and it is not this one.
func TestPortfolioRepair_NeverCreatesASnapshotRowThatDidNotExist(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secA := seedInvestmentFixture(ctx, t, tx, "repair-noinvent")
	backdateLiveness(ctx, t, tx, accountID, 30)

	day := dayUTC(ctx, t, tx, 10)
	seedHoldingSnapshot(ctx, t, tx, userID, accountID, secA, day, true, f64(4000), f64(5000))

	repo := NewPortfolioSnapshotRepository(tx)
	sum, err := repo.RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	assert.Equal(t, 0, sum.Examined)

	var n int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT count(*) FROM portfolio_snapshots WHERE user_id = $1`, userID).Scan(&n))
	assert.Equal(t, 0, n, "the repair must not invent an aggregate for a day the product never recorded")
}

// ── Test 8: an account that was NOT live on the day is excluded, exactly as Upsert excludes it ──
//
// Holdings for a disconnected account are retained forever (accounts are soft-removed and the only
// DELETE FROM holdings runs for accounts a sync reaches). The per-account capture path carries no
// liveness test of its own, so a row CAN exist for an account that was not live on the day; the
// repair applies the same predicate Upsert does so the rebuilt total cannot include institutions
// the user had already left.
func TestPortfolioRepair_ExcludesAccountsNotLiveOnTheDay(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, liveAccount, secA := seedInvestmentFixture(ctx, t, tx, "repair-liveness")
	backdateLiveness(ctx, t, tx, liveAccount, 30)
	deadAccount := seedAccount(ctx, t, tx, userID, linkedItemID, "repair-liveness-dead")

	day := dayUTC(ctx, t, tx, 11)
	// The dead account's 'active' interval closed 20 days ago — well before the day under repair.
	_, err = tx.Exec(ctx, `
		UPDATE account_status_history ash
		SET valid_from = (((now() AT TIME ZONE 'UTC')::date)::timestamp AT TIME ZONE 'UTC') - INTERVAL '30 days',
		    valid_to   = (((now() AT TIME ZONE 'UTC')::date)::timestamp AT TIME ZONE 'UTC') - INTERVAL '20 days'
		WHERE ash.account_id = $1 AND ash.valid_to IS NULL`, deadAccount)
	require.NoError(t, err)

	seedHoldingSnapshot(ctx, t, tx, userID, liveAccount, secA, day, true, f64(1000), f64(1500))
	seedHoldingSnapshot(ctx, t, tx, userID, deadAccount, secA, day, true, f64(9000), f64(9500))
	seedPortfolioSnapshot(ctx, t, tx, userID, day, 0, 0, 0, nil)

	repo := NewPortfolioSnapshotRepository(tx)
	_, err = repo.RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)

	after := readPortfolioSnapshot(ctx, t, tx, userID, day)
	assert.InDelta(t, 1500.0, after.totalValue, 0.001,
		"a row for an account that was not live on the day must not enter the rebuilt total")
}

// ── Test 9: the enumeration the daily job drives the repair from ──────────────────────────────
//
// It must find a user by their portfolio_snapshots rows, NOT by their current holdings: a user who
// disconnected every institution has no holdings left and is skipped by
// GetDistinctUserIDsWithHoldings — and their recorded curve is exactly the one most likely to be
// wrong.
func TestPortfolioRepair_EnumeratesUsersWithNoCurrentHoldings(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, _ := seedInvestmentFixture(ctx, t, tx, "repair-enum")
	backdateLiveness(ctx, t, tx, accountID, 30)
	day := dayUTC(ctx, t, tx, 12)
	seedPortfolioSnapshot(ctx, t, tx, userID, day, 500, 400, 100, f64(25))

	repo := NewPortfolioSnapshotRepository(tx)
	ids, err := repo.UserIDsWithUnexaminedSnapshots(ctx, 1000)
	require.NoError(t, err)
	assert.Contains(t, ids, userID.String(),
		"a user with zero current holdings still has a recorded curve to examine")

	_, err = repo.RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)

	ids, err = repo.UserIDsWithUnexaminedSnapshots(ctx, 1000)
	require.NoError(t, err)
	assert.NotContains(t, ids, userID.String(), "an examined user must drop out of the enumeration")
}

// ── Test 10: the ledger verdict and the stored row can never disagree ─────────────────────────
//
// Written because a mutation SURVIVED test 4: deleting the "skip days already in the ledger" gate
// left TestPortfolioRepair_SecondRunChangesNothing green, because the values a re-examination
// recomputes are identical and the UPDATE is guarded by IS DISTINCT FROM. So idempotence alone
// does not pin that gate — this does.
//
// What the gate really protects is the AGREEMENT between the ledger and the row. Without it, a day
// once refused that later acquires coverage would be silently repaired while its ledger row still
// says refused_no_coverage: the INSERT hits ON CONFLICT DO NOTHING and the verdict is never
// updated. An operator reading the trail would then be told a day was left alone that had in fact
// been rewritten — which is worse than either outcome on its own, because it makes the audit trail
// lie about a write to financial history.
//
// A verdict is final. Re-examining a day means deliberately deleting its ledger row.
func TestPortfolioRepair_RefusedDayThatLaterGainsCoverage_KeepsLedgerAndRowInAgreement(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secA := seedInvestmentFixture(ctx, t, tx, "repair-stale")
	backdateLiveness(ctx, t, tx, accountID, 30)
	seedCurrentHolding(ctx, t, tx, userID, accountID, secA, 100, 200)

	day := dayUTC(ctx, t, tx, 13)
	seedPortfolioSnapshot(ctx, t, tx, userID, day, 4242, 4000, 242, f64(6.05))

	repo := NewPortfolioSnapshotRepository(tx)
	_, err = repo.RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	l, ok := readLedger(ctx, t, tx, userID, day)
	require.True(t, ok)
	require.Equal(t, domain.PortfolioRepairOutcomeRefusedNoCoverage, l.outcome)

	// The day now acquires coverage — the only way a past day can, since captures only ever write
	// today, is somebody backfilling it.
	seedHoldingSnapshot(ctx, t, tx, userID, accountID, secA, day, true, f64(10), f64(11))

	sum, err := repo.RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	assert.Equal(t, 0, sum.Examined, "a day already carrying a verdict must not be re-examined")

	after := readPortfolioSnapshot(ctx, t, tx, userID, day)
	l2, ok := readLedger(ctx, t, tx, userID, day)
	require.True(t, ok)
	assert.Equal(t, domain.PortfolioRepairOutcomeRefusedNoCoverage, l2.outcome)
	assert.Nil(t, l2.repairedTotalValue)
	assert.InDelta(t, 4242.0, after.totalValue, 0.001,
		"the ledger says this day was refused; the row must still be the refused one, not a silent rewrite behind a stale verdict")
}
