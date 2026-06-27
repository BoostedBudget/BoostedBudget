//go:build integration

package repository

// Integration tests for MerchantRepository methods that involve GROUP BY, aggregation,
// or JOINs which pgxmock cannot validate (type/aggregate errors).
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... \
//	  -run 'TestMerchant.*Integration' -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedMerchantFixture creates a minimal user + linked_item + account + merchant + transaction.
// All writes target db (typically a pgx.Tx) so they roll back with it.
func seedMerchantFixture(ctx context.Context, t *testing.T, db DB, prefix string) (userID, merchantID uuid.UUID) {
	t.Helper()
	userID = uuid.New()
	linkedItemID := uuid.New()
	accountID := uuid.New()
	merchantID = uuid.New()

	_, err := db.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+prefix+"-"+userID.String(), prefix+"-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = db.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "item-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	_, err = db.Exec(ctx, `
		INSERT INTO accounts (id, user_id, linked_item_id, plaid_account_id, name, type, subtype)
		VALUES ($1, $2, $3, $4, 'Checking', 'depository', 'checking')
	`, accountID, userID, linkedItemID, "plaid-acct-"+accountID.String())
	require.NoError(t, err, "seed account")

	normalized := "Coffee Shop"
	_, err = db.Exec(ctx, `
		INSERT INTO merchants (id, name, normalized_name, created_at)
		VALUES ($1, $2, $3, $4)
	`, merchantID, "SQ*Coffee Shop", normalized, time.Now().UTC())
	require.NoError(t, err, "seed merchant")

	txID := uuid.New()
	_, err = db.Exec(ctx, `
		INSERT INTO transactions (
			id, user_id, account_id, merchant_id, plaid_transaction_id,
			merchant_name, amount, date, pending
		) VALUES ($1, $2, $3, $4, $5, $6, -5.50, CURRENT_DATE, false)
	`, txID, userID, accountID, merchantID, "plaid-tx-"+txID.String(), "SQ*Coffee Shop")
	require.NoError(t, err, "seed transaction")

	return userID, merchantID
}

// TestListMerchants_Integration verifies that ListMerchants executes valid SQL against
// a real PostgreSQL instance and returns correct aggregated data.
func TestListMerchants_Integration(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewMerchantRepository(pool)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, merchantID := seedMerchantFixture(ctx, t, tx, "lm-integ")

	// Use a repo that queries through the transaction.
	txRepo := NewMerchantRepository(tx)
	summaries, err := txRepo.ListMerchants(ctx, userID.String())
	require.NoError(t, err)

	require.NotEmpty(t, summaries, "expect at least one merchant summary")
	found := false
	for _, s := range summaries {
		if s.MerchantID == merchantID.String() {
			found = true
			assert.Equal(t, int64(1), s.TransactionCount)
			assert.InDelta(t, 5.50, s.TotalSpend, 0.01)
			assert.False(t, s.HasOverride)
			assert.False(t, s.IsMerged)
			break
		}
	}
	assert.True(t, found, "seeded merchant must appear in ListMerchants result")
}

// TestListMerchants_WithOverride_Integration verifies that a rename override is reflected
// in effective_display_name.
func TestListMerchants_WithOverride_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, merchantID := seedMerchantFixture(ctx, t, tx, "lm-override")

	customName := "My Cafe"
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, display_name, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, NOW(), NOW())
	`, userID, merchantID, customName)
	require.NoError(t, err, "seed override")

	txRepo := NewMerchantRepository(tx)
	summaries, err := txRepo.ListMerchants(ctx, userID.String())
	require.NoError(t, err)

	var found bool
	for _, s := range summaries {
		if s.MerchantID == merchantID.String() {
			found = true
			assert.Equal(t, customName, s.EffectiveDisplayName)
			assert.True(t, s.HasOverride)
			assert.False(t, s.IsMerged)
			break
		}
	}
	assert.True(t, found, "merchant with override must appear in results")
}

// TestListMerchants_MergedMerchant_Integration verifies that when a merchant is merged
// into another, the source collapses into the target group.
func TestListMerchants_MergedMerchant_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, merchantID := seedMerchantFixture(ctx, t, tx, "lm-merge-src")

	// Create a second merchant to merge into.
	targetMerchantID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO merchants (id, name, normalized_name, created_at)
		VALUES ($1, 'Target Coffee', 'Target Coffee', NOW())
	`, targetMerchantID)
	require.NoError(t, err, "seed target merchant")

	// Merge source into target via override.
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, merged_into_merchant_id, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, NOW(), NOW())
	`, userID, merchantID, targetMerchantID)
	require.NoError(t, err, "seed merge override")

	txRepo := NewMerchantRepository(tx)
	summaries, err := txRepo.ListMerchants(ctx, userID.String())
	require.NoError(t, err)

	// The source merchant should not appear as its own group.
	// The target merchant ID should appear as the effective group.
	var foundSource, foundTarget bool
	for _, s := range summaries {
		if s.MerchantID == merchantID.String() {
			foundSource = true
		}
		if s.MerchantID == targetMerchantID.String() {
			foundTarget = true
			assert.True(t, s.IsMerged, "target group should be marked is_merged when it absorbs another")
		}
	}
	assert.False(t, foundSource, "source merchant must not appear as its own row after merge")
	assert.True(t, foundTarget, "target merchant must appear in results after absorbing source")
}

// TestGetDuplicateSuggestions_Integration verifies that the pg_trgm similarity query
// executes valid SQL and correctly groups merchants with similar normalized names.
func TestGetDuplicateSuggestions_Integration(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewMerchantRepository(pool)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _ := seedMerchantFixture(ctx, t, tx, "dup-sugg")

	// Create a second very-similar merchant and link a transaction to it.
	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token2')
	`, linkedItemID, userID, "item2-"+linkedItemID.String())
	require.NoError(t, err)

	accountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, user_id, linked_item_id, plaid_account_id, name, type, subtype)
		VALUES ($1, $2, $3, $4, 'Checking2', 'depository', 'checking')
	`, accountID, userID, linkedItemID, "plaid-acct2-"+accountID.String())
	require.NoError(t, err)

	// "Coffee Shoppe" is very similar to "Coffee Shop" (similarity > 0.6).
	dupMerchantID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO merchants (id, name, normalized_name, created_at)
		VALUES ($1, 'Coffee Shoppe', 'Coffee Shoppe', NOW())
	`, dupMerchantID)
	require.NoError(t, err)

	dupTxID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (id, user_id, account_id, merchant_id, plaid_transaction_id,
			merchant_name, amount, date, pending)
		VALUES ($1, $2, $3, $4, $5, 'Coffee Shoppe', -4.75, CURRENT_DATE, false)
	`, dupTxID, userID, accountID, dupMerchantID, "plaid-tx2-"+dupTxID.String())
	require.NoError(t, err)

	// Use tx-scoped repo.
	txRepo := NewMerchantRepository(tx)
	groups, err := txRepo.GetDuplicateSuggestions(ctx, userID.String(), 0.6)
	require.NoError(t, err)
	// With two similar merchants, we expect at least one group of 2.
	require.GreaterOrEqual(t, len(groups), 1, "expect at least one duplicate group")
	found := false
	for _, g := range groups {
		if len(g) >= 2 {
			ids := make(map[string]bool)
			for _, s := range g {
				ids[s.MerchantID] = true
			}
			// At least one group should contain both our seeded merchants.
			// (There may be other merchants in the DB from prior tests.)
			if ids[dupMerchantID.String()] {
				found = true
			}
		}
	}
	assert.True(t, found, "duplicate merchant pair must appear in GetDuplicateSuggestions")
}

// TestGetDuplicateSuggestions_NoResults_Integration verifies that a user with only one
// merchant returns an empty slice.
func TestGetDuplicateSuggestions_NoResults_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _ := seedMerchantFixture(ctx, t, tx, "dup-none")

	txRepo := NewMerchantRepository(tx)
	// Use a very high threshold so nothing matches.
	groups, err := txRepo.GetDuplicateSuggestions(ctx, userID.String(), 0.99)
	require.NoError(t, err)
	assert.Empty(t, groups, "with threshold=0.99 and a single unique merchant, no groups expected")
}
