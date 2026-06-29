//go:build integration

package repository

// Integration tests for GetSpendingByMerchant effective-merchant grouping (issue #1348, Task 2).
//
// These tests require a real PostgreSQL instance (DATABASE_URL env var).
// They exercise the 4-join override chain and synthetic GROUP BY key that pgxmock cannot validate.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -tags integration \
//	  -run TestGetSpendingByMerchant_Integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestGetSpendingByMerchant_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()
	userStr := userID.String()

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userStr, "int-analmerch-"+userStr[:8]+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted,
			institution_name, institution_id, provider, created_at, updated_at)
		VALUES ($1, $2, $3, 'enc_test', 'Test Bank', 'ins_test', 'plaid', NOW(), NOW())
	`, linkedItemID, userID, "item-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	accountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, user_id, linked_item_id, plaid_account_id,
			name, type, subtype, current_balance, available_balance, currency_code,
			status, created_at, updated_at)
		VALUES ($1, $2, $3, 'ext_test', 'Test Account', 'depository', 'checking',
			1000.00, 900.00, 'USD', 'active', NOW(), NOW())
	`, accountID, userID, linkedItemID)
	require.NoError(t, err, "seed account")

	// Insert merchants
	merchantCanonical := uuid.New() // target of merge — display name "Whole Foods"
	merchantSource := uuid.New()    // merged into canonical — raw name "WHOLEFDSMRKT #123"
	merchantRenamed := uuid.New()   // renamed only — raw "STARBUX" → display "Starbucks"

	for _, mid := range []struct {
		id   uuid.UUID
		name string
	}{
		{merchantCanonical, "Whole Foods Market"},
		{merchantSource, "WHOLEFDSMRKT #123"},
		{merchantRenamed, "STARBUX"},
	} {
		_, err = tx.Exec(ctx, `
			INSERT INTO merchants (id, name, normalized_name, created_at)
			VALUES ($1, $2, $3, NOW())
			ON CONFLICT (id) DO NOTHING
		`, mid.id, mid.name, mid.name)
		require.NoError(t, err, "insert merchant %s", mid.name)
	}

	// Merge: merchantSource → merchantCanonical
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, merged_into_merchant_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
	`, uuid.New(), userID, merchantSource, merchantCanonical)
	require.NoError(t, err, "insert merge override source→canonical")

	// Override canonical display name to "Whole Foods"
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, display_name, created_at, updated_at)
		VALUES ($1, $2, $3, 'Whole Foods', NOW(), NOW())
	`, uuid.New(), userID, merchantCanonical)
	require.NoError(t, err, "insert display_name override for canonical")

	// Rename: merchantRenamed display_name = "Starbucks"
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, display_name, created_at, updated_at)
		VALUES ($1, $2, $3, 'Starbucks', NOW(), NOW())
	`, uuid.New(), userID, merchantRenamed)
	require.NoError(t, err, "insert display_name override for renamed")

	// Use transaction-wrapped repo.
	txRepo := NewAnalyticsRepository(tx)
	baseDate := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	insertTx := func(txID uuid.UUID, name string, merchantID *uuid.UUID, merchantName *string, amount float64) {
		var mid interface{} = nil
		if merchantID != nil {
			mid = *merchantID
		}
		extID := txID.String()
		_, err := tx.Exec(ctx, `
			INSERT INTO transactions (id, user_id, account_id, linked_item_id,
				source, external_transaction_id, external_transaction_id_blind_idx,
				amount, date, name, merchant_name, merchant_id,
				pending, pfc_primary, category_source,
				created_at, updated_at)
			VALUES ($1, $2, $3, $4,
				'plaid', $5, $6,
				$7, $8, $9, $10, $11,
				false, 'FOOD_AND_DRINK', 'plaid',
				NOW(), NOW())
		`, txID, userID, accountID, linkedItemID, extID, extID, amount, baseDate, name, merchantName, mid)
		require.NoError(t, err, "insert transaction %s", txID)
	}

	// Scenario A: two transactions both linked to merchantSource — should collapse with canonical display
	wfRaw1 := "WHOLEFDSMRKT #123"
	insertTx(uuid.New(), "Whole Foods Tx1", &merchantSource, &wfRaw1, 50.00)
	insertTx(uuid.New(), "Whole Foods Tx2", &merchantSource, &wfRaw1, 30.00)

	// Scenario B: one transaction linked to merchantCanonical directly
	insertTx(uuid.New(), "Whole Foods Direct", &merchantCanonical, nil, 20.00)

	// Scenario C: renamed merchant — one transaction
	sbName := "STARBUX"
	insertTx(uuid.New(), "Starbucks Tx", &merchantRenamed, &sbName, 15.00)

	// Scenario D: unlinked raw merchant (merchant_name set, merchant_id NULL) — 'raw:' bucket
	rawName := "SOME_RAW_MERCHANT"
	insertTx(uuid.New(), "Raw Merchant Tx", nil, &rawName, 25.00)

	// Scenario E: no merchant_name, no merchant_id — 'unknown' bucket
	insertTx(uuid.New(), "Unknown Tx", nil, nil, 10.00)

	filters := domain.SpendingFilters{
		StartDate:        baseDate.Add(-24 * time.Hour),
		EndDate:          baseDate.Add(24 * time.Hour),
		IncludeTransfers: true, // skip transfer filter so we can control the data
	}

	t.Run("merge collapses source and canonical into one row with canonical display name", func(t *testing.T) {
		results, err := txRepo.GetSpendingByMerchant(ctx, userStr, filters, 20)
		require.NoError(t, err)

		// Find the Whole Foods group
		var wfRow *domain.SpendingByMerchant
		for i := range results {
			if results[i].MerchantName == "Whole Foods" {
				wfRow = &results[i]
				break
			}
		}
		require.NotNil(t, wfRow, "expected a 'Whole Foods' merchant group in results; got: %+v", results)
		// All 3 transactions (50+30 from source, 20 from canonical) should collapse into one group
		assert.InDelta(t, 100.00, wfRow.Total, 0.01, "merged Whole Foods total should be 100")
		assert.Equal(t, 3, wfRow.TransactionCount, "merged Whole Foods should have 3 transactions")
		// merchant_id should be non-nil (canonical merchant)
		require.NotNil(t, wfRow.MerchantID, "merged merchant_id should not be nil")
		assert.Equal(t, merchantCanonical.String(), *wfRow.MerchantID)
	})

	t.Run("renamed merchant shows canonical display_name not raw name", func(t *testing.T) {
		results, err := txRepo.GetSpendingByMerchant(ctx, userStr, filters, 20)
		require.NoError(t, err)

		var sbRow *domain.SpendingByMerchant
		for i := range results {
			if results[i].MerchantName == "Starbucks" {
				sbRow = &results[i]
				break
			}
		}
		require.NotNil(t, sbRow, "expected a 'Starbucks' merchant group; raw name STARBUX should not appear")
		assert.InDelta(t, 15.00, sbRow.Total, 0.01)
		require.NotNil(t, sbRow.MerchantID)
		assert.Equal(t, merchantRenamed.String(), *sbRow.MerchantID)
	})

	t.Run("unlinked raw merchant is distinct bucket with NULL merchant_id", func(t *testing.T) {
		results, err := txRepo.GetSpendingByMerchant(ctx, userStr, filters, 20)
		require.NoError(t, err)

		var rawRow *domain.SpendingByMerchant
		for i := range results {
			if results[i].MerchantName == "SOME_RAW_MERCHANT" {
				rawRow = &results[i]
				break
			}
		}
		require.NotNil(t, rawRow, "unlinked raw merchant should appear in results")
		assert.InDelta(t, 25.00, rawRow.Total, 0.01)
		assert.Nil(t, rawRow.MerchantID, "unlinked raw merchant_id must be NULL")
	})

	t.Run("no-name no-merchant transaction falls into unknown bucket with NULL merchant_id", func(t *testing.T) {
		results, err := txRepo.GetSpendingByMerchant(ctx, userStr, filters, 20)
		require.NoError(t, err)

		var unknownRow *domain.SpendingByMerchant
		for i := range results {
			if results[i].MerchantName == "Unknown" {
				unknownRow = &results[i]
				break
			}
		}
		require.NotNil(t, unknownRow, "unknown bucket should appear in results")
		assert.InDelta(t, 10.00, unknownRow.Total, 0.01)
		assert.Nil(t, unknownRow.MerchantID, "unknown bucket merchant_id must be NULL")
	})

	t.Run("raw STARBUX name does not appear as a separate group", func(t *testing.T) {
		results, err := txRepo.GetSpendingByMerchant(ctx, userStr, filters, 20)
		require.NoError(t, err)

		for _, r := range results {
			assert.NotEqual(t, "STARBUX", r.MerchantName, "raw STARBUX should not appear — must show as Starbucks")
			assert.NotEqual(t, "WHOLEFDSMRKT #123", r.MerchantName, "raw WHOLEFDSMRKT should not appear — must show as Whole Foods")
		}
	})
}

func TestGetCategoryBreakdownForMerchant_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()
	userStr := userID.String()

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userStr, "int-catmerch-"+userStr[:8]+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted,
			institution_name, institution_id, provider, created_at, updated_at)
		VALUES ($1, $2, $3, 'enc_test', 'Test Bank', 'ins_test', 'plaid', NOW(), NOW())
	`, linkedItemID, userID, "item-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	accountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, user_id, linked_item_id, plaid_account_id,
			name, type, subtype, current_balance, available_balance, currency_code,
			status, created_at, updated_at)
		VALUES ($1, $2, $3, 'ext_test', 'Test Account', 'depository', 'checking',
			1000.00, 900.00, 'USD', 'active', NOW(), NOW())
	`, accountID, userID, linkedItemID)
	require.NoError(t, err, "seed account")

	// Insert merchants
	merchantCanonical := uuid.New() // target of merge — display name "Whole Foods"
	merchantSource := uuid.New()    // merged into canonical — raw name "WHOLEFDSMRKT #123"
	merchantOther := uuid.New()     // unrelated merchant with its own transaction

	for _, mid := range []struct {
		id   uuid.UUID
		name string
	}{
		{merchantCanonical, "Whole Foods Market"},
		{merchantSource, "WHOLEFDSMRKT #123"},
		{merchantOther, "Other Merchant"},
	} {
		_, err = tx.Exec(ctx, `
			INSERT INTO merchants (id, name, normalized_name, created_at)
			VALUES ($1, $2, $3, NOW())
			ON CONFLICT (id) DO NOTHING
		`, mid.id, mid.name, mid.name)
		require.NoError(t, err, "insert merchant %s", mid.name)
	}

	// Merge: merchantSource → merchantCanonical
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, merged_into_merchant_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
	`, uuid.New(), userID, merchantSource, merchantCanonical)
	require.NoError(t, err, "insert merge override source→canonical")

	// Override canonical display name to "Whole Foods"
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, display_name, created_at, updated_at)
		VALUES ($1, $2, $3, 'Whole Foods', NOW(), NOW())
	`, uuid.New(), userID, merchantCanonical)
	require.NoError(t, err, "insert display_name override for canonical")

	// Use transaction-wrapped repo.
	txRepo := NewAnalyticsRepository(tx)
	baseDate := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	insertTx := func(txID uuid.UUID, name string, merchantID *uuid.UUID, merchantName *string, amount float64, category string) {
		var mid interface{} = nil
		if merchantID != nil {
			mid = *merchantID
		}
		extID := txID.String()
		_, err := tx.Exec(ctx, `
			INSERT INTO transactions (id, user_id, account_id, linked_item_id,
				source, external_transaction_id, external_transaction_id_blind_idx,
				amount, date, name, merchant_name, merchant_id,
				pending, pfc_primary, category_source,
				created_at, updated_at)
			VALUES ($1, $2, $3, $4,
				'plaid', $5, $6,
				$7, $8, $9, $10, $11,
				false, $12, 'plaid',
				NOW(), NOW())
		`, txID, userID, accountID, linkedItemID, extID, extID, amount, baseDate, name, merchantName, mid, category)
		require.NoError(t, err, "insert transaction %s", txID)
	}

	// Scenario A: two transactions on merchantSource with different categories
	wfRaw := "WHOLEFDSMRKT #123"
	insertTx(uuid.New(), "Whole Foods Groceries", &merchantSource, &wfRaw, 50.00, "FOOD_AND_DRINK")
	insertTx(uuid.New(), "Whole Foods Flowers", &merchantSource, &wfRaw, 30.00, "GENERAL_MERCHANDISE")

	// Scenario B: one transaction on merchantCanonical directly with different category
	insertTx(uuid.New(), "Whole Foods Prepared", &merchantCanonical, nil, 20.00, "FOOD_AND_DRINK")

	// Scenario C: transaction on unrelated merchant (should be filtered out)
	otherName := "Other Merchant"
	insertTx(uuid.New(), "Other Purchase", &merchantOther, &otherName, 100.00, "FOOD_AND_DRINK")

	filters := domain.SpendingFilters{
		StartDate:        baseDate.Add(-24 * time.Hour),
		EndDate:          baseDate.Add(24 * time.Hour),
		IncludeTransfers: true,
	}

	t.Run("UUID filter finds all merged sources with canonical merchant", func(t *testing.T) {
		results, err := txRepo.GetCategoryBreakdownForMerchant(ctx, userStr, merchantCanonical.String(), filters)
		require.NoError(t, err)

		// Expect two rows: FOOD_AND_DRINK and GIFTS_AND_DONATIONS
		require.Len(t, results, 2, "expected 2 category groups for merged merchant")

		// Build a map by category for easier assertions
		byCategory := make(map[string]*domain.CategoryBreakdownForMerchantRow)
		for i := range results {
			byCategory[results[i].Category] = &results[i]
		}

		// FOOD_AND_DRINK: 50 (from source) + 20 (from canonical) = 70
		foodRow, ok := byCategory["FOOD_AND_DRINK"]
		require.True(t, ok, "expected FOOD_AND_DRINK category in results")
		assert.InDelta(t, 70.00, foodRow.Total, 0.01, "FOOD_AND_DRINK should include both source and canonical transactions")
		assert.Equal(t, 2, foodRow.Count, "FOOD_AND_DRINK should have 2 transactions")

		// GENERAL_MERCHANDISE: 30 (from source) = 30
		giftsRow, ok := byCategory["GENERAL_MERCHANDISE"]
		require.True(t, ok, "expected GENERAL_MERCHANDISE category in results")
		assert.InDelta(t, 30.00, giftsRow.Total, 0.01, "GENERAL_MERCHANDISE should include source transaction")
		assert.Equal(t, 1, giftsRow.Count, "GENERAL_MERCHANDISE should have 1 transaction")
	})

	t.Run("UUID filter excludes unrelated merchant transactions", func(t *testing.T) {
		results, err := txRepo.GetCategoryBreakdownForMerchant(ctx, userStr, merchantCanonical.String(), filters)
		require.NoError(t, err)

		// Calculate total across all categories
		var totalAmount float64
		for _, row := range results {
			totalAmount += row.Total
		}

		// Should be 70 + 30 = 100, NOT including the 100 from merchantOther
		assert.InDelta(t, 100.00, totalAmount, 0.01, "total should not include unrelated merchant's transaction")
		assert.NotContains(t, results, 100.00, "merchantOther's amount should not appear")
	})

	t.Run("direct canonical merchant without merge returns only its own transactions", func(t *testing.T) {
		// Create a new merchant with no merge involved
		directMerchant := uuid.New()
		_, err := tx.Exec(ctx, `
			INSERT INTO merchants (id, name, normalized_name, created_at)
			VALUES ($1, 'Direct Merchant', 'Direct Merchant', NOW())
		`, directMerchant)
		require.NoError(t, err, "insert direct merchant")

		// Add transactions to direct merchant only
		directName := "Direct Merchant"
		insertTx(uuid.New(), "Direct Tx1", &directMerchant, &directName, 25.00, "FOOD_AND_DRINK")
		insertTx(uuid.New(), "Direct Tx2", &directMerchant, &directName, 15.00, "ENTERTAINMENT")

		results, err := txRepo.GetCategoryBreakdownForMerchant(ctx, userStr, directMerchant.String(), filters)
		require.NoError(t, err)

		// Expect two rows for the two categories
		require.Len(t, results, 2, "expected 2 category groups for direct merchant")

		// Map results by category
		byCategory := make(map[string]*domain.CategoryBreakdownForMerchantRow)
		for i := range results {
			byCategory[results[i].Category] = &results[i]
		}

		// FOOD_AND_DRINK: 25
		foodRow, ok := byCategory["FOOD_AND_DRINK"]
		require.True(t, ok, "expected FOOD_AND_DRINK category")
		assert.InDelta(t, 25.00, foodRow.Total, 0.01)
		assert.Equal(t, 1, foodRow.Count)

		// ENTERTAINMENT: 15
		entRow, ok := byCategory["ENTERTAINMENT"]
		require.True(t, ok, "expected ENTERTAINMENT category")
		assert.InDelta(t, 15.00, entRow.Total, 0.01)
		assert.Equal(t, 1, entRow.Count)
	})
}
