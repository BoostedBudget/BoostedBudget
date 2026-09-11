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

// reconTxnAmount derives the cash effect a row with this share movement would really carry.
//
// ⚠ THE AMOUNT IS NOT DECORATION ON A STRUCTURAL ROW (#1631 CX3). This helper used to hard-code
// `amount 0` for every fixture, and a zero amount agrees with no direction at all -- so the
// structural reinvestment test below seeded a row the structural branch declines, and passed only
// because the pre-CX3 rule asked for nothing beyond the security flag. It was passing for the
// wrong reason, and went red the moment the rule started demanding the sign.
//
// Plaid signs an investment amount by the CASH movement, inverted relative to the Transactions
// product: positive when cash is DEBITED (shares acquired) and negative when cash is CREDITED
// (shares released). So the amount carries the same sign as the share movement, and deriving it
// here makes every fixture in this file realistic rather than leaving 17 hand-chosen numbers to
// each be wrong. Only the SIGN is ever read -- by isStructuralDividend/isStructuralReinvest and
// their SQL mirrors -- so the magnitude is one nominal unit per share.
//
// type=transfer is the deliberate exception: a corporate action such as a split moves SHARES and
// no cash, so a derived amount there would assert a cash movement that never happened. The
// transfer bucket reads no amount, so zero is both harmless and true.
func reconTxnAmount(txnType string, quantity float64) float64 {
	if txnType == "transfer" {
		return 0
	}
	return quantity
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
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'Test Txn', $8, $9, $10::date)
	`, id, userID, accountID, secID, "plaid-recon-"+id.String(), txnType, subtype,
		reconTxnAmount(txnType, quantity), quantity, date)
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
	//
	// The POSITIVE quantity is load-bearing twice over (#1631 CX3): it is the reinvested shares the
	// ledger has to count, and it is also what makes seedReconTxn derive a positive (cash-DEBITED)
	// amount, which is the second fact the structural rule requires. A share-acquiring row with a
	// zero or credited amount is not a purchase, and the rule declines it.
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
// not — for the SAME seeded positions across FOUR different users, the global summary's DELTA
// (after seeding minus before seeding, so pre-existing rows in a shared test database cannot
// produce a false pass or a flaky failure) exactly equals the sum of what the per-user,
// Go-classified report finds for each user individually.
//
// ⚠ #1631 M8b round-3 fix: the original two-user version of this test only exercised the
// has_buy split (drift vs. suppressed), both at a delta comfortably clear of the tolerance
// band. Users C and D below close the "which side of tolerance" gap that left: one sitting
// comfortably INSIDE the band (0.0005) and one comfortably OUTSIDE it (0.002). Together with
// the has_buy pair, this pins drift / suppressed / neither, plus the has_buy split within
// "beyond tolerance".
//
// ⚠ #1631 M8b round-4/5: C and D do NOT pin the `>` vs `>=` STRICTNESS at the boundary itself —
// 0.0005 and 0.002 are classified identically whether the comparison is strict or not. Round 4
// tried to close that here with a fifth user at a delta of EXACTLY
// domain.PositionReconciliationQuantityTolerance (holding 100.001, buy 100) and asserted
// agreement on both paths. That assertion was WRONG, and deliberately removed rather than
// retuned — see the next paragraph for why no value at the boundary can work in this test.
//
// ⚠ THE SQL AND GO PATHS PROVABLY DISAGREE AT A DELTA OF EXACTLY THE TOLERANCE, AND THIS
// TEST CANNOT PIN THAT BOUNDARY FOR EITHER REASON:
//   - holdings.quantity and investment_transactions.quantity are DECIMAL(18,8) (migration
//     000051), so Postgres computes `100.00100000 - 100.00000000 = 0.00100000` EXACTLY, and
//     `0.001 > 0.001` is false — the SQL path says "not a drift".
//   - h.quantity and ledger_quantity are scanned into float64 before
//     PositionReconciliation subtracts them, and float64 cannot represent 100.001 exactly:
//     `100.001 - 100` comes back as 0.0010000000000047748, which IS strictly greater than the
//     float64 tolerance (0.001, itself not exact) — the Go path says "a drift".
//     The two paths take opposite branches at this exact input, by construction, because Postgres
//     NUMERIC and Go float64 do not agree bit-for-bit within roughly 1e-15 of the tolerance value.
//     This is a genuine, documented divergence in PRODUCTION CODE, not a gap in test coverage —
//     see domain.PositionReconciliationQuantityTolerance's doc comment for the same note at the
//     source. No seeded value can close it: a value away from the boundary (like C/D above) does
//     not test strictness at all, and a value AT the boundary hits precisely the band where the
//     two numeric systems cannot agree.
//
// Go's OWN `>` strictness at the exact boundary — a question the two-system disagreement above
// does not touch — IS pinned, cleanly and reliably, by
// domain.TestClassifyPositionReconciliationMember_ExactlyAtToleranceIsNotADrift, which passes
// the tolerance constant directly as delta with no subtraction in between, so there is no
// float64-vs-NUMERIC question to raise. That is the only place in the tree this boundary is
// tested, and it is enough: this file's job is to prove SQL/Go AGREEMENT on the classification
// outcome across many rows, not to re-litigate what "strict `>`" means in isolation.
func TestPositionReconciliationSummary_AgreesWithPerUserReportAcrossUsers_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	repo := NewInvestmentAnalyticsRepository(tx)

	before, err := repo.PositionReconciliationSummary(ctx)
	require.NoError(t, err)

	// User A: a real drift (has a buy, mismatched quantity, comfortably beyond tolerance) — SQL
	// must count this as DriftCount.
	userA, _, accountA, secA := seedInvestmentFixture(ctx, t, tx, "recon-agree-a")
	seedReconTxn(ctx, t, tx, userA, accountA, secA, "buy", nil, 100, "2025-01-01")
	seedReconHolding(ctx, t, tx, userA, accountA, secA, 150)

	// User B: a suppressed pre-window position (no buy in the ledger, comfortably beyond
	// tolerance) — SQL must count this as SuppressedPreWindowCount, not DriftCount.
	userB, _, accountB, secB := seedInvestmentFixture(ctx, t, tx, "recon-agree-b")
	seedReconTxn(ctx, t, tx, userB, accountB, secB, "sell", nil, -5, "2025-01-01")
	seedReconHolding(ctx, t, tx, userB, accountB, secB, 45)

	// User C: delta 0.0005 — strictly INSIDE
	// domain.PositionReconciliationQuantityTolerance (0.001). Must be counted in
	// total_positions_checked but land in NEITHER drift nor suppressed, on both paths. A SQL
	// regression to `ABS(...) > 0` (or any comparison against zero) would misclassify this row
	// as a drift.
	userC, _, accountC, secC := seedInvestmentFixture(ctx, t, tx, "recon-agree-c")
	seedReconTxn(ctx, t, tx, userC, accountC, secC, "buy", nil, 100, "2025-01-01")
	seedReconHolding(ctx, t, tx, userC, accountC, secC, 100.0005)

	// User D: delta 0.002 — strictly OUTSIDE the tolerance, and has a buy. Must be a drift on
	// both paths.
	userD, _, accountD, secD := seedInvestmentFixture(ctx, t, tx, "recon-agree-d")
	seedReconTxn(ctx, t, tx, userD, accountD, secD, "buy", nil, 100, "2025-01-01")
	seedReconHolding(ctx, t, tx, userD, accountD, secD, 100.002)

	after, err := repo.PositionReconciliationSummary(ctx)
	require.NoError(t, err)

	reportA, err := repo.PositionReconciliation(ctx, userA.String())
	require.NoError(t, err)
	reportB, err := repo.PositionReconciliation(ctx, userB.String())
	require.NoError(t, err)
	reportC, err := repo.PositionReconciliation(ctx, userC.String())
	require.NoError(t, err)
	reportD, err := repo.PositionReconciliation(ctx, userD.String())
	require.NoError(t, err)

	wantDriftDelta := int64(len(reportA.Drifts) + len(reportB.Drifts) + len(reportC.Drifts) + len(reportD.Drifts))
	wantSuppressedDelta := int64(reportA.SuppressedPreWindowCount + reportB.SuppressedPreWindowCount +
		reportC.SuppressedPreWindowCount + reportD.SuppressedPreWindowCount)
	wantCheckedDelta := int64(reportA.TotalPositionsChecked + reportB.TotalPositionsChecked +
		reportC.TotalPositionsChecked + reportD.TotalPositionsChecked)

	assert.Equal(t, wantDriftDelta, after.DriftCount-before.DriftCount,
		"the SQL-classified global summary must agree with the sum of the Go-classified per-user reports for drift_count")
	assert.Equal(t, wantSuppressedDelta, after.SuppressedPreWindowCount-before.SuppressedPreWindowCount,
		"...and for suppressed_pre_window_count")
	assert.Equal(t, wantCheckedDelta, after.TotalPositionsChecked-before.TotalPositionsChecked,
		"...and for total_positions_checked")

	// Concretely pin all four seeded shapes so a future change that breaks BOTH paths
	// identically (and would therefore still pass the delta-agreement assertions above) cannot
	// slip through.
	assert.Equal(t, int64(2), wantDriftDelta, "user A's and user D's mismatches must both be real, reported drifts")
	assert.Equal(t, int64(1), wantSuppressedDelta, "only user B's no-buy mismatch must be suppressed")
	assert.Equal(t, int64(4), wantCheckedDelta, "all four seeded positions must be checked, including user C's clean (within-tolerance) match")
	assert.Empty(t, reportC.Drifts, "user C's 0.0005 delta is within tolerance and must not be a drift on the per-user path")
	assert.Equal(t, 0, reportC.SuppressedPreWindowCount, "user C's clean match must not be suppressed either — it was never a mismatch to suppress")
	require.Len(t, reportD.Drifts, 1, "user D's 0.002 delta is beyond tolerance and must be a drift on the per-user path")
	assert.True(t, before.Available)
	assert.True(t, after.Available)
}
