//go:build integration

package repository

// Production DLQ entry, 2026-09-11: webhook_processing job 9 (webhook_type HOLDINGS, item
// vbPwMYDb0bIzy6LayKKvCpveak1AMJfMXBAwj) burned all five attempts with the identical error
//
//	failed to replace holdings for account ae944376-3d01-41eb-bc3e-f275a3c36c02:
//	ERROR: duplicate key value violates unique constraint "unique_account_security" (SQLSTATE 23505)
//
// Two causes produce that error from perfectly ordinary Plaid data. This file is about the second
// one — a concurrent replace of the same account — and it establishes it as a FACT against real
// Postgres rather than as a plausible story:
//
//   - TestHoldingReplace_RawDeleteInsertRacesOnTheUniqueConstraint_Integration reproduces the bare
//     mechanism with two hand-driven transactions, no repository involved. It is the evidence that
//     the DELETE+INSERT shape is unsafe under READ COMMITTED at all.
//   - TestHoldingReplace_ConcurrentReplacesOfOneAccountAllSucceed_Integration is the regression
//     test for the fix: ReplaceForAccount now takes a per-account transaction-scoped advisory
//     lock, so concurrent replaces queue instead of colliding.
//
// The payload-duplicate cause is covered separately and at the layer where it arises, in
// services/investment_sync_duplicate_holding_test.go.

import (
	"context"
	"sync"
	"testing"

	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHoldingReplace_RawDeleteInsertRacesOnTheUniqueConstraint_Integration proves the mechanism.
//
// unique_account_security is UNIQUE (account_id, security_id). Two transactions each doing
// "DELETE the account's holdings, then INSERT the current ones" cannot see each other's
// uncommitted rows, so B's DELETE removes nothing of A's work; B's INSERT of the same security
// then waits on A's uncommitted index tuple and raises 23505 the instant A commits. No duplicate
// exists in either payload — one holding each.
func TestHoldingReplace_RawDeleteInsertRacesOnTheUniqueConstraint_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, pool, "inv-dlq-race-raw")
	t.Cleanup(func() { cleanupInvestmentFixture(context.Background(), t, pool, userID, accountID) })

	insert := `INSERT INTO holdings (id, user_id, account_id, security_id, quantity)
	           VALUES ($1, $2, $3, $4, $5)`
	del := `DELETE FROM holdings WHERE account_id = $1 AND user_id = $2`

	txA, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer txA.Rollback(ctx) //nolint:errcheck
	txB, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer txB.Rollback(ctx) //nolint:errcheck

	// Both replaces start from the same (empty) visible state — the ordinary case, not a contrived
	// one: this is what two HOLDINGS webhook jobs for one item do.
	_, err = txA.Exec(ctx, del, accountID, userID)
	require.NoError(t, err)
	_, err = txB.Exec(ctx, del, accountID, userID)
	require.NoError(t, err)

	_, err = txA.Exec(ctx, insert, uuid.New(), userID, accountID, secID, 10)
	require.NoError(t, err)

	// B's INSERT blocks on A's uncommitted index tuple, so it has to run concurrently with A's
	// commit rather than before it.
	bErr := make(chan error, 1)
	go func() {
		_, e := txB.Exec(ctx, insert, uuid.New(), userID, accountID, secID, 4)
		bErr <- e
	}()

	require.NoError(t, txA.Commit(ctx))
	err = <-bErr

	require.Error(t, err, "the second replace MUST fail — this is the production error")
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "expected a PgError, got %T", err)
	assert.Equal(t, "23505", pgErr.Code)
	assert.Equal(t, "unique_account_security", pgErr.ConstraintName,
		"the exact constraint named in the dead-lettered job")
}

// TestHoldingReplace_ConcurrentReplacesOfOneAccountAllSucceed_Integration is the regression test.
//
// Concurrency is genuinely reachable: the webhook path takes no linked-item lock, and River's
// webhook_processing uniqueness is ByArgs over args that include webhook_id, so two HOLDINGS
// webhooks for one item are two jobs that can run at once. Every one of these replaces carries a
// clean, single-security payload; none may fail.
func TestHoldingReplace_ConcurrentReplacesOfOneAccountAllSucceed_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, pool, "inv-dlq-race-fix")
	t.Cleanup(func() { cleanupInvestmentFixture(context.Background(), t, pool, userID, accountID) })

	repo := NewHoldingRepository(pool)

	const workers, rounds = 6, 4
	errs := make(chan error, workers*rounds)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				_, err := repo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{{
					UserID:     userID.String(),
					AccountID:  accountID.String(),
					SecurityID: secID.String(),
					Quantity:   float64(10 + w),
				}})
				if err != nil {
					errs <- err
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		assert.NoError(t, err,
			"a concurrent replace of one account must queue on the advisory lock, never surface 23505")
	}

	// And the end state is exactly one row: a replace is still a replace.
	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM holdings WHERE account_id = $1`, accountID).Scan(&count))
	assert.Equal(t, 1, count, "the last writer wins; the replace must not accumulate rows")
}

// cleanupInvestmentFixture removes the committed fixture. These two tests commit (a race needs
// separate connections, so the usual single-transaction-and-rollback pattern cannot express it),
// so they clean up after themselves explicitly. Child rows first: holding_snapshots and
// holding_observations are keyed on the account and are written by the replace itself.
func cleanupInvestmentFixture(ctx context.Context, t *testing.T, pool DB, userID, accountID uuid.UUID) {
	t.Helper()
	for _, stmt := range []string{
		`DELETE FROM holding_observations WHERE account_id = $1`,
		`DELETE FROM holding_snapshots WHERE account_id = $1`,
		`DELETE FROM holdings WHERE account_id = $1`,
	} {
		if _, err := pool.Exec(ctx, stmt, accountID); err != nil {
			t.Logf("cleanup %q: %v", stmt, err)
		}
	}
	// users cascades to accounts, linked_items and the rest (migration 000051's FKs).
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Logf("cleanup users: %v", err)
	}
}
