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

// seedIncompleteLedgerBudget seeds a USD budget with one FOOD_AND_DRINK line whose only matching
// expense fact carries an uninterpretable savings provenance, so the ledger withholds that line
// (Amount nil, Reason legacy_savings_origin) instead of publishing a figure it cannot trust.
func seedIncompleteLedgerBudget(t *testing.T, tx pgx.Tx, userID string) (budgetID, categoryID string) {
	t.Helper()
	ctx := context.Background()
	budgetID = seedActualsSyncBudget(t, tx, userID, 0)
	categoryID = seedActualsSyncCategory(t, tx, budgetID, "FOOD_AND_DRINK")

	linkedItemID := uuid.New().String()
	_, err := tx.Exec(ctx, `INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted) VALUES ($1, $2, $3, 'enc-token')`, linkedItemID, userID, "item-incomplete-"+linkedItemID)
	require.NoError(t, err, "seed linked item")

	accountID := uuid.New().String()
	_, err = tx.Exec(ctx, `INSERT INTO accounts (id, user_id, linked_item_id, plaid_account_id, name, type, currency_code) VALUES ($1, $2, $3, $4, 'Incomplete Ledger', 'depository', 'USD')`, accountID, userID, linkedItemID, accountID)
	require.NoError(t, err, "seed account")

	transactionID := uuid.New().String()
	_, err = tx.Exec(ctx, `INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, amount, name, date, tags, category_override_kind, pfc_primary_override) VALUES ($1, $2, $3, $4, 'manual', 100, 'Fixture', CURRENT_DATE, '[]'::jsonb, 'pfc', 'FOOD_AND_DRINK')`, transactionID, userID, accountID, linkedItemID)
	require.NoError(t, err, "seed transaction")

	goalID := uuid.New().String()
	_, err = tx.Exec(ctx, `INSERT INTO savings_goals (id, user_id, account_id, name, target_amount, progress_basis) VALUES ($1, $2, $3, 'Fixture', 1000, 'contributions')`, goalID, userID, accountID)
	require.NoError(t, err, "seed savings goal")

	// flow_kind is omitted on purpose: 000206 defaults it to legacy_unknown, which the expense-fact
	// CTE reports as legacy_savings_origin.
	_, err = tx.Exec(ctx, `INSERT INTO savings_goal_contributions (savings_goal_id, transaction_id, amount, source, status, occurred_on) VALUES ($1, $2, 20, 'manual', 'confirmed', CURRENT_DATE)`, goalID, transactionID)
	require.NoError(t, err, "seed contribution")
	return budgetID, categoryID
}

// TestCategoryTransactionsReturns422OnIncompleteLedger is the todo 11 failure path: the
// getBudgetCategoryTransactions barrier must carry the sentinel through the service and the
// handler, so an ambiguous ledger answers 422-with-reason instead of the hardcoded 500.
func TestCategoryTransactionsReturns422OnIncompleteLedger(t *testing.T) {
	pool := connectClaim1630DB(t)
	tx, err := pool.Begin(context.Background())
	require.NoError(t, err)
	defer tx.Rollback(context.Background()) //nolint:errcheck
	ctx := context.Background()

	userID := seedClaim1630User(t, tx)
	budgetID, categoryID := seedIncompleteLedgerBudget(t, tx, userID)
	handler := newActualsSyncHandler(t, tx)

	out, err := handler.GetCategoryTransactions(ctx, &GetCategoryTransactionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 budgetID,
		CategoryID:         categoryID,
		Limit:              20,
	})
	require.Nil(t, out)
	statusErr := budgetStatusErr(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, statusErr.GetStatus(),
		"an incomplete ledger must map to 422, not the hardcoded 500")
	assert.Contains(t, statusErr.Error(), "legacy_savings_origin",
		"the 422 must carry the actionable reason")
	assert.NotContains(t, statusErr.Error(), "failed to get category transactions",
		"the hardcoded 500 message must be gone")
}