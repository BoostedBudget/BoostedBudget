//go:build integration

package operations

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFatalActualsBarrierReturns422 pins both FATAL barriers end-to-end through the
// handler -> service -> repository -> mapper chain: each maps to 422 with a reason
// naming the problem, and the whole publish rolls back — the mutated rows,
// income_amount and the persisted reasons are all untouched.
//
// The two FATAL barriers are the observed-income currency mismatch
// (budget_actuals_repository.go:109-110) and invalid manual income (:119-120). Both
// are reachable only on the /sync path, which passes the budget's income sources to
// the publish; a config write passes nil sources.
func TestFatalActualsBarrierReturns422(t *testing.T) {
	t.Run("invalid manual income", func(t *testing.T) {
		pool := connectClaim1630DB(t)
		tx, err := pool.Begin(context.Background())
		require.NoError(t, err)
		defer tx.Rollback(context.Background()) //nolint:errcheck
		ctx := context.Background()

		userID := seedClaim1630User(t, tx)
		budgetID := seedActualsSyncBudget(t, tx, userID, 200)
		categoryID := seedActualsSyncCategory(t, tx, budgetID, "FOOD_AND_DRINK")
		_, err = tx.Exec(ctx, `UPDATE budgets SET income_sources='[{"type":"manual","value":"not-a-number"}]'::jsonb, actuals_incomplete_reasons='{"prior_reason"}' WHERE id=$1`, budgetID)
		require.NoError(t, err, "seed invalid manual income source and a prior diagnostic")

		handler := newActualsSyncHandler(t, tx)
		out, err := handler.SyncBudget(ctx, &SyncBudgetInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
			ID:                 budgetID,
		})
		require.Nil(t, out)
		statusErr := budgetStatusErr(t, err)
		assert.Equal(t, http.StatusUnprocessableEntity, statusErr.GetStatus(),
			"a FATAL actuals barrier must map to 422, not 500")
		assert.NotEqual(t, http.StatusOK, statusErr.GetStatus(), "a FATAL barrier must not report success")
		assert.Contains(t, statusErr.Error(), "invalid_manual_income",
			"the 422 must carry the actionable reason")

		requireFatalRollback(t, tx, budgetID, categoryID, 200, 7)
	})

	t.Run("observed income currency mismatch", func(t *testing.T) {
		pool := connectClaim1630DB(t)
		tx, err := pool.Begin(context.Background())
		require.NoError(t, err)
		defer tx.Rollback(context.Background()) //nolint:errcheck
		ctx := context.Background()

		userID := seedClaim1630User(t, tx)
		budgetID := seedActualsSyncBudget(t, tx, userID, 200)
		categoryID := seedActualsSyncCategory(t, tx, budgetID, "FOOD_AND_DRINK")
		_, err = tx.Exec(ctx, `UPDATE budgets SET income_sources='[{"type":"pfc_primary","value":"INCOME"}]'::jsonb, actuals_incomplete_reasons='{"prior_reason"}' WHERE id=$1`, budgetID)
		require.NoError(t, err, "seed a pfc_primary income source and a prior diagnostic")

		linkedItemID := uuid.New().String()
		_, err = tx.Exec(ctx, `INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted) VALUES ($1, $2, $3, 'enc-token')`, linkedItemID, userID, "item-fatal-"+linkedItemID)
		require.NoError(t, err, "seed linked item")
		seedForeignIncomeTransaction(t, tx, userID, linkedItemID, "EUR")

		handler := newActualsSyncHandler(t, tx)
		out, err := handler.SyncBudget(ctx, &SyncBudgetInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
			ID:                 budgetID,
		})
		require.Nil(t, out)
		statusErr := budgetStatusErr(t, err)
		assert.Equal(t, http.StatusUnprocessableEntity, statusErr.GetStatus(),
			"a FATAL actuals barrier must map to 422, not 500")
		assert.NotEqual(t, http.StatusOK, statusErr.GetStatus(), "a FATAL barrier must not report success")
		assert.Contains(t, statusErr.Error(), "mixed_currency",
			"the 422 must carry the actionable reason")

		requireFatalRollback(t, tx, budgetID, categoryID, 200, 7)
	})
}

// seedForeignIncomeTransaction inserts a posted income fact (amount < 0) on an
// account in the given currency, tagged with a PFC override so the budget's
// pfc_primary income source observes it.
func seedForeignIncomeTransaction(t *testing.T, tx pgx.Tx, userID, linkedItemID, currency string) {
	t.Helper()
	ctx := context.Background()
	accountID := uuid.New().String()
	_, err := tx.Exec(ctx, `
		INSERT INTO accounts (id, user_id, linked_item_id, plaid_account_id, name, type, currency_code)
		VALUES ($1, $2, $3, $4, 'Foreign Income', 'depository', $5)
	`, accountID, userID, linkedItemID, accountID, currency)
	require.NoError(t, err, "seed foreign account")

	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, amount, name, date, tags, category_override_kind, pfc_primary_override)
		VALUES ($1, $2, $3, $4, 'manual', -80, 'Income', CURRENT_DATE, '[]'::jsonb, 'pfc', 'INCOME')
	`, uuid.New().String(), userID, accountID, linkedItemID)
	require.NoError(t, err, "seed foreign income transaction")
}

// requireFatalRollback asserts a FATAL publish wrote nothing: the category's
// spent_amount, the budget's income_amount and its persisted reasons are all the
// values the test seeded before the request.
func requireFatalRollback(t *testing.T, tx pgx.Tx, budgetID, categoryID string, wantIncome, wantSpent float64) {
	t.Helper()
	ctx := context.Background()

	var spent float64
	require.NoError(t, tx.QueryRow(ctx, `SELECT spent_amount FROM budget_categories WHERE id=$1`, categoryID).Scan(&spent))
	require.Equal(t, wantSpent, spent, "a FATAL barrier must not write spent_amount")

	var income float64
	var reasons []string
	require.NoError(t, tx.QueryRow(ctx, `SELECT income_amount, actuals_incomplete_reasons FROM budgets WHERE id=$1`, budgetID).Scan(&income, &reasons))
	require.Equal(t, wantIncome, income, "a FATAL barrier must not write income_amount")
	require.Equal(t, []string{"prior_reason"}, reasons, "a FATAL barrier must not persist reasons")
}