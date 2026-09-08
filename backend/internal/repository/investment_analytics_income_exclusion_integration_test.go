//go:build integration

package repository

// Issue #1614 — IncomeSummary was the ONE live read of investment_transactions that did not
// carry investmentSyncExclusionCondition. A double-reported cash-management account's
// dividends/interest kept counting toward the income total even after the user flipped
// user_accounts.exclude_investment_sync, so the income total and the activity feed disagreed.
//
// Standing decision (also written into investment_analytics_repository.go): exclude_investment_sync
// governs investment_transactions reads only. Holdings-based surfaces (AllocationBreakdown,
// UnrealizedGainLoss, TopMovers) are governed by accounts.type instead — see #1616's fix in the
// same file. IncomeSummary is the investment_transactions side of that split.
//
// ⚠ Integration tests on purpose: pgxmock asserts the SQL string and returns invented rows, so it
// cannot catch a wrong join, a dropped NULL leg, or a fan-out.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIncomeSummary_ExcludeInvestmentSync_ExcludesFlaggedChainIncome_Integration proves that a
// dividend on a chain flagged exclude_investment_sync no longer counts toward IncomeSummary's
// total. Before the #1614 fix, IncomeSummary carried retainedAccountVisibilityLegs but no
// investmentSyncExclusionCondition, so this dividend counted regardless of the flag.
func TestIncomeSummary_ExcludeInvestmentSync_ExcludesFlaggedChainIncome_Integration(t *testing.T) {
	ctx := context.Background()
	tx := beginExcludeSyncTx(ctx, t)
	prefix := uniquePrefix("income-excl")

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, prefix)

	userAccountID := uuid.New()
	_, err := tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id) VALUES ($1, $2, $3)
	`, userAccountID, userID, "ins-"+prefix+"-"+userAccountID.String())
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE accounts SET user_account_id = $1 WHERE id = $2`, userAccountID, accountID)
	require.NoError(t, err)

	divID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, user_account_id, security_id, plaid_investment_transaction_id,
			 type, subtype, name, amount, date)
		VALUES ($1, $2, $3, $4, $5, $6, 'cash', 'dividend', 'AAPL Dividend', -200.00, '2026-03-15')
	`, divID, userID, accountID, userAccountID, secID, "plaid-income-excl-"+divID.String())
	require.NoError(t, err)

	repo := NewInvestmentAnalyticsRepository(tx)
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 31, 23, 59, 59, 0, time.UTC)

	before, err := repo.IncomeSummary(ctx, userID.String(), start, end)
	require.NoError(t, err)
	require.InDelta(t, 200.0, before.TotalIncome, 0.01, "precondition: the dividend counts before the flag is set")

	setExcludeInvestmentSync(ctx, t, tx, userAccountID, true)

	after, err := repo.IncomeSummary(ctx, userID.String(), start, end)
	require.NoError(t, err)
	assert.InDelta(t, 0.0, after.TotalIncome, 0.01,
		"a dividend on a chain flagged exclude_investment_sync must not count toward IncomeSummary — "+
			"the feed and the income total must agree")
	assert.Empty(t, after.ByType, "no income bucket should remain once the sole contributing row is excluded")
}

// TestIncomeSummary_ExcludeInvestmentSync_LegacyNullUserAccountIDStaysCounted_Integration is the
// brief's REQUIRED regression test: investmentSyncExclusionCondition handles a row whose
// user_account_id IS NULL through a second EXISTS leg over accounts.user_account_id
// (transaction_repository.go:2992-3000). A naive `JOIN user_accounts ... WHERE NOT
// exclude_investment_sync` — or an exclusion condition missing that second leg — would either
// drop every legacy-NULL row (data loss) or fail to exclude one that legitimately sits on a
// flagged chain. This test pins the NULL-tolerant, non-dropping half specifically: a legacy-NULL
// dividend on an account that belongs to NO flagged chain must stay counted even while a
// DIFFERENT chain in the same user's data is flagged — proving the fallback EXISTS leg does not
// accidentally match every legacy row once ANY chain is flagged.
func TestIncomeSummary_ExcludeInvestmentSync_LegacyNullUserAccountIDStaysCounted_Integration(t *testing.T) {
	ctx := context.Background()
	tx := beginExcludeSyncTx(ctx, t)
	prefix := uniquePrefix("income-legacy-null")

	userID, linkedItemID, flaggedAccountID, secID := seedInvestmentFixture(ctx, t, tx, prefix)

	// The chain that WILL be flagged — unrelated to the legacy row below.
	userAccountID := uuid.New()
	_, err := tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id) VALUES ($1, $2, $3)
	`, userAccountID, userID, "ins-"+prefix+"-"+userAccountID.String())
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE accounts SET user_account_id = $1 WHERE id = $2`, userAccountID, flaggedAccountID)
	require.NoError(t, err)
	setExcludeInvestmentSync(ctx, t, tx, userAccountID, true)

	// A second, UNCHAINED account (accounts.user_account_id IS NULL) for the same user, carrying
	// a legacy-NULL-attribution dividend (investment_transactions.user_account_id IS NULL too).
	legacyAccountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Legacy Brokerage', 'investment', 'active')
	`, legacyAccountID, linkedItemID, userID, "legacy-acct-"+prefix)
	require.NoError(t, err)

	legacyDivID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, user_account_id, security_id, plaid_investment_transaction_id,
			 type, subtype, name, amount, date)
		VALUES ($1, $2, $3, NULL, $4, $5, 'cash', 'dividend', 'Legacy Dividend', -75.00, '2026-03-20')
	`, legacyDivID, userID, legacyAccountID, secID, "plaid-legacy-div-"+legacyDivID.String())
	require.NoError(t, err)

	repo := NewInvestmentAnalyticsRepository(tx)
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 31, 23, 59, 59, 0, time.UTC)

	result, err := repo.IncomeSummary(ctx, userID.String(), start, end)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.InDelta(t, 75.0, result.TotalIncome, 0.01,
		"a legacy investment_transaction with a NULL user_account_id, on an account belonging to "+
			"no flagged chain, must stay counted in IncomeSummary even while a DIFFERENT chain is "+
			"flagged exclude_investment_sync — the NULL-tolerant fallback leg must not over-match")
	require.Len(t, result.ByType, 1)
	assert.Equal(t, "dividend", result.ByType[0].IncomeType)
	assert.InDelta(t, 75.0, result.ByType[0].TotalAmount, 0.01)
}
