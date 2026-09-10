//go:build integration

package jobs

// balance_snapshot_dispatch_sweep_cursor_integration_test.go — #1626 Phase 3, gap G13: real-Postgres
// proof that the daily balance-snapshot dispatcher cannot starve the newest linked items.
//
// THE DEFECT. GetItemsForBalanceCapturePage's predecessor took only a limit, and the dispatcher
// called it ONCE per run with a hard cap of balanceCaptureItemsPerRun. The enumeration is a
// deterministic `ORDER BY created_at, id LIMIT $1`, so above the cap the run did not take a rotating
// slice — it took the SAME oldest cap-worth on every run, forever, and every item created after them
// accrued no balance snapshot on ANY day. There is no backfill on this path by design, so that was a
// permanent, silent absence of financial history behind an otherwise-successful daily job whose only
// signal was a cap_reached log field.
//
// ⚠ WHAT MAKES THIS SWEEP DIFFERENT FROM THE OTHER FOUR that share sweep_cursors, and why it needed
// its own proof rather than inheriting theirs: their candidate rows LEAVE the candidate set when the
// per-row action succeeds (a marked item, a deleted user), so their starvation stories all require a
// permanently-failing head. This one's candidate set is "every active linked item" and never shrinks
// — a successful capture changes nothing about eligibility, because every item is a candidate again
// tomorrow. So no failure of any kind was needed to starve it: the plain, healthy, everything-works
// path starved it.
//
// ⚠ THE SHARED-DATABASE PROBLEM AND HOW IT IS HANDLED. Unlike the cleanup sweep, this enumeration
// has no narrowing predicate beyond `status = 'active'` — every active linked_item on the database
// is a candidate, including other suites' fixtures. The seeded rows are therefore BACKDATED to 1970
// so they occupy the head of `ORDER BY created_at, id`, and assertItemsAreTheOldestActive proves
// that before any cap-based assertion depends on it. Assertions are then made about the seeded set
// only, never about run totals.
//
// Run:
//
//	DATABASE_URL=postgres://... go test ./internal/queue/jobs/... \
//	  -run TestBalanceCaptureDispatchSweepCursor -tags integration -p 1 -count=1 -v

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// seedBalanceCaptureOwner creates the one user every seeded linked_item hangs off.
func seedBalanceCaptureOwner(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, id, "balcap|"+id.String(), "balcap-"+id.String()+"@example.com")
	require.NoError(t, err, "seed balance-capture owner")
	// context.Background(), never t.Context(): the cleanup runs after the test context is cancelled.
	// The linked_items rows cascade off this delete.
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	return id
}

// seedActiveCaptureItem inserts one ACTIVE linked item with an explicit created_at.
//
// created_at is set EXPLICITLY rather than left to its DEFAULT NOW(), for two independent reasons:
// it is the sweep's leading sort key and the test has to control the order exactly, and backdating
// it is what keeps foreign fixtures on this shared database from ranking ahead of the seeded set.
func seedActiveCaptureItem(t *testing.T, pool *pgxpool.Pool, owner uuid.UUID, createdAt time.Time) string {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, status, created_at)
		VALUES ($1, $2, $3, 'enc-balcap-token', 'active', $4)
	`, id, owner, "balcap-item-"+id.String(), createdAt)
	require.NoError(t, err, "seed active linked item")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM linked_items WHERE id = $1`, id)
	})
	return id.String()
}

// assertItemsAreTheOldestActive is the shared-database setup guard. Every cap-based assertion below
// assumes the seeded rows are the first len(seeded) rows the real query returns; this proves it
// against the real query rather than trusting the backdating.
func assertItemsAreTheOldestActive(t *testing.T, pool *pgxpool.Pool, seeded []string) {
	t.Helper()
	repo := repository.NewLinkedItemRepository(pool, newJobsTestFieldEncryptor(t))
	targets, err := repo.GetItemsForBalanceCapturePage(context.Background(), nil, len(seeded))
	require.NoError(t, err)
	got := make([]string, 0, len(targets))
	for _, tgt := range targets {
		got = append(got, tgt.LinkedItemID)
	}
	require.ElementsMatch(t, seeded, got,
		"setup guard: the first %d balance-capture candidates on this database must be exactly the rows "+
			"this test seeded. This enumeration's only predicate is status='active', so a foreign "+
			"fixture linked_item with an older created_at would rank ahead of them and invalidate every "+
			"cap-based assertion below", len(seeded))
}

// runBalanceCaptureDispatch performs ONE Work() invocation against the REAL linked-item repository
// and the REAL sweep-cursor repository, and returns the set of item ids it enqueued a capture for.
//
// A fresh recording queue per run is what makes "which items did THIS run reach" answerable.
func runBalanceCaptureDispatch(t *testing.T, pool *pgxpool.Pool, pageSize, capItems int) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, id := range runBalanceCaptureDispatchOrdered(t, pool, pageSize, capItems) {
		out[id] = true
	}
	return out
}

// runBalanceCaptureDispatchOrdered is the same run, returning every enqueue IN ORDER and WITH
// DUPLICATES. The duplicate-preserving form exists because a set cannot see a repeated dispatch, and
// a repeated dispatch is exactly what an off-by-one keyset predicate produces.
func runBalanceCaptureDispatchOrdered(t *testing.T, pool *pgxpool.Pool, pageSize, capItems int) []string {
	t.Helper()
	q := &captureRecordingQueue{}
	worker := NewBalanceSnapshotDispatchWorker(BalanceSnapshotDispatchWorkerDeps{
		ItemRepo:       repository.NewLinkedItemRepository(pool, newJobsTestFieldEncryptor(t)),
		JobQueue:       q,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		SweepCursors:   repository.NewSweepCursorRepository(pool),
		PageSize:       pageSize,
		MaxItemsPerRun: capItems,
	})
	require.NoError(t, worker.Work(context.Background(), captureDispatchJob()))
	out := make([]string, 0, len(q.calls))
	for _, c := range q.calls {
		out = append(out, c.args.ItemID)
	}
	return out
}

// restrictToSeeded drops everything a run dispatched that this test did not seed. Necessary because
// the candidate set is global; the seeded rows being the OLDEST is what the setup guard establishes,
// and that is all these assertions need.
func restrictToSeeded(dispatched map[string]bool, seeded []string) []string {
	var got []string
	for _, id := range seeded {
		if dispatched[id] {
			got = append(got, id)
		}
	}
	return got
}

// TestBalanceCaptureDispatchSweepCursor_ConsecutiveRunsAdvanceAndTheUnionReachesTheNewestItem is the
// G13 regression against real Postgres.
//
// ⚠ THE PROPERTY IS BOUNDED OVERLAP PLUS STRICT ADVANCE, NOT LITERAL DISJOINTNESS, and the
// difference is a real design decision rather than a weakened assertion. sweep_cursors stores the
// TIMESTAMP half of a position and never a row identity (repository/sweep_cursor_repository.go's
// header gives the GDPR reasoning), so a resume is INCLUSIVE of the boundary timestamp and re-visits
// the single row the previous run stopped on. That is the safe direction here: re-reading an item
// costs one indexed cohort check, whereas skipping one costs that item a day of balance history with
// no backfill to recover it.
func TestBalanceCaptureDispatchSweepCursor_ConsecutiveRunsAdvanceAndTheUnionReachesTheNewestItem(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorBalanceCapture)
	owner := seedBalanceCaptureOwner(t, pool)

	// 1970, so these rows are the oldest active items on the database. Distinct created_at per row,
	// one second apart: the watermark is a timestamp, so a fixture sharing one instant would be a
	// single over-cap tie group and would exercise the documented tie-group residual instead of the
	// resume this test is about.
	base := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	seeded := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		seeded = append(seeded, seedActiveCaptureItem(t, pool, owner, base.Add(time.Duration(i)*time.Second)))
	}
	assertItemsAreTheOldestActive(t, pool, seeded)

	const capItems = 2
	oldest, newest := seeded[0], seeded[len(seeded)-1]

	run1 := restrictToSeeded(runBalanceCaptureDispatch(t, pool, capItems, capItems), seeded)
	require.Equal(t, []string{seeded[0], seeded[1]}, run1,
		"run 1 must dispatch exactly the oldest %d seeded items", capItems)

	ts1, _, present := loadSweepCursorRow(t, pool, repository.SweepCursorBalanceCapture)
	require.True(t, present,
		"a dispatch run that stopped on its cap MUST persist where it got to. A River periodic "+
			"schedule builds fresh JobArgs for every run, so the job's own args cannot carry this "+
			"across runs — only the sweep_cursors row can")
	require.NotNil(t, ts1, "linked_items.created_at is NOT NULL, so the persisted ts1 must not be NULL")

	run2 := restrictToSeeded(runBalanceCaptureDispatch(t, pool, capItems, capItems), seeded)
	require.NotContains(t, run2, oldest,
		"run 2 re-dispatched the OLDEST seeded item, so the run did not advance at all. That is the "+
			"defect: with no persisted cursor every run re-reads the same deterministic prefix and "+
			"every item behind the cap gets no balance snapshot on any day, permanently. run 1 = %v, "+
			"run 2 = %v", run1, run2)
	require.NotEqual(t, run1, run2, "two consecutive capped runs must not cover the same set")

	overlap := 0
	for _, id := range run2 {
		for _, prev := range run1 {
			if id == prev {
				overlap++
			}
		}
	}
	require.LessOrEqual(t, overlap, 1,
		"consecutive runs may overlap only on the one boundary row an inclusive resume re-visits; "+
			"run 1 = %v, run 2 = %v", run1, run2)

	// The union reaches every seeded item, newest included, in a bounded number of runs.
	union := map[string]bool{}
	for _, id := range append(append([]string{}, run1...), run2...) {
		union[id] = true
	}
	const maxRuns = 12 // generous: ceil(5/(2-1)) plus wrap runs and foreign-fixture pages
	for i := 0; i < maxRuns && len(union) < len(seeded); i++ {
		for _, id := range restrictToSeeded(runBalanceCaptureDispatch(t, pool, capItems, capItems), seeded) {
			union[id] = true
		}
	}
	require.True(t, union[newest],
		"the NEWEST seeded item was dispatched on no run within %d runs. It is exactly the item a "+
			"capped, cursor-less dispatcher starves forever", maxRuns)
	for _, id := range seeded {
		require.True(t, union[id], "seeded item %s was reached on no run", id)
	}
}

// TestBalanceCaptureDispatchSweepCursor_PastTheEndPositionWrapsWithinTheSameRun proves the other
// half. A watermark that only ever advances eventually points past the last candidate, and a run
// that then found an empty page and stopped would report a healthy "no eligible items" forever — the
// same bug with extra steps.
//
// The stale position is written DIRECTLY into sweep_cursors rather than produced by walking the whole
// (globally shared, unbounded) candidate set, which keeps this test deterministic on a database
// whose other suites' fixtures it does not control.
func TestBalanceCaptureDispatchSweepCursor_PastTheEndPositionWrapsWithinTheSameRun(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorBalanceCapture)
	owner := seedBalanceCaptureOwner(t, pool)

	base := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	seeded := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		seeded = append(seeded, seedActiveCaptureItem(t, pool, owner, base.Add(time.Duration(i)*time.Second)))
	}
	assertItemsAreTheOldestActive(t, pool, seeded)

	// Past the end of the whole table, not merely past the seeded rows.
	pastTheEnd := time.Date(2999, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, repository.NewSweepCursorRepository(pool).Save(context.Background(),
		repository.SweepCursorBalanceCapture, repository.SweepPosition{TS1: &pastTheEnd}))

	// A cap and page size large enough that this run reaches the end of the candidate set, so the
	// wrap is the only thing that can put the seeded rows in its output.
	got := restrictToSeeded(runBalanceCaptureDispatch(t, pool, 500, 100000), seeded)
	require.ElementsMatch(t, seeded, got,
		"a run resuming from a past-the-end watermark must CLEAR it and re-query from the beginning "+
			"WITHIN the same run. A run that stopped at the empty page instead would report a healthy "+
			"'no eligible items' day for as long as the stale watermark stood — and because this "+
			"sweep's candidate set never shrinks, that watermark is what every completed cycle leaves "+
			"behind, so the wrap is its steady state rather than an edge case")

	_, _, present := loadSweepCursorRow(t, pool, repository.SweepCursorBalanceCapture)
	require.False(t, present,
		"the run reached the end of its candidate set, so it must DELETE its cursor row rather than "+
			"store a past-the-end position; only ever storing 'we stopped mid-set' is what keeps a "+
			"stale watermark from becoming durable state")
}

// TestBalanceCaptureDispatchSweepCursor_MultiPageWithinOneRunDispatchesEachItemExactlyOnce covers
// the WITHIN-run half of the keyset predicate, which the two tests above cannot reach: both of them
// stop after a single page, so the cursor they exercise is always the cross-run one built from the
// stored watermark and a uuid SENTINEL.
//
// ⚠ AND THAT DISTINCTION IS WHY THIS TEST EXISTS RATHER THAN BEING FOLDED IN. A sentinel-based
// cursor is insensitive to the strictness of the comparison — `> (T, 00000000-…)` and
// `>= (T, 00000000-…)` select the same rows, because no real uuid equals the minimum (see
// repository.MinSweepSentinelID). The WITHIN-run cursor is built from a REAL item id, and there the
// strictness is load-bearing: `>=` re-returns the row each page ended on, so every page after the
// first re-dispatches an item. A mutation from `>` to `>=` in
// GetItemsForBalanceCapturePage survived the two tests above for exactly that reason, and this test
// is the fixture that closes it. Duplicates, not coverage, are the signal — hence
// runBalanceCaptureDispatchOrdered.
func TestBalanceCaptureDispatchSweepCursor_MultiPageWithinOneRunDispatchesEachItemExactlyOnce(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorBalanceCapture)
	owner := seedBalanceCaptureOwner(t, pool)

	base := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	seeded := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		seeded = append(seeded, seedActiveCaptureItem(t, pool, owner, base.Add(time.Duration(i)*time.Second)))
	}
	assertItemsAreTheOldestActive(t, pool, seeded)

	// pageSize 2 against 5 seeded items forces at least three page round-trips inside ONE run, with
	// a cap high enough that the run cannot stop on it.
	ordered := runBalanceCaptureDispatchOrdered(t, pool, 2, 100000)

	counts := map[string]int{}
	for _, id := range ordered {
		counts[id]++
	}
	for _, id := range seeded {
		require.Equal(t, 1, counts[id],
			"seeded item %s was dispatched %d times in ONE run. Each item must be enqueued exactly "+
				"once per run: the within-run keyset cursor is built from a REAL item id, so a "+
				"non-strict comparison re-returns the row every page ended on and the run pays a "+
				"duplicate Plaid /accounts/get for it. Full dispatch order: %v",
			id, counts[id], ordered)
	}
}
