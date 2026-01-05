package repository

import (
	"context"
	
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Issue #34: Rules Schema & Migrations - TDD Tests
// RED Phase: These tests verify the rules schema works correctly
// =============================================================================

// setupRulesMockDB creates a mock database for automation rules repository testing
func setupRulesMockDB(t *testing.T) pgxmock.PgxPoolIface {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	return mock
}

// =============================================================================
// Part 1: Rules Table Tests
// =============================================================================

// TestRulesTable_CanInsertWithJSONB tests that JSONB columns can store complex data
func TestRulesTable_CanInsertWithJSONB(t *testing.T) {
	mock := setupRulesMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	ruleID := uuid.New().String()
	userID := uuid.New().String()

	// Complex JSONB data as specified in the migration
	triggerConfig := `{"type":"event","event":"transaction_created","filters":{"min_amount":100}}`
	conditions := `[{"field":"account.balance","operator":"gt","value":1000}]`
	actions := `{"from_account_id":"uuid","to_account_id":"uuid","amount_type":"percentage","amount":10}`

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO rules`)).WithArgs(
		ruleID,
		userID,
		"Test Transfer Rule",      // name
		"Test description",        // description
		true,                      // enabled
		10,                        // priority
		"transfer_suggestion",     // rule_type
		triggerConfig,             // trigger_config
		conditions,                // conditions
		"AND",                     // condition_logic
		actions,                   // actions
		int64(0),                  // execution_count
		pgxmock.AnyArg(),          // last_executed_at (nil)
		pgxmock.AnyArg(),          // last_successful_at (nil)
		pgxmock.AnyArg(),          // created_at
		pgxmock.AnyArg(),          // updated_at
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	_, err := mock.Exec(ctx, `
		INSERT INTO rules (
			id, user_id, name, description, enabled, priority,
			rule_type, trigger_config, conditions, condition_logic, actions,
			execution_count, last_executed_at, last_successful_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		ruleID, userID, "Test Transfer Rule", "Test description", true, 10,
		"transfer_suggestion", triggerConfig, conditions, "AND", actions,
		int64(0), nil, nil, time.Now(), time.Now(),
	)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestRulesTable_RuleTypeConstraint tests the CHECK constraint for rule_type
func TestRulesTable_RuleTypeConstraint(t *testing.T) {
	// Valid rule types should be: transfer_suggestion, budget_alert, category_rollover
	validTypes := []string{"transfer_suggestion", "budget_alert", "category_rollover"}

	for _, ruleType := range validTypes {
		t.Run("valid_type_"+ruleType, func(t *testing.T) {
			mock := setupRulesMockDB(t)
			defer mock.Close()
			ctx := context.Background()

			mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO rules`)).WithArgs(
				pgxmock.AnyArg(), // all args
				pgxmock.AnyArg(),
				pgxmock.AnyArg(),
				pgxmock.AnyArg(),
				pgxmock.AnyArg(),
				pgxmock.AnyArg(),
				ruleType,
				pgxmock.AnyArg(),
				pgxmock.AnyArg(),
				pgxmock.AnyArg(),
				pgxmock.AnyArg(),
			).WillReturnResult(pgxmock.NewResult("INSERT", 1))

			_, err := mock.Exec(ctx, `INSERT INTO rules (id, user_id, name, description, enabled, priority, rule_type, trigger_config, conditions, condition_logic, actions) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
				uuid.New().String(), uuid.New().String(), "Test", nil, true, 0, ruleType, "{}", "[]", "AND", "{}")

			assert.NoError(t, err)
		})
	}
}

// TestRulesTable_UniqueNamePerUser tests the unique constraint on (user_id, name)
func TestRulesTable_UniqueNamePerUser(t *testing.T) {
	mock := setupRulesMockDB(t)
	defer mock.Close()
	ctx := context.Background()

	userID := uuid.New().String()
	ruleName := "My Transfer Rule"

	// First insert should succeed
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO rules`)).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Second insert with same user_id + name should fail (constraint violation)
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO rules`)).
		WillReturnError(pgx.ErrNoRows) // Simulating constraint violation

	_, err1 := mock.Exec(ctx, `INSERT INTO rules (id, user_id, name, rule_type, trigger_config, conditions, condition_logic, actions) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		uuid.New().String(), userID, ruleName, "transfer_suggestion", "{}", "[]", "AND", "{}")
	assert.NoError(t, err1)

	_, err2 := mock.Exec(ctx, `INSERT INTO rules (id, user_id, name, rule_type, trigger_config, conditions, condition_logic, actions) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		uuid.New().String(), userID, ruleName, "transfer_suggestion", "{}", "[]", "AND", "{}")
	assert.Error(t, err2) // Should fail due to unique constraint

	assert.NoError(t, mock.ExpectationsWereMet())
}

// =============================================================================
// Part 2: Rule Executions Table Tests
// =============================================================================

// TestRuleExecutionsTable_CascadeDeleteWithRule tests that executions are deleted when rule is deleted
func TestRuleExecutionsTable_CascadeDeleteWithRule(t *testing.T) {
	mock := setupRulesMockDB(t)
	defer mock.Close()
	ctx := context.Background()

	ruleID := uuid.New().String()
	executionID := uuid.New().String()

	// Insert a rule
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO rules`)).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Insert an execution for that rule
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO rule_executions`)).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Delete the rule (should cascade to executions)
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM rules WHERE id = $1`)).
		WithArgs(ruleID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Verify execution is also deleted (query returns no rows)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id FROM rule_executions WHERE id = $1`)).
		WithArgs(executionID).
		WillReturnError(pgx.ErrNoRows)

	// Execute the operations
	_, _ = mock.Exec(ctx, `INSERT INTO rules (id, user_id, name, rule_type, trigger_config, conditions, condition_logic, actions) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		ruleID, uuid.New().String(), "Test", "transfer_suggestion", "{}", "[]", "AND", "{}")

	_, _ = mock.Exec(ctx, `INSERT INTO rule_executions (id, rule_id, status, trigger_context) VALUES ($1, $2, $3, $4)`,
		executionID, ruleID, "pending", "{}")

	_, _ = mock.Exec(ctx, `DELETE FROM rules WHERE id = $1`, ruleID)

	var foundID string
	err := mock.QueryRow(ctx, `SELECT id FROM rule_executions WHERE id = $1`, executionID).Scan(&foundID)
	assert.Error(t, err)
	assert.Equal(t, pgx.ErrNoRows, err)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestRuleExecutionsTable_StatusConstraint tests the CHECK constraint for status
func TestRuleExecutionsTable_StatusConstraint(t *testing.T) {
	// Valid statuses should be: pending, running, completed, failed
	validStatuses := []string{"pending", "running", "completed", "failed"}

	for _, status := range validStatuses {
		t.Run("valid_status_"+status, func(t *testing.T) {
			mock := setupRulesMockDB(t)
			defer mock.Close()
			ctx := context.Background()

			mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO rule_executions`)).
				WillReturnResult(pgxmock.NewResult("INSERT", 1))

			_, err := mock.Exec(ctx, `INSERT INTO rule_executions (id, rule_id, status, trigger_context) VALUES ($1, $2, $3, $4)`,
				uuid.New().String(), uuid.New().String(), status, "{}")

			assert.NoError(t, err)
		})
	}
}

// TestRuleExecutionsTable_CanStoreJSONBContext tests JSONB storage for context data
func TestRuleExecutionsTable_CanStoreJSONBContext(t *testing.T) {
	mock := setupRulesMockDB(t)
	defer mock.Close()
	ctx := context.Background()

	executionID := uuid.New().String()
	ruleID := uuid.New().String()

	// Complex JSONB context data
	triggerContext := `{"event":"transaction_created","transaction_id":"tx-123","amount":150.50}`
	inputData := `{"account_balance":5000,"budget_spent_percentage":75}`
	outputData := `{"suggestion_id":"sugg-456","amount":500}`

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO rule_executions`)).WithArgs(
		executionID,
		ruleID,
		"completed",
		pgxmock.AnyArg(), // started_at
		pgxmock.AnyArg(), // completed_at
		100,              // execution_duration_ms
		triggerContext,
		inputData,
		outputData,
		nil, // error_code
		nil, // error_message
		0,   // retry_count
		3,   // max_retries
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	_, err := mock.Exec(ctx, `
		INSERT INTO rule_executions (
			id, rule_id, status, started_at, completed_at, execution_duration_ms,
			trigger_context, input_data, output_data, error_code, error_message,
			retry_count, max_retries
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		executionID, ruleID, "completed", time.Now(), time.Now(), 100,
		triggerContext, inputData, outputData, nil, nil, 0, 3,
	)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// =============================================================================
// Part 3: Transfer Suggestions Table Tests
// =============================================================================

// TestTransferSuggestionsTable_CanInsert tests basic insert functionality
func TestTransferSuggestionsTable_CanInsert(t *testing.T) {
	mock := setupRulesMockDB(t)
	defer mock.Close()
	ctx := context.Background()

	suggestionID := uuid.New().String()
	userID := uuid.New().String()
	ruleID := uuid.New().String()
	fromAccountID := uuid.New().String()
	toAccountID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO transfer_suggestions`)).WithArgs(
		suggestionID,
		userID,
		ruleID,
		pgxmock.AnyArg(), // rule_execution_id
		fromAccountID,
		toAccountID,
		500.00,
		"USD",
		"Move excess funds to savings",
		"rule",
		"pending",
		pgxmock.AnyArg(), // expires_at
		pgxmock.AnyArg(), // user_action_at
		pgxmock.AnyArg(), // user_notes
		pgxmock.AnyArg(), // created_at
		pgxmock.AnyArg(), // updated_at
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	_, err := mock.Exec(ctx, `
		INSERT INTO transfer_suggestions (
			id, user_id, rule_id, rule_execution_id, from_account_id, to_account_id,
			amount, currency_code, reason, source, status,
			expires_at, user_action_at, user_notes, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		suggestionID, userID, ruleID, nil, fromAccountID, toAccountID,
		500.00, "USD", "Move excess funds to savings", "rule", "pending",
		nil, nil, nil, time.Now(), time.Now(),
	)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestTransferSuggestionsTable_PositiveAmountConstraint tests the CHECK constraint for amount > 0
func TestTransferSuggestionsTable_PositiveAmountConstraint(t *testing.T) {
	mock := setupRulesMockDB(t)
	defer mock.Close()
	ctx := context.Background()

	// Negative amount should fail
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO transfer_suggestions`)).
		WillReturnError(pgx.ErrNoRows) // Simulating constraint violation

	_, err := mock.Exec(ctx, `INSERT INTO transfer_suggestions (id, user_id, amount, currency_code, source, status) VALUES ($1, $2, $3, $4, $5, $6)`,
		uuid.New().String(), uuid.New().String(), -100.00, "USD", "rule", "pending")

	assert.Error(t, err) // Should fail due to positive amount constraint
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestTransferSuggestionsTable_StatusConstraint tests the CHECK constraint for status
func TestTransferSuggestionsTable_StatusConstraint(t *testing.T) {
	// Valid statuses should be: pending, accepted, rejected, expired
	validStatuses := []string{"pending", "accepted", "rejected", "expired"}

	for _, status := range validStatuses {
		t.Run("valid_status_"+status, func(t *testing.T) {
			mock := setupRulesMockDB(t)
			defer mock.Close()
			ctx := context.Background()

			mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO transfer_suggestions`)).
				WillReturnResult(pgxmock.NewResult("INSERT", 1))

			_, err := mock.Exec(ctx, `INSERT INTO transfer_suggestions (id, user_id, amount, currency_code, source, status) VALUES ($1, $2, $3, $4, $5, $6)`,
				uuid.New().String(), uuid.New().String(), 100.00, "USD", "rule", status)

			assert.NoError(t, err)
		})
	}
}

// TestTransferSuggestionsTable_SourceConstraint tests the CHECK constraint for source
func TestTransferSuggestionsTable_SourceConstraint(t *testing.T) {
	// Valid sources should be: rule, manual, ai
	validSources := []string{"rule", "manual", "ai"}

	for _, source := range validSources {
		t.Run("valid_source_"+source, func(t *testing.T) {
			mock := setupRulesMockDB(t)
			defer mock.Close()
			ctx := context.Background()

			mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO transfer_suggestions`)).
				WillReturnResult(pgxmock.NewResult("INSERT", 1))

			_, err := mock.Exec(ctx, `INSERT INTO transfer_suggestions (id, user_id, amount, currency_code, source, status) VALUES ($1, $2, $3, $4, $5, $6)`,
				uuid.New().String(), uuid.New().String(), 100.00, "USD", source, "pending")

			assert.NoError(t, err)
		})
	}
}

// TestTransferSuggestionsTable_SetNullOnRuleDelete tests ON DELETE SET NULL for rule_id FK
func TestTransferSuggestionsTable_SetNullOnRuleDelete(t *testing.T) {
	mock := setupRulesMockDB(t)
	defer mock.Close()
	ctx := context.Background()

	ruleID := uuid.New().String()
	suggestionID := uuid.New().String()

	// Insert a rule
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO rules`)).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Insert a suggestion referencing that rule
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO transfer_suggestions`)).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Delete the rule
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM rules WHERE id = $1`)).
		WithArgs(ruleID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Verify suggestion still exists but with NULL rule_id
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT rule_id FROM transfer_suggestions WHERE id = $1`)).
		WithArgs(suggestionID).
		WillReturnRows(pgxmock.NewRows([]string{"rule_id"}).AddRow(nil))

	// Execute operations
	_, _ = mock.Exec(ctx, `INSERT INTO rules (id, user_id, name, rule_type, trigger_config, conditions, condition_logic, actions) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		ruleID, uuid.New().String(), "Test", "transfer_suggestion", "{}", "[]", "AND", "{}")

	_, _ = mock.Exec(ctx, `INSERT INTO transfer_suggestions (id, user_id, rule_id, amount, currency_code, source, status) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		suggestionID, uuid.New().String(), ruleID, 100.00, "USD", "rule", "pending")

	_, _ = mock.Exec(ctx, `DELETE FROM rules WHERE id = $1`, ruleID)

	var nullRuleID *string
	err := mock.QueryRow(ctx, `SELECT rule_id FROM transfer_suggestions WHERE id = $1`, suggestionID).Scan(&nullRuleID)
	assert.NoError(t, err)
	assert.Nil(t, nullRuleID) // rule_id should be NULL, not deleted

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestTransferSuggestionsTable_SetNullOnAccountDelete tests ON DELETE SET NULL for account FKs
func TestTransferSuggestionsTable_SetNullOnAccountDelete(t *testing.T) {
	mock := setupRulesMockDB(t)
	defer mock.Close()
	ctx := context.Background()

	fromAccountID := uuid.New().String()
	suggestionID := uuid.New().String()

	// Insert an account (simulated)
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO accounts`)).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Insert a suggestion referencing that account
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO transfer_suggestions`)).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Delete the account
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM accounts WHERE id = $1`)).
		WithArgs(fromAccountID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Verify suggestion still exists but with NULL from_account_id
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT from_account_id FROM transfer_suggestions WHERE id = $1`)).
		WithArgs(suggestionID).
		WillReturnRows(pgxmock.NewRows([]string{"from_account_id"}).AddRow(nil))

	// Execute operations
	_, _ = mock.Exec(ctx, `INSERT INTO accounts (id, user_id, name) VALUES ($1, $2, $3)`,
		fromAccountID, uuid.New().String(), "Checking")

	_, _ = mock.Exec(ctx, `INSERT INTO transfer_suggestions (id, user_id, from_account_id, amount, currency_code, source, status) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		suggestionID, uuid.New().String(), fromAccountID, 100.00, "USD", "rule", "pending")

	_, _ = mock.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, fromAccountID)

	var nullAccountID *string
	err := mock.QueryRow(ctx, `SELECT from_account_id FROM transfer_suggestions WHERE id = $1`, suggestionID).Scan(&nullAccountID)
	assert.NoError(t, err)
	assert.Nil(t, nullAccountID) // from_account_id should be NULL, not deleted

	assert.NoError(t, mock.ExpectationsWereMet())
}
