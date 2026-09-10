//go:build integration

package repository

// Regression tests for uuid/text type-cast consistency (issue #1348).
// Verifies that all 4 query paths that use merchantSortJoins (mno.user_id=$1::text)
// correctly cast $1 as uuid in subqueries comparing against UUID columns
// (investment_transactions.user_id, custom_categories.user_id).

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// seedMinimalBankingFixture seeds user + linked_item + depository account + one transaction.
// Returns (userStr, accountIDStr, txnIDStr).
func seedMinimalBankingFixture(ctx context.Context, t *testing.T, db DB, prefix string, date time.Time) (userStr, accountIDStr, txnIDStr string) {
	t.Helper()
	userID := uuid.New()
	linkedItemID := uuid.New()
	accountID := uuid.New()
	txnID := uuid.New()
	userStr = userID.String()
	accountIDStr = accountID.String()
	txnIDStr = txnID.String()

	_, err := db.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+prefix+"-"+userStr, prefix+"-"+userStr+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = db.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, prefix+"-item-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	_, err = db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Test Checking', 'depository', 'active')
	`, accountID, linkedItemID, userID, prefix+"-acct-"+accountIDStr)
	require.NoError(t, err, "seed account")

	extID := txnID.String()
	_, err = db.Exec(ctx, `
		INSERT INTO transactions (id, user_id, account_id, linked_item_id,
			source, external_transaction_id, external_transaction_id_blind_idx,
			amount, date, name, pending, pfc_primary, category_source,
			created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'plaid', $5, $6, 10.00, $7, 'Test Tx', false, 'FOOD_AND_DRINK', 'plaid', NOW(), NOW())
	`, txnID, userID, accountID, linkedItemID, extID, extID, date)
	require.NoError(t, err, "seed transaction")

	return
}

// seedCustomCategory inserts a custom_category for userIDStr and returns its id string.
func seedCustomCategory(ctx context.Context, t *testing.T, db DB, userIDStr, prefix string) string {
	t.Helper()
	catID := uuid.New()
	_, err := db.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name)
		VALUES ($1, $2, $3)
	`, catID, userIDStr, prefix+"-cat-"+catID.String())
	require.NoError(t, err, "seed custom_category")
	return catID.String()
}

// TestUuidCastRegression_Analytics_SpendingByMerchantWithCustomCategory verifies
// GetSpendingByMerchant with CustomCategoryIDs+RollUpChildren succeeds despite
// merchantSortJoins (mno.user_id=$1::text) and custom_categories.user_id=$1 both
// appearing in the same prepared statement (analytics_repository.go:377).
func TestUuidCastRegression_Analytics_SpendingByMerchantWithCustomCategory(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	baseDate := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	userStr, _, _ := seedMinimalBankingFixture(ctx, t, tx, "uuid-cast-anl", baseDate)
	catID := seedCustomCategory(ctx, t, tx, userStr, "uuid-cast-anl")

	repo := NewAnalyticsRepository(tx)
	filters := domain.SpendingFilters{
		StartDate:         baseDate.AddDate(0, -1, 0),
		EndDate:           baseDate.AddDate(0, 1, 0),
		CustomCategoryIDs: []string{catID},
		RollUpChildren:    true,
	}
	_, err = repo.GetSpendingByMerchant(ctx, userStr, filters, 10)
	require.NoError(t, err)
}

// TestUuidCastRegression_Analytics_CategoryBreakdownWithCustomCategory verifies
// GetCategoryBreakdownForMerchant with CustomCategoryIDs+RollUpChildren succeeds
// despite merchantSortJoins and custom_categories subquery type mismatches.
func TestUuidCastRegression_Analytics_CategoryBreakdownWithCustomCategory(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	baseDate := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	userStr, _, _ := seedMinimalBankingFixture(ctx, t, tx, "uuid-cast-cat", baseDate)
	catID := seedCustomCategory(ctx, t, tx, userStr, "uuid-cast-cat")
	merchantID := uuid.New().String() // random merchant — category breakdown will return 0 rows but must not error

	repo := NewAnalyticsRepository(tx)
	filters := domain.SpendingFilters{
		StartDate:         baseDate.AddDate(0, -1, 0),
		EndDate:           baseDate.AddDate(0, 1, 0),
		CustomCategoryIDs: []string{catID},
		RollUpChildren:    true,
	}
	_, err = repo.GetCategoryBreakdownForMerchant(ctx, userStr, merchantID, filters)
	require.NoError(t, err)
}

// TestUuidCastRegression_Transactions_SearchWithCustomCategory verifies
// GetByUserIDWithFilters and CountByUserID with Search+CustomCategoryIDs succeed
// despite merchantSortJoins and custom_categories.user_id=$1 type conflicts
// (transaction_repository.go:1186).
func TestUuidCastRegression_Transactions_SearchWithCustomCategory(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck
	enc := newTestFieldEncryptor(t)

	baseDate := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	userStr, _, _ := seedMinimalBankingFixture(ctx, t, tx, "uuid-cast-srch", baseDate)
	catID := seedCustomCategory(ctx, t, tx, userStr, "uuid-cast-srch")

	repo := NewTransactionRepository(tx, enc)
	filters := domain.TransactionFilters{
		Search:            ptrStr("test"),
		CustomCategoryIDs: []string{catID},
	}

	// GetByUserIDWithFilters: search injects merchantSortJoins; custom cat subquery uses user_id=$1
	_, err = repo.GetByUserIDWithFilters(ctx, userStr, filters, 0, 10)
	require.NoError(t, err)

	// CountByUserID: same codepath
	_, err = repo.CountByUserID(ctx, userStr, filters)
	require.NoError(t, err)
}

// TestUuidCastRegression_UnifiedFeed_SearchWithInvestmentRow verifies
// GetByUserIDKeysetUnified with Search+investment rows succeeds despite
// banking branch having mno.user_id=$1::text and investment branch having
// investment_transactions.user_id=$1::uuid (transaction_repository.go:3114).
func TestUuidCastRegression_UnifiedFeed_SearchWithInvestmentRow(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck
	enc := newTestFieldEncryptor(t)

	baseDate := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	// Seed a banking fixture so there is a banking row for the user.
	userStr, _, _ := seedMinimalBankingFixture(ctx, t, tx, "uuid-cast-unif", baseDate)

	// Also seed an investment transaction for the same user.
	// Use seedInvestmentFixture but we need the same user — seed the investment
	// account and security manually under the pre-seeded user.
	userUUID, err := uuid.Parse(userStr)
	require.NoError(t, err)
	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userUUID, "uuid-cast-unif-item2-"+linkedItemID.String())
	require.NoError(t, err, "seed investment linked_item")

	investAcctID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Test Brokerage', 'investment', 'active')
	`, investAcctID, linkedItemID, userUUID, "uuid-cast-unif-acct-"+investAcctID.String())
	require.NoError(t, err, "seed investment account")

	secID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name, ticker_symbol) VALUES ($1, $2, 'Test Sec', 'TUCS')
	`, secID, "uuid-cast-unif-sec-"+secID.String())
	require.NoError(t, err, "seed security")

	invTxnID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, security_id, plaid_investment_transaction_id, type, name, amount, date)
		VALUES ($1, $2, $3, $4, $5, 'buy', 'Buy Test', 500.00, $6)
	`, invTxnID, userUUID, investAcctID, secID, "uuid-cast-unif-inv-"+invTxnID.String(), baseDate)
	require.NoError(t, err, "seed investment transaction")

	repo := NewTransactionRepository(tx, enc)
	filters := domain.TransactionFilters{
		IncludeInvestmentActivity: true,
		Search:                    ptrStr("test"), // activates merchantSortJoins on banking branch
		SortBy:                    "date",         // required by GetByUserIDKeysetUnified
	}
	// Banking branch has mno.user_id=$1::text; investment branch has it.user_id=$1::uuid.
	_, _, _, err = repo.GetByUserIDKeysetUnified(ctx, userStr, filters, nil, 10)
	require.NoError(t, err)
}

// TestUuidCastRegression_UnifiedFeed_SearchWithCustomCategory verifies
// GetByUserIDKeysetUnified with Search+CustomCategoryIDs succeeds despite
// banking branch having both merchantSortJoins ($1::text) and custom_categories
// subquery ($1::uuid) (transaction_repository.go:2910).
func TestUuidCastRegression_UnifiedFeed_SearchWithCustomCategory(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck
	enc := newTestFieldEncryptor(t)

	baseDate := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	userStr, _, _ := seedMinimalBankingFixture(ctx, t, tx, "uuid-cast-unif-cc", baseDate)
	catID := seedCustomCategory(ctx, t, tx, userStr, "uuid-cast-unif-cc")

	repo := NewTransactionRepository(tx, enc)
	filters := domain.TransactionFilters{
		IncludeInvestmentActivity: true,
		Search:                    ptrStr("test"),  // activates merchantSortJoins
		CustomCategoryIDs:         []string{catID}, // custom_categories.user_id=$1 subquery
		SortBy:                    "date",
	}
	_, _, _, err = repo.GetByUserIDKeysetUnified(ctx, userStr, filters, nil, 10)
	require.NoError(t, err)
}
