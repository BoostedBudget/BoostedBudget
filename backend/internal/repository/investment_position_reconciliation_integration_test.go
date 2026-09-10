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
