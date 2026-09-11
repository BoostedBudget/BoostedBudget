//go:build integration

package repository

// Production DLQ entry, 2026-09-11: webhook_processing, dead_letter_jobs.id 9 =
// river_job.id 350 (webhook_type HOLDINGS, item
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
	"time"

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
//
// ⚠ #1639: this test used to start six goroutines and assert no error, which proved NOTHING — six
// short transactions interleave only by luck, and the test passed just as happily with the
// advisory lock deleted. It now parks every replace on a barrier inside ReplaceForAccount, after
// BEGIN and before the lock (replaceForAccountTxOpenedHook), and releases them only once all six
// have arrived. The overlap is then a fact rather than a hope: with the lock they queue and all
// succeed; with the lock stubbed out they race their DELETEs and collide on
// unique_account_security exactly as production did. Verified by removing the lock and watching
// this go red.
func TestHoldingReplace_ConcurrentReplacesOfOneAccountAllSucceed_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, pool, "inv-dlq-race-fix")
	t.Cleanup(func() { cleanupInvestmentFixture(context.Background(), t, pool, userID, accountID) })

	const workers, rounds = 6, 4

	// The concrete type, because the barrier hook is not part of the interface — it is a test-only
	// field on the implementation.
	repo, ok := NewHoldingRepository(pool).(*holdingRepository)
	require.True(t, ok, "the barrier hook lives on the concrete holdingRepository")

	// Every round of every worker meets here, so all six transactions are open and past BEGIN
	// before any of them reaches the DELETE.
	barrier := newCyclicBarrier(workers)
	repo.replaceForAccountTxOpenedHook = func() { barrier.wait(t) }
	t.Cleanup(func() { repo.replaceForAccountTxOpenedHook = nil })

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

// cyclicBarrier releases all `parties` waiters at once, then rearms for the next round. The
// standard library has no barrier (sync.WaitGroup is one-shot and cannot be reused per round), and
// a barrier is the only construct that turns "these transactions might overlap" into "these
// transactions DO overlap", which is the whole difference between a concurrency test that can fail
// and one that cannot.
type cyclicBarrier struct {
	mu      sync.Mutex
	parties int
	waiting int
	release chan struct{}
}

func newCyclicBarrier(parties int) *cyclicBarrier {
	return &cyclicBarrier{parties: parties, release: make(chan struct{})}
}

// wait blocks until `parties` goroutines are waiting, then releases them all.
//
// It gives up after barrierTimeout instead of blocking forever: a waiter that never arrives (a
// replace that failed before reaching the hook) would otherwise hang the whole test binary with
// several transactions held open, which is a far worse failure than a reported timeout. Proceeding
// on timeout can only weaken the overlap this test asserts, never fake it — so the assertions
// downstream stay honest, and the t.Error says the barrier did not hold.
func (b *cyclicBarrier) wait(t *testing.T) {
	const barrierTimeout = 30 * time.Second

	b.mu.Lock()
	round := b.release
	b.waiting++
	if b.waiting == b.parties {
		b.waiting = 0
		b.release = make(chan struct{})
		b.mu.Unlock()
		close(round) // every waiter in this round, including this one, proceeds together
		return
	}
	b.mu.Unlock()

	select {
	case <-round:
	case <-time.After(barrierTimeout):
		t.Errorf("cyclic barrier timed out after %s waiting for %d concurrent replaces to arrive; "+
			"the overlap this test depends on did not happen", barrierTimeout, b.parties)
	}
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
