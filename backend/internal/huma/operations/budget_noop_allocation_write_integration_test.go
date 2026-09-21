//go:build integration

package operations

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/require"
)

// TestNoOpAllocationWriteSucceedsOnIncompleteActuals is the issue's exact
// reproduction: a no-op allocation write (215 -> 215) on an ambiguous budget
// must return 2xx, leave the value unchanged, persist the reason, and leave
// income_amount unchanged. Before the barrier taxonomy this returned an opaque
// 500 ("Internal server error").
//
// It is a regression guard, not a bug fix: a config write passes nil income
// sources to the publish, so observed income is never accumulated on this path
// (budget_accounting_inputs.go requires len(sources) > 0). The guard's value is
// that it fails if the write path starts returning 500 again.
func TestNoOpAllocationWriteSucceedsOnIncompleteActuals(t *testing.T) {
	pool := connectClaim1630DB(t)
	tx, err := pool.Begin(context.Background())
	require.NoError(t, err)
	defer tx.Rollback(context.Background()) //nolint:errcheck
	ctx := context.Background()

	userID := seedClaim1630User(t, tx)
	budgetID := seedActualsSyncBudget(t, tx, userID, 215)
	categoryID := seedActualsSyncCategory(t, tx, budgetID, "FOOD_AND_DRINK")
	_, err = tx.Exec(ctx, `UPDATE budget_categories SET allocated_amount=215 WHERE id=$1`, categoryID)
	require.NoError(t, err, "seed the issue's 215 allocation")

	linkedItemID := uuid.New().String()
	_, err = tx.Exec(ctx, `INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted) VALUES ($1, $2, $3, 'enc-token')`, linkedItemID, userID, "item-noop-"+linkedItemID)
	require.NoError(t, err, "seed linked item")
	seedActualsSyncTransaction(t, tx, userID, linkedItemID, "FOOD_AND_DRINK", "USD")
	_, err = tx.Exec(ctx, `UPDATE transactions SET tags='["shared"]'::jsonb WHERE user_id=$1`, userID)
	require.NoError(t, err, "tag the fact so a second line can overlap it")
	_, err = tx.Exec(ctx, `INSERT INTO budget_categories (id, budget_id, category_kind, pfc_primary, allocated_amount, spent_amount, created_at, updated_at, tags, match_logic) VALUES ($1,$2,'pfc','TRAVEL',100,7,$3,$3,'["shared"]'::jsonb,'tag_only')`, uuid.New().String(), budgetID, time.Now())
	require.NoError(t, err, "seed the overlapping tag_only line")

	handler := newActualsSyncHandler(t, tx)

	amount := 215.0
	out, err := handler.UpdateCategory(ctx, &UpdateBudgetCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 budgetID,
		CategoryID:         categoryID,
		Body:               domain.UpdateBudgetCategoryRequest{AllocatedAmount: &amount},
	})
	if err != nil {
		statusErr := budgetStatusErr(t, err)
		require.NotEqual(t, http.StatusInternalServerError, statusErr.GetStatus(),
			"the issue's no-op repro must not return 500")
		require.NotContains(t, statusErr.Error(), "Internal server error",
			"the issue's no-op repro must not return the opaque 500 body")
	}
	require.NoError(t, err, "a no-op allocation write on an ambiguous budget must not fail")
	require.NotNil(t, out)
	require.Equal(t, 215.0, out.Body.AllocatedAmount, "the value is unchanged")

	var persistedAllocation, income float64
	var reasons []string
	require.NoError(t, tx.QueryRow(ctx, `SELECT allocated_amount FROM budget_categories WHERE id=$1`, categoryID).Scan(&persistedAllocation))
	require.Equal(t, 215.0, persistedAllocation, "the value is persisted unchanged")
	require.NoError(t, tx.QueryRow(ctx, `SELECT income_amount, actuals_incomplete_reasons FROM budgets WHERE id=$1`, budgetID).Scan(&income, &reasons))
	require.Equal(t, 215.0, income, "a config write must not add observed income")
	require.Contains(t, reasons, "overlapping_individual_lines", "the reason is persisted")
}
