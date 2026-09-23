//go:build integration

// Package services — Issue #1616 (T8): close the type-gate split between the four gated
// investment_analytics_repository.go surfaces (AllocationBreakdown x2, UnrealizedGainLoss,
// TopMovers — #1612/T2/T2c) and the two live holdings-read endpoints, ListHoldings and
// GetPortfolioSummary, which both call holdingRepository.GetByUserIDWithSecurities. Before this
// fix that method joined accounts through currentStateAccountLeg("a") only, with no
// `a.type = 'investment'` predicate, so a depository cash-management account carrying holdings
// was excluded from allocation/gain-loss/top-movers but still included in the holdings list and
// portfolio summary — two live endpoints disagreeing about the same account.
//
// pgxmock cannot catch a wrong (or missing) SQL predicate — it asserts the query string and
// returns invented rows regardless of what the WHERE clause actually says. This needs a real
// Postgres so the JOIN predicate is actually evaluated.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/services/... -tags integration -p 1 -count=1 \
//	    -run TestInvestmentHoldings_DepositoryAccountHoldings_ExcludedFromLiveReads -v
package services

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// TestInvestmentHoldings_DepositoryAccountHoldings_ExcludedFromLiveReads seeds BOTH a depository
// account carrying holdings (proves exclusion) and a genuine investment account carrying holdings
// (proves the predicate keeps what it should — a test that only proved exclusion would pass just
// as happily if the query returned nothing at all). It asserts on the OBSERVABLE OUTPUT of both
// production callers, ListHoldings and GetPortfolioSummary, not just the repository method,
// because the point of the fix is that those two endpoints now agree with the other four gated
// surfaces.
func TestInvestmentHoldings_DepositoryAccountHoldings_ExcludedFromLiveReads(t *testing.T) {
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	linkedItemID := uuid.New().String()
	investAcctID := uuid.New().String()
	depositoryAcctID := uuid.New().String()
	investSecID := uuid.New().String()
	depSecID := uuid.New().String()

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "int-holdings-gate|"+userID, "int-holdings-gate-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-holdings-gate-token')
	`, linkedItemID, userID, "holdings-gate-item-"+linkedItemID)
	require.NoError(t, err, "seed linked_item")

	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, current_balance)
		VALUES ($1, $2, $3, $4, 'Test Brokerage', 'investment', 'active', 0)
	`, investAcctID, linkedItemID, userID, "holdings-gate-inv-"+investAcctID)
	require.NoError(t, err, "seed investment account")

	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, current_balance)
		VALUES ($1, $2, $3, $4, 'Cash Management', 'depository', 'active', 987.65)
	`, depositoryAcctID, linkedItemID, userID, "holdings-gate-dep-"+depositoryAcctID)
	require.NoError(t, err, "seed depository account")

	_, err = tx.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name, ticker_symbol, type, close_price)
		VALUES ($1, $2, 'Test Investment Security', 'TINV', 'stock', 90)
	`, investSecID, "holdings-gate-sec-inv-"+investSecID)
	require.NoError(t, err, "seed investment security")

	_, err = tx.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name, ticker_symbol, type, close_price)
		VALUES ($1, $2, 'Test Money Market', 'TMMF', 'money market', 9)
	`, depSecID, "holdings-gate-sec-dep-"+depSecID)
	require.NoError(t, err, "seed depository security")

	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value, institution_price)
		VALUES ($1, $2, $3, $4, 10, 800, 1000, 100)
	`, uuid.New().String(), userID, investAcctID, investSecID)
	require.NoError(t, err, "seed investment holding")

	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value, institution_price)
		VALUES ($1, $2, $3, $4, 20, 100, 300, 15)
	`, uuid.New().String(), userID, depositoryAcctID, depSecID)
	require.NoError(t, err, "seed depository holding")

	holdingRepo := repository.NewHoldingRepository(tx)
	snapshotRepo := repository.NewPortfolioSnapshotRepository(tx)
	invTxnRepo := repository.NewInvestmentTransactionRepository(tx)
	svc := NewInvestmentService(holdingRepo, snapshotRepo, invTxnRepo)

	t.Run("ListHoldings", func(t *testing.T) {
		views, err := svc.ListHoldings(ctx, userID, nil, nil)
		require.NoError(t, err)

		var sawInvest, sawDepository bool
		for _, v := range views {
			if v.AccountID == investAcctID {
				sawInvest = true
			}
			if v.AccountID == depositoryAcctID {
				sawDepository = true
			}
		}
		assert.True(t, sawInvest, "ListHoldings must still include the investment account's holding")
		assert.False(t, sawDepository,
			"ListHoldings must NOT include the depository account's holding — it disagrees with "+
				"the gated allocation/gain-loss/top-movers surfaces otherwise (Issue #1616)")
	})

	t.Run("GetPortfolioSummary", func(t *testing.T) {
		summary, err := svc.GetPortfolioSummary(ctx, userID, nil)
		require.NoError(t, err)
		require.NotNil(t, summary)

		// Only the investment account's holding (value=1000) may count. If the depository
		// holding (value=300) leaks in, TotalValue is 1300 and HoldingsCount is 2.
		assert.InDelta(t, 1000.0, summary.TotalValue, 0.01,
			"GetPortfolioSummary.TotalValue must not include the depository account's holding value")
		assert.Equal(t, 1, summary.HoldingsCount,
			"GetPortfolioSummary.HoldingsCount must not include the depository account's holding")
	})
}
