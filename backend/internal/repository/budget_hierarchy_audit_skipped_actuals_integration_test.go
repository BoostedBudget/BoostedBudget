//go:build integration

package repository

// Issue #1685 todo 12: AuditBudgetHierarchy must record a budget whose ledger is incomplete with a
// NAMED outcome instead of rolling its own work back and failing the job. The audit's recompute
// loop is the only place the sentinel can surface, and the repair loop runs strictly after it — so
// a sentinel discards every recomputed candidate and applies no partial repair.
//
// Run with:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=... REDIS_TEST_URL=... \
//	  go test ./internal/repository/... -tags integration -p 1 \
//	  -run 'TestAuditRecordsSkippedIncompleteActualsOutcome|TestAuditHealthyBudgetUnchanged' -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// seedAuditFixtureUser seeds a user, linked item and USD account, returning their ids.
func seedAuditFixtureUser(t *testing.T, tx pgx.Tx) (userID, linkedItemID, accountID string) {
	t.Helper()
	ctx := context.Background()
	userID = uuid.New().String()
	_, err := tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "audit-skip-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID = uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-token')
	`, linkedItemID, userID, "item-audit-skip-"+linkedItemID)
	require.NoError(t, err, "seed linked item")

	accountID = uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, currency_code)
		VALUES ($1, $2, $3, $4, 'Checking', 'depository', 'USD')
	`, accountID, linkedItemID, userID, "plaid-audit-skip-"+accountID)
	require.NoError(t, err, "seed account")
	return userID, linkedItemID, accountID
}

// seedAuditFixtureBudget inserts a monthly USD budget anchored on periodStart. The name carries a
// uuid because budgets are unique per (user, name) and one test seeds more than one.
func seedAuditFixtureBudget(t *testing.T, tx pgx.Tx, userID string, periodStart time.Time) *domain.Budget {
	t.Helper()
	budget := &domain.Budget{
		UserID:       userID,
		Name:         "Audit Skip Fixture " + uuid.New().String(),
		Period:       domain.BudgetPeriodMonthly,
		StartDate:    periodStart,
		Amount:       1000,
		CurrencyCode: "USD",
	}
	require.NoError(t, NewBudgetRepository(tx).Create(context.Background(), budget), "seed budget")
	return budget
}

// pinCategoryCreatedAt forces a category's created_at, because getCategoriesByBudgetIDQuerier
// orders by it and the audit's recompute loop therefore visits categories in that order. Without a
// pinned order the "already recomputed before the sentinel fired" discard is not observable.
func pinCategoryCreatedAt(t *testing.T, tx pgx.Tx, categoryID string, at time.Time) {
	t.Helper()
	_, err := tx.Exec(context.Background(),
		`UPDATE budget_categories SET created_at = $2 WHERE id = $1`, categoryID, at)
	require.NoError(t, err, "pin category created_at")
}

// seedIncompleteLedgerCategory seeds a pfc line whose only matching expense fact carries an
// uninterpretable savings provenance, so CalculateCategorySpent returns the #1685 sentinel.
func seedIncompleteLedgerCategory(t *testing.T, tx pgx.Tx, userID, linkedItemID, accountID, budgetID, pfcPrimary string, txDate time.Time) string {
	t.Helper()
	ctx := context.Background()
	category := &domain.BudgetCategory{
		BudgetID:        budgetID,
		CategoryKind:    "pfc",
		PFCPrimary:      &pfcPrimary,
		MatchLogic:      domain.MatchLogicCategoryOnly,
		AllocatedAmount: 200,
	}
	require.NoError(t, NewBudgetRepository(tx).CreateCategory(ctx, category), "seed incomplete category")

	transactionID := uuid.New().String()
	_, err := tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, source, amount, date, name, pending,
			 category_override_kind, pfc_primary_override)
		VALUES ($1, $2, $3, $4, 'plaid', 100, $5, 'Fixture', false, 'pfc', $6)
	`, transactionID, userID, accountID, linkedItemID, txDate, pfcPrimary)
	require.NoError(t, err, "seed transaction")

	goalID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO savings_goals (id, user_id, account_id, name, target_amount, progress_basis)
		VALUES ($1, $2, $3, 'Fixture', 1000, 'contributions')
	`, goalID, userID, accountID)
	require.NoError(t, err, "seed savings goal")

	// flow_kind is omitted on purpose: 000206 defaults it to legacy_unknown, which the expense-fact
	// CTE reports as legacy_savings_origin.
	_, err = tx.Exec(ctx, `
		INSERT INTO savings_goal_contributions
			(savings_goal_id, transaction_id, amount, source, status, occurred_on)
		VALUES ($1, $2, 20, 'manual', 'confirmed', $3)
	`, goalID, transactionID, txDate)
	require.NoError(t, err, "seed contribution")
	return category.ID
}

// TestAuditRecordsSkippedIncompleteActualsOutcome is the todo 12 happy path: the audit records the
// skip with the named outcome and the reason, discards the repair candidate it had already
// recomputed, commits, and is not re-selected on a second pass.
func TestAuditRecordsSkippedIncompleteActualsOutcome(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountID := seedAuditFixtureUser(t, tx)
	periodStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.January, 31, 23, 59, 59, 0, time.UTC)
	budget := seedAuditFixtureBudget(t, tx, userID, periodStart)
	budgetRepo := NewBudgetRepository(tx)

	// The STALE row is pinned FIRST so the audit recomputes it — and would repair it — before the
	// sentinel fires on the second category. No transactions are filed under FOOD_AND_DRINK, so the
	// narrow recomputation is 0 and the seeded 42.00 is stale by construction.
	stalePrimary := "FOOD_AND_DRINK"
	staleCategory := &domain.BudgetCategory{
		BudgetID:        budget.ID,
		CategoryKind:    "pfc",
		PFCPrimary:      &stalePrimary,
		MatchLogic:      domain.MatchLogicCategoryOnly,
		AllocatedAmount: 200,
		SpentAmount:     42.00,
	}
	require.NoError(t, budgetRepo.CreateCategory(ctx, staleCategory), "seed stale category")
	pinCategoryCreatedAt(t, tx, staleCategory.ID, periodStart)

	incompleteCategoryID := seedIncompleteLedgerCategory(t, tx, userID, linkedItemID, accountID, budget.ID, "RENT_AND_UTILITIES", periodStart.AddDate(0, 0, 15))
	pinCategoryCreatedAt(t, tx, incompleteCategoryID, periodStart.Add(time.Hour))

	loaded, err := budgetRepo.GetByID(ctx, budget.ID)
	require.NoError(t, err)
	require.Len(t, loaded.Categories, 2)

	spentCalc := directSpentCalculator{txRepo: NewTransactionRepository(tx, newTestFieldEncryptor(t))}
	verdict, err := budgetRepo.AuditBudgetHierarchy(ctx, loaded, periodStart, periodEnd, spentCalc)
	require.NoError(t, err, "an incomplete ledger is a precondition, not a fault — the audit must not fail")

	assert.Equal(t, domain.BudgetHierarchyAuditOutcomeSkippedIncompleteActuals, verdict.Outcome,
		"the skip must be recorded under its own named outcome")
	assert.Contains(t, verdict.Reason, "legacy_savings_origin",
		"the ledger row must name the reason the recomputation could not complete")
	assert.Equal(t, 0, verdict.CategoriesRepaired,
		"a skipped run applies no partial repair")

	rows, err := budgetRepo.GetCategoriesByBudgetID(ctx, budget.ID)
	require.NoError(t, err)
	for _, c := range rows {
		if c.ID == staleCategory.ID {
			assert.InDelta(t, 42.00, c.SpentAmount, 0.001,
				"the repair candidate recomputed before the sentinel fired must be discarded, not applied")
		}
	}

	var outcome, reason string
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT outcome, reason FROM budget_hierarchy_audits WHERE budget_id = $1`, budget.ID,
	).Scan(&outcome, &reason))
	assert.Equal(t, domain.BudgetHierarchyAuditOutcomeSkippedIncompleteActuals, outcome)
	assert.Contains(t, reason, "legacy_savings_origin")

	// A second pass must not re-select the budget: the ledger row is what makes the audit
	// at-most-once, and the skip is a recorded verdict like any other.
	unexamined, err := budgetRepo.BudgetIDsWithUnexaminedHierarchy(ctx, 1000)
	require.NoError(t, err)
	assert.NotContains(t, unexamined, budget.ID,
		"a skipped budget is recorded as examined and must not be re-enumerated")

	reloaded, err := budgetRepo.GetByID(ctx, budget.ID)
	require.NoError(t, err)
	second, err := budgetRepo.AuditBudgetHierarchy(ctx, reloaded, periodStart, periodEnd, spentCalc)
	require.NoError(t, err)
	assert.Equal(t, domain.BudgetHierarchyAuditOutcomeSkippedIncompleteActuals, second.Outcome)

	var ledgerCount int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT count(*) FROM budget_hierarchy_audits WHERE budget_id = $1`, budget.ID,
	).Scan(&ledgerCount))
	assert.Equal(t, 1, ledgerCount, "a second pass must not append a second ledger row")
}

// TestAuditHealthyBudgetUnchanged is the todo 12 failure path: a budget whose ledger is complete
// must be audited exactly as before — a stale row is still repaired, an already-correct budget is
// still verified, and neither is diverted into the skip outcome.
func TestAuditHealthyBudgetUnchanged(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountID := seedAuditFixtureUser(t, tx)
	periodStart := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.February, 28, 23, 59, 59, 0, time.UTC)
	budgetRepo := NewBudgetRepository(tx)
	spentCalc := directSpentCalculator{txRepo: NewTransactionRepository(tx, newTestFieldEncryptor(t))}

	t.Run("a stale row is still repaired", func(t *testing.T) {
		budget := seedAuditFixtureBudget(t, tx, userID, periodStart)
		primary := "FOOD_AND_DRINK"
		stale := &domain.BudgetCategory{
			BudgetID:        budget.ID,
			CategoryKind:    "pfc",
			PFCPrimary:      &primary,
			MatchLogic:      domain.MatchLogicCategoryOnly,
			AllocatedAmount: 200,
			SpentAmount:     42.00,
		}
		require.NoError(t, budgetRepo.CreateCategory(ctx, stale), "seed stale category")

		loaded, err := budgetRepo.GetByID(ctx, budget.ID)
		require.NoError(t, err)
		verdict, err := budgetRepo.AuditBudgetHierarchy(ctx, loaded, periodStart, periodEnd, spentCalc)
		require.NoError(t, err)
		assert.Equal(t, domain.BudgetHierarchyAuditOutcomeRepaired, verdict.Outcome)
		assert.Equal(t, 1, verdict.CategoriesRepaired)

		rows, err := budgetRepo.GetCategoriesByBudgetID(ctx, budget.ID)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.InDelta(t, 0, rows[0].SpentAmount, 0.001, "the stale row must still be corrected")
	})

	t.Run("an already-correct budget is still verified", func(t *testing.T) {
		budget := seedAuditFixtureBudget(t, tx, userID, periodStart)
		primary := "RENT_AND_UTILITIES"
		correct := &domain.BudgetCategory{
			BudgetID:        budget.ID,
			CategoryKind:    "pfc",
			PFCPrimary:      &primary,
			MatchLogic:      domain.MatchLogicCategoryOnly,
			AllocatedAmount: 200,
			SpentAmount:     100,
		}
		require.NoError(t, budgetRepo.CreateCategory(ctx, correct), "seed correct category")
		_, err := tx.Exec(ctx, `
			INSERT INTO transactions
				(id, user_id, account_id, linked_item_id, source, amount, date, name, pending,
				 category_override_kind, pfc_primary_override)
			VALUES ($1, $2, $3, $4, 'plaid', 100, $5, 'Fixture', false, 'pfc', $6)
		`, uuid.New().String(), userID, accountID, linkedItemID, periodStart.AddDate(0, 0, 10), primary)
		require.NoError(t, err, "seed matching transaction")

		loaded, err := budgetRepo.GetByID(ctx, budget.ID)
		require.NoError(t, err)
		verdict, err := budgetRepo.AuditBudgetHierarchy(ctx, loaded, periodStart, periodEnd, spentCalc)
		require.NoError(t, err)
		assert.Equal(t, domain.BudgetHierarchyAuditOutcomeVerified, verdict.Outcome)
		assert.Equal(t, 0, verdict.CategoriesRepaired)
	})
}