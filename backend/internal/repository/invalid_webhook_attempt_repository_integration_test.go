//go:build integration

package repository

// Integration tests for the invalid_webhook_attempts row cap (Issue #1514).
//
// pgxmock cannot catch a bug in the trim query's SQL (e.g. a bad LIMIT/subquery type, an
// off-by-one in the cap arithmetic, cross-provider eviction, or a concurrency race in the
// insert+trim critical section) — it only checks that the expected statement text/args were
// sent, not that Postgres actually enforces the cap. These tests run the real Record path
// against a live Postgres.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestInvalidWebhookAttempt -tags integration -p 1 -v

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestInvalidWebhookAttemptCap_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	// Isolate this test from any other data in the shared integration DB.
	_, err := pool.Exec(ctx, "DELETE FROM invalid_webhook_attempts")
	require.NoError(t, err)

	// Temporarily lower the cap so the test doesn't need thousands of inserts. The production
	// cap (invalidWebhookAttemptCap) is exercised by the mock-based unit test's WithArgs
	// assertion; this test only needs to prove the trim SQL itself enforces whatever cap it is
	// given.
	const testCap = 5
	repo := &invalidWebhookAttemptRepository{db: pool}

	// Insert well past the cap.
	for i := 0; i < testCap*3; i++ {
		_, err := pool.Exec(ctx, insertInvalidWebhookAttemptQuery, "paddle", 10+i, fmt.Sprintf("digest-%d", i))
		require.NoError(t, err)
		_, err = pool.Exec(ctx, trimInvalidWebhookAttemptsQuery, testCap)
		require.NoError(t, err)
	}

	var count int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM invalid_webhook_attempts").Scan(&count))
	require.LessOrEqual(t, count, testCap, "row count must never exceed the cap")

	// The full Record() path (real cap) must also succeed end-to-end against live Postgres.
	require.NoError(t, repo.Record(ctx, "plaid", 99, "final-digest"))

	var afterRecordCount int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM invalid_webhook_attempts").Scan(&afterRecordCount))
	require.LessOrEqual(t, afterRecordCount, invalidWebhookAttemptCap)
}

// TestInvalidWebhookAttemptCap_PerProviderIsolation_Integration is Issue #1514 Codex adversarial
// review finding (Medium): the trim must cap each provider independently — a flood against one
// provider must never evict another provider's unrelated audit rows.
func TestInvalidWebhookAttemptCap_PerProviderIsolation_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, "DELETE FROM invalid_webhook_attempts")
	require.NoError(t, err)

	const testCap = 3
	repo := &invalidWebhookAttemptRepository{db: pool}

	// Plaid gets exactly `testCap` rows first.
	for i := 0; i < testCap; i++ {
		_, err := pool.Exec(ctx, insertInvalidWebhookAttemptQuery, "plaid", 1, fmt.Sprintf("plaid-%d", i))
		require.NoError(t, err)
		_, err = pool.Exec(ctx, trimInvalidWebhookAttemptsQuery, testCap)
		require.NoError(t, err)
	}

	var plaidCountBeforeFlood int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM invalid_webhook_attempts WHERE provider = 'plaid'").Scan(&plaidCountBeforeFlood))
	require.Equal(t, testCap, plaidCountBeforeFlood)

	// Paddle then floods well past the SAME cap.
	for i := 0; i < testCap*10; i++ {
		_, err := pool.Exec(ctx, insertInvalidWebhookAttemptQuery, "paddle", 1, fmt.Sprintf("paddle-%d", i))
		require.NoError(t, err)
		_, err = pool.Exec(ctx, trimInvalidWebhookAttemptsQuery, testCap)
		require.NoError(t, err)
	}

	var plaidCountAfterFlood, paddleCount int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM invalid_webhook_attempts WHERE provider = 'plaid'").Scan(&plaidCountAfterFlood))
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM invalid_webhook_attempts WHERE provider = 'paddle'").Scan(&paddleCount))

	assert := require.New(t)
	assert.Equal(testCap, plaidCountAfterFlood, "a flood on paddle must not evict plaid's unrelated audit rows")
	assert.Equal(testCap, paddleCount, "paddle's own rows must still respect the cap")

	// repo.Record() (the real code path, real cap) must also work end-to-end.
	require.NoError(t, repo.Record(ctx, "plaid", 5, "final"))
}

// recordWithCap mirrors invalidWebhookAttemptRepository.Record but with a caller-supplied cap, so
// the concurrency test below can exercise the real lock+insert+trim critical section (unmodified
// production SQL and lock key) without needing >invalidWebhookAttemptCap concurrent inserts to
// prove anything.
func recordWithCap(ctx context.Context, pool rowQuerier2, provider string, digest string, cap int) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", invalidWebhookAttemptLockKey); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, insertInvalidWebhookAttemptQuery, provider, 1, digest); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, trimInvalidWebhookAttemptsQuery, cap); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// rowQuerier2 is satisfied by *pgxpool.Pool — named distinctly from this package's other
// integration helper interfaces to avoid any accidental shadowing.
type rowQuerier2 interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// TestInvalidWebhookAttemptCap_ConcurrentInserts_Integration is Issue #1514 Codex adversarial
// review finding (Medium): concurrent Record calls must not be able to push the per-provider row
// count past the cap. The pg_advisory_xact_lock inside Record serializes the insert+trim critical
// section; this test proves the cap holds under real concurrent load against live Postgres,
// which pgxmock (sequential, single mocked connection) cannot exercise.
func TestInvalidWebhookAttemptCap_ConcurrentInserts_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, "DELETE FROM invalid_webhook_attempts")
	require.NoError(t, err)

	const testCap = 5
	const goroutines = 20
	const perGoroutine = 5

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				_ = recordWithCap(ctx, pool, "paddle", fmt.Sprintf("concurrent-%d-%d", g, i), testCap)
			}
		}(g)
	}
	wg.Wait()

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM invalid_webhook_attempts WHERE provider = 'paddle'").Scan(&count))
	require.LessOrEqual(t, count, testCap,
		"concurrent Record calls must never push the per-provider row count past the cap")

	// The real Record() path (real cap, real lock key) must also still work after the flood.
	repo := NewInvalidWebhookAttemptRepository(pool)
	require.NoError(t, repo.Record(ctx, "paddle", 1, "post-flood"))
}
