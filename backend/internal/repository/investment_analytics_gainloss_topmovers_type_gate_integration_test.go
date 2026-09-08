//go:build integration

package repository

// Integration tests for #1616 task T2c: UnrealizedGainLoss and TopMovers are the other two
// holdings-based surfaces (AllocationBreakdown/allocationBreakdownByAccount were fixed by T2)
// that joined accounts through currentStateAccountLeg only, with no accounts.type predicate. A
// Plaid cash-management account — typed 'depository' by Plaid, but which also reports rows in
// holdings — therefore still contributed its holding to portfolio gain/loss and could appear in
// the gainers/losers list. This is NOT the #1616 double-count (neither query has a cash branch to
// double-count against); it is the narrower, explicitly user-authorised complaint that a checking
// account should never appear in a portfolio view. See the standing-decision comment on
// IncomeSummary in investment_analytics_repository.go for the accounts.type-governs-holdings
// rule this extends.
//
// Run with:
//   DATABASE_URL=postgres://... go test ./internal/repository/... \
//     -run 'TestUnrealizedGainLoss_|TestTopMovers_' -tags integration -v

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedGainLossTypeGateFixture creates a user with TWO accounts, each carrying one holding with a
// non-trivial gain (cost_basis != institution_value, so it is visible in gain/loss and would rank
// in top-movers):
//   - a genuine investment brokerage account (must be counted)
//   - a Plaid cash-management account typed 'depository' that also reports a holding (must NOT be
//     counted, post-fix)
//
// institution_price/close_price are set so day_change is non-zero and well-defined for TopMovers.
// Returns (userID, investAcctID, depositoryAcctID).
func seedGainLossTypeGateFixture(ctx context.Context, t *testing.T, db DB, prefix string) (
	userID, investAcctID, depositoryAcctID uuid.UUID,
) {
	t.Helper()
	userID = uuid.New()
	linkedItemID := uuid.New()
	investAcctID = uuid.New()
	depositoryAcctID = uuid.New()
	investSecID := uuid.New()
	depSecID := uuid.New()

	_, err := db.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+prefix+"-"+userID.String(), prefix+"-"+userID.String()+"@example.com")
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, prefix+"-item-"+linkedItemID.String())
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, current_balance)
		VALUES ($1, $2, $3, $4, 'Test Brokerage', 'investment', 'active', 0)
	`, investAcctID, linkedItemID, userID, prefix+"-inv-"+investAcctID.String())
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, current_balance)
		VALUES ($1, $2, $3, $4, 'Cash Management', 'depository', 'active', 987.65)
	`, depositoryAcctID, linkedItemID, userID, prefix+"-dep-"+depositoryAcctID.String())
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name, ticker_symbol, type, close_price)
		VALUES ($1, $2, 'Test Investment Security', 'TINV', 'stock', 90)
	`, investSecID, prefix+"-sec-inv-"+investSecID.String())
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name, ticker_symbol, type, close_price)
		VALUES ($1, $2, 'Test Money Market', 'TMMF', 'money market', 9)
	`, depSecID, prefix+"-sec-dep-"+depSecID.String())
	require.NoError(t, err)

	// Investment account holding: 10 shares, cost_basis=800, institution_price=100 (value=1000)
	// close_price=90 → day_change = 10*(100-90) = 100 (gainer), total_gain_loss = 1000-800 = 200.
	_, err = db.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value, institution_price)
		VALUES ($1, $2, $3, $4, 10, 800, 1000, 100)
	`, uuid.New(), userID, investAcctID, investSecID)
	require.NoError(t, err)

	// Depository (cash-management) account holding: distinct values from the investment holding
	// and from the account's own current_balance (987.65), so no coincidental-equality could mask
	// a wrong predicate. 20 units, cost_basis=100, institution_price=15 (value=300), close_price=9
	// → day_change = 20*(15-9) = 120 (gainer, larger than the investment holding's), total_gain_loss
	// = 300-100 = 200.
	_, err = db.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value, institution_price)
		VALUES ($1, $2, $3, $4, 20, 100, 300, 15)
	`, uuid.New(), userID, depositoryAcctID, depSecID)
	require.NoError(t, err)

	return
}

// TestUnrealizedGainLoss_DepositoryAccountHoldings_ExcludedFromPortfolio_Integration is the
// regression test for T2c: a depository (cash-management) account's holdings must not contribute
// to UnrealizedGainLoss, while a genuine investment account's holdings must still be counted with
// unchanged numbers.
func TestUnrealizedGainLoss_DepositoryAccountHoldings_ExcludedFromPortfolio_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, investAcctID, depositoryAcctID := seedGainLossTypeGateFixture(ctx, t, tx, "gl-typegate")
	repo := NewInvestmentAnalyticsRepository(tx)

	result, err := repo.UnrealizedGainLoss(ctx, userID.String())
	require.NoError(t, err)
	require.NotNil(t, result)

	var investRow, depRow *struct {
		Value              float64
		UnrealizedGainLoss float64
	}
	for _, ag := range result.ByAccount {
		ag := ag
		if ag.AccountID == investAcctID.String() {
			investRow = &struct {
				Value              float64
				UnrealizedGainLoss float64
			}{ag.Value, ag.UnrealizedGainLoss}
		}
		if ag.AccountID == depositoryAcctID.String() {
			depRow = &struct {
				Value              float64
				UnrealizedGainLoss float64
			}{ag.Value, ag.UnrealizedGainLoss}
		}
	}

	require.NotNil(t, investRow, "the investment account must still appear in ByAccount")
	assert.InDelta(t, 1000.0, investRow.Value, 0.01, "investment account value must be unchanged")
	assert.InDelta(t, 200.0, investRow.UnrealizedGainLoss, 0.01, "investment account gain must be unchanged")

	assert.Nil(t, depRow,
		"a depository (cash-management) account must NOT appear in ByAccount — holdings-based "+
			"surfaces are governed by accounts.type, and this account is not 'investment'")

	// Totals must reflect ONLY the investment account: the depository holding's value (300) and
	// gain (200) must not leak into the aggregate either.
	assert.InDelta(t, 1000.0, result.TotalValue, 0.01,
		"TotalValue must not include the depository account's holding value")
	assert.InDelta(t, 200.0, result.TotalUnrealizedGainLoss, 0.01,
		"TotalUnrealizedGainLoss must not include the depository account's gain")
}

// TestTopMovers_DepositoryAccountHoldings_ExcludedFromPortfolio_Integration is the regression
// test for T2c: a depository (cash-management) account's holdings must not appear in the
// gainers/losers lists, while a genuine investment account's holdings must still rank with
// unchanged numbers.
func TestTopMovers_DepositoryAccountHoldings_ExcludedFromPortfolio_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, investAcctID, depositoryAcctID := seedGainLossTypeGateFixture(ctx, t, tx, "tm-typegate")
	repo := NewInvestmentAnalyticsRepository(tx)

	result, err := repo.TopMovers(ctx, userID.String(), 10)
	require.NoError(t, err)
	require.NotNil(t, result)

	var investFound, depFound bool
	var investDayChange *float64
	for _, item := range result.Gainers {
		if item.AccountID == investAcctID.String() {
			investFound = true
			investDayChange = item.DayChange
		}
		if item.AccountID == depositoryAcctID.String() {
			depFound = true
		}
	}
	for _, item := range result.Losers {
		if item.AccountID == depositoryAcctID.String() {
			depFound = true
		}
	}

	require.True(t, investFound, "the investment account's holding must still appear as a gainer")
	require.NotNil(t, investDayChange)
	assert.InDelta(t, 100.0, *investDayChange, 0.01, "investment account day_change must be unchanged")

	assert.False(t, depFound,
		"a depository (cash-management) account's holding must NOT appear in gainers or losers — "+
			"holdings-based surfaces are governed by accounts.type, and this account is not 'investment'")
}
