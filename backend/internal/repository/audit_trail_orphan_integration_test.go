//go:build integration

package repository

// Issue #1569 Task 14 — the automation audit trail must outlive the definition that produced it.
//
// Five FKs were still ON DELETE CASCADE at the start of this task. Task 8 tombstoned four of the
// config parents, so most of these cascades no longer fire on an ordinary user delete — but
// `categorization_rules` was NOT one of the four (categorization_rule_repository.go:348 still
// issues a hard `DELETE FROM categorization_rules WHERE id = $1`, and the table has no deleted_at),
// so `rule_execution_history` is destroyed by an ordinary rule delete today. The rest fire on
// purge and GDPR paths.
//
// Everything here is a live-database test on purpose: pgxmock asserts query STRINGS and never
// executes them, and every claim in this task is about what Postgres actually does to a row when
// its parent disappears. None of that is observable against a mock.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

type auditTrailFixture struct {
	tx     pgx.Tx
	userID string

	ruleID      string
	ruleExecID  string
	boostID     string
	boostExecID string
	budgetID    string
	categoryID  string
	rolloverID  string
	periodEvtID string
}

// seedAuditTrailFixture builds one user carrying one of each history row whose FK this task flips,
// plus the budget_period_events marker whose FK this task deliberately leaves alone.
func seedAuditTrailFixture(t *testing.T) *auditTrailFixture {
	t.Helper()
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	f := &auditTrailFixture{
		tx:          tx,
		userID:      uuid.New().String(),
		ruleID:      uuid.New().String(),
		ruleExecID:  uuid.New().String(),
		boostID:     uuid.New().String(),
		boostExecID: uuid.New().String(),
		budgetID:    uuid.New().String(),
		categoryID:  uuid.New().String(),
		rolloverID:  uuid.New().String(),
		periodEvtID: uuid.New().String(),
	}

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	mustExec("user", `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, f.userID, "test|"+f.userID, "audit-trail-1569-"+f.userID+"@example.com")

	mustExec("categorization_rule", `
		INSERT INTO categorization_rules (id, user_id, name, enabled, priority, conditions, condition_logic, target_kind, target_pfc_primary)
		VALUES ($1, $2, 'Coffee is Food', true, 10,
		        '[{"field":"merchant_name","operator":"contains","value":"Blue Bottle"}]'::jsonb,
		        'AND', 'pfc', 'FOOD_AND_DRINK')
	`, f.ruleID, f.userID)

	mustExec("rule_execution_history", `
		INSERT INTO rule_execution_history (id, rule_id, transaction_id, action_type, result, user_id, rule_name, definition_snapshot)
		VALUES ($1, $2, NULL, 'category', 'applied', $3, 'Coffee is Food', $4::jsonb)
	`, f.ruleExecID, f.ruleID, f.userID, `{
		"rule_id": "`+f.ruleID+`",
		"name": "Coffee is Food",
		"enabled": true,
		"priority": 10,
		"condition_logic": "AND",
		"conditions": [{"field":"merchant_name","operator":"contains","value":"Blue Bottle"}],
		"target_kind": "pfc",
		"target_pfc_primary": "FOOD_AND_DRINK"
	}`)

	mustExec("boost", `
		INSERT INTO boosts (id, user_id, name, boost_type, enabled, priority, condition_logic)
		VALUES ($1, $2, 'Round Up To Savings', 'savings_goal', true, 5, 'AND')
	`, f.boostID, f.userID)

	mustExec("boost_execution", `
		INSERT INTO boost_executions (id, boost_id, status, completed_at, user_id, boost_name, definition_snapshot)
		VALUES ($1, $2, 'completed', NOW(), $3, 'Round Up To Savings', $4::jsonb)
	`, f.boostExecID, f.boostID, f.userID, `{
		"boost_id": "`+f.boostID+`",
		"name": "Round Up To Savings",
		"boost_type": "savings_goal",
		"enabled": true,
		"priority": 5,
		"condition_logic": "AND",
		"conditions": [],
		"actions": {"goal":"emergency"},
		"trigger_config": {"type":"event"}
	}`)

	mustExec("budget", `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, series_id)
		VALUES ($1, $2, 'August Budget', 'monthly', CURRENT_DATE - 30, 3000, $1)
	`, f.budgetID, f.userID)

	mustExec("budget_category", `
		INSERT INTO budget_categories (id, budget_id, category_kind, pfc_primary, allocated_amount, spent_amount)
		VALUES ($1, $2, 'pfc', 'FOOD_AND_DRINK', 500, 320)
	`, f.categoryID, f.budgetID)

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

func (f *auditTrailFixture) count(t *testing.T, ctx context.Context, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, f.tx.QueryRow(ctx, sql, args...).Scan(&n))
	return n
}

// ---------------------------------------------------------------------------------------------
// 1. Purging each parent leaves its history row present with a NULL parent reference.
// ---------------------------------------------------------------------------------------------

// TestAuditTrail_PurgingARuleKeepsItsExecutionHistory_Integration is the one case that is a live
// user-facing bug rather than a purge-path hardening: categorization_rules has no deleted_at, and
// CategorizationRuleRepository.Delete hard-deletes, so deleting a rule in the UI destroys the
// record of everything that rule ever did.
func TestAuditTrail_PurgingARuleKeepsItsExecutionHistory_Integration(t *testing.T) {
	f := seedAuditTrailFixture(t)
	ctx := context.Background()

	require.NoError(t, NewCategorizationRuleRepository(f.tx).Delete(ctx, f.ruleID))

	assert.Equal(t, 1,
		f.count(t, ctx, `SELECT COUNT(*) FROM rule_execution_history WHERE id = $1 AND rule_id IS NULL`, f.ruleExecID),
		"deleting the rule must leave the execution row present with rule_id NULL, not destroy it")
}

func TestAuditTrail_PurgingABoostKeepsItsExecutions_Integration(t *testing.T) {
	f := seedAuditTrailFixture(t)
	ctx := context.Background()

	_, err := f.tx.Exec(ctx, `DELETE FROM boosts WHERE id = $1`, f.boostID)
	require.NoError(t, err, "a genuine purge of the definition")

	assert.Equal(t, 1,
		f.count(t, ctx, `SELECT COUNT(*) FROM boost_executions WHERE id = $1 AND boost_id IS NULL`, f.boostExecID),
		"fk_boost_executions_boost_id must be SET NULL: the execution record is what the boost did")
}

func TestAuditTrail_PurgingABudgetKeepsItsRolloverHistory_Integration(t *testing.T) {
	f := seedAuditTrailFixture(t)
	ctx := context.Background()

	_, err := f.tx.Exec(ctx, `DELETE FROM budgets WHERE id = $1`, f.budgetID)
	require.NoError(t, err)

	// Purging the budget also purges its categories, so BOTH rollover FKs are exercised here.
	assert.Equal(t, 1,
		f.count(t, ctx, `
			SELECT COUNT(*) FROM rollover_history
			WHERE id = $1 AND budget_id IS NULL AND source_budget_category_id IS NULL`, f.rolloverID),
		"fk_rollover_history_budget_id and fk_rollover_history_source_budget_category_id must both "+
			"be SET NULL; the row records money that actually moved")
}

func TestAuditTrail_PurgingACategoryAloneKeepsItsRolloverHistory_Integration(t *testing.T) {
	f := seedAuditTrailFixture(t)
	ctx := context.Background()

	_, err := f.tx.Exec(ctx, `DELETE FROM budget_categories WHERE id = $1`, f.categoryID)
	require.NoError(t, err)

	assert.Equal(t, 1,
		f.count(t, ctx, `
			SELECT COUNT(*) FROM rollover_history
			WHERE id = $1 AND source_budget_category_id IS NULL AND budget_id = $2`, f.rolloverID, f.budgetID),
		"purging only the source category must NULL that one column and leave the budget link intact")
}

// TestAuditTrail_BudgetPeriodEventsStayCascaded_Integration pins the ONE FK of the five this task
// deliberately does NOT flip, so a later reader does not "finish the job" and reintroduce two bugs.
//
// budget_period_events is not history. It is an exactly-once marker table
// (BudgetPeriodEventsRepository: HasMarker / InsertMarker / DeleteMarker, all three keyed on the
// natural key `(user_id, budget_id, period_start, period_end, event_type)`), whose only purpose is
// to stop the daily period-close scanner enqueueing the same budget period twice. SET NULL there
// would be actively harmful three ways, each verified rather than reasoned:
//
//   - Unreadable. Every one of the three methods filters `budget_id = $2`, so a NULL-budget_id row
//     is invisible to the entire API of its own repository. Nothing else in the codebase reads the
//     table (`grep -rn budget_period_events backend --include='*.go'` outside tests: only this
//     repository, budget_repository/budget_service comments, and profile_repository's GDPR note).
//   - Meaningless. The marker suppresses re-processing of a budget period. With the budget purged
//     there is no period left to re-process, so a retained marker records nothing a user could
//     ever be shown.
//   - A GDPR leak. budget_period_events.user_id is a bare TEXT column with NO foreign key to users
//     (verified: `\d budget_period_events` lists exactly one FK, fk_budget_period_events_budget).
//     profile_repository.go's Issue #1440 Finding 3 comment states outright that the table is
//     excluded from the explicit FK-less erasure list *because* budget_id CASCADE reaches it. Flip
//     that FK and every marker of every erased user survives erasure forever.
func TestAuditTrail_BudgetPeriodEventsStayCascaded_Integration(t *testing.T) {
	f := seedAuditTrailFixture(t)
	ctx := context.Background()

	_, err := f.tx.Exec(ctx, `DELETE FROM budgets WHERE id = $1`, f.budgetID)
	require.NoError(t, err)

	assert.Equal(t, 0,
		f.count(t, ctx, `SELECT COUNT(*) FROM budget_period_events WHERE id = $1`, f.periodEvtID),
		"fk_budget_period_events_budget must stay ON DELETE CASCADE — it is the table's only path "+
			"to erasure, and an orphan marker is unreadable and meaningless")
}

// ---------------------------------------------------------------------------------------------
// 2. An orphaned row still answers "what did this automation do, and why".
// ---------------------------------------------------------------------------------------------

func TestAuditTrail_OrphanedRowStillExplainsItself_Integration(t *testing.T) {
	f := seedAuditTrailFixture(t)
	ctx := context.Background()

	require.NoError(t, NewCategorizationRuleRepository(f.tx).Delete(ctx, f.ruleID))
	_, err := f.tx.Exec(ctx, `DELETE FROM boosts WHERE id = $1`, f.boostID)
	require.NoError(t, err)

	// Scanned as pointers so a NULL column fails on the ASSERTION below rather than blowing up in
	// Scan -- a test that dies on a scan error proves nothing about what a reader would show.
	var ruleName *string
	var ruleSnap []byte
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT rule_name, definition_snapshot FROM rule_execution_history WHERE id = $1`,
		f.ruleExecID).Scan(&ruleName, &ruleSnap))
	require.NotNil(t, ruleName, "an orphan with no denormalised name cannot be labelled at all")
	assert.Equal(t, "Coffee is Food", *ruleName,
		"the denormalised name is what a reader shows where the parent's name used to go")

	require.NotEmpty(t, ruleSnap, "an orphan with no snapshot explains nothing")
	var rule map[string]any
	require.NoError(t, json.Unmarshal(ruleSnap, &rule))
	assert.Equal(t, f.ruleID, rule["rule_id"], "the snapshot must still name the definition that is gone")
	assert.Equal(t, "pfc", rule["target_kind"], "what it did")
	assert.NotEmpty(t, rule["conditions"], "why it fired")

	var boostName *string
	var boostSnap []byte
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT boost_name, definition_snapshot FROM boost_executions WHERE id = $1`,
		f.boostExecID).Scan(&boostName, &boostSnap))
	require.NotNil(t, boostName)
	assert.Equal(t, "Round Up To Savings", *boostName)

	require.NotEmpty(t, boostSnap)
	var boost map[string]any
	require.NoError(t, json.Unmarshal(boostSnap, &boost))
	assert.Equal(t, f.boostID, boost["boost_id"])
	assert.Equal(t, "savings_goal", boost["boost_type"], "what it did")
	assert.NotNil(t, boost["trigger_config"], "why it ran")
}

// ---------------------------------------------------------------------------------------------
// 3. No reader silently hides an orphan.
// ---------------------------------------------------------------------------------------------

// TestAuditTrail_OrphanedBoostExecutionIsStillReachable_Integration is the finding that shaped this
// task. Before it, EVERY user-facing read of boost_executions was keyed on `boost_id = $1`
// (GetByBoostID, GetByBoostIDWithFilter, GetLastExecutionForBoost, GetFailedExecutions), so a
// SET NULL orphan would have been physically present and invisible in every list — retention that
// reads as deletion, exactly the trap task 8 hit on savings_goal_contributions. The user_id column
// added by migration 000164 plus GetByUserID is what makes the retained row actually readable.
func TestAuditTrail_OrphanedBoostExecutionIsStillReachable_Integration(t *testing.T) {
	f := seedAuditTrailFixture(t)
	ctx := context.Background()

	_, err := f.tx.Exec(ctx, `DELETE FROM boosts WHERE id = $1`, f.boostID)
	require.NoError(t, err)

	execs, total, err := NewBoostExecutionRepository(f.tx).GetByUserID(ctx, f.userID, nil, 50, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total, "the orphan must be counted, not silently dropped")
	require.Len(t, execs, 1)
	assert.Nil(t, execs[0].BoostID, "the parent link is gone")
	require.NotNil(t, execs[0].BoostName)
	assert.Equal(t, "Round Up To Savings", *execs[0].BoostName,
		"and the reader can still say which automation this was")
}

// TestAuditTrail_OrphanedRolloverIsStillReachable_Integration. rollover_history was already safe on
// this axis — GetByUserID/GetByID are keyed on columns that survive — but the scan was not: both
// budget_id and source_budget_category_id were NOT NULL `string` fields, so the first orphan would
// have made the whole list query fail rather than hide a row. Pinned so the pointer types stay.
func TestAuditTrail_OrphanedRolloverIsStillReachable_Integration(t *testing.T) {
	f := seedAuditTrailFixture(t)
	ctx := context.Background()

	_, err := f.tx.Exec(ctx, `DELETE FROM budgets WHERE id = $1`, f.budgetID)
	require.NoError(t, err)

	rows, err := NewRolloverHistoryRepository(f.tx).GetByUserID(ctx, f.userID, 50, 0)
	require.NoError(t, err, "a NULL parent column must not break the scan")
	require.Len(t, rows, 1)
	assert.Nil(t, rows[0].BudgetID)
	assert.Nil(t, rows[0].SourceBudgetCategoryID)
	assert.InDelta(t, 180.0, rows[0].Amount, 0.001, "the money that moved is still recorded")
}

// ---------------------------------------------------------------------------------------------
// 4. GDPR erasure still destroys every one of these rows.
// ---------------------------------------------------------------------------------------------

// TestAuditTrail_GDPRErasureStillDestroysEverything_Integration is the hazard this task's flips
// create and must close. Three of the five FKs were their table's ONLY path to users:
// rule_execution_history and boost_executions have no user_id column at all, and
// budget_period_events' user_id is bare TEXT with no FK. Flipping any of them to SET NULL without
// a replacement erasure path strands an erased user's rows forever.
//
// It calls the production DeleteUserCascade rather than a hand-rolled `DELETE FROM users`, so what
// is proven is the erasure, not merely the FK graph.
func TestAuditTrail_GDPRErasureStillDestroysEverything_Integration(t *testing.T) {
	f := seedAuditTrailFixture(t)
	ctx := context.Background()

	requestedAt := time.Now().Add(-31 * 24 * time.Hour).UTC().Truncate(time.Microsecond)
	_, err := f.tx.Exec(ctx, `
		UPDATE users SET deletion_requested_at = $1, deletion_scheduled_at = $1 WHERE id = $2
	`, requestedAt, f.userID)
	require.NoError(t, err)

	profileRepo := NewProfileRepository(f.tx, newTestFieldEncryptor(t))
	deleted, _, err := profileRepo.DeleteUserCascade(ctx, f.userID, &requestedAt)
	require.NoError(t, err)
	require.True(t, deleted,
		"a guard abort returns (false, nil, nil) and would make every assertion below pass for the wrong reason")

	for _, c := range []struct {
		table string
		sql   string
		id    string
	}{
		{"rule_execution_history", `SELECT COUNT(*) FROM rule_execution_history WHERE id = $1`, f.ruleExecID},
		{"boost_executions", `SELECT COUNT(*) FROM boost_executions WHERE id = $1`, f.boostExecID},
		{"rollover_history", `SELECT COUNT(*) FROM rollover_history WHERE id = $1`, f.rolloverID},
		{"budget_period_events", `SELECT COUNT(*) FROM budget_period_events WHERE id = $1`, f.periodEvtID},
	} {
		assert.Equal(t, 0, f.count(t, ctx, c.sql, c.id),
			"%s must still be destroyed by erasure — retention is never an exemption from GDPR", c.table)
	}
}

// ---------------------------------------------------------------------------------------------
// 5. The snapshot is written at execution time, against a real database, and never rewritten.
// ---------------------------------------------------------------------------------------------

// TestAuditTrail_CreatePersistsSnapshotAndUpdateNeverRewritesIt_Integration exists because pgxmock
// asserts query STRINGS and never executes them: boost_execution_repository_test.go would happily
// pass against an INSERT naming columns that do not exist, or a column list out of step with the
// argument list. This runs the real Create/Update/GetByID against Postgres.
//
// It also pins the freeze at the storage layer rather than only in the service: Update() must not
// carry a mutated in-memory snapshot back over the stored one.
func TestAuditTrail_CreatePersistsSnapshotAndUpdateNeverRewritesIt_Integration(t *testing.T) {
	f := seedAuditTrailFixture(t)
	ctx := context.Background()
	repo := NewBoostExecutionRepository(f.tx)

	boostID := f.boostID
	name := "Round Up To Savings"
	exec := &domain.BoostExecution{
		BoostID:            &boostID,
		UserID:             f.userID,
		BoostName:          &name,
		DefinitionSnapshot: json.RawMessage(`{"boost_id":"` + f.boostID + `","name":"Round Up To Savings","boost_type":"savings_goal","enabled":true}`),
		Status:             domain.ExecutionStatusRunning,
	}
	require.NoError(t, repo.Create(ctx, exec))

	stored, err := repo.GetByID(ctx, exec.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.BoostName)
	assert.Equal(t, "Round Up To Savings", *stored.BoostName)
	assert.Equal(t, f.userID, stored.UserID)
	require.NotEmpty(t, stored.DefinitionSnapshot, "the INSERT must actually persist the snapshot column")

	// Mark it complete the way the service does, with a mutated snapshot in hand.
	stored.Status = domain.ExecutionStatusCompleted
	renamed := "Renamed After The Fact"
	stored.BoostName = &renamed
	stored.DefinitionSnapshot = json.RawMessage(`{"name":"Renamed After The Fact"}`)
	require.NoError(t, repo.Update(ctx, stored))

	after, err := repo.GetByID(ctx, exec.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ExecutionStatusCompleted, after.Status, "Update must still do its job")
	require.NotNil(t, after.BoostName)
	assert.Equal(t, "Round Up To Savings", *after.BoostName,
		"Update must not rewrite the name recorded at execution time")
	var snap map[string]any
	require.NoError(t, json.Unmarshal(after.DefinitionSnapshot, &snap))
	assert.Equal(t, "Round Up To Savings", snap["name"],
		"Update must not rewrite the definition snapshot recorded at execution time")
}

// TestAuditTrail_GetByUserIDFiltersByStatus_Integration exercises the status-filtered branch of the
// new reader against a real database (the arg-numbering shifts when the filter is present, which a
// mock would not catch).
func TestAuditTrail_GetByUserIDFiltersByStatus_Integration(t *testing.T) {
	f := seedAuditTrailFixture(t)
	ctx := context.Background()
	repo := NewBoostExecutionRepository(f.tx)

	failed := domain.ExecutionStatusFailed
	execs, total, err := repo.GetByUserID(ctx, f.userID, &failed, 50, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, total, "the seeded execution is 'completed'")
	assert.Empty(t, execs)

	completed := domain.ExecutionStatusCompleted
	execs, total, err = repo.GetByUserID(ctx, f.userID, &completed, 50, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	require.Len(t, execs, 1)
	assert.Equal(t, f.boostExecID, execs[0].ID)
}
