//go:build integration

package repository

// Issue #1569 Task 8 — the real-database half of the config-parent tombstone work.
//
// pgxmock asserts query STRINGS and never executes them; three compile-clean, execute-broken
// queries were found in Task 7 alone. Everything that actually decides whether this task is
// correct is semantics — whether a contribution row still exists after a delete, whether
// `deleted_at IS NULL` genuinely hides a goal from the reads that used to show it, whether the
// audit entry and the state change share a transaction, what the FK graph does on erasure, and
// whether the partial unique indexes free a name. None of that is observable against a mock, so it
// is all here.

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

type configTombstoneFixture struct {
	tx     pgx.Tx
	userID string

	accountID     string
	linkedItemID  string
	userAccountID string

	goalRepo    SavingsGoalRepository
	contribRepo SavingsContributionRepository
	boostRepo   BoostsRepository
	budgetRepo  BudgetRepository
	auditRepo   AuditLogRepository

	goalID       string
	contribIDs   []string
	boostID      string
	boostExecID  string
	budgetID     string
	categoryID   string
	rolloverID   string
	periodEvtID  string
	goalCategory string
}

// seedConfigTombstoneFixture builds one user carrying one of each of the four config parents, each
// with a HISTORY child that its DELETE used to cascade away:
//
//	savings goal     -> two savings_goal_contributions (one hand-logged, one reconciler-derived)
//	                 -> one auto-created savings budget_category
//	boost            -> one boost_execution
//	budget           -> one rollover_history row and one budget_period_event
//	budget category  -> the same rollover_history row, as its source_budget_category_id
func seedConfigTombstoneFixture(t *testing.T) *configTombstoneFixture {
	t.Helper()
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	f := &configTombstoneFixture{
		tx:            tx,
		userID:        uuid.New().String(),
		accountID:     uuid.New().String(),
		linkedItemID:  uuid.New().String(),
		userAccountID: uuid.New().String(),
		goalID:        uuid.New().String(),
		boostID:       uuid.New().String(),
		boostExecID:   uuid.New().String(),
		budgetID:      uuid.New().String(),
		categoryID:    uuid.New().String(),
		rolloverID:    uuid.New().String(),
		periodEvtID:   uuid.New().String(),
		goalCategory:  uuid.New().String(),
	}
	f.goalRepo = NewSavingsGoalRepository(tx)
	f.contribRepo = NewSavingsContributionRepository(tx)
	f.boostRepo = NewBoostsRepository(tx)
	f.budgetRepo = NewBudgetRepository(tx)
	f.auditRepo = NewAuditLogRepository(tx)

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	mustExec("user", `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, f.userID, "test|"+f.userID, "config-tomb-1569-"+f.userID+"@example.com")

	mustExec("user_account", `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, 'ins-config-tomb-1569', 'depository')
	`, f.userAccountID, f.userID)

	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, f.linkedItemID, f.userID, "config-tomb-item-"+f.linkedItemID)

	mustExec("account", `
		INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, $5, 'Config Tombstone Savings', 'depository', 'active')
	`, f.accountID, f.linkedItemID, f.userID, f.userAccountID, "plaid-acc-cfg-"+f.accountID)

	mustExec("savings_goal", `
		INSERT INTO savings_goals (id, user_id, name, target_amount, current_amount, account_id, progress_basis)
		VALUES ($1, $2, 'Emergency Fund', 5000, 900, $3, 'contributions')
	`, f.goalID, f.userID, f.accountID)

	// Two contributions. The hand-logged one is the whole point of this task: it is money the user
	// typed in, and no provider can supply it again.
	for i, spec := range []struct {
		source  string
		managed bool
		amount  float64
	}{
		{"manual", false, 400},
		{"confirmed_transfer", true, 500},
	} {
		id := uuid.New().String()
		f.contribIDs = append(f.contribIDs, id)
		mustExec("savings_goal_contribution", `
			INSERT INTO savings_goal_contributions
				(id, savings_goal_id, amount, source, status, managed_by_reconciliation, confirmed_by_user, occurred_on, flow_kind)
			VALUES ($1, $2, $3, $4, 'confirmed', $5, true, CURRENT_DATE - $6::int, 'contribution')
		`, id, f.goalID, spec.amount, spec.source, spec.managed, i)
	}

	mustExec("boost", `
		INSERT INTO boosts (id, user_id, name, boost_type, enabled)
		VALUES ($1, $2, 'Round Up To Savings', 'savings_goal', true)
	`, f.boostID, f.userID)

	mustExec("boost_execution", `
		INSERT INTO boost_executions (id, boost_id, status, completed_at, user_id)
		VALUES ($1, $2, 'completed', NOW(), $3)
	`, f.boostExecID, f.boostID, f.userID)

	mustExec("budget", `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, series_id)
		VALUES ($1, $2, 'August Budget', 'monthly', CURRENT_DATE - 30, 3000, $1)
	`, f.budgetID, f.userID)

	mustExec("budget_category", `
		INSERT INTO budget_categories (id, budget_id, category_kind, pfc_primary, allocated_amount, spent_amount)
		VALUES ($1, $2, 'pfc', 'FOOD_AND_DRINK', 500, 320)
	`, f.categoryID, f.budgetID)

	// The savings category a goal auto-creates in every active budget. Deleting the goal used to
	// remove this row by ON DELETE CASCADE; it must keep disappearing, but as a tombstone.
	mustExec("savings budget_category", `
		INSERT INTO budget_categories (id, budget_id, category_kind, savings_goal_id, allocated_amount, auto_created)
		VALUES ($1, $2, 'savings', $3, 250, true)
	`, f.goalCategory, f.budgetID, f.goalID)

	mustExec("rollover_history", `
		INSERT INTO rollover_history
			(id, user_id, budget_id, source_budget_category_id, amount, rollover_type,
			 budget_period_start, budget_period_end, status)
		VALUES ($1, $2, $3, $4, 180, 'full', NOW() - INTERVAL '30 days', NOW(), 'applied')
	`, f.rolloverID, f.userID, f.budgetID, f.categoryID)

	mustExec("budget_period_event", `
		INSERT INTO budget_period_events (id, user_id, budget_id, period_start, period_end, event_type)
		VALUES ($1, $2, $3, CURRENT_DATE - 30, CURRENT_DATE, 'closed')
	`, f.periodEvtID, f.userID, f.budgetID)

	return f
}

func (f *configTombstoneFixture) count(t *testing.T, ctx context.Context, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, f.tx.QueryRow(ctx, sql, args...).Scan(&n))
	return n
}

// auditActions returns every audit_logs action recorded for resourceID.
func (f *configTombstoneFixture) auditActions(t *testing.T, ctx context.Context, resourceID string) []string {
	t.Helper()
	rows, err := f.tx.Query(ctx, `SELECT action FROM audit_logs WHERE resource_id = $1 ORDER BY action`, resourceID)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		require.NoError(t, rows.Scan(&a))
		out = append(out, a)
	}
	require.NoError(t, rows.Err())
	return out
}

// ---------------------------------------------------------------------------------------------
// 1. The savings goal — the worst of the four, because the cascaded rows are hand-logged money.
// ---------------------------------------------------------------------------------------------

func TestConfigTombstone_DeletingAGoalKeepsEveryContribution_Integration(t *testing.T) {
	f := seedConfigTombstoneFixture(t)
	ctx := context.Background()

	before := f.count(t, ctx, `SELECT COUNT(*) FROM savings_goal_contributions WHERE savings_goal_id = $1`, f.goalID)
	require.Equal(t, 2, before, "fixture must seed two contributions or this test proves nothing")

	require.NoError(t, f.goalRepo.Delete(ctx, f.goalID, f.userID, f.auditRepo))

	assert.Equal(t, 2,
		f.count(t, ctx, `SELECT COUNT(*) FROM savings_goal_contributions WHERE savings_goal_id = $1`, f.goalID),
		"deleting the goal must leave every contribution row physically present — that is the whole task")

	// And the goal row itself is still there, tombstoned rather than gone.
	assert.Equal(t, 1,
		f.count(t, ctx, `SELECT COUNT(*) FROM savings_goals WHERE id = $1 AND deleted_at IS NOT NULL`, f.goalID))
	var reason, by string
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT deleted_reason, deleted_by FROM savings_goals WHERE id = $1`, f.goalID).Scan(&reason, &by))
	assert.Equal(t, domain.SoftDeleteReasonUser, reason)
	assert.Equal(t, domain.SoftDeletedByUser, by)

	assert.Equal(t, []string{"savings_goal.deleted"}, f.auditActions(t, ctx, f.goalID),
		"a destructive verb with no recorded cause is not auditable")
}

// TestConfigTombstone_DeletedGoalIsInvisibleEverywhere_Integration is the hiding half. A retained
// row that still shows up in a list, a sum or an ownership probe is not retention — it is a delete
// button that does not work.
//
// MUTATION-CHECKED: removing `AND deleted_at IS NULL` from any single one of the ten reads below
// turns exactly the sub-test naming it RED. Verified by hand for GetByIDForUser, GetByUserID,
// CountByUserID, ListByGoal and SumConfirmedByGoal.
func TestConfigTombstone_DeletedGoalIsInvisibleEverywhere_Integration(t *testing.T) {
	f := seedConfigTombstoneFixture(t)
	ctx := context.Background()

	// Prove each read SEES the goal first. Without this, every assertion below would also pass
	// against a query that returns nothing for any input.
	pre, err := f.goalRepo.GetByUserID(ctx, f.userID)
	require.NoError(t, err)
	require.Len(t, pre, 1, "the fixture goal must be visible before the delete")

	require.NoError(t, f.goalRepo.Delete(ctx, f.goalID, f.userID, f.auditRepo))

	t.Run("GetByID", func(t *testing.T) {
		_, err := f.goalRepo.GetByID(ctx, f.goalID)
		assert.ErrorIs(t, err, domain.ErrSavingsGoalNotFound)
	})
	t.Run("GetByIDForUser", func(t *testing.T) {
		_, err := f.goalRepo.GetByIDForUser(ctx, f.goalID, f.userID)
		assert.ErrorIs(t, err, domain.ErrSavingsGoalNotFound)
	})
	t.Run("GetByUserID", func(t *testing.T) {
		goals, err := f.goalRepo.GetByUserID(ctx, f.userID)
		require.NoError(t, err)
		assert.Empty(t, goals)
	})
	t.Run("GetActiveByUserID", func(t *testing.T) {
		goals, err := f.goalRepo.GetActiveByUserID(ctx, f.userID)
		require.NoError(t, err)
		assert.Empty(t, goals)
	})
	t.Run("GetByName", func(t *testing.T) {
		_, err := f.goalRepo.GetByName(ctx, f.userID, "Emergency Fund")
		assert.ErrorIs(t, err, domain.ErrSavingsGoalNotFound)
	})
	t.Run("GetByAccountID", func(t *testing.T) {
		goals, err := f.goalRepo.GetByAccountID(ctx, f.accountID)
		require.NoError(t, err)
		assert.Empty(t, goals)
	})
	t.Run("CountByUserID", func(t *testing.T) {
		n, err := f.goalRepo.CountByUserID(ctx, f.userID)
		require.NoError(t, err)
		assert.Zero(t, n, "a tombstoned goal must not consume the user's tier quota")
	})
	t.Run("contributions ListByGoal", func(t *testing.T) {
		got, err := f.contribRepo.ListByGoal(ctx, f.userID, f.goalID)
		require.NoError(t, err)
		assert.Empty(t, got, "the goal is gone, so its contribution list must read as gone too")
	})
	t.Run("contributions SumConfirmedByGoal", func(t *testing.T) {
		total, err := f.contribRepo.SumConfirmedByGoal(ctx, f.userID, f.goalID)
		require.NoError(t, err)
		assert.Zero(t, total, "a deleted goal's contributions must not keep counting toward any total")
	})
	t.Run("the auto-created savings budget category goes with it", func(t *testing.T) {
		cats, err := f.budgetRepo.GetCategoriesByBudgetID(ctx, f.budgetID)
		require.NoError(t, err)
		for _, c := range cats {
			assert.NotEqual(t, f.goalCategory, c.ID,
				"a savings category budgeting for a deleted goal must not outlive it")
		}
		var reason string
		require.NoError(t, f.tx.QueryRow(ctx,
			`SELECT deleted_reason FROM budget_categories WHERE id = $1`, f.goalCategory).Scan(&reason))
		assert.Equal(t, domain.SoftDeleteReasonParent, reason)
	})
}

// TestConfigTombstone_DeletedGoalFreesItsName_Integration pins the reason migration 000158 had to
// convert three UNIQUE CONSTRAINTS into partial UNIQUE INDEXES. Without that, "Emergency Fund"
// could be created exactly once per account, ever, and the second attempt would fail against a
// goal the user cannot see.
func TestConfigTombstone_DeletedGoalFreesItsName_Integration(t *testing.T) {
	f := seedConfigTombstoneFixture(t)
	ctx := context.Background()

	require.NoError(t, f.goalRepo.Delete(ctx, f.goalID, f.userID, f.auditRepo))

	replacement := &domain.SavingsGoal{
		ID: uuid.New().String(), UserID: f.userID, Name: "Emergency Fund",
		TargetAmount: 6000, AccountID: f.accountID, ProgressBasis: domain.ProgressBasisManual,
	}
	assert.NoError(t, f.goalRepo.Create(ctx, replacement),
		"the tombstoned goal must not hold its name hostage")

	// The constraint is still real for LIVE rows.
	dup := &domain.SavingsGoal{
		ID: uuid.New().String(), UserID: f.userID, Name: "Emergency Fund",
		TargetAmount: 10, AccountID: f.accountID, ProgressBasis: domain.ProgressBasisManual,
	}
	assert.ErrorIs(t, f.goalRepo.Create(ctx, dup), domain.ErrSavingsGoalAlreadyExists,
		"a partial index must still reject a duplicate among live rows, and the repository must "+
			"still map it by index NAME")

	// MUTATION PROOF, run in-line rather than by hand-editing the migration: DDL is transactional
	// in Postgres, so this swaps the partial index back for the unconditional one migration 000158
	// replaced, inside the fixture's own transaction, and the whole thing rolls back afterwards.
	// If the WHERE clause were not load-bearing this sub-test would pass.
	t.Run("without the partial predicate the name stays taken", func(t *testing.T) {
		other := seedConfigTombstoneFixture(t)
		octx := context.Background()
		_, err := other.tx.Exec(octx, `DROP INDEX unique_savings_goal_name_per_user`)
		require.NoError(t, err)
		_, err = other.tx.Exec(octx,
			`CREATE UNIQUE INDEX unique_savings_goal_name_per_user ON savings_goals (user_id, name)`)
		require.NoError(t, err)

		require.NoError(t, other.goalRepo.Delete(octx, other.goalID, other.userID, other.auditRepo))
		blocked := &domain.SavingsGoal{
			ID: uuid.New().String(), UserID: other.userID, Name: "Emergency Fund",
			TargetAmount: 6000, AccountID: other.accountID, ProgressBasis: domain.ProgressBasisManual,
		}
		assert.ErrorIs(t, other.goalRepo.Create(octx, blocked), domain.ErrSavingsGoalAlreadyExists,
			"this is the failure migration 000158's partial index exists to prevent")
	})
}

// TestConfigTombstone_PurgeIsGatedOnTheDelete_Integration pins the two-step. Destroying hand-logged
// money is never something one request can do by accident.
func TestConfigTombstone_PurgeIsGatedOnTheDelete_Integration(t *testing.T) {
	f := seedConfigTombstoneFixture(t)
	ctx := context.Background()

	assert.ErrorIs(t, f.goalRepo.PurgeGoal(ctx, f.goalID, f.userID, f.auditRepo),
		domain.ErrSavingsGoalNotDeleted, "a live goal must not be purgeable")
	assert.Equal(t, 2,
		f.count(t, ctx, `SELECT COUNT(*) FROM savings_goal_contributions WHERE savings_goal_id = $1`, f.goalID),
		"a refused purge must change nothing")

	require.NoError(t, f.goalRepo.Delete(ctx, f.goalID, f.userID, f.auditRepo))
	require.NoError(t, f.goalRepo.PurgeGoal(ctx, f.goalID, f.userID, f.auditRepo))

	assert.Zero(t, f.count(t, ctx, `SELECT COUNT(*) FROM savings_goals WHERE id = $1`, f.goalID))
	assert.Zero(t, f.count(t, ctx, `SELECT COUNT(*) FROM savings_goal_contributions WHERE savings_goal_id = $1`, f.goalID),
		"the purge is the act that actually destroys; if it did not, the two-step would be theatre")
	assert.Equal(t, []string{"savings_goal.deleted", "savings_goal.history_purged"},
		f.auditActions(t, ctx, f.goalID))
}

// TestConfigTombstone_PurgeRefusesAnotherUsersGoal_Integration — the purge query is the only place
// ownership is enforced on that path (the service cannot re-read a tombstoned goal to compare).
func TestConfigTombstone_PurgeRefusesAnotherUsersGoal_Integration(t *testing.T) {
	f := seedConfigTombstoneFixture(t)
	ctx := context.Background()
	require.NoError(t, f.goalRepo.Delete(ctx, f.goalID, f.userID, f.auditRepo))

	stranger := uuid.New().String()
	assert.ErrorIs(t, f.goalRepo.PurgeGoal(ctx, f.goalID, stranger, f.auditRepo), domain.ErrSavingsGoalNotFound)
	assert.Equal(t, 1, f.count(t, ctx, `SELECT COUNT(*) FROM savings_goals WHERE id = $1`, f.goalID),
		"a cross-user purge must destroy nothing")
}

// ---------------------------------------------------------------------------------------------
// 2-4. The other three parents keep their history.
// ---------------------------------------------------------------------------------------------

func TestConfigTombstone_DeletingABoostKeepsItsExecutions_Integration(t *testing.T) {
	f := seedConfigTombstoneFixture(t)
	ctx := context.Background()

	require.NoError(t, f.boostRepo.Delete(ctx, f.boostID, f.userID, f.auditRepo))

	assert.Equal(t, 1, f.count(t, ctx, `SELECT COUNT(*) FROM boost_executions WHERE id = $1`, f.boostExecID),
		"fk_boost_executions_boost_id is ON DELETE CASCADE; the tombstone is what stops it firing")

	// Still reachable through the repository, not merely present in the table.
	exec, err := NewBoostExecutionRepository(f.tx).GetByID(ctx, f.boostExecID)
	require.NoError(t, err)
	assert.Equal(t, f.boostExecID, exec.ID)

	got, err := f.boostRepo.GetByUserID(ctx, f.userID)
	require.NoError(t, err)
	assert.Empty(t, got, "the boost itself must be invisible")

	assert.Equal(t, []string{"boost.deleted"}, f.auditActions(t, ctx, f.boostID))
}

// TestConfigTombstone_DeletedBoostKeepsItsSuggestionsProvenance_Integration pins the ONE deliberate
// behaviour change Task 8 makes outside the four delete paths themselves, and pins BOTH halves of
// it so neither can drift silently.
//
// fk_transfer_suggestions_boost_id is ON DELETE SET NULL. Under the OLD hard delete, deleting a
// boost NULLed transfer_suggestions.boost_id, and every suggestion that boost had ever produced
// lost its provenance line: "proposed by <boost>" went blank. Under the tombstone the link
// survives and the name keeps rendering.
//
// That is RULED CORRECT, not tolerated. A transfer suggestion is a historical record; naming the
// automation that produced it stays true after the user deletes that automation, and blanking it
// was itself a small instance of the destroy-the-explanation failure #1569 exists to end. The
// unfiltered `LEFT JOIN boosts r` in GetByUserIDStatuses therefore carries an explicit
// `-- tombstone-exempt:` comment; before fix round 1 it carried nothing and was waved through by
// the loose guard.
//
// The containment argument, which is what makes the exemption safe rather than merely intended:
// r contributes exactly one expression to that query (r.name AS boost_name), so a tombstoned boost
// cannot re-enter any list, count or action surface through this join. Asserted below by reading
// the boost back through its OWN repository and finding nothing.
func TestConfigTombstone_DeletedBoostKeepsItsSuggestionsProvenance_Integration(t *testing.T) {
	f := seedConfigTombstoneFixture(t)
	ctx := context.Background()

	suggestionID := uuid.New().String()
	_, err := f.tx.Exec(ctx, `
		INSERT INTO transfer_suggestions
			(id, user_id, boost_id, boost_execution_id, from_account_id, to_account_id,
			 amount, currency_code, reason, source, status)
		VALUES ($1, $2, $3, $4, $5, $5, 125.00, 'USD', 'Round up', 'boost', 'pending')
	`, suggestionID, f.userID, f.boostID, f.boostExecID, f.accountID)
	require.NoError(t, err, "seed transfer_suggestion")

	suggestionRepo := NewTransferSuggestionRepository(f.tx)

	before, _, err := suggestionRepo.GetByUserIDStatuses(ctx, f.userID, nil, 50, 0)
	require.NoError(t, err)
	require.Len(t, before, 1)
	require.NotNil(t, before[0].BoostName, "the provenance must be present BEFORE the delete, or "+
		"the assertion after it proves nothing")
	require.Equal(t, "Round Up To Savings", *before[0].BoostName)

	require.NoError(t, f.boostRepo.Delete(ctx, f.boostID, f.userID, f.auditRepo))

	// Half one: the FK link SURVIVES, where the old hard delete would have SET NULL'd it.
	var boostID *string
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT boost_id FROM transfer_suggestions WHERE id = $1`, suggestionID).Scan(&boostID))
	require.NotNil(t, boostID,
		"ON DELETE SET NULL must not have fired — that is precisely what the tombstone prevents")
	assert.Equal(t, f.boostID, *boostID)

	// Half two: the rendered provenance is still there.
	after, _, err := suggestionRepo.GetByUserIDStatuses(ctx, f.userID, nil, 50, 0)
	require.NoError(t, err)
	require.Len(t, after, 1, "the suggestion itself is unaffected by the boost's deletion")
	require.NotNil(t, after[0].BoostName,
		"a suggestion must still be able to say which automation proposed it")
	assert.Equal(t, "Round Up To Savings", *after[0].BoostName)

	// Containment: the boost is reachable ONLY as that one name. It is gone from its own surface.
	live, err := f.boostRepo.GetByUserID(ctx, f.userID)
	require.NoError(t, err)
	assert.Empty(t, live, "the exemption must not let a tombstoned boost back into any boost list")
	_, err = f.boostRepo.GetByID(ctx, f.boostID)
	assert.ErrorIs(t, err, domain.ErrBoostNotFound)
}

func TestConfigTombstone_BoostDeleteRefusesAnotherUsersBoost_Integration(t *testing.T) {
	f := seedConfigTombstoneFixture(t)
	ctx := context.Background()

	// Before this task the statement was `DELETE FROM boosts WHERE id = $1` — no ownership
	// predicate at all. It was not exploitable (boostService.DeleteBoost checks first), but the
	// repository depended on a check it could not see.
	stranger := uuid.New().String()
	assert.ErrorIs(t, f.boostRepo.Delete(ctx, f.boostID, stranger, f.auditRepo), domain.ErrBoostNotFound)
	assert.Zero(t, f.count(t, ctx, `SELECT COUNT(*) FROM boosts WHERE id = $1 AND deleted_at IS NOT NULL`, f.boostID))
}

func TestConfigTombstone_DeletingABudgetKeepsItsHistory_Integration(t *testing.T) {
	f := seedConfigTombstoneFixture(t)
	ctx := context.Background()

	require.NoError(t, f.budgetRepo.Delete(ctx, f.budgetID, f.userID, f.auditRepo))

	assert.Equal(t, 1, f.count(t, ctx, `SELECT COUNT(*) FROM rollover_history WHERE id = $1`, f.rolloverID),
		"fk_rollover_history_budget_id is ON DELETE CASCADE")
	assert.Equal(t, 1, f.count(t, ctx, `SELECT COUNT(*) FROM budget_period_events WHERE id = $1`, f.periodEvtID),
		"fk_budget_period_events_budget is ON DELETE CASCADE")

	got, err := f.budgetRepo.GetByUserID(ctx, f.userID)
	require.NoError(t, err)
	assert.Empty(t, got)

	cats, err := f.budgetRepo.GetCategoriesByBudgetID(ctx, f.budgetID)
	require.NoError(t, err)
	assert.Empty(t, cats, "a deleted budget's categories must go with it")

	assert.Equal(t, []string{"budget.deleted"}, f.auditActions(t, ctx, f.budgetID))
}

func TestConfigTombstone_DeletingABudgetCategoryKeepsItsRollovers_Integration(t *testing.T) {
	f := seedConfigTombstoneFixture(t)
	ctx := context.Background()

	require.NoError(t, f.budgetRepo.DeleteCategory(ctx, f.categoryID, f.budgetID, f.auditRepo))

	assert.Equal(t, 1,
		f.count(t, ctx, `SELECT COUNT(*) FROM rollover_history WHERE source_budget_category_id = $1`, f.categoryID),
		"fk_rollover_history_source_budget_category_id is ON DELETE CASCADE and the column is NOT "+
			"NULL, so SET NULL was never an option — tombstoning the parent is")

	cats, err := f.budgetRepo.GetCategoriesByBudgetID(ctx, f.budgetID)
	require.NoError(t, err)
	for _, c := range cats {
		assert.NotEqual(t, f.categoryID, c.ID)
	}

	_, err = f.budgetRepo.GetCategoryByIDAndUser(ctx, f.categoryID, f.userID)
	assert.Error(t, err, "a tombstoned category must not pass an ownership probe")

	assert.Equal(t, []string{"budget_category.deleted"}, f.auditActions(t, ctx, f.categoryID))
}

// ---------------------------------------------------------------------------------------------
// 5. Atomicity. A failure mid-operation must leave NOTHING half-applied — not the tombstone
//    without the audit row, and not the audit row without the tombstone.
// ---------------------------------------------------------------------------------------------

// failingAuditLog satisfies AuditLogRepository and fails only inside a transaction, which is the
// last statement of each of the four deletes. If the delete were not transactional, the tombstone
// would already be committed by the time this fires.
type failingAuditLog struct{ err error }

func (f failingAuditLog) Log(ctx context.Context, action, resourceType, resourceID string) error {
	return f.err
}
func (f failingAuditLog) LogInTx(ctx context.Context, tx pgx.Tx, action, resourceType, resourceID string) error {
	return f.err
}

func TestConfigTombstone_AFailedAuditRollsBackTheWholeDelete_Integration(t *testing.T) {
	boom := failingAuditLog{err: assertAnError}

	t.Run("savings goal", func(t *testing.T) {
		f := seedConfigTombstoneFixture(t)
		ctx := context.Background()
		require.Error(t, f.goalRepo.Delete(ctx, f.goalID, f.userID, boom))
		assert.Equal(t, 1,
			f.count(t, ctx, `SELECT COUNT(*) FROM savings_goals WHERE id = $1 AND deleted_at IS NULL`, f.goalID),
			"the goal must still be live")
		assert.Equal(t, 1,
			f.count(t, ctx, `SELECT COUNT(*) FROM budget_categories WHERE id = $1 AND deleted_at IS NULL`, f.goalCategory),
			"and so must the category the same transaction tombstoned")
	})

	t.Run("boost", func(t *testing.T) {
		f := seedConfigTombstoneFixture(t)
		ctx := context.Background()
		require.Error(t, f.boostRepo.Delete(ctx, f.boostID, f.userID, boom))
		assert.Equal(t, 1,
			f.count(t, ctx, `SELECT COUNT(*) FROM boosts WHERE id = $1 AND deleted_at IS NULL`, f.boostID))
	})

	t.Run("budget", func(t *testing.T) {
		f := seedConfigTombstoneFixture(t)
		ctx := context.Background()
		require.Error(t, f.budgetRepo.Delete(ctx, f.budgetID, f.userID, boom))
		assert.Equal(t, 1,
			f.count(t, ctx, `SELECT COUNT(*) FROM budgets WHERE id = $1 AND deleted_at IS NULL`, f.budgetID))
		assert.Equal(t, 2,
			f.count(t, ctx, `SELECT COUNT(*) FROM budget_categories WHERE budget_id = $1 AND deleted_at IS NULL`, f.budgetID),
			"both categories the same transaction tombstoned must be live again")
	})

	t.Run("budget category", func(t *testing.T) {
		f := seedConfigTombstoneFixture(t)
		ctx := context.Background()
		require.Error(t, f.budgetRepo.DeleteCategory(ctx, f.categoryID, f.budgetID, boom))
		assert.Equal(t, 1,
			f.count(t, ctx, `SELECT COUNT(*) FROM budget_categories WHERE id = $1 AND deleted_at IS NULL`, f.categoryID))
	})

	t.Run("savings goal purge", func(t *testing.T) {
		f := seedConfigTombstoneFixture(t)
		ctx := context.Background()
		require.NoError(t, f.goalRepo.Delete(ctx, f.goalID, f.userID, f.auditRepo))
		require.Error(t, f.goalRepo.PurgeGoal(ctx, f.goalID, f.userID, boom))
		assert.Equal(t, 1, f.count(t, ctx, `SELECT COUNT(*) FROM savings_goals WHERE id = $1`, f.goalID))
		assert.Equal(t, 2,
			f.count(t, ctx, `SELECT COUNT(*) FROM savings_goal_contributions WHERE savings_goal_id = $1`, f.goalID),
			"a purge that could not be audited must not have destroyed anything")
	})
}

// ---------------------------------------------------------------------------------------------
// 6. GDPR. A tombstoned row is still a row, and erasure must still destroy it.
// ---------------------------------------------------------------------------------------------

func TestConfigTombstone_GDPRErasureStillDestroysTombstonedRows_Integration(t *testing.T) {
	f := seedConfigTombstoneFixture(t)
	ctx := context.Background()

	require.NoError(t, f.goalRepo.Delete(ctx, f.goalID, f.userID, f.auditRepo))
	require.NoError(t, f.boostRepo.Delete(ctx, f.boostID, f.userID, f.auditRepo))
	require.NoError(t, f.budgetRepo.Delete(ctx, f.budgetID, f.userID, f.auditRepo))

	require.Equal(t, 2,
		f.count(t, ctx, `SELECT COUNT(*) FROM savings_goal_contributions WHERE savings_goal_id = $1`, f.goalID),
		"the contributions must be present BEFORE erasure, or the assertion below proves nothing")

	// THE PRODUCTION PATH, not a hand-written `DELETE FROM users`. An approximation would only
	// re-assert what the FK graph does; the thing worth pinning is that ProfileRepository's real
	// cascade -- the method a right-to-be-forgotten request actually runs -- still destroys these
	// rows after Task 8 gave four of their tables a soft delete. A future change to
	// DeleteUserCascade that started respecting deleted_at would break this test, and would break
	// nothing if the test issued its own DELETE.
	profileRepo := NewProfileRepository(f.tx, newTestFieldEncryptor(t))

	// DeleteUserCascade's C2 guard refuses while any linked_items row still owes Plaid-side
	// cleanup (`plaid_removed_at IS NULL`). The fixture seeds a live item, so mark it cleaned --
	// this is the state the deletion worker reaches before it calls the cascade, not a bypass.
	_, err := f.tx.Exec(ctx,
		`UPDATE linked_items SET plaid_removed_at = NOW() WHERE user_id = $1`, f.userID)
	require.NoError(t, err)

	// Put the user in the USER-REQUESTED authorisation state -- an outstanding, already-due
	// deletion request. That is the literal right-to-be-forgotten path, and the one this test is
	// about; the other branch DeleteUserCascade accepts is retention expiry, which is a different
	// question. expectedDeletionRequestedAt must be the value the caller observed, so it is read
	// back rather than assumed.
	var requestedAt time.Time
	require.NoError(t, f.tx.QueryRow(ctx, `
		UPDATE users
		SET deletion_requested_at = NOW() - INTERVAL '31 days',
		    deletion_scheduled_at = NOW() - INTERVAL '1 day'
		WHERE id = $1
		RETURNING deletion_requested_at
	`, f.userID).Scan(&requestedAt))

	deleted, affectedOwners, err := profileRepo.DeleteUserCascade(ctx, f.userID, &requestedAt)
	require.NoError(t, err, "erasure must not be blocked by anything this task added")
	require.True(t, deleted,
		"the cascade reported a legitimate skip rather than running — the assertions below would "+
			"then be vacuous (they would pass against a cascade that did nothing if the rows had "+
			"never existed, and this test would stop testing erasure at all)")
	assert.Empty(t, affectedOwners, "no other user's rows are involved in this fixture")

	for _, probe := range []struct {
		what string
		sql  string
		arg  any
	}{
		{"savings_goals", `SELECT COUNT(*) FROM savings_goals WHERE id = $1`, f.goalID},
		{"savings_goal_contributions", `SELECT COUNT(*) FROM savings_goal_contributions WHERE savings_goal_id = $1`, f.goalID},
		{"boosts", `SELECT COUNT(*) FROM boosts WHERE id = $1`, f.boostID},
		{"boost_executions", `SELECT COUNT(*) FROM boost_executions WHERE id = $1`, f.boostExecID},
		{"budgets", `SELECT COUNT(*) FROM budgets WHERE id = $1`, f.budgetID},
		{"budget_categories", `SELECT COUNT(*) FROM budget_categories WHERE budget_id = $1`, f.budgetID},
		{"rollover_history", `SELECT COUNT(*) FROM rollover_history WHERE id = $1`, f.rolloverID},
		{"budget_period_events", `SELECT COUNT(*) FROM budget_period_events WHERE id = $1`, f.periodEvtID},
	} {
		assert.Zero(t, f.count(t, ctx, probe.sql, probe.arg),
			"%s survived erasure — a tombstone must never become a way to keep data after a right-to-be-forgotten request", probe.what)
	}
}

// ---------------------------------------------------------------------------------------------
// Carried item (Ruling R25): ListByGoal was fixed in Task 7 for an ambiguous unqualified `id` that
// made it fail 100% against a real database, and it is the only one of the four affected methods
// without a dedicated integration test. This is that test — it must actually EXECUTE, which is
// exactly what pgxmock never does.
// ---------------------------------------------------------------------------------------------

func TestSavingsContributionRepository_ListByGoal_Integration(t *testing.T) {
	f := seedConfigTombstoneFixture(t)
	ctx := context.Background()

	got, err := f.contribRepo.ListByGoal(ctx, f.userID, f.goalID)
	require.NoError(t, err, "the JOIN's column list must be unambiguous against a real database")
	require.Len(t, got, 2)

	// Newest occurred_on first. The fixture seeds today and yesterday, in that order.
	assert.Equal(t, f.contribIDs[0], got[0].ID)
	assert.Equal(t, f.contribIDs[1], got[1].ID)

	// occurred_on::text is load-bearing: the column is DATE and OccurredOn is a string, which pgx
	// cannot scan from a binary-format date.
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, got[0].OccurredOn)
	assert.InDelta(t, 400.0, got[0].Amount, 0.001)

	t.Run("another user sees nothing", func(t *testing.T) {
		other, err := f.contribRepo.ListByGoal(ctx, uuid.New().String(), f.goalID)
		require.NoError(t, err)
		assert.Empty(t, other)
	})
}

// assertAnError is a sentinel for the atomicity tests. It is deliberately not a domain error: the
// point is that ANY audit failure aborts the delete, not one particular one.
var assertAnError = errTestAuditWriteRefused

type testAuditErr struct{}

func (testAuditErr) Error() string { return "audit write refused (test)" }

var errTestAuditWriteRefused error = testAuditErr{}
