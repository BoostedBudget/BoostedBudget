//go:build integration

package services

// Issue #1631: a custom-period budget reported as "not current" for almost the whole of
// its FINAL day.
//
// Mechanism, end to end:
//   1. budgets.end_date is a PostgreSQL DATE column (migration 000022).
//   2. #1630 made createBudget/UpdateBudget write req.EndDate.EndOfDay() —
//      23:59:59.999999999 — to make the final day inclusive.
//   3. A DATE column cannot hold a time, so the write TRUNCATES it. GetActiveByUserID
//      scans it back as midnight.
//   4. GetCurrentBudget skipped the budget when now.After(*budget.EndDate), which is true
//      from 00:00:00.000000001 on the final day onward.
//
// EndOfDay() therefore cannot fix this where it sat: the column discards exactly the part
// that mattered. The fix is at the COMPARISON, which must treat end_date as an INCLUSIVE
// calendar day. Only a real Postgres round trip can see the defect — the pre-existing
// service unit tests inspect the object BEFORE it is persisted, which is precisely why
// #1630's review missed this.

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

func TestBudgetService_GetCurrentBudget_CustomPeriodEndingToday_IsStillCurrent_1631(t *testing.T) {
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	budgetService := NewBudgetService(
		repository.NewBudgetRepository(pool),
		repository.NewCustomCategoryRepository(pool),
		nil,
		slog.Default(),
	)

	seed := func(t *testing.T, endOffsetDays int) string {
		t.Helper()
		userID := uuid.New().String()
		budgetID := uuid.New().String()
		_, err := pool.Exec(ctx, `
			INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1, $2, $3, 'premium', 'active', 'iad')
		`, userID, "test|1631final|"+userID, "1631-final-"+userID+"@example.com")
		require.NoError(t, err, "seed user")
		t.Cleanup(func() {
			cleanupCtx := context.Background()
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM budget_categories WHERE budget_id = $1`, budgetID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM budgets WHERE id = $1`, budgetID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, userID)
		})
		// start_date well inside the past, end_date offset from Postgres' own CURRENT_DATE.
		_, err = pool.Exec(ctx, `
			INSERT INTO budgets (id, user_id, name, period, start_date, end_date, amount, is_active, series_id)
			VALUES ($1, $2, 'Custom Period 1631', 'custom',
			        CURRENT_DATE - 10, CURRENT_DATE + $3::int,
			        1000.00, true, $1)
		`, budgetID, userID, endOffsetDays)
		require.NoError(t, err, "seed custom-period budget")
		return userID
	}

	t.Run("ending TODAY is still current", func(t *testing.T) {
		userID := seed(t, 0)

		budget, err := budgetService.GetCurrentBudget(ctx, userID)

		require.NoError(t, err,
			"a custom-period budget whose end_date is TODAY must still be current: end_date is a "+
				"DATE column, so it names an INCLUSIVE calendar day, not the instant of its midnight")
		require.NotNil(t, budget)
		assert.Equal(t, domain.BudgetPeriodCustom, budget.Period)
	})

	// The create-response / read-response agreement half of the fix (#1631 item 3):
	// EndOfDay() made CreateBudget return 23:59:59.999999999 for a value the DATE column
	// stores — and hands back — as midnight. A field whose meaning depends on whether it has
	// been through Postgres yet is a trap for every caller downstream, so the write sites now
	// normalise to the calendar day the column will actually hold.
	t.Run("CreateBudget response agrees with the persisted row", func(t *testing.T) {
		userID := uuid.New().String()
		_, err := pool.Exec(ctx, `
			INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1, $2, $3, 'premium', 'active', 'iad')
		`, userID, "test|1631roundtrip|"+userID, "1631-roundtrip-"+userID+"@example.com")
		require.NoError(t, err, "seed user")
		t.Cleanup(func() {
			cleanupCtx := context.Background()
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM budget_categories WHERE budget_id IN (SELECT id FROM budgets WHERE user_id = $1)`, userID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM budgets WHERE user_id = $1`, userID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, userID)
		})

		today := time.Now().UTC()
		startDate := domain.FlexibleDate(today.AddDate(0, 0, -10).Truncate(24 * time.Hour))
		endDate := domain.FlexibleDate(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC))

		created, err := budgetService.CreateBudget(ctx, userID, &domain.CreateBudgetRequest{
			Name:         "Round Trip 1631",
			Period:       domain.BudgetPeriodCustom,
			StartDate:    startDate,
			EndDate:      &endDate,
			Amount:       1000.00,
			CurrencyCode: "USD",
		})
		require.NoError(t, err)
		require.NotNil(t, created.EndDate)

		reread, err := repository.NewBudgetRepository(pool).GetByID(ctx, created.ID)
		require.NoError(t, err)
		require.NotNil(t, reread.EndDate)

		assert.True(t, created.EndDate.Equal(*reread.EndDate),
			"the EndDate CreateBudget returns must equal the EndDate Postgres hands back: "+
				"got %v from create, %v from the DATE column", created.EndDate, reread.EndDate)
		assert.True(t, created.StartDate.Equal(reread.StartDate),
			"the StartDate CreateBudget returns must equal the StartDate Postgres hands back: "+
				"got %v from create, %v from the DATE column", created.StartDate, reread.StartDate)

		// And the budget it just created, ending today, must be the CURRENT one.
		current, err := budgetService.GetCurrentBudget(ctx, userID)
		require.NoError(t, err, "the budget just created, ending today, must be current")
		assert.Equal(t, created.ID, current.ID)
	})

	t.Run("ending YESTERDAY is no longer current", func(t *testing.T) {
		userID := seed(t, -1)

		budget, err := budgetService.GetCurrentBudget(ctx, userID)

		require.Error(t, err,
			"a custom-period budget whose end_date was YESTERDAY must NOT be current — "+
				"making the final day inclusive must not spill into the day after it")
		assert.ErrorIs(t, err, domain.ErrBudgetNotFound)
		assert.Nil(t, budget)
	})
}
