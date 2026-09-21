//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

// actualsTaxonomyFixture is one isolated user + budget for the publish-taxonomy
// tests. Every case gets its own fixture so a withheld run in one case cannot
// leak into another.
type actualsTaxonomyFixture struct {
	t        *testing.T
	ctx      context.Context
	tx       pgx.Tx
	user     string
	item     string
	account  string
	budget   *domain.Budget
	repo     *budgetRepository
	seededAt time.Time
}

func newActualsTaxonomyFixture(t *testing.T) *actualsTaxonomyFixture {
	t.Helper()
	ctx := context.Background()
	tx, err := connectTestDB(t).Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	user, item, account := uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO users (id,clerk_user_id,email,subscription_tier,subscription_status,primary_region) VALUES ($1,$2,$3,'premium','active','iad')`, user, user, user+"@example.com")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO linked_items (id,user_id,item_id,access_token_encrypted) VALUES ($1,$2,$3,'fixture')`, item, user, item)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO accounts (id,user_id,linked_item_id,plaid_account_id,name,type,currency_code) VALUES ($1,$2,$3,$4,'Fixture','depository','USD')`, account, user, item, account)
	require.NoError(t, err)
	budget := &domain.Budget{ID: uuid.NewString(), UserID: user, Name: "Taxonomy", Period: domain.BudgetPeriodMonthly, StartDate: time.Now().UTC().AddDate(0, 0, -1), Amount: 1000, ExpectedIncome: 1000, CurrencyCode: "USD", IsActive: true}
	require.NoError(t, NewBudgetRepository(tx).Create(ctx, budget))
	return &actualsTaxonomyFixture{t: t, ctx: ctx, tx: tx, user: user, item: item, account: account, budget: budget, repo: NewBudgetRepository(tx).(*budgetRepository), seededAt: time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)}
}

// addCategory inserts a budget line with an explicit prior spent_amount and
// updated_at. The INSERT is deliberately raw: budget_categories carries a
// BEFORE UPDATE trigger that rewrites updated_at to the transaction timestamp,
// so a seeded value cannot survive an UPDATE inside the test's transaction and
// "was this row written?" would become undecidable.
func (f *actualsTaxonomyFixture) addCategory(kind string, spent float64, mutate func(*domain.BudgetCategory)) *domain.BudgetCategory {
	f.t.Helper()
	c := &domain.BudgetCategory{ID: uuid.NewString(), BudgetID: f.budget.ID, CategoryKind: kind, AllocatedAmount: 100, SpentAmount: spent}
	if mutate != nil {
		mutate(c)
	}
	tags := c.Tags
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, err := json.Marshal(tags)
	require.NoError(f.t, err)
	matchLogic := c.MatchLogic
	if matchLogic == "" {
		matchLogic = domain.MatchLogicCategoryOnly
	}
	_, err = f.tx.Exec(f.ctx, `INSERT INTO budget_categories (id,budget_id,category_kind,pfc_primary,pfc_detailed,custom_category_id,allocated_amount,spent_amount,created_at,updated_at,tags,match_logic,savings_goal_id,group_name) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		c.ID, c.BudgetID, c.CategoryKind, c.PFCPrimary, c.PFCDetailed, c.CustomCategoryID, c.AllocatedAmount, c.SpentAmount, f.seededAt, f.seededAt, []byte(tagsJSON), matchLogic, c.SavingsGoalID, c.GroupName)
	require.NoError(f.t, err)
	return c
}

// addTransaction inserts a posted expense fact. A non-USD currency gets its own
// account so the ledger reports that currency for the fact.
func (f *actualsTaxonomyFixture) addTransaction(amount float64, pfcPrimary, currency string, tags []string) string {
	f.t.Helper()
	account := f.account
	if currency != "USD" {
		account = uuid.NewString()
		_, err := f.tx.Exec(f.ctx, `INSERT INTO accounts (id,user_id,linked_item_id,plaid_account_id,name,type,currency_code) VALUES ($1,$2,$3,$4,'Foreign','depository',$5)`, account, f.user, f.item, account, currency)
		require.NoError(f.t, err)
	}
	tagsJSON, err := json.Marshal(tags)
	require.NoError(f.t, err)
	id := uuid.NewString()
	_, err = f.tx.Exec(f.ctx, `INSERT INTO transactions (id,user_id,account_id,linked_item_id,source,amount,name,date,tags,category_override_kind,pfc_primary_override) VALUES ($1,$2,$3,$4,'manual',$5,'Fixture',CURRENT_DATE,$6,'pfc',$7)`, id, f.user, account, f.item, amount, []byte(tagsJSON), pfcPrimary)
	require.NoError(f.t, err)
	return id
}

func (f *actualsTaxonomyFixture) addGoal(currency string) string {
	f.t.Helper()
	goal := uuid.NewString()
	_, err := f.tx.Exec(f.ctx, `INSERT INTO savings_goals (id,user_id,account_id,name,target_amount,progress_basis,currency_code) VALUES ($1,$2,$3,'Fixture',1000,'contributions',$4)`, goal, f.user, f.account, currency)
	require.NoError(f.t, err)
	return goal
}

func (f *actualsTaxonomyFixture) addContribution(goalID string, transactionID *string, amount float64, flowKind string) {
	f.t.Helper()
	_, err := f.tx.Exec(f.ctx, `INSERT INTO savings_goal_contributions (savings_goal_id,transaction_id,amount,source,status,occurred_on,flow_kind) VALUES ($1,$2,$3,'manual','confirmed',CURRENT_DATE,$4)`, goalID, transactionID, amount, flowKind)
	require.NoError(f.t, err)
}

func (f *actualsTaxonomyFixture) row(id string) (float64, time.Time) {
	f.t.Helper()
	var spent float64
	var updatedAt time.Time
	require.NoError(f.t, f.tx.QueryRow(f.ctx, `SELECT spent_amount, updated_at FROM budget_categories WHERE id=$1`, id).Scan(&spent, &updatedAt))
	return spent, updatedAt
}

func (f *actualsTaxonomyFixture) sumSpent() float64 {
	f.t.Helper()
	var sum float64
	require.NoError(f.t, f.tx.QueryRow(f.ctx, `SELECT COALESCE(SUM(spent_amount),0) FROM budget_categories WHERE budget_id=$1 AND deleted_at IS NULL`, f.budget.ID).Scan(&sum))
	return sum
}

// publish drives the private publish path directly so the returned reasons and
// the withheld discriminator are observable. The public RecalculateBudgetActuals
// signature is unchanged and is exercised by TestWithheldSyncReturnsPriorIncome.
func (f *actualsTaxonomyFixture) publish(sources []domain.IncomeSource) ([]domain.BudgetCategory, []string, bool, error) {
	f.t.Helper()
	period := domain.CurrentBudgetActualsRange(f.budget, time.Now())
	income := f.budget.IncomeAmount
	return f.repo.publishBudgetActualsInTx(f.ctx, f.tx, f.user, f.budget.ID, period.Start, period.ActualThrough, &income, sources)
}

// requireSameInstant asserts two timestamps are the same instant to microsecond
// precision — the strongest available form of "byte-identical" for a
// timestamptz read back through pgx.
func requireSameInstant(t *testing.T, before, after time.Time, msg string) {
	t.Helper()
	require.True(t, before.Equal(after), "%s: %s != %s", msg, before, after)
	require.Equal(t, before.UnixMicro(), after.UnixMicro(), msg)
}

// TestPublishActualsTaxonomy is the binding three-class taxonomy table: one case
// per barrier, asserting the returned reasons directly (sorted, de-duplicated)
// and the withheld discriminator.
func TestPublishActualsTaxonomy(t *testing.T) {
	food, travel := "FOOD_AND_DRINK", "TRAVEL"
	shared := []string{"shared"}

	cases := []struct {
		name         string
		setup        func(f *actualsTaxonomyFixture) (map[string]*domain.BudgetCategory, []domain.IncomeSource)
		wantReasons  []string
		wantWithheld bool
		wantSpent    map[string]float64
		wantErr      string
	}{
		{
			name: "clean run publishes every line and reports nothing",
			setup: func(f *actualsTaxonomyFixture) (map[string]*domain.BudgetCategory, []domain.IncomeSource) {
				foodLine := f.addCategory("pfc", 7, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
				f.addTransaction(100, food, "USD", nil)
				return map[string]*domain.BudgetCategory{"food": foodLine}, nil
			},
			wantReasons:  []string{},
			wantWithheld: false,
			wantSpent:    map[string]float64{"food": 100},
		},
		{
			name: "unattributed ledger reason withholds the whole run",
			setup: func(f *actualsTaxonomyFixture) (map[string]*domain.BudgetCategory, []domain.IncomeSource) {
				a := f.addCategory("pfc", 5, func(c *domain.BudgetCategory) {
					c.PFCPrimary = &food
					c.Tags = shared
					c.MatchLogic = domain.MatchLogicTagOnly
				})
				b := f.addCategory("pfc", 6, func(c *domain.BudgetCategory) {
					c.PFCPrimary = &travel
					c.Tags = shared
					c.MatchLogic = domain.MatchLogicTagOnly
				})
				f.addTransaction(100, food, "USD", shared)
				return map[string]*domain.BudgetCategory{"a": a, "b": b}, nil
			},
			wantReasons:  []string{"overlapping_individual_lines"},
			wantWithheld: true,
			wantSpent:    map[string]float64{"a": 5, "b": 6},
		},
		{
			name: "observed income currency is fatal",
			setup: func(f *actualsTaxonomyFixture) (map[string]*domain.BudgetCategory, []domain.IncomeSource) {
				foodLine := f.addCategory("pfc", 7, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
				f.addTransaction(100, food, "EUR", nil)
				f.addTransaction(-80, "INCOME", "EUR", nil)
				return map[string]*domain.BudgetCategory{"food": foodLine}, []domain.IncomeSource{{Type: domain.IncomeSourceTypePFCPrimary, Value: "INCOME"}}
			},
			wantErr:   "mixed_currency",
			wantSpent: map[string]float64{"food": 7},
		},
		{
			name: "invalid manual income is fatal",
			setup: func(f *actualsTaxonomyFixture) (map[string]*domain.BudgetCategory, []domain.IncomeSource) {
				foodLine := f.addCategory("pfc", 7, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
				f.addTransaction(100, food, "USD", nil)
				return map[string]*domain.BudgetCategory{"food": foodLine}, []domain.IncomeSource{{Type: domain.IncomeSourceTypeManual, Value: "not-a-number"}}
			},
			wantErr:   "invalid_manual_income",
			wantSpent: map[string]float64{"food": 7},
		},
		{
			name: "budget currency mismatch withholds",
			setup: func(f *actualsTaxonomyFixture) (map[string]*domain.BudgetCategory, []domain.IncomeSource) {
				foodLine := f.addCategory("pfc", 7, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
				f.addTransaction(100, food, "EUR", nil)
				return map[string]*domain.BudgetCategory{"food": foodLine}, nil
			},
			wantReasons:  []string{"currency_mismatch"},
			wantWithheld: true,
			wantSpent:    map[string]float64{"food": 7},
		},
		{
			// valid_budget_category_discriminator forbids a savings row without a
			// savings_goal_id, so the only reachable shape of "no activity for this
			// goal" is a goal that is absent from the current activity read.
			name: "savings goal absent from current activity is retained",
			setup: func(f *actualsTaxonomyFixture) (map[string]*domain.BudgetCategory, []domain.IncomeSource) {
				goal := f.addGoal("USD")
				savingsLine := f.addCategory("savings", 7, func(c *domain.BudgetCategory) { c.SavingsGoalID = &goal })
				foodLine := f.addCategory("pfc", 7, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
				f.addTransaction(100, food, "USD", nil)
				_, err := f.tx.Exec(f.ctx, `UPDATE savings_goals SET deleted_at=NOW(), deleted_reason='user_deleted', deleted_by='user' WHERE id=$1`, goal)
				require.NoError(f.t, err)
				return map[string]*domain.BudgetCategory{"savings": savingsLine, "food": foodLine}, nil
			},
			wantReasons:  []string{"savings_activity_missing"},
			wantWithheld: false,
			wantSpent:    map[string]float64{"savings": 7, "food": 100},
		},
		{
			name: "savings goal currency mismatch is retained",
			setup: func(f *actualsTaxonomyFixture) (map[string]*domain.BudgetCategory, []domain.IncomeSource) {
				goal := f.addGoal("EUR")
				savingsLine := f.addCategory("savings", 7, func(c *domain.BudgetCategory) { c.SavingsGoalID = &goal })
				foodLine := f.addCategory("pfc", 7, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
				f.addTransaction(100, food, "USD", nil)
				return map[string]*domain.BudgetCategory{"savings": savingsLine, "food": foodLine}, nil
			},
			wantReasons:  []string{"savings_currency_mismatch"},
			wantWithheld: false,
			wantSpent:    map[string]float64{"savings": 7, "food": 100},
		},
		{
			name: "savings activity reason is retained",
			setup: func(f *actualsTaxonomyFixture) (map[string]*domain.BudgetCategory, []domain.IncomeSource) {
				goal := f.addGoal("USD")
				savingsLine := f.addCategory("savings", 7, func(c *domain.BudgetCategory) { c.SavingsGoalID = &goal })
				f.addContribution(goal, nil, 20, "legacy_unknown")
				return map[string]*domain.BudgetCategory{"savings": savingsLine}, nil
			},
			wantReasons:  []string{"legacy_unknown"},
			wantWithheld: false,
			wantSpent:    map[string]float64{"savings": 7},
		},
		{
			name: "line reason is retained",
			setup: func(f *actualsTaxonomyFixture) (map[string]*domain.BudgetCategory, []domain.IncomeSource) {
				goal := f.addGoal("USD")
				foodLine := f.addCategory("pfc", 7, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
				transaction := f.addTransaction(100, food, "USD", nil)
				f.addContribution(goal, &transaction, 1, "legacy_unknown")
				return map[string]*domain.BudgetCategory{"food": foodLine}, nil
			},
			wantReasons:  []string{"legacy_savings_origin"},
			wantWithheld: false,
			wantSpent:    map[string]float64{"food": 7},
		},
		{
			name: "reasons are de-duplicated and sorted",
			setup: func(f *actualsTaxonomyFixture) (map[string]*domain.BudgetCategory, []domain.IncomeSource) {
				goal := f.addGoal("USD")
				foodLine := f.addCategory("pfc", 7, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
				travelLine := f.addCategory("pfc", 7, func(c *domain.BudgetCategory) { c.PFCPrimary = &travel })
				savingsLine := f.addCategory("savings", 7, func(c *domain.BudgetCategory) { c.SavingsGoalID = &goal })
				foodTransaction := f.addTransaction(100, food, "USD", nil)
				travelTransaction := f.addTransaction(50, travel, "USD", nil)
				f.addContribution(goal, &foodTransaction, 1, "legacy_unknown")
				f.addContribution(goal, &travelTransaction, 1, "legacy_unknown")
				f.addContribution(goal, nil, 20, "legacy_unknown")
				return map[string]*domain.BudgetCategory{"food": foodLine, "travel": travelLine, "savings": savingsLine}, nil
			},
			wantReasons:  []string{"legacy_savings_origin", "legacy_unknown"},
			wantWithheld: false,
			wantSpent:    map[string]float64{"food": 7, "travel": 7, "savings": 7},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newActualsTaxonomyFixture(t)
			cats, sources := tc.setup(f)
			before := map[string]float64{}
			for name, c := range cats {
				spent, _ := f.row(c.ID)
				before[name] = spent
			}
			_, reasons, withheld, err := f.publish(sources)
			if tc.wantErr != "" {
				require.Error(t, err)
				var incomplete *domain.BudgetActualsIncompleteError
				require.ErrorAs(t, err, &incomplete)
				require.Equal(t, []string{tc.wantErr}, incomplete.Reasons)
				require.ErrorIs(t, err, domain.ErrBudgetActualsIncomplete)
				for name, c := range cats {
					spent, _ := f.row(c.ID)
					require.Equal(t, before[name], spent, "a FATAL barrier must not write %s", name)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantWithheld, withheld)
			require.Equal(t, tc.wantReasons, reasons)
			for name, want := range tc.wantSpent {
				spent, _ := f.row(cats[name].ID)
				require.Equal(t, want, spent, "spent_amount for %s", name)
			}
		})
	}
}

// TestPublishActualsNeverWritesWithheldLine pins the anti-fabrication invariant:
// a RETAIN or WITHHOLD line keeps both its spent_amount and its updated_at, and
// is never rewritten to 0.
func TestPublishActualsNeverWritesWithheldLine(t *testing.T) {
	food := "FOOD_AND_DRINK"

	t.Run("retain keeps the affected row untouched and publishes its sibling", func(t *testing.T) {
		f := newActualsTaxonomyFixture(t)
		goal := f.addGoal("USD")
		savingsLine := f.addCategory("savings", 7, func(c *domain.BudgetCategory) { c.SavingsGoalID = &goal })
		foodLine := f.addCategory("pfc", 3, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
		f.addContribution(goal, nil, 20, "legacy_unknown")
		f.addTransaction(100, food, "USD", nil)
		beforeSavings, beforeSavingsAt := f.row(savingsLine.ID)
		beforeFood, beforeFoodAt := f.row(foodLine.ID)

		_, reasons, withheld, err := f.publish(nil)
		require.NoError(t, err)
		require.False(t, withheld)
		require.Equal(t, []string{"legacy_unknown"}, reasons)

		afterSavings, afterSavingsAt := f.row(savingsLine.ID)
		require.Equal(t, 7.0, afterSavings, "a retained line must never be fabricated to 0")
		require.Equal(t, beforeSavings, afterSavings)
		requireSameInstant(t, beforeSavingsAt, afterSavingsAt, "a retained line's updated_at must be untouched")

		afterFood, afterFoodAt := f.row(foodLine.ID)
		require.Equal(t, 100.0, afterFood, "the sibling line still publishes")
		require.Equal(t, beforeFood, 3.0)
		require.False(t, afterFoodAt.Equal(beforeFoodAt), "the published line's updated_at advances")
	})

	t.Run("withhold writes no row at all", func(t *testing.T) {
		f := newActualsTaxonomyFixture(t)
		foodLine := f.addCategory("pfc", 7, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
		f.addTransaction(100, food, "EUR", nil)
		before, beforeAt := f.row(foodLine.ID)

		_, reasons, withheld, err := f.publish(nil)
		require.NoError(t, err)
		require.True(t, withheld)
		require.Equal(t, []string{"currency_mismatch"}, reasons)

		after, afterAt := f.row(foodLine.ID)
		require.Equal(t, 7.0, after, "a withheld line must never be fabricated to 0")
		require.Equal(t, before, after)
		requireSameInstant(t, beforeAt, afterAt, "a withheld line's updated_at must be untouched")
	})
}

// TestOverlapDoesNotDoubleCountAggregate preserves the guarantee that
// budget_configuration_actuals_integration_test.go:89 used to assert as a hard
// error: an ambiguous global total must not publish individually known
// duplicated lines. Under the taxonomy it withholds, so the aggregate is
// unchanged rather than doubled.
func TestOverlapDoesNotDoubleCountAggregate(t *testing.T) {
	food, travel := "FOOD_AND_DRINK", "TRAVEL"
	shared := []string{"shared"}
	f := newActualsTaxonomyFixture(t)
	f.addCategory("pfc", 5, func(c *domain.BudgetCategory) {
		c.PFCPrimary = &food
		c.Tags = shared
		c.MatchLogic = domain.MatchLogicTagOnly
	})
	f.addCategory("pfc", 6, func(c *domain.BudgetCategory) {
		c.PFCPrimary = &travel
		c.Tags = shared
		c.MatchLogic = domain.MatchLogicTagOnly
	})
	f.addTransaction(100, food, "USD", shared)

	before := f.sumSpent()
	require.Equal(t, 11.0, before)

	_, reasons, withheld, err := f.publish(nil)
	require.NoError(t, err)
	require.True(t, withheld)
	require.Equal(t, []string{"overlapping_individual_lines"}, reasons)
	require.Equal(t, before, f.sumSpent(), "a withheld run must not double-count the aggregate")
	require.Equal(t, 11.0, f.sumSpent(), "the aggregate must not become 2x the ambiguous transaction")
}

// TestUnbudgetedReasonCollisionStillWithholds proves clause (b) is not masked by
// clause (a): the same reason on a matched fact and on an unbudgeted fact still
// withholds, because the unbudgeted fact's reason is unattributable.
func TestUnbudgetedReasonCollisionStillWithholds(t *testing.T) {
	food, travel := "FOOD_AND_DRINK", "TRAVEL"
	f := newActualsTaxonomyFixture(t)
	goal := f.addGoal("USD")
	foodLine := f.addCategory("pfc", 7, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
	matched := f.addTransaction(100, food, "USD", nil)
	unbudgeted := f.addTransaction(50, travel, "USD", nil)
	f.addContribution(goal, &matched, 1, "legacy_unknown")
	f.addContribution(goal, &unbudgeted, 1, "legacy_unknown")

	_, reasons, withheld, err := f.publish(nil)
	require.NoError(t, err)
	require.True(t, withheld, "a reason on an unbudgeted fact withholds even when a matched fact shares it")
	require.Equal(t, []string{"legacy_savings_origin"}, reasons)
	spent, _ := f.row(foodLine.ID)
	require.Equal(t, 7.0, spent, "a withheld run writes nothing")
}

// TestUnbudgetedFactReasonIsReportedAndWithholds is a REGRESSION GUARD for the
// withhold rule implemented in todo 3 (commit cf94e866). It pins the rule's
// attribution semantics: a per-fact reason on a fact that matches NO budgeted
// line is reported AND withholds the whole run, while the SAME reason on a fact
// that DOES match a budgeted line is RETAINED. The negative control is what
// proves the rule discriminates by attribution rather than by the mere presence
// of a reason.
//
// The guard's value is that it fails if the withhold rule regresses. Reverting
// the `len(ledger.Reasons) > 0` barrier to its pre-todo-3 abort makes the
// unbudgeted case return an error instead of withholding (observed RED); a rule
// that published the unbudgeted case would fail the withheld assertion.
func TestUnbudgetedFactReasonIsReportedAndWithholds(t *testing.T) {
	food, travel := "FOOD_AND_DRINK", "TRAVEL"

	t.Run("unbudgeted fact reason is reported and withholds", func(t *testing.T) {
		f := newActualsTaxonomyFixture(t)
		goal := f.addGoal("USD")
		foodLine := f.addCategory("pfc", 7, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
		// A fact that matches no budgeted line, carrying a per-fact reason
		// (legacy_unknown contribution -> legacy_savings_origin).
		unbudgeted := f.addTransaction(50, travel, "USD", nil)
		f.addContribution(goal, &unbudgeted, 1, "legacy_unknown")

		_, reasons, withheld, err := f.publish(nil)
		require.NoError(t, err, "an unbudgeted reason must withhold, not abort")
		require.True(t, withheld, "a per-fact reason on an unbudgeted fact withholds the run")
		require.Equal(t, []string{"legacy_savings_origin"}, reasons)
		spent, _ := f.row(foodLine.ID)
		require.Equal(t, 7.0, spent, "a withheld run writes no spent_amount")
	})

	t.Run("the same reason on a matched fact is retained, not withheld", func(t *testing.T) {
		f := newActualsTaxonomyFixture(t)
		goal := f.addGoal("USD")
		foodLine := f.addCategory("pfc", 7, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
		// The same per-fact reason, but on a fact that DOES match the budgeted line.
		matched := f.addTransaction(100, food, "USD", nil)
		f.addContribution(goal, &matched, 1, "legacy_unknown")

		_, reasons, withheld, err := f.publish(nil)
		require.NoError(t, err)
		require.False(t, withheld, "a reason attributable to a line must RETAIN, not withhold")
		require.Equal(t, []string{"legacy_savings_origin"}, reasons)
		spent, _ := f.row(foodLine.ID)
		require.Equal(t, 7.0, spent, "a retained line keeps its prior spent_amount")
	})
}

// TestWithheldSyncReturnsPriorIncome pins the /sync income restore: the sync
// path zeroes the income base before publishing, so a withheld run must restore
// the prior income on the returned budget and leave the row untouched.
func TestWithheldSyncReturnsPriorIncome(t *testing.T) {
	food := "FOOD_AND_DRINK"
	f := newActualsTaxonomyFixture(t)
	foodLine := f.addCategory("pfc", 7, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
	f.addTransaction(100, food, "EUR", nil)
	_, err := f.tx.Exec(f.ctx, `UPDATE budgets SET income_amount=200 WHERE id=$1`, f.budget.ID)
	require.NoError(t, err)

	fresh, err := f.repo.RecalculateBudgetActuals(f.ctx, f.user, f.budget.ID, true)
	require.NoError(t, err, "a withheld sync must not fail the caller")
	require.NotNil(t, fresh)
	require.Equal(t, 200.0, fresh.IncomeAmount, "a withheld sync returns the prior income, not the zeroed base")

	var persisted float64
	require.NoError(t, f.tx.QueryRow(f.ctx, `SELECT income_amount FROM budgets WHERE id=$1`, f.budget.ID).Scan(&persisted))
	require.Equal(t, 200.0, persisted, "a withheld sync must not mutate income_amount")
	spent, _ := f.row(foodLine.ID)
	require.Equal(t, 7.0, spent, "a withheld sync writes no spent_amount")
}

// reasonsOrEmpty normalises a nil slice to an empty one so an empty text[]
// (which pgx may decode to nil) compares equal to []string{}.
func reasonsOrEmpty(r []string) []string {
	if r == nil {
		return []string{}
	}
	return r
}

// budgetRowReasons reads the persisted diagnostic column directly, independent
// of any user-facing read path, so a test can distinguish "written" from
// "returned".
func (f *actualsTaxonomyFixture) budgetRowReasons() []string {
	f.t.Helper()
	var reasons []string
	require.NoError(f.t, f.tx.QueryRow(f.ctx, `SELECT actuals_incomplete_reasons FROM budgets WHERE id=$1`, f.budget.ID).Scan(&reasons))
	return reasons
}

// requireAllUserFacingReadsReturn asserts the three user-facing budget reads —
// GetByID, the list read (GetByUserID) and the current read (GetActiveByUserID)
// — each carry want, which is what the last publish recorded.
func (f *actualsTaxonomyFixture) requireAllUserFacingReadsReturn(want []string) {
	f.t.Helper()

	byID, err := f.repo.GetByID(f.ctx, f.budget.ID)
	require.NoError(f.t, err)
	require.Equal(f.t, want, reasonsOrEmpty(byID.ActualsIncompleteReasons), "GetByID must return the last-publish reasons")

	list, err := f.repo.GetByUserID(f.ctx, f.user)
	require.NoError(f.t, err)
	listBudget := findBudgetByID(list, f.budget.ID)
	require.NotNil(f.t, listBudget, "budget missing from GetByUserID")
	require.Equal(f.t, want, reasonsOrEmpty(listBudget.ActualsIncompleteReasons), "the list read must return the last-publish reasons")

	active, err := f.repo.GetActiveByUserID(f.ctx, f.user)
	require.NoError(f.t, err)
	activeBudget := findBudgetByID(active, f.budget.ID)
	require.NotNil(f.t, activeBudget, "budget missing from GetActiveByUserID")
	require.Equal(f.t, want, reasonsOrEmpty(activeBudget.ActualsIncompleteReasons), "the current read must return the last-publish reasons")
}

func findBudgetByID(budgets []*domain.Budget, id string) *domain.Budget {
	for _, b := range budgets {
		if b.ID == id {
			return b
		}
	}
	return nil
}

// publishOn drives the private publish path against an arbitrary Querier (the
// fixture tx, or a savepoint) so a test can roll a FATAL publish back.
func (f *actualsTaxonomyFixture) publishOn(q Querier, sources []domain.IncomeSource) ([]domain.BudgetCategory, []string, bool, error) {
	f.t.Helper()
	period := domain.CurrentBudgetActualsRange(f.budget, time.Now())
	income := f.budget.IncomeAmount
	return f.repo.publishBudgetActualsInTx(f.ctx, q, f.user, f.budget.ID, period.Start, period.ActualThrough, &income, sources)
}

// TestUserFacingBudgetReadsReturnPersistedReasons pins todo 6's read contract:
// the three user-facing budget reads return exactly what the last publish
// recorded. It covers a RETAIN run (reasons written on the publish path), a
// WITHHOLD run (reasons written on the withhold path, which writes no
// spent_amount) and a clean run (the column is emptied to '{}').
func TestUserFacingBudgetReadsReturnPersistedReasons(t *testing.T) {
	food := "FOOD_AND_DRINK"

	t.Run("a retain publish's reasons reach all three reads", func(t *testing.T) {
		f := newActualsTaxonomyFixture(t)
		goal := f.addGoal("USD")
		f.addCategory("savings", 7, func(c *domain.BudgetCategory) { c.SavingsGoalID = &goal })
		f.addCategory("pfc", 3, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
		f.addContribution(goal, nil, 20, "legacy_unknown")
		f.addTransaction(100, food, "USD", nil)

		_, reasons, withheld, err := f.publish(nil)
		require.NoError(t, err)
		require.False(t, withheld)
		require.Equal(t, []string{"legacy_unknown"}, reasons)

		require.Equal(t, []string{"legacy_unknown"}, reasonsOrEmpty(f.budgetRowReasons()), "the column records the last publish")
		f.requireAllUserFacingReadsReturn([]string{"legacy_unknown"})
	})

	t.Run("a withheld publish's reasons reach all three reads", func(t *testing.T) {
		f := newActualsTaxonomyFixture(t)
		foodLine := f.addCategory("pfc", 7, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
		f.addTransaction(100, food, "EUR", nil)

		_, reasons, withheld, err := f.publish(nil)
		require.NoError(t, err)
		require.True(t, withheld)
		require.Equal(t, []string{"currency_mismatch"}, reasons)

		require.Equal(t, []string{"currency_mismatch"}, reasonsOrEmpty(f.budgetRowReasons()), "a withheld run still records its reasons")
		f.requireAllUserFacingReadsReturn([]string{"currency_mismatch"})
		spent, _ := f.row(foodLine.ID)
		require.Equal(t, 7.0, spent, "a withheld run writes no spent_amount")
	})

	t.Run("a clean publish is empty in the column and all three reads", func(t *testing.T) {
		f := newActualsTaxonomyFixture(t)
		foodLine := f.addCategory("pfc", 7, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
		f.addTransaction(100, food, "USD", nil)

		_, reasons, withheld, err := f.publish(nil)
		require.NoError(t, err)
		require.False(t, withheld)
		require.Empty(t, reasons)

		require.Empty(t, reasonsOrEmpty(f.budgetRowReasons()), "a clean publish writes '{}'")
		f.requireAllUserFacingReadsReturn([]string{})
		spent, _ := f.row(foodLine.ID)
		require.Equal(t, 100.0, spent)
	})
}

// TestReasonsClearedAfterCleanPublish proves the publish path CLEARS a stale
// diagnostic: a degraded run records a reason, and a later clean publish writes
// '{}' rather than leaving the previous reasons in place. The degraded
// condition is made transient by reclassifying the contribution's flow_kind
// (an UPDATE, never a history DELETE).
func TestReasonsClearedAfterCleanPublish(t *testing.T) {
	food := "FOOD_AND_DRINK"
	f := newActualsTaxonomyFixture(t)
	goal := f.addGoal("USD")
	f.addCategory("savings", 7, func(c *domain.BudgetCategory) { c.SavingsGoalID = &goal })
	f.addCategory("pfc", 3, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
	f.addContribution(goal, nil, 20, "legacy_unknown")
	f.addTransaction(100, food, "USD", nil)

	_, reasons, withheld, err := f.publish(nil)
	require.NoError(t, err)
	require.False(t, withheld)
	require.Equal(t, []string{"legacy_unknown"}, reasons)
	require.Equal(t, []string{"legacy_unknown"}, reasonsOrEmpty(f.budgetRowReasons()))

	// The contribution is now a real contribution, so the run is clean.
	_, err = f.tx.Exec(f.ctx, `UPDATE savings_goal_contributions SET flow_kind='contribution' WHERE savings_goal_id=$1`, goal)
	require.NoError(t, err)

	_, reasons, withheld, err = f.publish(nil)
	require.NoError(t, err)
	require.False(t, withheld)
	require.Empty(t, reasons)
	require.Empty(t, reasonsOrEmpty(f.budgetRowReasons()), "a clean publish clears the prior reasons")
	f.requireAllUserFacingReadsReturn([]string{})
}

// TestFATALPublishLeavesNoHalfWrittenReasons pins that a FATAL structural
// defect aborts before any write: after a degraded run recorded reasons, a
// FATAL publish inside a rolled-back savepoint leaves the prior reasons intact
// and writes no spent_amount. A write that ran before the FATAL check would
// change the column and be caught here.
func TestFATALPublishLeavesNoHalfWrittenReasons(t *testing.T) {
	food := "FOOD_AND_DRINK"
	f := newActualsTaxonomyFixture(t)
	goal := f.addGoal("USD")
	savingsLine := f.addCategory("savings", 7, func(c *domain.BudgetCategory) { c.SavingsGoalID = &goal })
	foodLine := f.addCategory("pfc", 3, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
	f.addContribution(goal, nil, 20, "legacy_unknown")
	f.addTransaction(100, food, "USD", nil)

	_, reasons, withheld, err := f.publish(nil)
	require.NoError(t, err)
	require.False(t, withheld)
	require.Equal(t, []string{"legacy_unknown"}, reasons)
	require.Equal(t, []string{"legacy_unknown"}, reasonsOrEmpty(f.budgetRowReasons()))
	beforeFood, _ := f.row(foodLine.ID)
	beforeSavings, _ := f.row(savingsLine.ID)

	savepoint, err := f.tx.Begin(f.ctx)
	require.NoError(t, err)
	fatalIncome := []domain.IncomeSource{{Type: domain.IncomeSourceTypeManual, Value: "not-a-number"}}
	_, _, _, err = f.publishOn(savepoint, fatalIncome)
	require.Error(t, err)
	require.ErrorIs(t, err, domain.ErrBudgetActualsIncomplete)

	// Before the rollback: the FATAL path itself wrote nothing.
	require.Equal(t, []string{"legacy_unknown"}, reasonsOrEmpty(f.budgetRowReasons()), "a FATAL must not half-write the reasons")
	afterFood, _ := f.row(foodLine.ID)
	afterSavings, _ := f.row(savingsLine.ID)
	require.Equal(t, beforeFood, afterFood)
	require.Equal(t, beforeSavings, afterSavings)

	require.NoError(t, savepoint.Rollback(f.ctx))
	require.Equal(t, []string{"legacy_unknown"}, reasonsOrEmpty(f.budgetRowReasons()), "the rolled-back FATAL leaves the prior reasons intact")
}

// TestRecalculateBudgetActualsReturnsPersistedReasons pins that the returned
// *domain.Budget carries the reasons the publish just recorded. Without the
// post-publish assignment it reports the stale pre-publish value read before
// the publish.
func TestRecalculateBudgetActualsReturnsPersistedReasons(t *testing.T) {
	food := "FOOD_AND_DRINK"
	f := newActualsTaxonomyFixture(t)
	goal := f.addGoal("USD")
	f.addCategory("savings", 7, func(c *domain.BudgetCategory) { c.SavingsGoalID = &goal })
	f.addCategory("pfc", 3, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
	f.addContribution(goal, nil, 20, "legacy_unknown")
	f.addTransaction(100, food, "USD", nil)

	fresh, err := f.repo.RecalculateBudgetActuals(f.ctx, f.user, f.budget.ID, false)
	require.NoError(t, err)
	require.NotNil(t, fresh)
	require.Equal(t, []string{"legacy_unknown"}, reasonsOrEmpty(fresh.ActualsIncompleteReasons), "the returned budget must carry the reasons the publish recorded")
	require.Equal(t, []string{"legacy_unknown"}, reasonsOrEmpty(f.budgetRowReasons()))
}

// TestRefreshBudgetActualsInTxPropagatesReasons pins todo 7's propagation
// contract for the private configuration-writer barrier: refreshBudgetActualsInTx
// must return the reasons the publish recorded (and the withheld discriminator),
// not swallow them. The five config writers adapt to this arity; the two
// interface-method callers surface the reasons. A regression that discarded the
// reasons here would leave the config-write path unable to report why a publish
// was withheld.
func TestRefreshBudgetActualsInTxPropagatesReasons(t *testing.T) {
	food, travel := "FOOD_AND_DRINK", "TRAVEL"
	shared := []string{"shared"}
	f := newActualsTaxonomyFixture(t)
	f.addCategory("pfc", 5, func(c *domain.BudgetCategory) {
		c.PFCPrimary = &food
		c.Tags = shared
		c.MatchLogic = domain.MatchLogicTagOnly
	})
	f.addCategory("pfc", 6, func(c *domain.BudgetCategory) {
		c.PFCPrimary = &travel
		c.Tags = shared
		c.MatchLogic = domain.MatchLogicTagOnly
	})
	f.addTransaction(100, food, "USD", shared)

	_, reasons, withheld, err := f.repo.refreshBudgetActualsInTx(f.ctx, f.tx, f.budget.ID)
	require.NoError(t, err)
	require.True(t, withheld, "the overlap barrier withholds")
	require.Equal(t, []string{"overlapping_individual_lines"}, reasons, "refreshBudgetActualsInTx must propagate the publish's reasons")
	require.Equal(t, []string{"overlapping_individual_lines"}, reasonsOrEmpty(f.budgetRowReasons()), "the propagated reasons are the ones persisted")
}
