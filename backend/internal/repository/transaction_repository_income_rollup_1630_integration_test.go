//go:build integration

package repository

// Issue #1630, Task 6 — the income-side roll-up hole.
//
// GetIncomeSumByCategory matched effective_pfc_primary directly and did NOT roll up custom
// categories nested under that primary (via custom_categories.parent_pfc_primary), even though
// the spend-side buildCategoryClause ("pfc" branch, #1203) does exactly that roll-up. A user who
// created a custom category nested under INCOME had that income invisible to a pfc_primary/INCOME
// income source, while the equivalent spend category would have been counted.
//
// pgxmock cannot catch this: the pre-fix query was syntactically valid and any pgxmock test only
// asserts the literal SQL string/args, never executes real roll-up semantics against real rows.
// This test seeds one real income transaction filed under a custom category whose
// parent_pfc_primary = 'INCOME', and asserts GetIncomeSumByCategory("INCOME") counts it.
//
// Run with:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=... REDIS_TEST_URL=... \
//	  go test ./internal/repository/... -tags integration -p 1 \
//	  -run TestGetIncomeSumByCategory_RollsUpCustomChildOfPFCPrimary -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetIncomeSumByCategory_RollsUpCustomChildOfPFCPrimary(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "claim1630-t6-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-token')
	`, linkedItemID, userID, "item-claim1630-t6-"+linkedItemID)
	require.NoError(t, err, "seed linked item")

	accountID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-claim1630-t6', 'Checking', 'depository')
	`, accountID, linkedItemID, userID)
	require.NoError(t, err, "seed account")

	// A custom category nested under the INCOME pfc_primary — e.g. "Freelance Income".
	customCatID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary)
		VALUES ($1, $2, 'Freelance Income', 'INCOME')
	`, customCatID, userID)
	require.NoError(t, err, "seed custom category nested under INCOME")

	periodStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.January, 31, 23, 59, 59, 0, time.UTC)
	depositDate := time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC)

	// A direct INCOME deposit ($1000) plus a deposit filed under the custom child ($750).
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, source, amount, date, name, pending, pfc_primary)
		VALUES ($1, $2, $3, $4, 'plaid', -1000.00, $5, 'Paycheck', false, 'INCOME')
	`, uuid.New().String(), userID, accountID, linkedItemID, depositDate)
	require.NoError(t, err, "seed direct INCOME deposit")

	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, source, amount, date, name, pending,
			 category_override_kind, custom_category_override_id)
		VALUES ($1, $2, $3, $4, 'plaid', -750.00, $5, 'Freelance client payment', false,
			'custom', $6)
	`, uuid.New().String(), userID, accountID, linkedItemID, depositDate, customCatID)
	require.NoError(t, err, "seed custom-category-filed deposit under INCOME's child")

	enc := newTestFieldEncryptor(t)
	repo := NewTransactionRepository(tx, enc)

	sum, err := repo.GetIncomeSumByCategory(ctx, userID, "INCOME", periodStart, periodEnd)
	require.NoError(t, err)
	assert.Equal(t, 1750.00, sum,
		"#1630: a pfc_primary/INCOME income source must roll up custom categories nested "+
			"under INCOME (parent_pfc_primary='INCOME'), the same roll-up the spend side "+
			"applies via buildCategoryClause's \"pfc\" branch — direct $1000 + custom-child "+
			"$750 = $1750")
}
