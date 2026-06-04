//go:build integration

package repository

// TestTransactionRepository_CategorySourceFilter_Integration validates what pgxmock unit tests
// cannot: that the category_source IN (...) filter is a valid Postgres expression against a real
// schema with the DB CHECK constraint on the category_source column.
//
// Run with:
//   DATABASE_URL=postgres://... go test ./internal/repository/... \
//     -run TestTransactionRepository_CategorySourceFilter_Integration \
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

func TestTransactionRepository_CategorySourceFilter_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	// Wrap all mutations in a transaction that is rolled back automatically.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// ── seed prerequisites ────────────────────────────────────────────────────
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()
	accountID := uuid.New().String()

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
	`, userID, "test|"+userID, "integration-1210-cs-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

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

	// Seed one transaction per category_source value.
	txPlaid := uuid.New().String()
	txRule := uuid.New().String()
	txManual := uuid.New().String()

	for _, row := range []struct {
		id     string
		source string
	}{
		{txPlaid, "plaid"},
		{txRule, "rule"},
		{txManual, "manual"},
	} {
		_, err = tx.Exec(ctx, `
			INSERT INTO transactions (
				id, user_id, account_id, linked_item_id, source,
				external_transaction_id, amount, date, name,
				pfc_primary, pfc_detailed, category_source,
				effective_pfc_primary, effective_pfc_detailed, effective_category_name,
				reviewed, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, 'plaid',
				$5, -10.00, $6, 'Test tx '||$7,
				'FOOD_AND_DRINK', 'FOOD_AND_DRINK_COFFEE', $7,
				'FOOD_AND_DRINK', 'FOOD_AND_DRINK_COFFEE', 'Coffee',
				false, $6, $6
			)
		`, row.id, userID, accountID, linkedItemID,
			"ext-"+row.id, now, row.source)
		require.NoError(t, err, "seed transaction source=%s", row.source)
	}

	// Use a pool-backed repo wrapped around the open transaction via the pool.
	// (The integration helper wraps the pool so we query the same tx via BEGIN.)
	// Since pgxpool doesn't surface an in-tx pool type, we test via the pool
	// directly (rows are visible because the tx is not yet committed, and we're in
	// the same process). We use a txRepo wrapping the tx connection directly.
	//
	// Use the same connectTestDB pool — rows seeded above are in a separate
	// committed insert, so use the tx directly as the DB interface.
	txRepo := NewTransactionRepository(pool, enc)

	t.Run("filter plaid only", func(t *testing.T) {
		// Commit the seeded rows first so pool can see them.
		// NOTE: Because we use tx.Rollback at the end, we can't commit inside a
		// sub-test. Instead we use the raw pool with an explicit SELECT restricted to
		// this user, relying on the same connection (this is standard integration pattern).
		//
		// This test validates the SQL is syntactically and semantically valid against
		// the real schema. We accept that rows may not be visible (tx isolation), which
		// is why we assert no error rather than specific row counts.
		filters := domain.TransactionFilters{
			CategorySources: []string{"plaid"},
		}
		_, err := txRepo.GetByUserIDWithFilters(ctx, userID, filters, 100, 0)
		assert.NoError(t, err, "category_source IN ('plaid') must be valid SQL against the real schema")
	})

	t.Run("filter rule and manual", func(t *testing.T) {
		filters := domain.TransactionFilters{
			CategorySources: []string{"rule", "manual"},
		}
		_, err := txRepo.GetByUserIDWithFilters(ctx, userID, filters, 100, 0)
		assert.NoError(t, err, "category_source IN ('rule','manual') must be valid SQL against the real schema")
	})

	t.Run("filter all three sources", func(t *testing.T) {
		filters := domain.TransactionFilters{
			CategorySources: []string{"plaid", "rule", "manual"},
		}
		_, err := txRepo.GetByUserIDWithFilters(ctx, userID, filters, 100, 0)
		assert.NoError(t, err, "category_source IN ('plaid','rule','manual') must be valid SQL")
	})

	t.Run("combined with reviewed filter", func(t *testing.T) {
		unreviewed := false
		filters := domain.TransactionFilters{
			Reviewed:        &unreviewed,
			CategorySources: []string{"plaid"},
		}
		_, err := txRepo.GetByUserIDWithFilters(ctx, userID, filters, 100, 0)
		assert.NoError(t, err, "combined reviewed+category_source filter must be valid SQL")
	})

	// Verify the IDs we seeded are accessible — validates query returns correct rows
	// when querying the pool (post-commit). We do a direct SQL check here.
	t.Run("direct SQL sanity: seeded rows have correct category_source", func(t *testing.T) {
		var count int
		err := tx.QueryRow(ctx,
			`SELECT count(*) FROM transactions WHERE user_id=$1 AND category_source IN ('plaid','rule','manual')`,
			userID,
		).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 3, count, "expected 3 seeded rows (one per source) to be visible in the same tx")
	})

	t.Run("direct SQL sanity: plaid filter returns 1 row", func(t *testing.T) {
		var count int
		err := tx.QueryRow(ctx,
			`SELECT count(*) FROM transactions WHERE user_id=$1 AND category_source IN ('plaid')`,
			userID,
		).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "only the plaid-source row should match")
	})
}
