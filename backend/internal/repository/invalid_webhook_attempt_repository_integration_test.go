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
	"time"

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

// TestInvalidWebhookAttemptCap_RestartHole_Integration is Issue #1514 G3: the amortised trim
// interval counter (insertCounters) is IN-PROCESS state that resets to zero on every restart. Fly
// restarts machines on every deploy/scale/host-migration, so an attacker (or ordinary traffic)
// sending fewer than the trim interval's worth of invalid attempts per process lifetime never
// reaches the modulo boundary that triggers a trim — rows accumulate across restarts with nothing
// ever reclaiming them.
//
// This simulates that by constructing a FRESH repository instance per "process" (each one via
// NewInvalidWebhookAttemptRepository, exactly like production main.go does on every boot) and
// issuing fewer than trimInterval calls through each before "restarting" (constructing the next
// one). Against the pre-fix code, NO trim ever fires (perRestart < trimInterval, and the counter
// never survives to the next instance), so the row count grows linearly with the number of
// restarts — unbounded. Against the fix, the very first Record call on each fresh instance forces
// an eager trim (see invalidWebhookAttemptRepository.startupTrimEnabled), so the count converges
// to a small, restart-count-independent ceiling instead.
func TestInvalidWebhookAttemptCap_RestartHole_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, "DELETE FROM invalid_webhook_attempts")
	require.NoError(t, err)

	const testCap = 5
	const trimInterval = 50 // matches the production default; never reached within one "process"
	const perRestart = 3    // deliberately << trimInterval
	const numRestarts = 20

	for restart := 0; restart < numRestarts; restart++ {
		// A fresh instance per "process" — mirrors main.go calling
		// NewInvalidWebhookAttemptRepository(db, ...) once per boot. trimInterval/cap are
		// production-shaped (large interval, small cap only so this test doesn't need 10,000
		// rows to observe the count-based trim) but startupTrimEnabled comes from the real
		// constructor, not a hand-set override.
		repo := NewInvalidWebhookAttemptRepository(pool, 30).(*invalidWebhookAttemptRepository)
		repo.trimInterval = trimInterval
		repo.cap = testCap

		for i := 0; i < perRestart; i++ {
			require.NoError(t, repo.Record(ctx, "plaid", 1, fmt.Sprintf("restart-%d-call-%d", restart, i)))
		}
	}

	var finalCount int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM invalid_webhook_attempts WHERE provider = 'plaid'").Scan(&finalCount))

	// Bounded, restart-count-independent ceiling: each fresh instance's first call forces a trim
	// down to (at most) testCap, then the remaining (perRestart-1) calls in that same instance
	// land without a trim (they never reach trimInterval). The pre-fix code has no such ceiling —
	// with numRestarts=20 and perRestart=3, it would land 60 rows, growing linearly with
	// numRestarts.
	const expectedCeiling = testCap + (perRestart - 1)
	require.LessOrEqual(t, finalCount, expectedCeiling,
		"a startup trim on the first call of every fresh instance must stop rows from growing "+
			"without bound across restarts that individually never reach the trim interval")
	// Non-vacuity: without the fix, no trim would EVER fire (perRestart < trimInterval and the
	// counter never survives a restart), so finalCount would equal numRestarts*perRestart exactly
	// (60). Asserting strictly less than that proves this test actually exercises the fix rather
	// than passing regardless of whether it's present.
	require.Less(t, finalCount, numRestarts*perRestart,
		"sanity: without the startup trim, count would equal numRestarts*perRestart exactly — "+
			"this assertion must fail if that's what's happening")
}

// TestInvalidWebhookAttemptCap_RestartHole_WithoutStartupTrim_Integration is the RED-evidence
// sibling to the test above (round-1 review, Important 2): a compile failure from the constructor
// signature change is not a behavioural demonstration that the fix is load-bearing. This exercises
// the PRE-fix behaviour through the CURRENT API — a repository built via struct literal, exactly
// like every other test in this file that predates G3, with startupTrimEnabled left at its zero
// value (false) — running the identical restart/insert pattern as the test above. Because no
// force-trim ever fires, no call in any instance's short lifetime reaches trimInterval, so the
// count-based trim never runs either: this must land exactly numRestarts*perRestart rows,
// unboundedly exceeding the fixed instance's ceiling.
//
// This stays in the suite permanently as a mutation guard: if startupTrimEnabled's effect were
// ever silently disabled in Record, this test's assertion (mirrored from the sibling's ceiling)
// would start passing for the WRONG reason on the fixed path too, but this test's own exact-count
// assertion would immediately reveal that the "manual, no-startup-trim" construction still behaves
// identically to the "real constructor" one — which is exactly the regression this pair of tests
// is designed to catch.
func TestInvalidWebhookAttemptCap_RestartHole_WithoutStartupTrim_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, "DELETE FROM invalid_webhook_attempts")
	require.NoError(t, err)

	const testCap = 5
	const trimInterval = 50
	const perRestart = 3
	const numRestarts = 20
	// Mirrors the sibling test's ceiling exactly, so a reader can compare the two numbers directly.
	const fixedCeiling = testCap + (perRestart - 1)

	for restart := 0; restart < numRestarts; restart++ {
		// Deliberately NOT NewInvalidWebhookAttemptRepository — a raw struct literal has
		// startupTrimEnabled at its zero value (false), so the first call of each "restart" gets
		// no forced trim. This is the pre-G3 behaviour, reachable through the current API by any
		// caller that (like every pre-G3 test in this file) builds the struct directly.
		repo := &invalidWebhookAttemptRepository{db: pool, trimInterval: trimInterval, cap: testCap, retentionDays: 30}

		for i := 0; i < perRestart; i++ {
			require.NoError(t, repo.Record(ctx, "plaid", 1, fmt.Sprintf("norestart-%d-call-%d", restart, i)))
		}
	}

	var finalCount int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM invalid_webhook_attempts WHERE provider = 'plaid'").Scan(&finalCount))
	t.Logf("without startupTrimEnabled: finalCount=%d (fixedCeiling=%d, numRestarts*perRestart=%d)",
		finalCount, fixedCeiling, numRestarts*perRestart)

	require.Equal(t, numRestarts*perRestart, finalCount,
		"without startupTrimEnabled, no call in any short-lived instance ever reaches trimInterval, "+
			"so NO trim (count-based or age-based) ever fires — the row count grows exactly linearly "+
			"with the number of restarts")
	require.Greater(t, finalCount, fixedCeiling,
		"this must exceed the fixed test's ceiling — that gap IS the forced startup trim's effect")
}

// TestInvalidWebhookAttemptCap_AgeRetention_Integration is Issue #1514 G3, requirement 2: bound
// growth by time as well as count. A row-count cap alone assumes trims eventually fire; an
// age-based retention window is a second, orthogonal bound that reclaims rows purely on elapsed
// time, independent of how many Record calls (if any) ever happen again for that provider.
//
// Inserts rows with explicit created_at timestamps (not real sleeps) straddling the retention
// window, then calls trim() directly (bypassing Record/the interval counter entirely) to isolate
// the age-based deletion from the count-based one — cap is set far above the fixture row count so
// the ranked cap-DELETE cannot be what removes the old row.
func TestInvalidWebhookAttemptCap_AgeRetention_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, "DELETE FROM invalid_webhook_attempts")
	require.NoError(t, err)

	const retentionDays = 30
	const testCap = 1000 // far above the fixture count; isolates the age-based delete
	repo := &invalidWebhookAttemptRepository{db: pool, trimInterval: 1, cap: testCap, retentionDays: retentionDays}

	now := time.Now().UTC()
	insertAt := func(sha string, createdAt time.Time) {
		_, err := pool.Exec(ctx,
			`INSERT INTO invalid_webhook_attempts (provider, body_length, body_sha256, created_at) VALUES ($1, $2, $3, $4)`,
			"plaid", 1, sha, createdAt)
		require.NoError(t, err)
	}

	insertAt("outside-window", now.Add(-(retentionDays+1)*24*time.Hour)) // older than the window
	insertAt("inside-window", now.Add(-(retentionDays-1)*24*time.Hour))  // within the window

	require.NoError(t, repo.trim(ctx, "plaid"))

	var remaining []string
	rows, err := pool.Query(ctx, "SELECT body_sha256 FROM invalid_webhook_attempts WHERE provider = 'plaid'")
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var sha string
		require.NoError(t, rows.Scan(&sha))
		remaining = append(remaining, sha)
	}
	require.NoError(t, rows.Err())

	require.ElementsMatch(t, []string{"inside-window"}, remaining,
		"trim must delete rows older than the retention window and keep rows inside it")
}

// TestInvalidWebhookAttemptGetStats24h_Integration is Issue #1514 requirement #5 — the repository
// read method that restores operator visibility into rejected webhook attempts. pgxmock cannot
// catch a bad GROUP BY/aggregate (project memory: "pgxmock never catches type/aggregate errors"),
// so this exercises the real query against live Postgres: distinct per-provider counts, a
// window boundary that correctly excludes rows just outside 24h, and an overall MostRecentAt that
// reflects the single latest row across BOTH providers rather than either provider's own latest.
func TestInvalidWebhookAttemptGetStats24h_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, "DELETE FROM invalid_webhook_attempts")
	require.NoError(t, err)

	repo := &invalidWebhookAttemptRepository{db: pool}

	now := time.Now().UTC()
	insertAt := func(provider, sha string, createdAt time.Time) {
		_, err := pool.Exec(ctx,
			`INSERT INTO invalid_webhook_attempts (provider, body_length, body_sha256, created_at) VALUES ($1, $2, $3, $4)`,
			provider, 1, sha, createdAt)
		require.NoError(t, err)
	}

	// Inside the 24h window.
	insertAt("paddle", "paddle-1", now.Add(-1*time.Hour))
	insertAt("paddle", "paddle-2", now.Add(-2*time.Hour))
	insertAt("paddle", "paddle-3-latest-overall", now.Add(-10*time.Minute))
	insertAt("plaid", "plaid-1", now.Add(-23*time.Hour))

	// Outside the window — must not be counted, and must not win MostRecentAt even though it is
	// a real row for a provider that also has an in-window row.
	insertAt("paddle", "paddle-too-old", now.Add(-25*time.Hour))
	insertAt("plaid", "plaid-too-old", now.Add(-48*time.Hour))
	// A THIRD provider with ONLY an out-of-window row — this is the one that actually isolates
	// "no in-window rows means entirely absent from the map, not present with 0". Codex
	// adversarial review caught that using "plaid" for this (which also has an in-window row
	// above) doesn't prove anything: plaid would appear in the map regardless, for an unrelated
	// reason. A provider with zero in-window rows of its own is the only fixture that pins it.
	insertAt("stripe-not-a-real-provider", "stripe-too-old", now.Add(-48*time.Hour))

	byProvider, mostRecentAt, err := repo.GetStats24h(ctx)
	require.NoError(t, err)

	require.Equal(t, int64(3), byProvider["paddle"], "must count only in-window paddle rows")
	require.Equal(t, int64(1), byProvider["plaid"], "must count only in-window plaid rows")
	_, stripePresent := byProvider["stripe-not-a-real-provider"]
	require.False(t, stripePresent,
		"a provider with NO in-window rows of its own must be entirely absent from the map, not present with 0")
	require.Len(t, byProvider, 2, "only providers with at least one in-window row appear")

	require.NotNil(t, mostRecentAt)
	require.WithinDuration(t, now.Add(-10*time.Minute), *mostRecentAt, 2*time.Second,
		"MostRecentAt must be the latest row across ALL providers (paddle's 10-minutes-ago row), not plaid's own latest or the oldest row")
}

// TestInvalidWebhookAttemptGetStats24h_NoRowsInWindow_Integration proves the true-empty case
// against live Postgres: a table with rows, none of them inside the window, must report an empty
// map and a nil MostRecentAt — not a zero-valued map entry, and not an error.
func TestInvalidWebhookAttemptGetStats24h_NoRowsInWindow_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, "DELETE FROM invalid_webhook_attempts")
	require.NoError(t, err)

	repo := &invalidWebhookAttemptRepository{db: pool}

	_, err = pool.Exec(ctx,
		`INSERT INTO invalid_webhook_attempts (provider, body_length, body_sha256, created_at) VALUES ($1, $2, $3, $4)`,
		"paddle", 1, "ancient", time.Now().UTC().Add(-72*time.Hour))
	require.NoError(t, err)

	byProvider, mostRecentAt, err := repo.GetStats24h(ctx)
	require.NoError(t, err)
	require.Empty(t, byProvider)
	require.Nil(t, mostRecentAt)
}
