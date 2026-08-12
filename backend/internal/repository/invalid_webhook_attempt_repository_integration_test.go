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
// round 2/3, Important 2: proves the EXPLICIT hard ceiling that amortised trimming (trimInterval>1)
// guarantees, and — round 3 — proves it non-vacuously (a real transient overshoot actually
// happened, not just "the count never exceeded a bound it also never approached").
//
// Deliberately SEQUENTIAL, not concurrent (round 3 fix): under concurrency, which goroutine's
// INSERT vs. which goroutine's trim-DELETE actually lands on the DB first is a genuine race — a
// later-committing trim can retroactively also clean up rows from calls with a HIGHER counter
// value than the one that triggered it, so the exact final count depends on wall-clock scheduling,
// not just call-counter order (ConcurrentInserts_Integration above already covers that race, at
// the tight interval=1 bound). Sequential calls remove the race: each call (insert, and its trim
// if any) fully completes before the next call starts, so the resulting count is EXACT and
// reproducible, not merely bounded-with-high-probability.
//
// Trace for 25 = 2*trimInterval + 5 sequential calls (trimInterval=10, cap=5):
//
//	calls  1- 9: insert only (no call is a multiple of 10 yet)      -> count 1..9
//	call     10: insert (count 10) THEN trims down to cap           -> count 5
//	calls 11-19: insert only                                        -> count 6..14
//	call     20: insert (count 15) THEN trims down to cap           -> count 5
//	calls 21-25: insert only (5 = totalCalls - 2*trimInterval)      -> count 6..10
//
// Expected final count: exactly cap + 5 = 10.
func TestInvalidWebhookAttemptCap_AmortisedTrim_HardCeiling_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, "DELETE FROM invalid_webhook_attempts")
	require.NoError(t, err)

	const testCap = 5
	const trimInterval = 10
	// This test reads the count only at a completed-call boundary (no Record call for this
	// provider is in flight when we query) — that is the tighter cap+interval-1 bound from the
	// invalidWebhookAttemptTrimInterval doc comment, NOT the looser instantaneous cap+interval
	// bound (which can only be observed mid-call, by a concurrent reader on another connection).
	const hardCeilingAtCompletedCallBoundary = testCap + trimInterval - 1

	repo := &invalidWebhookAttemptRepository{db: pool, trimInterval: trimInterval, cap: testCap}

	const totalCalls = 2*trimInterval + 5
	const expectedFinalCount = testCap + 5 // see trace in the doc comment above

	for i := 0; i < totalCalls; i++ {
		require.NoError(t, repo.Record(ctx, "plaid", 1, fmt.Sprintf("ceiling-%d", i)))
	}

	var countAfterFlood int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM invalid_webhook_attempts WHERE provider = 'plaid'").Scan(&countAfterFlood))

	require.Equal(t, expectedFinalCount, countAfterFlood,
		"sequential calls make the resulting count fully deterministic -- see the trace in this test's doc comment")
	// Round 3 fix: the missing non-vacuity check. Without this, the test would still pass even if
	// amortisation were silently disabled (e.g. trimInterval forced to 1, which never overshoots)
	// — LessOrEqual alone can't distinguish "never overshot" from "correctly bounded overshoot".
	require.Greater(t, countAfterFlood, testCap,
		"must prove a REAL transient overshoot occurred, not just that count stayed under a bound it never approached")
	require.LessOrEqual(t, countAfterFlood, hardCeilingAtCompletedCallBoundary,
		"amortised trimming must never let the row count exceed cap+trimInterval-1 at a completed-call boundary")

	// One more partial cycle: issue exactly enough more sequential calls to land ON the next
	// multiple of trimInterval (25 -> next multiple is 30, so 5 more), so the very LAST call is
	// guaranteed to be the one that trims — proving the count doesn't just stay bounded, it
	// actually settles back to the EXACT tight cap once a trim fires.
	callsToNextMultiple := trimInterval - (totalCalls % trimInterval)
	for i := 0; i < callsToNextMultiple; i++ {
		require.NoError(t, repo.Record(ctx, "plaid", 1, fmt.Sprintf("settle-%d", i)))
	}

	var countAfterSettle int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM invalid_webhook_attempts WHERE provider = 'plaid'").Scan(&countAfterSettle))
	require.Equal(t, testCap, countAfterSettle,
		"once a trim actually fires, the row count must settle back down to EXACTLY the tight cap")
}
