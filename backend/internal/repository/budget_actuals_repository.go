package repository

import (
	"context"
	"fmt"
	"math"
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
	income := fresh.IncomeAmount
	if recalculateIncome {
		income = 0
	}
	categories, err := r.publishBudgetActualsInTx(ctx, tx, userID, budgetID, period.Start, period.ActualThrough, &income, sources)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	fresh.Categories = categories
	fresh.IncomeAmount = income
	return fresh, nil
}

// refreshBudgetActualsInTx is the configuration writer's publication barrier.
// The caller must already hold the user hierarchy and budget row locks.
func (r *budgetRepository) refreshBudgetActualsInTx(ctx context.Context, q Querier, budgetID string) ([]domain.BudgetCategory, error) {
	b := &domain.Budget{ID: budgetID}
	if err := q.QueryRow(ctx, `SELECT user_id,period,start_date,end_date,is_active,income_amount FROM budgets WHERE id=$1 AND deleted_at IS NULL`, budgetID).Scan(&b.UserID, &b.Period, &b.StartDate, &b.EndDate, &b.IsActive, &b.IncomeAmount); err != nil {
		return nil, err
	}
	period := domain.CurrentBudgetActualsRange(b, time.Now())
	if !period.Eligible {
		return nil, nil
	}
	return r.recalculateBudgetActualsInTx(ctx, q, b.UserID, budgetID, period.Start, period.ActualThrough, b.IncomeAmount)
}

func (r *budgetRepository) recalculateBudgetActualsInTx(ctx context.Context, q Querier, userID, budgetID string, start, end time.Time, incomeAmount float64) ([]domain.BudgetCategory, error) {
	return r.publishBudgetActualsInTx(ctx, q, userID, budgetID, start, end, &incomeAmount, nil)
}

func (r *budgetRepository) publishBudgetActualsInTx(ctx context.Context, q Querier, userID, budgetID string, start, end time.Time, incomeAmount *float64, sources []domain.IncomeSource) ([]domain.BudgetCategory, error) {
	var currency string
	if err := q.QueryRow(ctx, `SELECT currency_code FROM budgets WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL`, budgetID, userID).Scan(&currency); err != nil {
		return nil, err
	}
	categories, err := r.getCategoriesByBudgetIDQuerier(ctx, q, budgetID)
	if err != nil {
		return nil, err
	}
	ledger, savings, observedIncome, err := readBudgetAccountingInputs(ctx, q, userID, categories, start, end, sources)
	if err != nil {
		return nil, err
	}
	if len(ledger.Reasons) > 0 {
		return nil, fmt.Errorf("budget actuals incomplete: %s", ledger.Reasons[0])
	}
	for _, observed := range observedIncome {
		if reason := budgetCurrencyReason(observed.CurrencyCode, currency); reason != "" {
			return nil, fmt.Errorf("income actuals incomplete: %s", reason)
		}
		*incomeAmount += observed.Amount
	}
	for _, source := range sources {
		if source.Type != domain.IncomeSourceTypeManual {
			continue
		}
		amount, err := strconv.ParseFloat(source.Value, 64)
		if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
			return nil, fmt.Errorf("income actuals incomplete: invalid_manual_income")
		}
		*incomeAmount += amount
	}
	for _, code := range ledger.CurrencyCodes {
		if code == "" || code != currency {
			return nil, fmt.Errorf("budget actuals incomplete: currency mismatch")
		}
	}
	byGoal := map[string]SavingsPeriodActivity{}
	for _, s := range savings {
		byGoal[s.GoalID] = s
	}
	for i := range categories {
		c := &categories[i]
		if c.CategoryKind == "savings" {
			if c.SavingsGoalID == nil {
				return nil, fmt.Errorf("savings actuals missing goal")
			}
			activity, ok := byGoal[*c.SavingsGoalID]
			if activity.CurrencyCode == "" || activity.CurrencyCode != currency {
				return nil, fmt.Errorf("savings actuals incomplete: currency mismatch")
			}
			if !ok || activity.ActualAmount == nil {
				return nil, fmt.Errorf("savings actuals incomplete: %s", activity.Reason)
			}
			c.SpentAmount = *activity.ActualAmount
		} else {
			actual := ledger.Lines[c.ID]
			if actual.Amount == nil {
				return nil, fmt.Errorf("budget actuals incomplete: %s", actual.Reason)
			}
			c.SpentAmount = *actual.Amount
		}
	}
	for _, c := range categories {
		if _, err := q.Exec(ctx, `UPDATE budget_categories SET spent_amount=$2,updated_at=NOW() WHERE id=$1 AND budget_id=$3 AND deleted_at IS NULL`, c.ID, c.SpentAmount, budgetID); err != nil {
			return nil, err
		}
	}
	if _, err := q.Exec(ctx, `UPDATE budgets SET income_amount=$2,updated_at=NOW() WHERE id=$1 AND user_id=$3 AND deleted_at IS NULL`, budgetID, *incomeAmount, userID); err != nil {
		return nil, err
	}

	return categories, nil
}
