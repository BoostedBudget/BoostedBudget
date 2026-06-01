//go:build integration

package repository

// Integration test for GetRecurringSubscriptions.
// Tests the window function + LAG SQL that pgxmock cannot validate.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestGetRecurringSubscriptions_Integration -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRecurringSubscriptions_Integration(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewAnalyticsRepository(pool)
	ctx := context.Background()

	// Wrap all seed data in a transaction so it is automatically rolled back.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
	`, userID, "test|"+userID.String(), "int-recur-"+userID.String()+"@example.com")
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

	// Recurring subscription: Netflix, $15.99 — 4 charges ~30 days apart.
	// pfc_primary = 'ENTERTAINMENT' so effective_pfc_primary will not match TRANSFER%.
	now := time.Now().UTC()
	for i := 0; i < 4; i++ {
		// Charges 3, 2, 1, 0 months ago — each ~30 days apart.
		txDate := now.AddDate(0, -(3 - i), 0)
		txID := uuid.New()
		_, err = tx.Exec(ctx, `
			INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, name, merchant_name, amount, date, pending, pfc_primary)
			VALUES ($1, $2, $3, $4, 'plaid', 'Netflix', 'Netflix', 15.99, $5, false, 'ENTERTAINMENT')
		`, txID, userID, accountID, linkedItemID, txDate.Format("2006-01-02"))
		require.NoError(t, err, "seed Netflix transaction %d", i)
	}

	// One-off purchase: Amazon, $99, only once — should NOT be detected.
	oneOffID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, name, merchant_name, amount, date, pending, pfc_primary)
		VALUES ($1, $2, $3, $4, 'plaid', 'Amazon', 'Amazon', 99.00, $5, false, 'SHOPPING')
	`, oneOffID, userID, accountID, linkedItemID, now.AddDate(0, -1, 0).Format("2006-01-02"))
	require.NoError(t, err, "seed one-off Amazon transaction")

	// Use the transaction-wrapped pool so the test data is visible to the repo call.
	txRepo := NewAnalyticsRepository(tx)
	lookbackStart := now.AddDate(-1, 0, 0)
	results, err := txRepo.GetRecurringSubscriptions(ctx, userID.String(), lookbackStart)
	require.NoError(t, err)

	assert.Len(t, results, 1, "only Netflix should be detected as recurring")
	assert.Equal(t, "Netflix", results[0].MerchantName)
	assert.InDelta(t, 15.99, results[0].Amount, 0.01)
	assert.Equal(t, 4, results[0].OccurrenceCount)
	assert.InDelta(t, 30, results[0].AvgGapDays, 5, "avg gap should be roughly 30 days")
}
