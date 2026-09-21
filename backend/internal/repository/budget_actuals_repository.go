package repository

import (
	"context"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// beginBudgetAccountingTx deliberately retains READ COMMITTED: a hierarchy-lock
// waiter must read the configuration committed by the previous lock holder.
// The financial inputs share one SQL statement snapshot in ReadBudgetAccountingInputs.
func beginBudgetAccountingTx(ctx context.Context, db DB) (pgx.Tx, error) {
	return db.Begin(ctx)
}

// BudgetActualsRepository is the atomic writer used by sync and hierarchy jobs.
// Keeping it named also exposes this background write to the cache inventory.
type BudgetActualsRepository interface {
	RecalculateBudgetActuals(context.Context, string, string, bool) (*domain.Budget, error)
}

var _ BudgetActualsRepository = (*budgetRepository)(nil)

// RecalculateBudgetActuals commits all sibling actuals together. Hierarchy lock
// order agrees with category configuration/reparent writers (user, then budget).
// A nil budget means its freshly locked configuration has no eligible current period.
// recalculateIncome=false preserves the locked income for hierarchy-only refreshes.
func (r *budgetRepository) RecalculateBudgetActuals(ctx context.Context, userID, budgetID string, recalculateIncome bool) (*domain.Budget, error) {
	tx, err := beginBudgetAccountingTx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := LockBudgetConfiguration(ctx, tx, userID); err != nil {
		return nil, err
	}
	var owner string
	if err := tx.QueryRow(ctx, `SELECT user_id FROM budgets WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL FOR UPDATE`, budgetID, userID).Scan(&owner); err != nil {
		return nil, err
	}
	fresh, err := (&budgetRepository{db: tx}).GetByID(ctx, budgetID)
	if err != nil {
		return nil, err
	}
	period := domain.CurrentBudgetActualsRange(fresh, time.Now())
	if !period.Eligible {
		return nil, nil
	}
	var sources []domain.IncomeSource
	if recalculateIncome {
		sources = fresh.IncomeSources
	}
	priorIncome := fresh.IncomeAmount
	income := fresh.IncomeAmount
	if recalculateIncome {
		income = 0
	}
	categories, _, withheld, err := r.publishBudgetActualsInTx(ctx, tx, userID, budgetID, period.Start, period.ActualThrough, &income, sources)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	fresh.Categories = categories
	if withheld {
		// A withheld run writes neither spent_amount nor income_amount, so the
		// returned budget must report the income the row retains rather than the
		// zeroed /sync base.
		income = priorIncome
	}
	fresh.IncomeAmount = income
	return fresh, nil
}

// refreshBudgetActualsInTx is the configuration writer's publication barrier.
// The caller must already hold the user hierarchy and budget row locks.
func (r *budgetRepository) refreshBudgetActualsInTx(ctx context.Context, q Querier, budgetID string) ([]domain.BudgetCategory, []string, bool, error) {
	b := &domain.Budget{ID: budgetID}
	if err := q.QueryRow(ctx, `SELECT user_id,period,start_date,end_date,is_active,income_amount FROM budgets WHERE id=$1 AND deleted_at IS NULL`, budgetID).Scan(&b.UserID, &b.Period, &b.StartDate, &b.EndDate, &b.IsActive, &b.IncomeAmount); err != nil {
		return nil, nil, false, err
	}
	period := domain.CurrentBudgetActualsRange(b, time.Now())
	if !period.Eligible {
		return nil, nil, false, nil
	}
	return r.recalculateBudgetActualsInTx(ctx, q, b.UserID, budgetID, period.Start, period.ActualThrough, b.IncomeAmount)
}

func (r *budgetRepository) recalculateBudgetActualsInTx(ctx context.Context, q Querier, userID, budgetID string, start, end time.Time, incomeAmount float64) ([]domain.BudgetCategory, []string, bool, error) {
	return r.publishBudgetActualsInTx(ctx, q, userID, budgetID, start, end, &incomeAmount, nil)
}

// sortedReasons normalises the reasons a publish reports: de-duplicated by the
// caller's set, empty strings dropped, sorted for a stable user-facing list.
func sortedReasons(reasons map[string]bool) []string {
	out := make([]string, 0, len(reasons))
	for reason := range reasons {
		if reason != "" {
			out = append(out, reason)
		}
	}
	sort.Strings(out)
	return out
}

// publishBudgetActualsInTx applies the three-class barrier taxonomy over the
// accounting inputs:
//   - FATAL (observed-income currency, invalid manual income) aborts with
//     domain.ErrBudgetActualsIncomplete before any withhold decision, so a
//     structural income defect always beats a withhold.
//   - WITHHOLD_AND_REPORT (an unattributed ledger reason, an unbudgeted reason,
//     or a budget-currency mismatch) writes neither spent_amount nor
//     income_amount: a double-counted aggregate is worse than a stale one.
//   - RETAIN_AND_REPORT (a per-category savings or line reason) keeps that
//     category's prior spent_amount and publishes every other line.
//
// The withheld discriminator is required because reasons is non-empty for
// RETAIN runs too. This function never commits; the caller's own configuration
// change commits in the caller's transaction.
func (r *budgetRepository) publishBudgetActualsInTx(ctx context.Context, q Querier, userID, budgetID string, start, end time.Time, incomeAmount *float64, sources []domain.IncomeSource) ([]domain.BudgetCategory, []string, bool, error) {
	var currency string
	if err := q.QueryRow(ctx, `SELECT currency_code FROM budgets WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL`, budgetID, userID).Scan(&currency); err != nil {
		return nil, nil, false, err
	}
	categories, err := r.getCategoriesByBudgetIDQuerier(ctx, q, budgetID)
	if err != nil {
		return nil, nil, false, err
	}
	ledger, savings, observedIncome, err := readBudgetAccountingInputs(ctx, q, userID, categories, start, end, sources)
	if err != nil {
		return nil, nil, false, err
	}

	// Income validation is FATAL and runs before the withhold decision. The
	// delta stays local so a withheld run never mutates the caller's base.
	incomeDelta := 0.0
	for _, observed := range observedIncome {
		if reason := budgetCurrencyReason(observed.CurrencyCode, currency); reason != "" {
			return nil, nil, false, domain.NewBudgetActualsIncompleteError(reason)
		}
		incomeDelta += observed.Amount
	}
	for _, source := range sources {
		if source.Type != domain.IncomeSourceTypeManual {
			continue
		}
		amount, err := strconv.ParseFloat(source.Value, 64)
		if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
			return nil, nil, false, domain.NewBudgetActualsIncompleteError("invalid_manual_income")
		}
		incomeDelta += amount
	}

	reasons := map[string]bool{}
	withheld := false
	attributed := map[string]bool{}
	for _, line := range ledger.Lines {
		for _, reason := range line.Reasons {
			attributed[reason] = true
		}
	}
	for _, reason := range ledger.Reasons {
		if !attributed[reason] {
			withheld = true
			reasons[reason] = true
		}
	}
	for _, reason := range ledger.UnbudgetedReasons {
		withheld = true
		reasons[reason] = true
	}
	for _, code := range ledger.CurrencyCodes {
		if code == "" || code != currency {
			withheld = true
			reasons["currency_mismatch"] = true
		}
	}
	if withheld {
		return categories, sortedReasons(reasons), true, nil
	}

	retained := map[string]bool{}
	byGoal := map[string]SavingsPeriodActivity{}
	for _, s := range savings {
		byGoal[s.GoalID] = s
	}
	for i := range categories {
		c := &categories[i]
		if c.CategoryKind == "savings" {
			if c.SavingsGoalID == nil {
				retained[c.ID] = true
				reasons["savings_goal_missing"] = true
				continue
			}
			activity, ok := byGoal[*c.SavingsGoalID]
			if !ok {
				retained[c.ID] = true
				reasons["savings_activity_missing"] = true
				continue
			}
			if activity.CurrencyCode == "" || activity.CurrencyCode != currency {
				retained[c.ID] = true
				reasons["savings_currency_mismatch"] = true
				continue
			}
			if activity.ActualAmount == nil {
				reason := activity.Reason
				if reason == "" {
					reason = "savings_activity_missing"
				}
				retained[c.ID] = true
				reasons[reason] = true
				continue
			}
			c.SpentAmount = *activity.ActualAmount
			continue
		}
		actual := ledger.Lines[c.ID]
		if actual.Amount == nil {
			retained[c.ID] = true
			reasons[actual.Reason] = true
			continue
		}
		c.SpentAmount = *actual.Amount
	}

	*incomeAmount += incomeDelta
	for _, c := range categories {
		if retained[c.ID] {
			continue
		}
		if _, err := q.Exec(ctx, `UPDATE budget_categories SET spent_amount=$2,updated_at=NOW() WHERE id=$1 AND budget_id=$3 AND deleted_at IS NULL`, c.ID, c.SpentAmount, budgetID); err != nil {
			return nil, nil, false, err
		}
	}
	if _, err := q.Exec(ctx, `UPDATE budgets SET income_amount=$2,updated_at=NOW() WHERE id=$1 AND user_id=$3 AND deleted_at IS NULL`, budgetID, *incomeAmount, userID); err != nil {
		return nil, nil, false, err
	}

	return categories, sortedReasons(reasons), false, nil
}
