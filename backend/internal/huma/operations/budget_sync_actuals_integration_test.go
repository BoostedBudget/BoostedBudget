//go:build integration

package operations

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issue #1685 todo 10: SyncBudget must route a FATAL actuals barrier through
// LoggingMapError (422 with the reason) and return 2xx for a degraded barrier,
// with the reasons readable from the re-fetched budget.

// newActualsSyncHandler wires the REAL budget service + REAL sync service against
// the given transaction, so the handler -> service -> repository -> mapper chain
// actually runs.
func newActualsSyncHandler(t *testing.T, tx pgx.Tx) *BudgetHandler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	budgetRepo := repository.NewBudgetRepository(tx)
	customCategoryRepo := repository.NewCustomCategoryRepository(tx)
	budgetSvc := services.NewBudgetService(budgetRepo, customCategoryRepo, nil, logger)
	txnRepo := repository.NewTransactionRepository(tx, newHappyPathFieldEncryptor(t))
	incomeSvc := services.NewBudgetIncomeService(txnRepo.(services.IncomeTransactionGetter), logger)
	matchingSvc := services.NewBudgetCategoryMatchingService(txnRepo)
	syncSvc := services.NewBudgetSyncService(budgetRepo, incomeSvc, matchingSvc, logger)
	return NewBudgetHandler(budgetSvc, syncSvc, services.NewTransactionService(txnRepo, customCategoryRepo))
}

// seedActualsSyncBudget inserts a USD budget owned by userID with the given
// income_amount and returns its id.
func seedActualsSyncBudget(t *testing.T, tx pgx.Tx, userID string, incomeAmount float64) string {
	t.Helper()
	ctx := context.Background()
	budgetID := uuid.New().String()
	now := time.Now()
	_, err := tx.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, expected_income, currency_code, is_active, income_amount, created_at, updated_at, series_id)
		VALUES ($1, $2, 'Actuals Sync', 'monthly', $3, 1000, 1000, 'USD', true, $4, $5, $5, $1)
	`, budgetID, userID, now, incomeAmount, now)
	require.NoError(t, err, "seed budget")
	return budgetID
}

// seedActualsSyncCategory inserts a PFC budget line for pfcPrimary.
func seedActualsSyncCategory(t *testing.T, tx pgx.Tx, budgetID, pfcPrimary string) string {
	t.Helper()
	ctx := context.Background()
	categoryID := uuid.New().String()
	now := time.Now()
	_, err := tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, pfc_primary, allocated_amount, spent_amount, created_at, updated_at, tags, match_logic)
		VALUES ($1, $2, 'pfc', $3, 100, 7, $4, $4, '[]'::jsonb, 'category_only')
	`, categoryID, budgetID, pfcPrimary, now)
	require.NoError(t, err, "seed category")
	return categoryID
}

// seedActualsSyncTransaction inserts a posted expense fact on an account in the
// given currency, tagged with a PFC override so it matches the budget line.
func seedActualsSyncTransaction(t *testing.T, tx pgx.Tx, userID, linkedItemID, pfcPrimary, currency string) {
	t.Helper()
	ctx := context.Background()
	accountID := uuid.New().String()
	_, err := tx.Exec(ctx, `
		INSERT INTO accounts (id, user_id, linked_item_id, plaid_account_id, name, type, currency_code)
		VALUES ($1, $2, $3, $4, 'Actuals Sync', 'depository', $5)
	`, accountID, userID, linkedItemID, accountID, currency)
	require.NoError(t, err, "seed account")

	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, amount, name, date, tags, category_override_kind, pfc_primary_override)
		VALUES ($1, $2, $3, $4, 'manual', 100, 'Fixture', CURRENT_DATE, '[]'::jsonb, 'pfc', $5)
	`, uuid.New().String(), userID, accountID, linkedItemID, pfcPrimary)
	require.NoError(t, err, "seed transaction")
}

// TestSyncBudgetReturns2xxOnDegradedActuals is the todo 10 happy path: a
// currency-mismatch ledger withholds the publish, but the handler returns 2xx and
// the re-fetched budget carries the reason and the prior income.
func TestSyncBudgetReturns2xxOnDegradedActuals(t *testing.T) {
	pool := connectClaim1630DB(t)
	tx, err := pool.Begin(context.Background())
	require.NoError(t, err)
	defer tx.Rollback(context.Background()) //nolint:errcheck
	ctx := context.Background()

	userID := seedClaim1630User(t, tx)
	budgetID := seedActualsSyncBudget(t, tx, userID, 200)
	seedActualsSyncCategory(t, tx, budgetID, "FOOD_AND_DRINK")

	linkedItemID := uuid.New().String()
	_, err = tx.Exec(ctx, `INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted) VALUES ($1, $2, $3, 'enc-token')`, linkedItemID, userID, "item-actuals-"+linkedItemID)
	require.NoError(t, err, "seed linked item")
	seedActualsSyncTransaction(t, tx, userID, linkedItemID, "FOOD_AND_DRINK", "EUR")

	handler := newActualsSyncHandler(t, tx)

	out, err := handler.SyncBudget(ctx, &SyncBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 budgetID,
	})
	require.NoError(t, err, "a degraded barrier must not fail the request")
	require.NotNil(t, out)
	assert.Contains(t, out.Body.ActualsIncompleteReasons, "currency_mismatch",
		"the re-fetched budget must carry the persisted reason")
	assert.Equal(t, 200.0, out.Body.IncomeAmount,
		"a withheld sync must return the prior income, not the zeroed base")
}

// TestSyncBudgetFatalReturns422WithReason is the todo 10 failure path: a FATAL
// barrier must map through LoggingMapError to 422 with the reason, not an opaque
// 500.
func TestSyncBudgetFatalReturns422WithReason(t *testing.T) {
	pool := connectClaim1630DB(t)
	tx, err := pool.Begin(context.Background())
	require.NoError(t, err)
	defer tx.Rollback(context.Background()) //nolint:errcheck
	ctx := context.Background()

	userID := seedClaim1630User(t, tx)
	budgetID := seedActualsSyncBudget(t, tx, userID, 0)
	_, err = tx.Exec(ctx, `UPDATE budgets SET income_sources='[{"type":"manual","value":"not-a-number"}]'::jsonb WHERE id=$1`, budgetID)
	require.NoError(t, err, "seed invalid manual income source")

	handler := newActualsSyncHandler(t, tx)

	out, err := handler.SyncBudget(ctx, &SyncBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 budgetID,
	})
	require.Nil(t, out)
	statusErr := budgetStatusErr(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, statusErr.GetStatus(),
		"a FATAL actuals barrier must map to 422, not 500")
	assert.Contains(t, statusErr.Error(), "invalid_manual_income",
		"the 422 must carry the actionable reason")
	assert.NotContains(t, statusErr.Error(), "Failed to sync budget",
		"the hardcoded 500 message must be gone")
}