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
		_, err = pool.Exec(ctx, trimInvalidWebhookAttemptsQuery, testCap, "paddle")
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
		_, err = pool.Exec(ctx, trimInvalidWebhookAttemptsQuery, testCap, "plaid")
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
		_, err = pool.Exec(ctx, trimInvalidWebhookAttemptsQuery, testCap, "paddle")
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

// TestInvalidWebhookAttemptCap_ConcurrentInserts_Integration is Issue #1514 Codex adversarial
// review finding (Medium, round 1) — kept intact and UNMODIFIED in its security assertion per
// the round-2 review ruling: concurrent Record calls must not be able to push the per-provider
// row count past the cap. This exercises the REAL invalidWebhookAttemptRepository.Record method
// (not a hand-rolled copy) configured with trimInterval=1 — i.e. every call trims, the tight
// case that is mathematically identical to the pre-amortisation behavior this test was written
// to prove. The pg_advisory_xact_lock inside trim() serializes the insert+trim critical section;
// this test proves the cap holds under real concurrent load against live Postgres, which pgxmock
// (sequential, single mocked connection) cannot exercise.
func TestInvalidWebhookAttemptCap_ConcurrentInserts_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, "DELETE FROM invalid_webhook_attempts")
	require.NoError(t, err)

	const testCap = 5
	const goroutines = 20
	const perGoroutine = 5

	repo := &invalidWebhookAttemptRepository{db: pool, trimInterval: 1, cap: testCap}

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				_ = repo.Record(ctx, "paddle", 1, fmt.Sprintf("concurrent-%d-%d", g, i))
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
	require.NoError(t, repo.Record(ctx, "paddle", 1, "post-flood"))
}

// TestInvalidWebhookAttemptCap_AmortisedTrim_HardCeiling_Integration is Issue #1514 Codex review
// round 2, Important 2: proves the EXPLICIT hard ceiling that amortised trimming (trimInterval>1)
// guarantees. Between two trims, at most trimInterval-1 rows can accumulate for a provider beyond
// whatever the previous trim brought it down to (<= cap) — because the in-process counter that
// schedules trims is a strictly increasing, atomically-incremented sequence, so the Nth call (in
// increment order) is always the one that trims, never later. The row count for a provider can
// therefore transiently exceed cap, but must NEVER exceed cap + trimInterval - 1.
func TestInvalidWebhookAttemptCap_AmortisedTrim_HardCeiling_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, "DELETE FROM invalid_webhook_attempts")
	require.NoError(t, err)

	const testCap = 5
	const trimInterval = 10
	const hardCeiling = testCap + trimInterval - 1

	repo := &invalidWebhookAttemptRepository{db: pool, trimInterval: trimInterval, cap: testCap}

	// 15*17=255, deliberately NOT a multiple of trimInterval (10) — so the flood does not
	// coincidentally end exactly on a trim boundary, and countAfterFlood below actually observes
	// a transient in-between state rather than trivially landing back at the tight cap already.
	const goroutines = 15
	const perGoroutine = 17

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				_ = repo.Record(ctx, "plaid", 1, fmt.Sprintf("ceiling-%d-%d", g, i))
			}
		}(g)
	}
	wg.Wait()

	var countAfterFlood int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM invalid_webhook_attempts WHERE provider = 'plaid'").Scan(&countAfterFlood))
	require.LessOrEqual(t, countAfterFlood, hardCeiling,
		"amortised trimming must never let the row count exceed cap+trimInterval-1, even transiently between trims")

	// The in-process counter is exactly goroutines*perGoroutine at this point (every call
	// increments it exactly once, whether or not that call trimmed) — issue exactly enough more
	// calls to LAND ON the next multiple of trimInterval, so the very LAST call in this loop is
	// guaranteed to be the one that trims (not just "a trim occurs somewhere in this range").
	totalSoFar := goroutines * perGoroutine
	remainder := totalSoFar % trimInterval
	callsToNextMultiple := trimInterval - remainder
	if remainder == 0 {
		callsToNextMultiple = trimInterval
	}
	for i := 0; i < callsToNextMultiple; i++ {
		require.NoError(t, repo.Record(ctx, "plaid", 1, fmt.Sprintf("settle-%d", i)))
	}

	var countAfterSettle int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM invalid_webhook_attempts WHERE provider = 'plaid'").Scan(&countAfterSettle))
	require.LessOrEqual(t, countAfterSettle, testCap,
		"once a trim actually fires, the row count must settle back down to the tight cap")
}
