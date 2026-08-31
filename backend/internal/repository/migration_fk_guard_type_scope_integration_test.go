//go:build integration

package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #1569 r18 T2. Five migrations add a history-bearing ON DELETE CASCADE behind an existence guard.
// Those guards used to test only the constraint NAME (plus, since r17, the table), never the
// constraint TYPE — and information_schema.table_constraints lists CHECK, UNIQUE and PRIMARY KEY
// constraints alongside foreign keys. A same-named non-FK constraint therefore answered the guard,
// the ADD was skipped, and:
//
//   - in 000164 the VALIDATE sits INSIDE the same IF, so the migration exits 0 with the cascade
//     simply absent — reproduced before the fix (exit 0, zero FKs to users);
//   - in 000065/000097 the VALIDATE sits after the IF, and VALIDATE CONSTRAINT accepts a CHECK, so
//     it passes too — also silent.
//
// The cascade to users is what makes GDPR erasure reach rule_execution_history and boost_executions
// (see CLAUDE.md § Financial History Is Retained by Default), so a silently-absent edge is the
// failure mode that matters.
//
// These tests run the migrations' own bytes: each guard's DO $$ ... END $$ block is extracted from
// the file verbatim (plus its trailing VALIDATE, when the file has one) and executed. Whole-file
// execution is not usable here: 000065 is not re-runnable on its own against a HEAD-state schema,
// because a later migration (000083/000158) replaced its UNIQUE constraint
// unique_budget_category_discriminated with a partial unique INDEX of the same name, which step 2g
// then collides with. That is pre-existing and out of scope for this task.
//
// Everything runs inside a transaction that is always rolled back, so the shared integration
// database is left exactly as it was found.

type fkGuardCase struct {
	name       string // constraint name
	migration  string // path to the .up.sql that owns the guard
	childTable string
	childCol   string
	parentTbl  string
}

func fkGuardCases() []fkGuardCase {
	return []fkGuardCase{
		{
			name:       "fk_budget_categories_custom_category",
			migration:  "../../migrations/000065_pfc_v2_canonical.up.sql",
			childTable: "budget_categories",
			childCol:   "custom_category_id",
			parentTbl:  "custom_categories",
		},
		{
			name:       "fk_contributions_savings_goal",
			migration:  "../../migrations/000097_savings_progress_and_budget_linkage.up.sql",
			childTable: "savings_goal_contributions",
			childCol:   "savings_goal_id",
			parentTbl:  "savings_goals",
		},
		{
			name:       "fk_budget_categories_savings_goal",
			migration:  "../../migrations/000097_savings_progress_and_budget_linkage.up.sql",
			childTable: "budget_categories",
			childCol:   "savings_goal_id",
			parentTbl:  "savings_goals",
		},
		{
			name:       "fk_rule_execution_history_user_id",
			migration:  "../../migrations/000164_audit_trail_survives_definition_purge.up.sql",
			childTable: "rule_execution_history",
			childCol:   "user_id",
			parentTbl:  "users",
		},
		{
			name:       "fk_boost_executions_user_id",
			migration:  "../../migrations/000164_audit_trail_survives_definition_purge.up.sql",
			childTable: "boost_executions",
			childCol:   "user_id",
			parentTbl:  "users",
		},
	}
}

// extractFKGuardBlock returns the verbatim `DO $$ ... END $$;` block that adds constraintName,
// together with the `... VALIDATE CONSTRAINT constraintName;` statement that follows it outside the
// block, when the file places one there. It fails the test rather than guessing.
func extractFKGuardBlock(t *testing.T, path, constraintName string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	sql := string(raw)

	addIdx := strings.Index(sql, "ADD CONSTRAINT "+constraintName+"\n")
	require.GreaterOrEqual(t, addIdx, 0, "%s must contain an ADD CONSTRAINT for %s", path, constraintName)

	start := strings.LastIndex(sql[:addIdx], "DO $$")
	require.GreaterOrEqual(t, start, 0, "the ADD for %s must sit inside a DO $$ block", constraintName)

	endMarker := "END $$;"
	end := strings.Index(sql[addIdx:], endMarker)
	require.GreaterOrEqual(t, end, 0, "the DO block for %s must be terminated", constraintName)
	end = addIdx + end + len(endMarker)

	block := sql[start:end]

	// Take the trailing VALIDATE only when it is the very next statement in the file.
	rest := strings.TrimLeft(sql[end:], "\n")
	if line, _, ok := strings.Cut(rest, "\n"); ok &&
		strings.HasPrefix(line, "ALTER TABLE ") &&
		strings.Contains(line, "VALIDATE CONSTRAINT "+constraintName+";") {
		block += "\n" + line
	}

	require.Contains(t, block, "FOREIGN KEY",
		"extracted the wrong region of %s for %s", path, constraintName)
	return block
}

// withRolledBackTx runs fn inside a transaction that is always rolled back.
func withRolledBackTx(t *testing.T, fn func(ctx context.Context, tx pgx.Tx)) {
	t.Helper()

	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err, "begin transaction")
	defer func() {
		require.NoError(t, tx.Rollback(context.Background()), "roll back the decoy transaction")
	}()

	fn(ctx, tx)
}

func dropRealFK(t *testing.T, ctx context.Context, tx pgx.Tx, c fkGuardCase) {
	t.Helper()
	_, err := tx.Exec(ctx, fmt.Sprintf(
		`ALTER TABLE public.%s DROP CONSTRAINT %s`, c.childTable, c.name))
	require.NoError(t, err,
		"%s must exist on public.%s before this test can remove it", c.name, c.childTable)
}

func fkDefOnPublicChild(ctx context.Context, tx pgx.Tx, c fkGuardCase) (string, error) {
	var def string
	err := tx.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = ('public.' || $1)::regclass
		  AND conname = $2
		  AND contype = 'f'`, c.childTable, c.name).Scan(&def)
	return def, err
}

// TestMigrationFKGuards_SameNamedNonForeignKeyMustNotSuppressTheCascade plants a CHECK constraint
// carrying the foreign key's own name on the foreign key's own table, then runs the guard's real
// bytes. Before the fix all five guards accepted the CHECK as proof the FK existed and skipped the
// ADD without reporting anything.
//
// Constraint names are unique per table, so once the guard demands a FOREIGN KEY the only two
// outcomes available are (1) the ADD runs and fails loudly on the name collision, or (2) the FK is
// present. What must never happen again is the third outcome: success with no cascade.
func TestMigrationFKGuards_SameNamedNonForeignKeyMustNotSuppressTheCascade(t *testing.T) {
	for _, c := range fkGuardCases() {
		t.Run(c.name, func(t *testing.T) {
			block := extractFKGuardBlock(t, c.migration, c.name)

			withRolledBackTx(t, func(ctx context.Context, tx pgx.Tx) {
				dropRealFK(t, ctx, tx, c)

				_, err := tx.Exec(ctx, fmt.Sprintf(
					`ALTER TABLE public.%s ADD CONSTRAINT %s CHECK (%s IS NOT NULL OR %s IS NULL)`,
					c.childTable, c.name, c.childCol, c.childCol))
				require.NoError(t, err, "plant the same-named CHECK decoy")

				if _, err := tx.Exec(ctx, block); err != nil {
					// Loud refusal. Acceptable: the operator sees the schema anomaly.
					assert.Contains(t, err.Error(), c.name,
						"the failure must name the constraint it could not add")
					return
				}

				def, err := fkDefOnPublicChild(ctx, tx, c)
				require.NoError(t, err,
					"the guard reported success while public.%s has NO FOREIGN KEY named %s: a "+
						"same-named non-FK constraint suppressed the ADD and the migration passed "+
						"with the cascade absent", c.childTable, c.name)
				assert.Contains(t, def, "REFERENCES "+c.parentTbl,
					"the added constraint must be the intended edge")
			})
		})
	}
}

// TestMigrationFKGuards_SameNamedConstraintInAnotherSchemaMustNotSuppressTheCascade plants a
// same-named foreign key on a same-named table in a second visible schema. The three
// information_schema guards filtered on constraint_name and table_name only, so the decoy answered
// them while the unqualified ALTER TABLE below still targeted the search_path table — the ADD was
// skipped and the trailing VALIDATE then errored, leaving the migration failed and the FK absent.
// The two pg_constraint guards resolve `'x'::regclass` through the search_path and were already
// immune; they are included so the property is pinned for all five.
func TestMigrationFKGuards_SameNamedConstraintInAnotherSchemaMustNotSuppressTheCascade(t *testing.T) {
	for i, c := range fkGuardCases() {
		t.Run(c.name, func(t *testing.T) {
			block := extractFKGuardBlock(t, c.migration, c.name)
			decoySchema := fmt.Sprintf("decoy_fk_guard_%d", i)

			withRolledBackTx(t, func(ctx context.Context, tx pgx.Tx) {
				dropRealFK(t, ctx, tx, c)

				for _, stmt := range []string{
					fmt.Sprintf(`CREATE SCHEMA %s`, decoySchema),
					fmt.Sprintf(`CREATE TABLE %s.%s (id uuid PRIMARY KEY)`, decoySchema, c.parentTbl),
					fmt.Sprintf(`CREATE TABLE %s.%s (%s uuid)`, decoySchema, c.childTable, c.childCol),
					fmt.Sprintf(`ALTER TABLE %s.%s ADD CONSTRAINT %s FOREIGN KEY (%s) `+
						`REFERENCES %s.%s(id) ON DELETE CASCADE`,
						decoySchema, c.childTable, c.name, c.childCol, decoySchema, c.parentTbl),
				} {
					_, err := tx.Exec(ctx, stmt)
					require.NoError(t, err, "build the other-schema decoy: %s", stmt)
				}

				_, err := tx.Exec(ctx, block)
				require.NoError(t, err,
					"the guard must ignore %s.%s and add the edge to the search_path table",
					decoySchema, c.childTable)

				def, err := fkDefOnPublicChild(ctx, tx, c)
				require.NoError(t, err,
					"a same-named constraint in %s suppressed the ADD on public.%s",
					decoySchema, c.childTable)
				assert.Contains(t, def, "REFERENCES "+c.parentTbl,
					"the added constraint must be the intended edge")
				assert.Contains(t, def, "ON DELETE",
					"the added constraint must keep its ON DELETE action")
			})
		})
	}
}
