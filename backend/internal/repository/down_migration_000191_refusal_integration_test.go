//go:build integration

package repository

// #1625 Task 13: 000191's down migration DOCUMENTS a case where it fails (two live rows for one
// (transaction_id, savings_goal_id) across periods) and nothing exercised it. scripts/test-migrations.sh
// only rolls back schema-only databases and says outright it does NOT prove data survives (see
// test_rollback in that script) -- this is the DATA-carrying case that script cannot reach.
//
// THE SCENARIO
//   000191's up index is partial on `archived_at IS NULL`, so a goal that leaves and re-enters the
//   contributions basis can legitimately hold TWO live (deleted_at IS NULL) rows for the same
//   (transaction_id, savings_goal_id): one archived (closed period) and one not (the new period).
//   000191's down index drops the archived_at clause and is scoped only by `deleted_at IS NULL`,
//   so both rows now collide under it -- CREATE UNIQUE INDEX fails with a natural Postgres unique
//   violation. That is the correct outcome: refuse the rollback rather than silently deleting one
//   of the two contributions to make the narrower index fit.
//
// Unlike the other down-migration guards in down_migration_refusal_integration_test.go, 000191's
// down.sql has no explicit `RAISE EXCEPTION` -- the refusal is the ordinary unique-index-violation
// error CREATE UNIQUE INDEX raises on its own ("could not create unique index ... SQLSTATE
// 23505"). This test asserts THAT failure mode specifically, not a custom message, because there
// is no custom message to find.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownMigration000191_RefusesWhenTwoLiveRowsSpanPeriods_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	down := readDownMigration(t, "000191_scope_contribution_uniqueness_to_live_period.down.sql")

	t.Run("refuses when an archived row and a live row share (transaction_id, savings_goal_id)", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck // always rolled back

		f := seedDownGuardFixture(t, ctx, tx, "n191-roundtrip")

		// Archived row: closed period, still live (not soft-deleted).
		_, err = tx.Exec(ctx, `
			INSERT INTO savings_goal_contributions
				(id, savings_goal_id, transaction_id, amount, source, occurred_on, archived_at)
			VALUES ($1, $2, $3, 25.00, 'confirmed_transfer', CURRENT_DATE - INTERVAL '30 days', NOW())`,
			uuid.New().String(), f.savingsGoalID, f.transactionID)
		require.NoError(t, err, "seed the archived (closed-period) row")

		// Live row: same transaction, same goal, new period -- allowed only because 000191's up
		// index excludes archived rows from the uniqueness check.
		_, err = tx.Exec(ctx, `
			INSERT INTO savings_goal_contributions
				(id, savings_goal_id, transaction_id, amount, source, occurred_on)
			VALUES ($1, $2, $3, 25.00, 'confirmed_transfer', CURRENT_DATE)`,
			uuid.New().String(), f.savingsGoalID, f.transactionID)
		require.NoError(t, err, "the round-trip pair must be insertable under the CURRENT (000191) index, "+
			"or this test is not exercising the case it claims to")

		var n int
		require.NoError(t, tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM savings_goal_contributions
			WHERE savings_goal_id = $1 AND transaction_id = $2 AND deleted_at IS NULL`,
			f.savingsGoalID, f.transactionID).Scan(&n))
		require.Equal(t, 2, n, "the fixture must exist or this test proves nothing")

		_, err = tx.Exec(ctx, down)
		require.Error(t, err, "000191.down.sql must refuse rather than silently deleting one of two "+
			"live cross-period contributions to make the narrower index fit")
		assert.Contains(t, err.Error(), "could not create unique index",
			"the refusal must be the natural unique-index violation, not some other unrelated failure")
		assert.Contains(t, err.Error(), "SQLSTATE 23505",
			"must be a genuine unique-violation SQLSTATE, not a different error that happens to mention indexes")
	})

	t.Run("proceeds when only one live row exists per (transaction_id, savings_goal_id)", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck // always rolled back

		f := seedDownGuardFixture(t, ctx, tx, "n191-single")

		_, err = tx.Exec(ctx, `
			INSERT INTO savings_goal_contributions
				(id, savings_goal_id, transaction_id, amount, source, occurred_on)
			VALUES ($1, $2, $3, 25.00, 'confirmed_transfer', CURRENT_DATE)`,
			uuid.New().String(), f.savingsGoalID, f.transactionID)
		require.NoError(t, err, "seed a single ordinary contribution")

		_, err = tx.Exec(ctx, down)
		require.NoError(t, err, "with no cross-period collision, 000191's down migration must proceed -- "+
			"otherwise the migration suite's down -all cycle can never pass and the guard gets deleted")
	})
}
