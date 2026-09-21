//go:build integration

package database

// Issue #1685 todo 12, Migration B: budget_hierarchy_audits is a LIVE table, so widening
// chk_budget_hierarchy_audits_outcome must re-add the constraint NOT VALID and then VALIDATE it —
// a bare ADD CONSTRAINT would hold ACCESS EXCLUSIVE for the whole validation scan. These tests
// apply the real up SQL inside a rolled-back transaction and assert the widened CHECK admits the
// new outcome while still rejecting a bogus one, that the up is idempotent, and that the down
// restores four-value enforcement for new writes without being blocked by a skipped row.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/database/... \
//	  -run TestMigration000222 -tags integration -v

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedMig222AuditFixture inserts the user + budget rows budget_hierarchy_audits' two foreign keys
// require, returning their ids.
func seedMig222AuditFixture(t *testing.T, tx pgx.Tx) (userID, budgetID string) {
	t.Helper()
	ctx := context.Background()
	userID = uuid.New().String()
	_, err := tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|mig222-"+userID, "mig222-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	budgetID = uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, expected_income, currency_code, is_active, series_id)
		VALUES ($1, $2, 'Mig 222', 'monthly', NOW(), 1000, 1000, 'USD', true, $1)
	`, budgetID, userID)
	require.NoError(t, err, "seed budget")
	return userID, budgetID
}

// insertMig222AuditRow inserts one ledger row with the given outcome, returning the raw error so a
// caller can assert on the CHECK violation.
func insertMig222AuditRow(tx pgx.Tx, budgetID, userID, outcome string) error {
	_, err := tx.Exec(context.Background(), `
		INSERT INTO budget_hierarchy_audits (budget_id, user_id, outcome, reason, categories_examined, categories_repaired)
		VALUES ($1, $2, $3, 'fixture', 0, 0)
	`, budgetID, userID, outcome)
	return err
}

// expectMig222InsertRejected runs one insert inside a SAVEPOINT and asserts the CHECK rejects it.
// The savepoint is required, not stylistic: a failed statement aborts the whole transaction, so
// without it every later statement in this test would fail with 25P02 instead of the constraint
// violation under test.
func expectMig222InsertRejected(t *testing.T, tx pgx.Tx, budgetID, userID, outcome string) {
	t.Helper()
	savepoint, err := tx.Begin(context.Background())
	require.NoError(t, err)
	defer func() { _ = savepoint.Rollback(context.Background()) }()

	err = insertMig222AuditRow(savepoint, budgetID, userID, outcome)
	require.Error(t, err, "the CHECK must reject %q", outcome)
	assert.Contains(t, err.Error(), "chk_budget_hierarchy_audits_outcome")
}

// seedMig222Budget inserts one extra budget row, since budget_hierarchy_audits is unique per
// budget_id and each insert below needs its own.
func seedMig222Budget(t *testing.T, tx pgx.Tx, userID, name string) string {
	t.Helper()
	budgetID := uuid.New().String()
	_, err := tx.Exec(context.Background(), `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, expected_income, currency_code, is_active, series_id)
		VALUES ($1, $2, $3, 'monthly', NOW(), 1000, 1000, 'USD', true, $1)
	`, budgetID, userID, name)
	require.NoError(t, err, "seed budget %s", name)
	return budgetID
}

func TestMigration000222_WidensBudgetHierarchyAuditOutcomeCheck(t *testing.T) {
	pool := connectMigrationTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	userID, budgetID := seedMig222AuditFixture(t, tx)

	upSQL := readMigrationFile(t, "000222_widen_budget_hierarchy_audit_outcome.up.sql")
	_, err = tx.Exec(ctx, upSQL)
	require.NoError(t, err, "migration 000222 failed")

	require.NoError(t, insertMig222AuditRow(tx, budgetID, userID, "skipped_incomplete_actuals"),
		"the widened CHECK must accept skipped_incomplete_actuals")

	expectMig222InsertRejected(t, tx, seedMig222Budget(t, tx, userID, "Mig 222 Bogus"), userID, "not_a_real_outcome")

	_, err = tx.Exec(ctx, upSQL)
	require.NoError(t, err, "migration 000222 must be idempotent")

	downSQL := readMigrationFile(t, "000222_widen_budget_hierarchy_audit_outcome.down.sql")
	_, err = tx.Exec(ctx, downSQL)
	require.NoError(t, err, "the down must not be blocked by a row carrying the new outcome")

	expectMig222InsertRejected(t, tx, seedMig222Budget(t, tx, userID, "Mig 222 Third"), userID, "skipped_incomplete_actuals")

	_, err = tx.Exec(ctx, downSQL)
	require.NoError(t, err, "migration 000222 down must be idempotent")
}