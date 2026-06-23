//go:build integration

package repository

// Integration test for GetUnusualSpend.
// Tests the CTE with STDDEV_POP that pgxmock cannot validate.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestGetUnusualSpend_Integration -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUnusualSpend_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	// Wrap all seed data in a transaction so it is automatically rolled back.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID.String(), "int-unusual-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "test-item-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	accountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, $4, 'Test Checking', 'depository')
	`, accountID, linkedItemID, userID, "plaid-acc-"+accountID.String())
	require.NoError(t, err, "seed account")

	now := time.Now().UTC()
	// Baseline window: 3 months before startDate.
	// startDate = today, so baseline = [today-3months, today).
	startDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	endDate := startDate.AddDate(0, 0, 30) // query window: today to +30 days
	baselineStart := startDate.AddDate(0, -3, 0)

	// Seed 5 consistent FOOD_AND_DRINK transactions at $50 each in the baseline window.
	// This gives mean=$50, stddev~$0 (very low variance across 5 points at same amount).
	// To avoid zero stddev (which is excluded), spread amounts slightly: $48, $50, $50, $50, $52.
	baselineAmounts := []float64{48, 50, 50, 50, 52}
	for i, amount := range baselineAmounts {
		txDate := baselineStart.AddDate(0, 0, i*14) // every 2 weeks in baseline
		txID := uuid.New()
		_, err = tx.Exec(ctx, `
			INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, name, merchant_name, amount, date, pending, pfc_primary)
			VALUES ($1, $2, $3, $4, 'plaid', 'RestaurantA', 'RestaurantA', $5, $6, false, 'FOOD_AND_DRINK')
		`, txID, userID, accountID, linkedItemID, amount, txDate.Format("2006-01-02"))
		require.NoError(t, err, "seed baseline transaction %d", i)
	}

	// Seed 1 outlier FOOD_AND_DRINK transaction at $200 in the query window.
	// mean~50, stddev~1.4 → sigma=(200-50)/1.4 ≈ 107 >> 2.0 threshold → should be detected.
	outlierTxID := uuid.New()
	outlierDate := startDate.AddDate(0, 0, 5)
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, name, merchant_name, amount, date, pending, pfc_primary)
		VALUES ($1, $2, $3, $4, 'plaid', 'FancyRestaurant', 'FancyRestaurant', 200.0, $5, false, 'FOOD_AND_DRINK')
	`, outlierTxID, userID, accountID, linkedItemID, outlierDate.Format("2006-01-02"))
	require.NoError(t, err, "seed outlier transaction")

	// Seed 1 normal FOOD_AND_DRINK transaction at $50 in the query window → should NOT be returned.
	normalTxID := uuid.New()
	normalDate := startDate.AddDate(0, 0, 10)
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, name, merchant_name, amount, date, pending, pfc_primary)
		VALUES ($1, $2, $3, $4, 'plaid', 'RegularCafe', 'RegularCafe', 50.0, $5, false, 'FOOD_AND_DRINK')
	`, normalTxID, userID, accountID, linkedItemID, normalDate.Format("2006-01-02"))
	require.NoError(t, err, "seed normal transaction")

	// Use transaction-wrapped repo.
	txRepo := NewAnalyticsRepository(tx)
	results, err := txRepo.GetUnusualSpend(ctx, userID.String(), baselineStart, startDate, endDate, 2.0)
	require.NoError(t, err)

	// Only the $200 outlier should be returned.
	require.Len(t, results, 1, "only the $200 outlier should be flagged as unusual")
	assert.Equal(t, "FancyRestaurant", results[0].MerchantName)
	assert.InDelta(t, 200.0, results[0].Amount, 0.01)
	assert.Equal(t, "FOOD_AND_DRINK", results[0].Category)
	assert.Greater(t, results[0].SigmaAbove, 2.0, "sigma_above should exceed threshold of 2.0")
	assert.InDelta(t, 50.0, results[0].BaselineAvg, 2.0, "baseline avg should be ~$50")
}
