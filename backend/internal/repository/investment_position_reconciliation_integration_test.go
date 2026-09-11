//go:build integration

package repository

// #1631 M8b: trade-vs-position reconciliation. This is a DIAGNOSTIC — Plaid's holdings snapshot
// stays the position of record unconditionally; these tests only prove the drift/suppression
// arithmetic against real Postgres. See domain/investment_reconciliation.go for the full
// reasoning, in particular the 2-year-lookback suppression rule pinned by the third test below.
//
// ⚠ Integration tests on purpose: the report's whole value is a JOIN across holdings,
// investment_transactions and securities via domain.TradeTypeToSQLPredicate's generated SQL —
// exactly the class of bug (ambiguous columns, wrong join cardinality, placeholder
// off-by-one) pgxmock cannot catch.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedHolding inserts a minimal holdings row for the given account/security.
func seedReconHolding(ctx context.Context, t *testing.T, db DB, userID, accountID, secID uuid.UUID, quantity float64) {
	t.Helper()
	_, err := db.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity)
		VALUES ($1, $2, $3, $4, $5)
	`, uuid.New(), userID, accountID, secID, quantity)
	require.NoError(t, err)
}

// seedInvestmentTxn inserts a minimal investment_transactions row.
func seedReconTxn(ctx context.Context, t *testing.T, db DB, userID, accountID, secID uuid.UUID,
	txnType string, subtype *string, quantity float64, date string,
) {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, security_id, plaid_investment_transaction_id,
			 type, subtype, name, amount, quantity, date)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'Test Txn', 0, $8, $9::date)
	`, id, userID, accountID, secID, "plaid-recon-"+id.String(), txnType, subtype, quantity, date)
	require.NoError(t, err)
}

// TestPositionReconciliation_MatchingLedgerAndHoldingIsNotADrift_Integration is the clean-match
// case: buys and sells that exactly net to the holding's quantity produce neither a Drift nor a
// SuppressedPreWindow entry.
func TestPositionReconciliation_MatchingLedgerAndHoldingIsNotADrift_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "recon-match")

	seedReconTxn(ctx, t, tx, userID, accountID, secID, "buy", nil, 100, "2025-06-01")
	seedReconTxn(ctx, t, tx, userID, accountID, secID, "sell", nil, -20, "2025-07-01")
	seedReconHolding(ctx, t, tx, userID, accountID, secID, 80)

	repo := NewInvestmentAnalyticsRepository(tx)
	report, err := repo.PositionReconciliation(ctx, userID.String())
	require.NoError(t, err)

	assert.Empty(t, report.Drifts, "buy 100 minus sell 20 exactly matches the 80-share holding — no drift")
	assert.Equal(t, 0, report.SuppressedPreWindowCount)
	assert.Equal(t, 1, report.TotalPositionsChecked)
}

// TestPositionReconciliation_MismatchWithOpeningBuyIsReportedAsADrift_Integration is the RED test
// for the diagnostic's whole purpose: a ledger that HAS a buy but nets to the wrong quantity is
// exactly the case #1631 M8b exists to surface (a real drift, not a lookback artifact).
func TestPositionReconciliation_MismatchWithOpeningBuyIsReportedAsADrift_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "recon-drift")

	seedReconTxn(ctx, t, tx, userID, accountID, secID, "buy", nil, 100, "2025-06-01")
	// Holding says 120 shares — 20 more than the ledger implies.
	seedReconHolding(ctx, t, tx, userID, accountID, secID, 120)

	repo := NewInvestmentAnalyticsRepository(tx)
	report, err := repo.PositionReconciliation(ctx, userID.String())
	require.NoError(t, err)

	require.Len(t, report.Drifts, 1)
	d := report.Drifts[0]
	assert.Equal(t, accountID.String(), d.AccountID)
	assert.Equal(t, secID.String(), d.SecurityID)
	assert.InDelta(t, 120.0, d.HoldingQuantity, 0.001)
	assert.InDelta(t, 100.0, d.LedgerQuantity, 0.001)
	assert.InDelta(t, 20.0, d.Delta, 0.001)
	assert.Equal(t, 0, report.SuppressedPreWindowCount)
}

// TestPositionReconciliation_ReinvestmentSharesCountTowardTheLedger_Integration pins the brief's
// explicit callout: a reinvestment row (type='cash', subtype='dividend reinvestment') carries a
// POSITIVE quantity of newly-acquired shares and must be added into the ledger net, not skipped
// the way a naive `type IN ('buy','sell')` filter would skip it.
func TestPositionReconciliation_ReinvestmentSharesCountTowardTheLedger_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "recon-reinvest")

	reinvestSubtype := "dividend reinvestment"
	seedReconTxn(ctx, t, tx, userID, accountID, secID, "buy", nil, 100, "2025-01-01")
	seedReconTxn(ctx, t, tx, userID, accountID, secID, "cash", &reinvestSubtype, 2.5, "2025-06-01")
	seedReconHolding(ctx, t, tx, userID, accountID, secID, 102.5)

	repo := NewInvestmentAnalyticsRepository(tx)
	report, err := repo.PositionReconciliation(ctx, userID.String())
	require.NoError(t, err)

	assert.Empty(t, report.Drifts,
		"102.5 = 100 buy + 2.5 reinvested shares — a ledger that ignored the reinvestment would wrongly report a 2.5-share drift here")
	assert.Equal(t, 0, report.SuppressedPreWindowCount)
}

// TestPositionReconciliation_StructuralWithdrawalReinvestmentCountsTowardTheLedger_Integration is
// the PRODUCTION shape the plain "dividend reinvestment" test above does NOT exercise. #1631's own
// NormalizeTradeType doc comment records that this brokerage NEVER emits the "dividend
// reinvestment" subtype literal — it reports a reinvestment as a bare cash/withdrawal row against
// the security, caught only by the STRUCTURAL branch (a cash movement against a security Plaid did
// not flag is_cash_equivalent=true). The 42.93 reinvested shares this issue cites come from exactly
// this shape. seedInvestmentFixture's security omits is_cash_equivalent, which defaults to FALSE,
// so SecurityIsStructuralSQL's `IS FALSE` check is already satisfied without any extra seeding.
func TestPositionReconciliation_StructuralWithdrawalReinvestmentCountsTowardTheLedger_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "recon-structural-reinvest")

	withdrawal := "withdrawal"
	seedReconTxn(ctx, t, tx, userID, accountID, secID, "buy", nil, 100, "2025-01-01")
	// Structural reinvestment: type='cash', subtype='withdrawal' (NOT a literal reinvest subtype),
	// against a security this fixture leaves is_cash_equivalent=FALSE — the exact shape production
	// data has for this brokerage.
	seedReconTxn(ctx, t, tx, userID, accountID, secID, "cash", &withdrawal, 2.93, "2025-06-01")
	seedReconHolding(ctx, t, tx, userID, accountID, secID, 102.93)

	repo := NewInvestmentAnalyticsRepository(tx)
	report, err := repo.PositionReconciliation(ctx, userID.String())
	require.NoError(t, err)

	assert.Empty(t, report.Drifts,
		"102.93 = 100 buy + 2.93 structurally-reinvested shares — a ledger that only recognised the "+
			"literal 'dividend reinvestment' subtype would miss this brokerage's cash/withdrawal form "+
			"entirely and wrongly report a 2.93-share drift")
	assert.Equal(t, 0, report.SuppressedPreWindowCount)
}

// TestPositionReconciliation_StockSplitCountsTowardTheLedger_Integration is the C2 fix's own test:
// a stock split (type='transfer', subtype='split') modifies holdings.quantity with no
// corresponding buy/sell/reinvest row. Plaid's own docs call type='transfer' "activity that
// modifies a position, but not through buy/sell activity" — before this fix the ledger ignored
// ALL type='transfer' rows, so every split on a held position produced a permanent, un-suppressed
// drift (the position has a buy, so the pre-window proxy would not catch it either).
func TestPositionReconciliation_StockSplitCountsTowardTheLedger_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "recon-split")

	split := "split"
	seedReconTxn(ctx, t, tx, userID, accountID, secID, "buy", nil, 100, "2025-01-01")
	// A 2-for-1 split: Plaid represents the 100 NEW shares as a transfer/split row.
	seedReconTxn(ctx, t, tx, userID, accountID, secID, "transfer", &split, 100, "2025-06-01")
	seedReconHolding(ctx, t, tx, userID, accountID, secID, 200)

	repo := NewInvestmentAnalyticsRepository(tx)
	report, err := repo.PositionReconciliation(ctx, userID.String())
	require.NoError(t, err)

	assert.Empty(t, report.Drifts,
		"200 = 100 original + 100 split-issued shares — a ledger that ignored type='transfer' rows "+
			"would report a permanent 100-share drift after every split on a held position")
	assert.Equal(t, 0, report.SuppressedPreWindowCount)
}

// TestPositionReconciliation_InKindTransferCountsTowardTheLedger_Integration covers the OTHER
// type='transfer' shape: an in-kind/ACATS transfer of shares between accounts, which Plaid also
// types 'transfer' but WITHOUT the 'split' subtype. Both must count toward the ledger net.
func TestPositionReconciliation_InKindTransferCountsTowardTheLedger_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "recon-acats")

	seedReconTxn(ctx, t, tx, userID, accountID, secID, "buy", nil, 50, "2025-01-01")
	// An in-kind transfer INTO this account from another brokerage — no subtype, not a split.
	seedReconTxn(ctx, t, tx, userID, accountID, secID, "transfer", nil, 25, "2025-06-01")
	seedReconHolding(ctx, t, tx, userID, accountID, secID, 75)

	repo := NewInvestmentAnalyticsRepository(tx)
	report, err := repo.PositionReconciliation(ctx, userID.String())
	require.NoError(t, err)

	assert.Empty(t, report.Drifts,
		"75 = 50 bought + 25 shares transferred in-kind — a ledger that ignored type='transfer' rows "+
			"would report a permanent 25-share drift")
	assert.Equal(t, 0, report.SuppressedPreWindowCount)
}

// TestPositionReconciliation_NoOpeningBuyIsSuppressedNotReported_Integration is the pre-window
// suppression rule's own test: a position with ONLY a sell in the ledger (no buy at all) could
// not have been opened inside Plaid's 2-year lookback, so a quantity mismatch here must land in
// SuppressedPreWindowCount, never in Drifts.
func TestPositionReconciliation_NoOpeningBuyIsSuppressedNotReported_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "recon-prewindow")

	// A partial sale of a position opened before the visible window — no buy anywhere in the ledger.
	seedReconTxn(ctx, t, tx, userID, accountID, secID, "sell", nil, -10, "2025-06-01")
	seedReconHolding(ctx, t, tx, userID, accountID, secID, 90)

	repo := NewInvestmentAnalyticsRepository(tx)
	report, err := repo.PositionReconciliation(ctx, userID.String())
	require.NoError(t, err)

	assert.Empty(t, report.Drifts,
		"no buy anywhere in the ledger means the opening trade predates the 2-year lookback — must not be reported as a drift")
	assert.Equal(t, 1, report.SuppressedPreWindowCount)
	assert.Equal(t, 1, report.TotalPositionsChecked)
}

// TestPositionReconciliation_HoldingWithNoLedgerRowsAtAllIsSuppressed_Integration covers the
// simplest pre-window shape: a holding with NO investment_transactions rows whatsoever (the
// account was linked and its whole trade history predates the lookback).
func TestPositionReconciliation_HoldingWithNoLedgerRowsAtAllIsSuppressed_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "recon-nohist")

	seedReconHolding(ctx, t, tx, userID, accountID, secID, 50)

	repo := NewInvestmentAnalyticsRepository(tx)
	report, err := repo.PositionReconciliation(ctx, userID.String())
	require.NoError(t, err)

	assert.Empty(t, report.Drifts)
	assert.Equal(t, 1, report.SuppressedPreWindowCount)
	assert.Equal(t, 1, report.TotalPositionsChecked)
}

// TestPositionReconciliationSummary_AgreesWithPerUserReportAcrossUsers_Integration is the
// regression guard #1631 M8b N2 requires: PositionReconciliationSummary pushes its
// classification into SQL (COUNT(*) FILTER) instead of calling
// domain.ClassifyPositionReconciliationMember per row like PositionReconciliation does, so the
// two paths could in principle drift out of step. This test proves, empirically, that they do
// not — for the SAME seeded positions across TWO different users, the global summary's DELTA
// (after seeding minus before seeding, so pre-existing rows in a shared test database cannot
// produce a false pass or a flaky failure) exactly equals the sum of what the per-user,
// Go-classified report finds for each user individually.
func TestPositionReconciliationSummary_AgreesWithPerUserReportAcrossUsers_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	repo := NewInvestmentAnalyticsRepository(tx)

	before, err := repo.PositionReconciliationSummary(ctx)
	require.NoError(t, err)

	// User A: a real drift (has a buy, mismatched quantity) — SQL must count this as DriftCount.
	userA, _, accountA, secA := seedInvestmentFixture(ctx, t, tx, "recon-agree-a")
	seedReconTxn(ctx, t, tx, userA, accountA, secA, "buy", nil, 100, "2025-01-01")
	seedReconHolding(ctx, t, tx, userA, accountA, secA, 150)

	// User B: a suppressed pre-window position (no buy in the ledger) — SQL must count this as
	// SuppressedPreWindowCount, not DriftCount.
	userB, _, accountB, secB := seedInvestmentFixture(ctx, t, tx, "recon-agree-b")
	seedReconTxn(ctx, t, tx, userB, accountB, secB, "sell", nil, -5, "2025-01-01")
	seedReconHolding(ctx, t, tx, userB, accountB, secB, 45)

	after, err := repo.PositionReconciliationSummary(ctx)
	require.NoError(t, err)

	reportA, err := repo.PositionReconciliation(ctx, userA.String())
	require.NoError(t, err)
	reportB, err := repo.PositionReconciliation(ctx, userB.String())
	require.NoError(t, err)

	wantDriftDelta := int64(len(reportA.Drifts) + len(reportB.Drifts))
	wantSuppressedDelta := int64(reportA.SuppressedPreWindowCount + reportB.SuppressedPreWindowCount)
	wantCheckedDelta := int64(reportA.TotalPositionsChecked + reportB.TotalPositionsChecked)

	assert.Equal(t, wantDriftDelta, after.DriftCount-before.DriftCount,
		"the SQL-classified global summary must agree with the sum of the Go-classified per-user reports for drift_count")
	assert.Equal(t, wantSuppressedDelta, after.SuppressedPreWindowCount-before.SuppressedPreWindowCount,
		"...and for suppressed_pre_window_count")
	assert.Equal(t, wantCheckedDelta, after.TotalPositionsChecked-before.TotalPositionsChecked,
		"...and for total_positions_checked")

	// Concretely pin the two seeded shapes so a future change that breaks BOTH paths identically
	// (and would therefore still pass the delta-agreement assertions above) cannot slip through.
	assert.Equal(t, int64(1), wantDriftDelta, "user A's mismatch must be a real, reported drift")
	assert.Equal(t, int64(1), wantSuppressedDelta, "user B's no-buy mismatch must be suppressed, not reported")
	assert.True(t, before.Available)
	assert.True(t, after.Available)
}
