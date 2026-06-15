//go:build integration

package repository

// Integration tests for the extended AllocationBreakdown (#1296).
// These tests validate the UNION SQL path that merges investment holdings
// with Plaid accounts (depository→cash, credit/loan→liabilities).
//
// Run with:
//   DATABASE_URL=postgres://... go test ./internal/repository/... \
//     -run TestAllocationBreakdown_ -tags integration -v

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// assertPercentagesSum100 is a helper that validates allocation percentages sum to 100%.
func assertPercentagesSum100(t *testing.T, buckets []domain.AllocationItem, msg string) {
	t.Helper()
	var total float64
	for _, b := range buckets {
		total += b.Percentage
	}
	assert.InDelta(t, 100.0, total, 0.1, msg)
}

// seedAllocationFixture creates a user with three accounts:
//   - investment brokerage with one stock holding (value=5000)
//   - depository checking account (current_balance=3000)
//   - credit card account (current_balance=-1200)
//
// Returns (userID, investAcctID, depositoryAcctID, creditAcctID, secID).
func seedAllocationFixture(ctx context.Context, t *testing.T, db DB, prefix string) (
	userID, investAcctID, depositoryAcctID, creditAcctID, secID uuid.UUID,
) {
	t.Helper()
	userID = uuid.New()
	linkedItemID := uuid.New()
	investAcctID = uuid.New()
	depositoryAcctID = uuid.New()
	creditAcctID = uuid.New()
	secID = uuid.New()

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
		VALUES ($1, $2, $3, $4, 'Checking', 'depository', 'active', 3000)
	`, depositoryAcctID, linkedItemID, userID, prefix+"-dep-"+depositoryAcctID.String())
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, current_balance)
		VALUES ($1, $2, $3, $4, 'Visa Card', 'credit', 'active', -1200)
	`, creditAcctID, linkedItemID, userID, prefix+"-cred-"+creditAcctID.String())
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name, type)
		VALUES ($1, $2, 'AAPL', 'stock')
	`, secID, prefix+"-sec-"+secID.String())
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, institution_value)
		VALUES ($1, $2, $3, $4, 50, 5000)
	`, uuid.New(), userID, investAcctID, secID)
	require.NoError(t, err)

	return
}

// TestAllocationBreakdown_Default_InvestmentsOnly_Integration proves that the default
// (no options) returns only investment holdings — backward-compatible with #138 behavior.
func TestAllocationBreakdown_Default_InvestmentsOnly_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, _, _, _ := seedAllocationFixture(ctx, t, tx, "alloc-default")
	repo := NewInvestmentAnalyticsRepository(tx)

	result, err := repo.AllocationBreakdown(ctx, userID.String(), domain.AllocationOptions{})

	require.NoError(t, err)
	require.NotNil(t, result)
	// Default: only investment holdings (stock=5000). Cash/liabilities NOT included.
	require.Len(t, result.Buckets, 1, "default must include only investment holdings bucket")
	assert.Equal(t, "stock", result.Buckets[0].SecurityType)
	assert.InDelta(t, 5000.0, result.Buckets[0].TotalValue, 0.01)
	assert.InDelta(t, 5000.0, result.TotalValue, 0.01)
}

// TestAllocationBreakdown_WithCash_AddsDepositoryBucket_Integration proves that
// include=["cash"] adds the depository account balance as a "cash" bucket.
func TestAllocationBreakdown_WithCash_AddsDepositoryBucket_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, _, _, _ := seedAllocationFixture(ctx, t, tx, "alloc-cash")
	repo := NewInvestmentAnalyticsRepository(tx)

	opts := domain.AllocationOptions{Include: []string{"cash"}}
	result, err := repo.AllocationBreakdown(ctx, userID.String(), opts)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Should have stock (5000) + cash from depository (3000) = total 8000.
	assert.InDelta(t, 8000.0, result.TotalValue, 0.01, "total must include investment + depository cash")

	var stockBucket, cashBucket *domain.AllocationItem
	for i := range result.Buckets {
		switch result.Buckets[i].SecurityType {
		case "stock":
			b := result.Buckets[i]
			stockBucket = &b
		case "cash":
			b := result.Buckets[i]
			cashBucket = &b
		}
	}
	require.NotNil(t, stockBucket, "must have a 'stock' bucket from holdings")
	require.NotNil(t, cashBucket, "must have a 'cash' bucket from depository accounts")
	assert.InDelta(t, 5000.0, stockBucket.TotalValue, 0.01)
	assert.InDelta(t, 3000.0, cashBucket.TotalValue, 0.01)

	// Credit card must NOT appear (liabilities not requested).
	for _, b := range result.Buckets {
		assert.NotEqual(t, "liabilities", b.SecurityType,
			"credit/loan liabilities must not appear when only 'cash' is included")
	}
}

// TestAllocationBreakdown_WithLiabilities_AddsLiabilitiesBucket_Integration proves that
// include=["liabilities"] adds credit/loan account balances as a "liabilities" bucket.
// Uses default basis (assets) so the liabilities bucket is preserved in the response.
func TestAllocationBreakdown_WithLiabilities_AddsLiabilitiesBucket_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, _, _, _ := seedAllocationFixture(ctx, t, tx, "alloc-liab")
	repo := NewInvestmentAnalyticsRepository(tx)

	// Basis: domain.AllocationBasisAssets (default) so liabilities bucket is NOT removed.
	// basis=net would remove the bucket via applyNetBasis — tested separately in
	// TestAllocationBreakdown_BasisNet_RemovesLiabilitiesFromTotal_Integration.
	opts := domain.AllocationOptions{Include: []string{"liabilities"}}
	result, err := repo.AllocationBreakdown(ctx, userID.String(), opts)

	require.NoError(t, err)
	require.NotNil(t, result)

	var liabBucket *domain.AllocationItem
	for i := range result.Buckets {
		if result.Buckets[i].SecurityType == "liabilities" {
			b := result.Buckets[i]
			liabBucket = &b
		}
	}
	require.NotNil(t, liabBucket, "must have a 'liabilities' bucket from credit account")
	// Credit balance is -1200; stored as absolute value in the bucket.
	assert.InDelta(t, 1200.0, liabBucket.TotalValue, 0.01)
}

// TestAllocationBreakdown_CashPlusLiabilities_BothBuckets_Integration proves that
// include=["cash","liabilities"] produces both extra buckets.
func TestAllocationBreakdown_CashPlusLiabilities_BothBuckets_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, _, _, _ := seedAllocationFixture(ctx, t, tx, "alloc-both")
	repo := NewInvestmentAnalyticsRepository(tx)

	opts := domain.AllocationOptions{Include: []string{"cash", "liabilities"}}
	result, err := repo.AllocationBreakdown(ctx, userID.String(), opts)

	require.NoError(t, err)
	require.NotNil(t, result)

	var hasStock, hasCash, hasLiab bool
	for _, b := range result.Buckets {
		switch b.SecurityType {
		case "stock":
			hasStock = true
		case "cash":
			hasCash = true
		case "liabilities":
			hasLiab = true
		}
	}
	assert.True(t, hasStock, "must have stock bucket from holdings")
	assert.True(t, hasCash, "must have cash bucket from depository")
	assert.True(t, hasLiab, "must have liabilities bucket from credit account")
}

// TestAllocationBreakdown_BasisNet_RemovesLiabilitiesFromTotal_Integration proves that
// basis=net drops the liabilities bucket from the response and sets TotalValue to the
// net-of-liabilities total (Option A: TotalValue=assets−liabilities, percentages on asset
// denominator so the donut ring sums to 100%).
//
// Fixture: stock=5000, cash=3000, liabilities(credit)=1200.
// grandTotal inside repo = 9200 (all buckets positive).
// Expected: TotalValue=6800, stock%=62.5% (5000/8000), cash%=37.5% (3000/8000), Σ=100%.
func TestAllocationBreakdown_BasisNet_RemovesLiabilitiesFromTotal_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, _, _, _ := seedAllocationFixture(ctx, t, tx, "alloc-basis-net")
	repo := NewInvestmentAnalyticsRepository(tx)

	opts := domain.AllocationOptions{
		Include: []string{"cash", "liabilities"},
		Basis:   domain.AllocationBasisNet,
	}
	result, err := repo.AllocationBreakdown(ctx, userID.String(), opts)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Option A: TotalValue = asset_total − liabilities = (5000+3000) − 1200 = 6800.
	assert.InDelta(t, 6800.0, result.TotalValue, 0.01, "net total must be asset_total minus liabilities")

	// Liabilities bucket must NOT appear — consumed by the net calculation.
	for _, b := range result.Buckets {
		assert.NotEqual(t, "liabilities", b.SecurityType,
			"liabilities bucket must not appear in basis=net response")
	}

	// Asset buckets: stock + cash.
	var stockBucket, cashBucket *domain.AllocationItem
	for i := range result.Buckets {
		switch result.Buckets[i].SecurityType {
		case "stock":
			b := result.Buckets[i]
			stockBucket = &b
		case "cash":
			b := result.Buckets[i]
			cashBucket = &b
		}
	}
	require.NotNil(t, stockBucket, "stock bucket must be present")
	require.NotNil(t, cashBucket, "cash bucket must be present")

	// Option A: percentages on asset denominator (8000) so donut ring sums to 100%.
	assert.InDelta(t, 5000.0/8000.0*100, stockBucket.Percentage, 0.1,
		"stock pct on asset denominator (not net denominator)")
	assert.InDelta(t, 3000.0/8000.0*100, cashBucket.Percentage, 0.1,
		"cash pct on asset denominator (not net denominator)")
	assertPercentagesSum100(t, result.Buckets, "asset percentages must sum to 100%% in basis=net")
}

// TestAllocationBreakdown_GroupByAccount_Integration proves that group_by=account returns
// one bucket per active account, labeled by account name, with asset_class = account type.
func TestAllocationBreakdown_GroupByAccount_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Fixture: "Test Brokerage" (investment, holdings value=5000),
	//          "Checking"        (depository, balance=3000),
	//          "Visa Card"       (credit, balance=−1200).
	userID, _, _, _, _ := seedAllocationFixture(ctx, t, tx, "alloc-by-acct")
	repo := NewInvestmentAnalyticsRepository(tx)

	// include=[cash] only — credit account must NOT appear.
	opts := domain.AllocationOptions{
		Include: []string{"cash"},
		GroupBy: domain.AllocationGroupByAccount,
	}
	result, err := repo.AllocationBreakdown(ctx, userID.String(), opts)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Must have exactly two buckets: brokerage + checking.
	require.Len(t, result.Buckets, 2, "must have one bucket per included account type")

	var brokerageBucket, checkingBucket *domain.AllocationItem
	for i := range result.Buckets {
		switch result.Buckets[i].SecurityType {
		case "Test Brokerage":
			b := result.Buckets[i]
			brokerageBucket = &b
		case "Checking":
			b := result.Buckets[i]
			checkingBucket = &b
		}
	}
	require.NotNil(t, brokerageBucket, "must have 'Test Brokerage' account bucket")
	require.NotNil(t, checkingBucket, "must have 'Checking' account bucket")

	assert.InDelta(t, 5000.0, brokerageBucket.TotalValue, 0.01)
	assert.Equal(t, "investment", brokerageBucket.AssetClass)
	assert.InDelta(t, 3000.0, checkingBucket.TotalValue, 0.01)
	assert.Equal(t, "depository", checkingBucket.AssetClass)

	// Net total: 5000 + 3000 = 8000 (credit not included → basis=assets implicit).
	assert.InDelta(t, 8000.0, result.TotalValue, 0.01)

	// Credit/loan accounts must not appear (not in Include).
	for _, b := range result.Buckets {
		assert.NotEqual(t, "credit", b.AssetClass, "credit account must not appear")
		assert.NotEqual(t, "loan", b.AssetClass, "loan account must not appear")
	}
	assertPercentagesSum100(t, result.Buckets, "percentages must sum to 100%% when basis=assets")
}

// TestAllocationBreakdown_GroupByAccount_BasisNet_Integration proves that group_by=account
// combined with basis=net removes credit/loan account buckets and sets TotalValue to the
// net total (Option A: TotalValue=asset_total−liabilities; percentages on asset denominator).
func TestAllocationBreakdown_GroupByAccount_BasisNet_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, _, _, _ := seedAllocationFixture(ctx, t, tx, "alloc-acct-net")
	repo := NewInvestmentAnalyticsRepository(tx)

	opts := domain.AllocationOptions{
		Include: []string{"cash", "liabilities"},
		GroupBy: domain.AllocationGroupByAccount,
		Basis:   domain.AllocationBasisNet,
	}
	result, err := repo.AllocationBreakdown(ctx, userID.String(), opts)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Option A: TotalValue = (5000+3000) − 1200 = 6800; credit bucket removed.
	assert.InDelta(t, 6800.0, result.TotalValue, 0.01, "net total = asset_total minus liabilities")
	for _, b := range result.Buckets {
		assert.NotEqual(t, "credit", b.AssetClass, "credit account must not appear in basis=net")
		assert.NotEqual(t, "loan", b.AssetClass, "loan account must not appear in basis=net")
	}

	// Two buckets: brokerage + checking only.
	require.Len(t, result.Buckets, 2, "only asset accounts in basis=net response")

	// Percentages on asset denominator (8000) so ring sums to 100%.
	assertPercentagesSum100(t, result.Buckets, "percentages must sum to 100%% in basis=net+account mode")
}

// TestAllocationBreakdown_GroupByAssetClass_Integration proves that group_by=asset_class
// collapses security types into high-level asset class buckets (investments, cash, liabilities).
func TestAllocationBreakdown_GroupByAssetClass_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, _, _, _ := seedAllocationFixture(ctx, t, tx, "alloc-assetclass")
	repo := NewInvestmentAnalyticsRepository(tx)

	opts := domain.AllocationOptions{
		Include: []string{"cash"},
		GroupBy: domain.AllocationGroupByAssetClass,
	}
	result, err := repo.AllocationBreakdown(ctx, userID.String(), opts)

	require.NoError(t, err)
	require.NotNil(t, result)

	// With group_by=asset_class, the stock holding AND any investment-type holdings
	// should collapse to an "investments" bucket; depository → "cash" bucket.
	var hasInvestments, hasCash bool
	for _, b := range result.Buckets {
		switch b.AssetClass {
		case "investments":
			hasInvestments = true
		case "cash":
			hasCash = true
		}
	}
	assert.True(t, hasInvestments, "must have 'investments' asset class bucket")
	assert.True(t, hasCash, "must have 'cash' asset class bucket from depository")
}
