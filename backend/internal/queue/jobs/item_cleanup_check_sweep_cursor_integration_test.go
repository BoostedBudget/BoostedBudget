//go:build integration

package jobs

// item_cleanup_check_sweep_cursor_integration_test.go — Issue #1477, real-Postgres proof for the
// item-cleanup sweep.
//
// ⚠ WHAT MAKES A BACKLOG STARVE THIS SWEEP, precisely. The head has to be UNMARKABLE — MarkForCleanup
// itself failing — NOT un-removable at Plaid. A successfully stamped row leaves the candidate set
// immediately, because cleanupCandidateSelectQuery requires cleanup_scheduled_at IS NULL, so however
// badly the downstream /item/remove goes the row is no longer in this query's result set. That makes
// the starvation story here a DATABASE-failure story, and these tests inject exactly that: a
// decorator over the real repository that fails MarkForCleanup for a nominated set of ids and
// delegates everything else, so the paging query itself stays entirely real.
//
// Run:
//
//	DATABASE_URL=postgres://... go test ./internal/queue/jobs/... \
//	  -run TestItemCleanupSweepCursor -tags integration -p 1 -count=1 -v

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/require"
)

// unmarkableItemRepo wraps the REAL linked-item repository and fails MarkForCleanup for a nominated
// set of item ids. Everything else — above all GetItemsForCleanupPage, whose ordering and keyset
// predicate are what these tests are about — goes straight to Postgres.
type unmarkableItemRepo struct {
	// The REAL repository, embedded: every method except MarkForCleanup below — above all
	// GetItemsForCleanupPage, whose ordering and keyset predicate are what these tests are about —
	// goes straight to Postgres, unwrapped.
	LinkedItemCleanupRepository

	failFor   map[string]bool
	attempted []string
}

func (r *unmarkableItemRepo) MarkForCleanup(ctx context.Context, itemID string, cleanupAt time.Time) error {
	r.attempted = append(r.attempted, itemID)
	if r.failFor[itemID] {
		return fmt.Errorf("simulated permanent database failure marking %s for cleanup", itemID)
	}
	return r.LinkedItemCleanupRepository.MarkForCleanup(ctx, itemID, cleanupAt)
}

func (r *unmarkableItemRepo) attemptedSet() map[string]bool {
	set := make(map[string]bool, len(r.attempted))
	for _, id := range r.attempted {
		set[id] = true
	}
	return set
}

// noopItemQueue accepts every enqueue. The enqueue path is not what these tests exercise — the
// unmarkable head fails BEFORE reaching it, which is the whole point of the correction above.
type noopItemQueue struct {
	queue.JobQueue // embedded and always nil: only Enqueue is reachable from this sweep

	nextID int64
}

func (q *noopItemQueue) Enqueue(_ context.Context, _ queue.Job, _ ...queue.EnqueueOption) (*queue.JobResult, error) {
	q.nextID++
	return &queue.JobResult{ID: q.nextID}, nil
}

// seedCleanupItemOwner creates the one user every seeded linked_item hangs off. Returned as a uuid
// so the caller can pass it straight back into the linked_items insert.
func seedCleanupItemOwner(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, id, "sweepitem|"+id.String(), "sweepitem-"+id.String()+"@example.com")
	require.NoError(t, err, "seed cleanup item owner")
	// context.Background(), never t.Context() — see seedDeletionCandidate. The linked_items rows
	// cascade off this delete.
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	return id
}

// seedCleanupCandidate inserts one revoked linked item — the "revoked" leg of
// cleanupCandidateReasonPredicate, which is a candidate at any age — with an explicit
// (last_error_at, last_synced_at) so the test controls the sort key exactly.
func seedCleanupCandidate(t *testing.T, pool *pgxpool.Pool, owner uuid.UUID, lastErrorAt, lastSyncedAt *time.Time) string {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, status, last_error_at, last_synced_at)
		VALUES ($1, $2, $3, 'enc-sweep-token', 'revoked', $4, $5)
	`, id, owner, "sweep-item-"+id.String(), lastErrorAt, lastSyncedAt)
	require.NoError(t, err, "seed cleanup candidate")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM linked_items WHERE id = $1`, id)
	})
	return id.String()
}

func timePtr(ts time.Time) *time.Time { return &ts }

// runItemCleanupSweep performs ONE Work() invocation over the real repository (wrapped) and the real
// sweep-cursor repository.
func runItemCleanupSweep(t *testing.T, pool *pgxpool.Pool, pageSize, capItems int, failFor map[string]bool) *unmarkableItemRepo {
	t.Helper()
	repo := &unmarkableItemRepo{
		LinkedItemCleanupRepository: repository.NewLinkedItemRepository(pool, newJobsTestFieldEncryptor(t)),
		failFor:                     failFor,
	}
	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:          repo,
		JobQueue:                &noopItemQueue{},
		Logger:                  slog.New(slog.NewTextHandler(io.Discard, nil)),
		CleanupPageSize:         pageSize,
		MaxItemsPerCleanupSweep: capItems,
		SweepCursors:            repository.NewSweepCursorRepository(pool),
	})
	require.NoError(t, worker.Work(context.Background(),
		newTestItemCleanupCheckJob(ItemCleanupCheckArgs{TriggerSource: "sweep-cursor-integration"})))
	return repo
}

// assertItemSweepSeesExactlySeeded is the shared-database setup guard — see
// assertSweepSeesExactlySeeded.
func assertItemSweepSeesExactlySeeded(t *testing.T, pool *pgxpool.Pool, seeded []string) {
	t.Helper()
	repo := repository.NewLinkedItemRepository(pool, newJobsTestFieldEncryptor(t))
	items, err := repo.GetItemsForCleanupPage(context.Background(),
		defaultCleanupErrorDays, defaultCleanupStaleDays, nil, len(seeded))
	require.NoError(t, err)
	got := make([]string, 0, len(items))
	for _, it := range items {
		got = append(got, it.ID)
	}
	require.ElementsMatch(t, seeded, got,
		"setup guard: the first %d cleanup candidates on this database must be exactly the rows this test "+
			"seeded; a foreign fixture linked_item ranking ahead of them would invalidate every cap-based "+
			"assertion below", len(seeded))
}

// ---------------------------------------------------------------------------------------------
// 1. THE STARVATION CASE — with the correction this brief demands: the head is UNMARKABLE.
// ---------------------------------------------------------------------------------------------

func TestItemCleanupSweepCursor_UnmarkableHead_LaterRunReachesTheRowBehindIt(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorItemCleanup)
	owner := seedCleanupItemOwner(t, pool)

	base := time.Date(1992, 1, 1, 0, 0, 0, 0, time.UTC)
	headA := seedCleanupCandidate(t, pool, owner, timePtr(base), nil)
	headB := seedCleanupCandidate(t, pool, owner, timePtr(base.Add(1*time.Second)), nil)
	behind := seedCleanupCandidate(t, pool, owner, timePtr(base.Add(2*time.Second)), nil)
	assertItemSweepSeesExactlySeeded(t, pool, []string{headA, headB, behind})

	// The head can never be marked, so it never leaves the candidate set.
	failing := map[string]bool{headA: true, headB: true}

	run1 := runItemCleanupSweep(t, pool, 2, 2, failing)
	require.True(t, run1.attemptedSet()[headA], "run 1 must reach the first candidate")
	require.True(t, run1.attemptedSet()[headB], "run 1 must reach the second candidate")
	require.False(t, run1.attemptedSet()[behind], "run 1 is capped at 2 and must not reach the third")

	_, _, present := loadSweepCursorRow(t, pool, repository.SweepCursorItemCleanup)
	require.True(t, present, "a run that stopped on its cap must persist where it got to")

	run2 := runItemCleanupSweep(t, pool, 2, 2, failing)
	require.True(t, run2.attemptedSet()[behind],
		"THE DEFECT: the second run must reach the item ranked behind the UNMARKABLE head. Without a "+
			"persisted cursor it restarts from the first candidate and this one is never reached on any run")
}

// ---------------------------------------------------------------------------------------------
// 2. THE MANDATORY SIBLING.
// ---------------------------------------------------------------------------------------------

func TestItemCleanupSweepCursor_UnderCapRun_ProcessesEveryRowAndSkipsNone(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorItemCleanup)
	owner := seedCleanupItemOwner(t, pool)

	base := time.Date(1992, 2, 1, 0, 0, 0, 0, time.UTC)
	seeded := []string{
		seedCleanupCandidate(t, pool, owner, timePtr(base), nil),
		seedCleanupCandidate(t, pool, owner, timePtr(base.Add(1*time.Second)), nil),
		seedCleanupCandidate(t, pool, owner, timePtr(base.Add(2*time.Second)), nil),
		seedCleanupCandidate(t, pool, owner, timePtr(base.Add(3*time.Second)), nil),
		seedCleanupCandidate(t, pool, owner, timePtr(base.Add(4*time.Second)), nil),
	}
	assertItemSweepSeesExactlySeeded(t, pool, seeded)

	run := runItemCleanupSweep(t, pool, 2, 100, nil)

	got := run.attemptedSet()
	for _, id := range seeded {
		require.True(t, got[id], "under-cap run must reach every seeded candidate; missed %s", id)
	}

	_, _, present := loadSweepCursorRow(t, pool, repository.SweepCursorItemCleanup)
	require.False(t, present,
		"a run that reached the END of the candidate set must CLEAR its position, not store a past-the-end one")
}

// ---------------------------------------------------------------------------------------------
// 3. WRAP-AROUND. This sweep's sort columns are NULLABLE and NULL sorts LAST, so the position that
//    is genuinely past the end of everything is (NULL, NULL) — the very value that also means "no
//    watermark" if absence were not the encoding. That makes this the sweep where the wrap and the
//    "row present means resume position" choice are both load-bearing.
// ---------------------------------------------------------------------------------------------

func TestItemCleanupSweepCursor_PositionPastTheEnd_WrapsAndFindsCandidates(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorItemCleanup)
	owner := seedCleanupItemOwner(t, pool)

	base := time.Date(1992, 3, 1, 0, 0, 0, 0, time.UTC)
	seeded := []string{
		seedCleanupCandidate(t, pool, owner, timePtr(base), nil),
		seedCleanupCandidate(t, pool, owner, timePtr(base.Add(1*time.Second)), nil),
	}
	assertItemSweepSeesExactlySeeded(t, pool, seeded)

	// (NULL, NULL) coalesces to ('infinity', 'infinity'): strictly past every finite sort key, so
	// nothing at all is at or after it.
	cursors := repository.NewSweepCursorRepository(pool)
	require.NoError(t, cursors.Save(context.Background(), repository.SweepCursorItemCleanup,
		repository.SweepPosition{TS1: nil, TS2: nil, Exclusive: true}))

	run := runItemCleanupSweep(t, pool, 10, 100, nil)

	got := run.attemptedSet()
	for _, id := range seeded {
		require.True(t, got[id],
			"a cursor parked at the NULLS-LAST end must WRAP to the beginning within the same run and "+
				"reach %s; reporting 'no items need cleanup' here is the silent stall", id)
	}

	_, _, present := loadSweepCursorRow(t, pool, repository.SweepCursorItemCleanup)
	require.False(t, present, "after wrapping and exhausting the set, the position must be cleared")
}

// ---------------------------------------------------------------------------------------------
// 4. THE TIE-BREAKER. This sweep's tie-breaker is the one most obviously load-bearing and least
//    obviously tested: BOTH of its sort columns are nullable, and its zero-active-accounts leg
//    matches items that have never errored and never synced — so (NULL, NULL) is a real, and
//    potentially large, tie group. These rows share that exact key.
// ---------------------------------------------------------------------------------------------

func TestItemCleanupSweepCursor_NullNullTieGroup_TotalOrderAcrossPageBoundaries(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorItemCleanup)
	owner := seedCleanupItemOwner(t, pool)

	seeded := []string{
		seedCleanupCandidate(t, pool, owner, nil, nil),
		seedCleanupCandidate(t, pool, owner, nil, nil),
		seedCleanupCandidate(t, pool, owner, nil, nil),
		seedCleanupCandidate(t, pool, owner, nil, nil),
		seedCleanupCandidate(t, pool, owner, nil, nil),
	}
	assertItemSweepSeesExactlySeeded(t, pool, seeded)

	// Every row fails to mark, so nothing leaves the candidate set mid-run and the keyset predicate
	// has to carry the whole burden of not repeating and not skipping across three page boundaries.
	failing := map[string]bool{}
	for _, id := range seeded {
		failing[id] = true
	}

	run := runItemCleanupSweep(t, pool, 2, 100, failing)

	seen := map[string]int{}
	for _, id := range run.attempted {
		seen[id]++
	}
	for _, id := range seeded {
		require.Equal(t, 1, seen[id],
			"item %s in the (NULL, NULL) tie group must be visited EXACTLY once across the page "+
				"boundaries; %d visits means the li.id tie-breaker is not making the order total", id, seen[id])
	}
	require.Len(t, seen, len(seeded), "the run must have visited exactly the tied rows and nothing else")
}

// ---------------------------------------------------------------------------------------------
// 5. A NULL POSITION IS A POSITION, NOT "UNSET". The row-present/row-absent encoding is what makes
//    that expressible; if NULL doubled as "no watermark" this sweep could never park at the end of
//    its own NULLS-LAST order, and test 3 above would silently degrade into "starts from the
//    beginning anyway" — passing for the wrong reason.
// ---------------------------------------------------------------------------------------------

func TestItemCleanupSweepCursor_NullPositionRoundTripsAsAPositionNotAsUnset(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorItemCleanup)
	cursors := repository.NewSweepCursorRepository(pool)
	ctx := context.Background()

	loaded, err := cursors.Load(ctx, repository.SweepCursorItemCleanup)
	require.NoError(t, err)
	require.Nil(t, loaded, "no row means no position")

	require.NoError(t, cursors.Save(ctx, repository.SweepCursorItemCleanup,
		repository.SweepPosition{TS1: nil, TS2: nil, Exclusive: true}))

	loaded, err = cursors.Load(ctx, repository.SweepCursorItemCleanup)
	require.NoError(t, err)
	require.NotNil(t, loaded,
		"a stored (NULL, NULL) position must load as a POSITION, not as 'unset' — absence of the row is "+
			"the only encoding of 'unset', precisely so this value stays available")
	require.Nil(t, loaded.TS1)
	require.Nil(t, loaded.TS2)
	require.True(t, loaded.Exclusive)
	require.Equal(t, repository.MaxSweepSentinelID, loaded.SentinelID(),
		"an exclusive position must pair with the MAXIMUM uuid sentinel, which is what makes the keyset "+
			"predicate mean strictly-after rather than at-or-after")

	require.NoError(t, cursors.Clear(ctx, repository.SweepCursorItemCleanup))
	loaded, err = cursors.Load(ctx, repository.SweepCursorItemCleanup)
	require.NoError(t, err)
	require.Nil(t, loaded)
}
