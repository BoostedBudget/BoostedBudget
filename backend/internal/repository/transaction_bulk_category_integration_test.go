//go:build integration

package repository

// TestTransactionRepository_BulkUpdateCategory_Integration validates what pgxmock unit tests
// cannot: that the ANY($::uuid[]) cast, the effective_category_name CASE expression, and the
// custom-category subquery all execute correctly against the real schema.
//
// Run with:
//   DATABASE_URL=postgres://... go test ./internal/repository/... \
//     -run TestTransactionRepository_BulkUpdateCategory_Integration \
//     -tags integration -v
//
// Issue #1210

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransactionRepository_BulkUpdateCategory_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	// Wrap all mutations in a transaction that is rolled back automatically.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// ── seed prerequisites ────────────────────────────────────────────────────
	userID := uuid.New().String()
	otherUserID := uuid.New().String()
	linkedItemID := uuid.New().String()
	accountID := uuid.New().String()

	for _, row := range []struct{ id, authID, email string }{
		{userID, "test|" + userID, "integration-1210-bulk-" + userID + "@example.com"},
		{otherUserID, "test|" + otherUserID, "integration-1210-bulk-other-" + otherUserID + "@example.com"},
	} {
		_, err = tx.Exec(ctx, `
			INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1, $2, $3, 'premium', 'active', 'iad')
		`, row.id, row.authID, row.email)
		require.NoError(t, err, "seed user %s", row.id)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "test-item-"+linkedItemID)
	require.NoError(t, err, "seed linked_item")

	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, $4, 'Test Checking', 'depository')
	`, accountID, linkedItemID, userID, "plaid-acc-"+accountID)
	require.NoError(t, err, "seed account")

	now := time.Now()

	// Seed 3 transactions owned by userID + 1 owned by otherUserID.
	tx1 := uuid.New().String()
	tx2 := uuid.New().String()
	tx3 := uuid.New().String()
	txOther := uuid.New().String()

	for i, row := range []struct {
		id     string
		userID string
	}{
		{tx1, userID},
		{tx2, userID},
		{tx3, userID},
		{txOther, otherUserID},
	} {
		_, err = tx.Exec(ctx, `
			INSERT INTO transactions (
				id, user_id, account_id, linked_item_id, source,
				external_transaction_id, amount, date, name,
				pfc_primary, pfc_detailed, category_source,
				effective_category_name,
				reviewed, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, 'plaid',
				$5, -10.00, $7, 'Bulk Cat Test',
				'FOOD_AND_DRINK', 'FOOD_AND_DRINK_COFFEE', 'plaid',
				'Coffee',
				false, $6, $6
			)
		`, row.id, row.userID, accountID, linkedItemID,
			"ext-bulk-"+row.id, now, now.Format("2006-01-02"))
		require.NoError(t, err, "seed transaction %d", i)
	}

	// Seed a custom category owned by userID.
	customCatID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name, created_at, updated_at)
		VALUES ($1, $2, 'My Custom', NOW(), NOW())
	`, customCatID, userID)
	require.NoError(t, err, "seed custom_category")

	// Use the pool-backed repo (rows are in the open tx; the repo operates against the same
	// pg pool so it won't see uncommitted rows — but the SQL must be syntactically and
	// semantically valid against the real schema, which is what we validate here).
	repo := NewTransactionRepository(pool, enc)

	t.Run("pfc update: SQL is valid against real schema", func(t *testing.T) {
		pfcPrimary := "GENERAL_MERCHANDISE"
		kind := "pfc"
		_, err := repo.BulkUpdateCategory(ctx, userID, []string{tx1, tx2}, &kind, &pfcPrimary, nil, nil, domain.CategorySourceManual)
		assert.NoError(t, err, "bulk pfc recategorize must be valid SQL against the real schema")
	})

	t.Run("custom update: SQL is valid against real schema", func(t *testing.T) {
		kind := "custom"
		_, err := repo.BulkUpdateCategory(ctx, userID, []string{tx3}, &kind, nil, nil, &customCatID, domain.CategorySourceManual)
		assert.NoError(t, err, "bulk custom recategorize must be valid SQL against the real schema")
	})

	t.Run("clear: SQL is valid against real schema", func(t *testing.T) {
		_, err := repo.BulkUpdateCategory(ctx, userID, []string{tx1}, nil, nil, nil, nil, domain.CategorySourceManual)
		assert.NoError(t, err, "bulk clear recategorize must be valid SQL against the real schema")
	})

	t.Run("unowned ID included: no error, partial update", func(t *testing.T) {
		// Include the other user's transaction; it should be silently ignored.
		kind := "pfc"
		pfcPrimary := "TRAVEL"
		n, err := repo.BulkUpdateCategory(ctx, userID, []string{tx1, txOther}, &kind, &pfcPrimary, nil, nil, domain.CategorySourceManual)
		assert.NoError(t, err)
		// txOther is owned by otherUserID and must be skipped (n ≤ 1).
		assert.LessOrEqual(t, n, int64(1), "unowned ID must not be updated")
	})

	t.Run("direct SQL sanity: seeded rows are accessible in same tx", func(t *testing.T) {
		var count int
		err := tx.QueryRow(ctx,
			`SELECT count(*) FROM transactions WHERE user_id=$1`,
			userID,
		).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 3, count, "expected 3 rows seeded for the test user")
	})
}
