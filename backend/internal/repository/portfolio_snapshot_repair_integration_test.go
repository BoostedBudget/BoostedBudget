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
	"sort"
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

// backdateActiveIntervalStart moves the START of every 'active' interval on the account back
// `back` days, whether or not that interval is still open. backdateLiveness above only reaches the
// OPEN one, which a re-linked predecessor no longer has.
func backdateActiveIntervalStart(ctx context.Context, t *testing.T, db DB, accountID uuid.UUID, back int) {
	t.Helper()
	ct, err := db.Exec(ctx, `
		UPDATE account_status_history ash
		SET valid_from = (((now() AT TIME ZONE 'UTC')::date)::timestamp AT TIME ZONE 'UTC')
		                 - make_interval(days => $2::int)
		WHERE ash.account_id = $1 AND ash.status = 'active'`, accountID, back)
	require.NoError(t, err)
	require.EqualValues(t, 1, ct.RowsAffected(), "exactly one 'active' interval must exist to rewrite")
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
	beforeCTID := readPortfolioSnapshotCTID(ctx, t, tx, userID, day)

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
	// #1569 T8 (r17): ctid, not updated_at. The updated_at form of this assertion was VACUOUS and
	// was proven so rather than assumed: with the production UPDATE mutated to rewrite every
	// candidate day (verdict filter and IS DISTINCT FROM guard removed), the row's ctid moved
	// (0,30) -> (0,31) and the updated_at assertion still passed. portfolio_snapshots has a
	// BEFORE UPDATE trigger setting updated_at = NOW(), NOW() is transaction_timestamp(), and this
	// whole test is one transaction — so updated_at is frozen and cannot witness a rewrite here.
	// ⚠ ctid depends on Postgres moving the tuple pointer on UPDATE (heap MVCC writes a new tuple
	// version even when no column value changes); empirically confirmed above, but it is a
	// storage-engine dependency. Same fix and same limit as readPortfolioSnapshotCTID's own header.
	assert.Equal(t, beforeCTID, readPortfolioSnapshotCTID(ctx, t, tx, userID, day),
		"a refusal must not even rewrite the row — an untouched row must read as untouched")

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
	afterFirstCTID := map[string]string{}
	for _, d := range []time.Time{full, none, partial} {
		afterFirst[d.Format("2006-01-02")] = readPortfolioSnapshot(ctx, t, tx, userID, d)
		afterFirstCTID[d.Format("2006-01-02")] = readPortfolioSnapshotCTID(ctx, t, tx, userID, d)
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
		// #1569 T8 (r17): ctid, for the same reason and with the same proof as test 3 above — under
		// the mutated UPDATE all three rows moved ((0,35)->(0,38), (0,36)->(0,39), (0,37)->(0,40))
		// and the updated_at form stayed green. Storage-engine dependency noted there applies here.
		assert.Equal(t, afterFirstCTID[d.Format("2006-01-02")], readPortfolioSnapshotCTID(ctx, t, tx, userID, d),
			"a second run must not rewrite the row on %s", d.Format("2006-01-02"))
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

// ── Fix round 1 ───────────────────────────────────────────────────────────────────────────────

// F1: no user can be starved by the per-run cap.
//
// The repair pass is NOT one-shot: every day each user's newly-past portfolio_snapshots row becomes
// an unexamined day, so every user re-enters the enumeration daily and the cap is a standing queue.
// Under the original ORDER BY user_id, above `limit` users the same lowest-sorted uuids refilled the
// cap on every run and everyone after them was never examined — while the job logged a full,
// successful pass each time.
//
// This simulates the steady state directly: examine whoever the enumeration returns, then hand every
// already-examined user a new past day, and repeat. The assertion is about the user whose uuid sorts
// LAST — the one a uuid ordering can never reach.
func TestPortfolioRepair_EnumerationCannotStarveAUserBeyondTheCap(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Neutralise anything already committed in the shared database: this test reasons about the
	// GLOBAL enumeration, so any other user owing a verdict would occupy cap slots forever (nothing
	// here examines them) and starve this fixture regardless of the ordering under test.
	_, err = tx.Exec(ctx, `
		INSERT INTO portfolio_snapshot_repairs
			(user_id, snapshot_date, outcome, reason, expected_account_count, observed_account_count,
			 previous_total_value, previous_total_cost_basis, previous_total_gain_loss)
		SELECT ps.user_id, ps.snapshot_date, 'verified', 'pre-existing row neutralised by the F1 fixture',
		       0, 0, 0, 0, 0
		FROM portfolio_snapshots ps
		WHERE ps.snapshot_date < (now() AT TIME ZONE 'UTC')::date
		ON CONFLICT (user_id, snapshot_date) DO NOTHING`)
	require.NoError(t, err)

	const userCount = 210 // > the 200-per-run cap the daily job uses
	const cap = 200

	// One cohort of users, all owing the SAME oldest day, so uuid is the only tiebreak on run 1 —
	// the exact shape that pinned the head of the queue before.
	var mine []string
	rows, err := tx.Query(ctx, `
		WITH new_users AS (
			INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
			SELECT gen_random_uuid(), 'test|f1-' || g::text || '-' || gen_random_uuid()::text,
			       'f1-' || g::text || '-' || gen_random_uuid()::text || '@example.com',
			       'premium', 'active', 'iad'
			FROM generate_series(1, $1::int) g
			RETURNING id
		), seeded AS (
			INSERT INTO portfolio_snapshots (user_id, snapshot_date, total_value)
			SELECT id, (now() AT TIME ZONE 'UTC')::date - 10, 1 FROM new_users
			RETURNING user_id
		)
		SELECT user_id::text FROM seeded`, userCount)
	require.NoError(t, err)
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		mine = append(mine, id)
	}
	rows.Close()
	require.NoError(t, rows.Err())
	require.Len(t, mine, userCount)

	sort.Strings(mine)
	lastSorted := mine[len(mine)-1]

	repo := NewPortfolioSnapshotRepository(tx)
	examined := map[string]bool{}

	// ceil(210/200) = 2 runs is the bound the ordering promises; give it a little slack so a
	// failure reads as "starved", not "needed one more run".
	for run := 1; run <= 4 && !examined[lastSorted]; run++ {
		ids, err := repo.UserIDsWithUnexaminedSnapshots(ctx, cap)
		require.NoError(t, err)
		require.NotEmpty(t, ids, "run %d found nobody owing a verdict, which cannot be true here", run)
		require.LessOrEqual(t, len(ids), cap)

		for _, id := range ids {
			_, err := repo.RepairUserFromHistory(ctx, id)
			require.NoError(t, err)
			examined[id] = true
		}

		// The next day arrives: every examined user gains one new past day. This is what makes the
		// queue standing rather than draining, and it is what the uuid ordering could not survive.
		_, err = tx.Exec(ctx, `
			INSERT INTO portfolio_snapshots (user_id, snapshot_date, total_value)
			SELECT u, (now() AT TIME ZONE 'UTC')::date - $2::int, 1
			FROM unnest($1::uuid[]) u
			ON CONFLICT (user_id, snapshot_date) DO NOTHING`, ids, 9-run)
		require.NoError(t, err)
	}

	assert.True(t, examined[lastSorted],
		"the highest-sorted user must be reached within ceil(N/cap) runs; under ORDER BY user_id the "+
			"lowest 200 uuids refill the cap every run and this user is never examined at all")
}

// F2: the two refusals are different facts, and the ledger must not report one as the other.
//
// A day whose only rows belong to an account that was NOT live on it yields expected = observed = 0,
// which is the same arithmetic as a day that was never captured — but "no rows for this day" is
// FALSE about that data. The ledger's entire justification is that an operator can learn why.
func TestPortfolioRepair_RowsExcludedByLiveness_AreNotReportedAsNoRowsAtAll(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, deadAccount, secA := seedInvestmentFixture(ctx, t, tx, "repair-f2")
	day := dayUTC(ctx, t, tx, 14)
	_, err = tx.Exec(ctx, `
		UPDATE account_status_history ash
		SET valid_from = (((now() AT TIME ZONE 'UTC')::date)::timestamp AT TIME ZONE 'UTC') - INTERVAL '40 days',
		    valid_to   = (((now() AT TIME ZONE 'UTC')::date)::timestamp AT TIME ZONE 'UTC') - INTERVAL '30 days'
		WHERE ash.account_id = $1 AND ash.valid_to IS NULL`, deadAccount)
	require.NoError(t, err)

	seedHoldingSnapshot(ctx, t, tx, userID, deadAccount, secA, day, true, f64(1000), f64(1200))
	seedPortfolioSnapshot(ctx, t, tx, userID, day, 1200, 1000, 200, f64(20))

	repo := NewPortfolioSnapshotRepository(tx)
	sum, err := repo.RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	assert.Equal(t, 1, sum.RefusedNoCoverage, "the refusal itself is right — nothing may be counted")

	after := readPortfolioSnapshot(ctx, t, tx, userID, day)
	assert.InDelta(t, 1200.0, after.totalValue, 0.001, "and the day is still left alone")

	l, ok := readLedger(ctx, t, tx, userID, day)
	require.True(t, ok)
	assert.Contains(t, l.reason, "none belongs to an account recorded live on it",
		"rows DO exist for this day; saying they do not tells an operator something untrue about their own data")
	assert.NotContains(t, l.reason, "no holding_snapshots rows at all")
}

// F3: Examined must mean days examined.
//
// It used to be counted from the ledger INSERT's RETURNING, which drops any row ON CONFLICT DO
// NOTHING swallows. Counted from the decided set instead.
//
// ⚠ Read the honest limit of this test before trusting it. With the candidate gate in place the two
// countings CANNOT diverge: a day with a ledger row is not a candidate, so no INSERT ever conflicts,
// and both counts equal the candidate count. A first draft of this test planted a ledger row and
// asserted 0, which is true under either counting and pinned nothing — the mutation survived it.
//
// The divergence is only reachable when the gate is gone, which is exactly mutation P5, and that is
// where the fix is really pinned: with the gate removed, SecondRunChangesNothing now reports
// examined = 3 and FAILS, where under the old counting it reported 0 and passed. Measured both ways.
// What remains here is the contract itself — the number means candidate days — asserted across a
// fixture with more than one of them so an off-by-one or a per-outcome mix-up would show.
func TestPortfolioRepair_ExaminedCountsEveryCandidateDay(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secA := seedInvestmentFixture(ctx, t, tx, "repair-f3")
	backdateLiveness(ctx, t, tx, accountID, 30)

	// Four candidate days: one repaired, one verified, one refused for no coverage, and one already
	// carrying a verdict — which is NOT a candidate and must not be counted.
	repaired := dayUTC(ctx, t, tx, 15)
	verified := dayUTC(ctx, t, tx, 16)
	refused := dayUTC(ctx, t, tx, 17)
	alreadyJudged := dayUTC(ctx, t, tx, 18)

	seedHoldingSnapshot(ctx, t, tx, userID, accountID, secA, repaired, true, f64(100), f64(150))
	seedPortfolioSnapshot(ctx, t, tx, userID, repaired, 1, 1, 0, nil)

	seedHoldingSnapshot(ctx, t, tx, userID, accountID, secA, verified, true, f64(100), f64(150))
	seedPortfolioSnapshot(ctx, t, tx, userID, verified, 150, 100, 50, f64(50))

	seedPortfolioSnapshot(ctx, t, tx, userID, refused, 999, 999, 0, nil)

	seedPortfolioSnapshot(ctx, t, tx, userID, alreadyJudged, 5, 5, 0, nil)
	_, err = tx.Exec(ctx, `
		INSERT INTO portfolio_snapshot_repairs
			(user_id, snapshot_date, outcome, reason, expected_account_count, observed_account_count,
			 previous_total_value, previous_total_cost_basis, previous_total_gain_loss)
		VALUES ($1, $2::date, 'verified', 'planted by the F3 fixture', 1, 1, 5, 5, 0)`, userID, alreadyJudged)
	require.NoError(t, err)

	repo := NewPortfolioSnapshotRepository(tx)
	sum, err := repo.RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)

	assert.Equal(t, 3, sum.Examined, "three candidate days; the day already carrying a verdict is not one")
	assert.Equal(t, 1, sum.Repaired)
	assert.Equal(t, 1, sum.Verified)
	assert.Equal(t, 1, sum.RefusedNoCoverage)
	assert.Equal(t, sum.Repaired+sum.Verified+sum.RefusedNoCoverage+sum.RefusedPartialCoverage,
		sum.Examined, "the per-outcome counts must account for every examined day, with none left over")

	// The verified day must not have been written at all.
	l, ok := readLedger(ctx, t, tx, userID, verified)
	require.True(t, ok)
	assert.Equal(t, domain.PortfolioRepairOutcomeVerified, l.outcome)
	require.NotNil(t, l.repairedTotalValue, "a verified day records the reconstruction it agreed with")
	assert.InDelta(t, 150.0, *l.repairedTotalValue, 0.001)
}

// ── #1569 task 9.1, item A (repair half): a past re-link day must reconstruct ONCE ────────────
//
// The capture applies accountLiveOnDateClause, so on a re-link day it writes holding_snapshots for
// BOTH chain members — that is correct at the capture grain (each physical account really was
// observed) and wrong the moment the two are summed into one portfolio total. Upsert and the
// repair must agree, so the succession exclusion lives in both.
//
// The predecessor's row is deliberately the LARGER of the two, so a rebuild that keeps the wrong
// sibling cannot pass by arithmetic accident either.
func TestPortfolioRepair_SameDayRelink_ReconstructsTheChainOnce(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, oldAccountID, newAccountID, secID := seedRelinkedInvestmentChain(ctx, t, tx, "repair-relink")
	// The predecessor's 'active' interval is already CLOSED (the re-link closed it), so the
	// valid_to IS NULL form cannot reach it — only its start needs moving back.
	backdateActiveIntervalStart(ctx, t, tx, oldAccountID, 30)
	backdateActiveIntervalStart(ctx, t, tx, newAccountID, 30)

	day := dayUTC(ctx, t, tx, 3)
	seedHoldingSnapshot(ctx, t, tx, userID, oldAccountID, secID, day, true, f64(3000), f64(7000))
	seedHoldingSnapshot(ctx, t, tx, userID, newAccountID, secID, day, true, f64(3000), f64(5000))

	seedPortfolioSnapshot(ctx, t, tx, userID, day, 1, 1, 0, nil)

	sum, err := NewPortfolioSnapshotRepository(tx).RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	require.Equal(t, 1, sum.Examined)
	require.Equal(t, 1, sum.Repaired, "the day is fully covered once the chain counts as one account")

	got := readPortfolioSnapshot(ctx, t, tx, userID, day)
	assert.InDelta(t, 5000.0, got.totalValue, 0.001,
		"12000 is the double-count (7000 retained + 5000 successor); 7000 means the exclusion ran "+
			"backwards and dropped the successor instead of the superseded predecessor")
	assert.InDelta(t, 3000.0, got.totalCostBasis, 0.001)

	l, ok := readLedger(ctx, t, tx, userID, day)
	require.True(t, ok)
	assert.Equal(t, 1, l.expectedCount,
		"a re-link chain is ONE logical account for coverage too — counting it as two makes every "+
			"re-link day look partially covered the moment one sibling stops being captured")
	assert.Equal(t, 1, l.observedCount)
}

// TestPortfolioRepair_ConfirmedEmptySuccessor_RebuildsToZeroNotTheStalePredecessor is the repair
// half of #1569 T9-FIX item 2.
//
// The successor was observed on the day and held nothing, so it left no holding_snapshots row at
// all — an account with no prior history has nothing for the mark-absent half to mark. Before the
// fix that made it look UNOBSERVED twice over: the re-link rule kept the retained predecessor
// beside it, and the coverage test then rebuilt the day from the predecessor's rows. The repair
// wrote the predecessor's STALE valuation into a day whose true total was zero — a plausible
// number, which is the failure mode this whole file exists to refuse.
func TestPortfolioRepair_ConfirmedEmptySuccessor_RebuildsToZeroNotTheStalePredecessor(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, oldAccountID, newAccountID, secID := seedRelinkedInvestmentChain(ctx, t, tx, "repair-emptysucc")
	backdateActiveIntervalStart(ctx, t, tx, oldAccountID, 30)
	backdateActiveIntervalStart(ctx, t, tx, newAccountID, 30)

	day := dayUTC(ctx, t, tx, 3)
	seedHoldingSnapshot(ctx, t, tx, userID, oldAccountID, secID, day, true, f64(3000), f64(7000))

	// The successor: observed, and holding nothing. No positions to record, so the ONLY trace is
	// the observation itself.
	_, err = tx.Exec(ctx, `
		INSERT INTO holding_observations (user_id, account_id, as_of_date)
		VALUES ($1, $2, $3::date)`, userID, newAccountID, day)
	require.NoError(t, err)

	seedPortfolioSnapshot(ctx, t, tx, userID, day, 7000, 3000, 4000, nil)

	sum, err := NewPortfolioSnapshotRepository(tx).RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	require.Equal(t, 1, sum.Examined)
	require.Equal(t, 1, sum.Repaired,
		"a refusal here means the confirmed-empty successor still reads as never-captured, so the "+
			"day stays at the predecessor's stale valuation")

	got := readPortfolioSnapshot(ctx, t, tx, userID, day)
	assert.InDelta(t, 0.0, got.totalValue, 0.001,
		"7000 means the retained predecessor was summed into a day on which the successor that "+
			"replaced it had already been observed holding nothing")
	assert.InDelta(t, 0.0, got.totalCostBasis, 0.001)
}

// ── #1613 P4-T3 item I, Codex finding 4: the repair must not leave the GATED total stale ──────
//
// RepairUserFromHistory rewrites a past day's total_value from holding_snapshots. Once
// portfolio_snapshots also carries total_value_investment_only (migration 000189), a repair that
// corrects the ungated column and leaves the gated one untouched produces a row whose two totals
// describe DIFFERENT days — and GetPortfolioSummary, which only checks the gated column for NULL,
// would happily subtract that stale number and call it DayChange.
//
// ⚠ This corrects the first draft of this work, which deliberately left the repair alone on the
// reasoning that "nothing can reconstruct an as-of account type". That reasoning was wrong in the
// direction that matters: the repair does not NEED an as-of type, because the figure it must be
// comparable with — the LIVE read in GetPortfolioSummary — is itself gated on the account's
// CURRENT type. Reconstructing from current type is not an approximation here; it is the same
// predicate the other operand uses.
func TestPortfolioRepair_RepairsTheTypeGatedTotalToo_NeverLeavingItStale(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, invAccountID, secA := seedInvestmentFixture(ctx, t, tx, "repair-gated")
	backdateLiveness(ctx, t, tx, invAccountID, 30)

	// A depository account carrying holdings — the cash-management shape this whole work stream
	// exists for. It belongs in total_value and must NOT appear in total_value_investment_only.
	depAccountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Cash Management', 'depository', 'active')
	`, depAccountID, linkedItemID, userID, "repair-gated-dep-"+depAccountID.String())
	require.NoError(t, err)
	backdateLiveness(ctx, t, tx, depAccountID, 30)

	secB := seedSecurity(ctx, t, tx, "repair-gated-b")

	day := dayUTC(ctx, t, tx, 3)
	seedHoldingSnapshot(ctx, t, tx, userID, invAccountID, secA, day, true, f64(4000), f64(5000))
	seedHoldingSnapshot(ctx, t, tx, userID, depAccountID, secB, day, true, f64(3900), f64(3900))

	// A row that is wrong in the ungated column AND carries a stale gated value. 1234 is not
	// reconstructible from anything on this day, so seeing it survive is unambiguous.
	seedPortfolioSnapshot(ctx, t, tx, userID, day, 1500, 1500, 0, nil)
	_, err = tx.Exec(ctx, `
		UPDATE portfolio_snapshots SET total_value_investment_only = 1234
		WHERE user_id = $1 AND snapshot_date = $2::date`, userID, day)
	require.NoError(t, err)

	repo := NewPortfolioSnapshotRepository(tx)
	sum, err := repo.RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	require.Equal(t, 1, sum.Repaired, "the day disagrees, so it must be repaired")

	var totalValue float64
	var gated *float64
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT total_value, total_value_investment_only
		FROM portfolio_snapshots WHERE user_id = $1 AND snapshot_date = $2::date`,
		userID, day).Scan(&totalValue, &gated))

	require.InDelta(t, 8900.0, totalValue, 0.001,
		"total_value stays UNGATED — it is the historical series and both accounts were live")
	require.NotNil(t, gated, "the repair must write the gated total, not blank it")
	require.InDelta(t, 5000.0, *gated, 0.001,
		"1234 here means the repair rewrote total_value and left the gated column describing a "+
			"different day — GetPortfolioSummary would subtract that stale number and call it "+
			"DayChange. 8900 here means the type gate is not being applied in the repair.")
}

// TestPortfolioRepair_SecondRunChangesNothing_IncludingTheGatedTotal extends the byte-identical
// guarantee to the new column. If total_value_investment_only were written but left OUT of the
// IS DISTINCT FROM guard, an already-correct day would be rewritten on every pass and
// "the repair changed nothing the second time" would stop being observable.
func TestPortfolioRepair_SecondRunChangesNothing_IncludingTheGatedTotal(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secA := seedInvestmentFixture(ctx, t, tx, "repair-gated-stable")
	backdateLiveness(ctx, t, tx, accountID, 30)

	day := dayUTC(ctx, t, tx, 3)
	seedHoldingSnapshot(ctx, t, tx, userID, accountID, secA, day, true, f64(4000), f64(5000))
	seedPortfolioSnapshot(ctx, t, tx, userID, day, 1500, 1500, 0, nil)

	repo := NewPortfolioSnapshotRepository(tx)
	first, err := repo.RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	require.Equal(t, 1, first.Repaired)

	var gatedAfterFirst *float64
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT total_value_investment_only FROM portfolio_snapshots
		WHERE user_id = $1 AND snapshot_date = $2::date`, userID, day).Scan(&gatedAfterFirst))
	require.NotNil(t, gatedAfterFirst)
	require.InDelta(t, 5000.0, *gatedAfterFirst, 0.001)

	second, err := repo.RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	require.Equal(t, 0, second.Repaired,
		"an already-correct day must come out of a second repair untouched; a non-zero count here "+
			"means the new column is written but missing from the IS DISTINCT FROM guard")
}
