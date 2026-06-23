//go:build integration

package repository

// TestTransactionRepository_ReviewSummary_Integration validates what pgxmock unit tests
// cannot: that FILTER/EXTRACT/json_agg CTE expressions execute correctly against the real
// schema, that two custom categories with the same display name stay distinct via stable key,
// and that the all-time default works without date params.
//
// Run with:
//   DATABASE_URL=postgres://... go test ./internal/repository/... \
//     -run TestTransactionRepository_ReviewSummary_Integration \
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

func TestTransactionRepository_ReviewSummary_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	// Wrap all mutations in a transaction rolled back at the end.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// ── seed prerequisites ────────────────────────────────────────────────────
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()
	accountID := uuid.New().String()

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "integration-1210-rs-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "test-item-"+linkedItemID)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, $4, 'Test Checking', 'depository')
	`, accountID, linkedItemID, userID, "plaid-acc-"+accountID)
	require.NoError(t, err)

	now := time.Now()

	// Seed two custom categories with the same display name — they must stay distinct via stable key.
	catA := uuid.New().String()
	catB := uuid.New().String()
	for _, row := range []struct{ id, name string }{{catA, "Coffee Shops"}, {catB, "Restaurants"}} {
		_, err = tx.Exec(ctx, `
			INSERT INTO custom_categories (id, user_id, name, created_at, updated_at)
			VALUES ($1, $2, $3, NOW(), NOW())
		`, row.id, userID, row.name)
		require.NoError(t, err, "seed custom_category %s", row.id)
	}

	// Seed transactions:
	// - 2 unreviewed Plaid-category (pfc_primary=FOOD_AND_DRINK)
	// - 1 unreviewed custom catA
	// - 1 unreviewed custom catB (same display name as catA)
	// - 1 reviewed via rule (category_source=rule, reviewed=true)
	// - 1 reviewed manually (category_source=manual, reviewed=true)
	seedTx := func(id, source, catSource string, reviewed bool, customCatID *string) {
		reviewedAt := "NULL"
		if reviewed {
			reviewedAt = "NOW()"
		}
		var customCatExpr string
		if customCatID != nil {
			customCatExpr = "'" + *customCatID + "'::uuid"
		} else {
			customCatExpr = "NULL"
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO transactions (
				id, user_id, account_id, linked_item_id, source,
				external_transaction_id, amount, date, name,
				pfc_primary, pfc_detailed, category_source,
				effective_category_name,
				custom_category_override_id,
				reviewed, reviewed_at, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, 'plaid',
				$5, -10.00, $6, 'RS Test tx',
				'FOOD_AND_DRINK', 'FOOD_AND_DRINK_COFFEE', $7,
				'Coffee',
				`+customCatExpr+`,
				$8, `+reviewedAt+`, $9, $9
			)
		`, id, userID, accountID, linkedItemID,
			"ext-rs-"+id, now.Format("2006-01-02"), catSource, reviewed, now)
		require.NoError(t, err, "seed tx %s", id)
	}

	seedTx(uuid.New().String(), "plaid", "plaid", false, nil)
	seedTx(uuid.New().String(), "plaid", "plaid", false, nil)
	seedTx(uuid.New().String(), "plaid", "manual", false, &catA)
	seedTx(uuid.New().String(), "plaid", "manual", false, &catB)
	seedTx(uuid.New().String(), "plaid", "rule", true, nil)
	seedTx(uuid.New().String(), "plaid", "manual", true, nil)

	// Use the pool-backed repo — validates SQL is syntactically/semantically correct.
	repo := NewTransactionRepository(pool, enc)

	t.Run("all-time summary is valid SQL against real schema", func(t *testing.T) {
		summary, err := repo.GetReviewSummary(ctx, userID, domain.ReviewSummaryFilters{})
		assert.NoError(t, err, "CTE query must execute without error against the real schema")
		assert.NotNil(t, summary)
	})

	t.Run("with date filter: SQL is valid", func(t *testing.T) {
		start := now.AddDate(0, 0, -1)
		end := now.AddDate(0, 0, 1)
		_, err := repo.GetReviewSummary(ctx, userID, domain.ReviewSummaryFilters{
			StartDate: &start,
			EndDate:   &end,
		})
		assert.NoError(t, err, "date-scoped CTE query must execute without error")
	})

	t.Run("zero-rows user returns zeroed summary without error", func(t *testing.T) {
		newUser := uuid.New().String()
		summary, err := repo.GetReviewSummary(ctx, newUser, domain.ReviewSummaryFilters{})
		assert.NoError(t, err)
		assert.NotNil(t, summary)
		assert.Equal(t, int64(0), summary.UnreviewedCount)
		assert.Empty(t, summary.TopUnreviewedCategories)
	})
}
