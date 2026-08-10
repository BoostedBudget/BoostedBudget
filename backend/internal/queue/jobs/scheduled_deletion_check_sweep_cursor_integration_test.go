//go:build integration

package jobs

// scheduled_deletion_check_sweep_cursor_integration_test.go — Issue #1477, real-Postgres proof for
// the scheduled-deletion sweep that a run resumes where the last CAPPED run stopped.
//
// Why real Postgres and not pgxmock: every claim here is about SQL ordering and keyset-cursor
// semantics — a row comparison against a synthesized uuid sentinel, an `id ASC` tie-breaker on
// genuinely identical timestamps, and NULL handling. pgxmock validates call shape and echoes canned
// rows; it cannot show that `(deletion_scheduled_at, id) > (T, 00000000-…)` selects exactly the
// rows tied at T, nor that removing the tie-breaker starts skipping them.
//
// Run:
//
//	DATABASE_URL=postgres://... go test ./internal/queue/jobs/... \
//	  -run TestScheduledDeletionSweepCursor -tags integration -p 1 -count=1 -v

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/require"
)

// recordingJobQueue records every Enqueue it is asked to perform and fails permanently for a
// nominated set of user ids. Hand-rolled rather than a testify mock because these tests care about
// the SEQUENCE and MEMBERSHIP of what was attempted across several runs, which .On()/.AssertCalled
// expresses far less directly.
//
// failFor models the head of a queue that can never drain: the enqueue keeps failing, so the user
// row is never deleted, so it keeps matching GetUsersScheduledForDeletionPage's predicate on every
// single run. That is the exact shape of the starvation this issue exists to fix.
type recordingJobQueue struct {
	queue.JobQueue // embedded and always nil: only Enqueue is reachable from these sweeps

	attempted []string
	failFor   map[string]bool
	nextID    int64
}

func (q *recordingJobQueue) Enqueue(_ context.Context, job queue.Job, _ ...queue.EnqueueOption) (*queue.JobResult, error) {
	args, ok := job.(AccountDeletionArgs)
	if !ok {
		return nil, fmt.Errorf("recordingJobQueue: unexpected job type %T", job)
	}
	q.attempted = append(q.attempted, args.UserID)
	if q.failFor[args.UserID] {
		return nil, fmt.Errorf("simulated permanent enqueue failure for %s", args.UserID)
	}
	q.nextID++
	return &queue.JobResult{ID: q.nextID}, nil
}

// attemptedSet is the membership view every assertion in this file uses. Deliberately membership,
// not a count and never a delta over a measured baseline: a broadened predicate would shift a
// baseline and a result identically and the arithmetic would stay green, which is exactly the
// failure mode Y4's own test shipped with.
func (q *recordingJobQueue) attemptedSet() map[string]bool {
	set := make(map[string]bool, len(q.attempted))
	for _, id := range q.attempted {
		set[id] = true
	}
	return set
}

// seedDeletionCandidate inserts one user already past their deletion grace period, at an explicit
// deletion_scheduled_at so the test controls the sort order exactly.
func seedDeletionCandidate(t *testing.T, pool *pgxpool.Pool, scheduledAt time.Time) string {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region,
		                   deletion_requested_at, deletion_scheduled_at)
		VALUES ($1, $2, $3, 'premium', 'cancelled', 'iad', $4, $4)
	`, id, "sweepcur|"+id.String(), "sweepcur-"+id.String()+"@example.com", scheduledAt)
	require.NoError(t, err, "seed deletion candidate")

	// ⚠ context.Background(), never t.Context(): t.Context() is cancelled BEFORE t.Cleanup runs, so
	// the DELETE would fail silently and leak a fixture row that breaks a DIFFERENT test on this
	// shared database.
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	return id.String()
}

// resetSweepCursor clears the sweep's persisted position before and after a test. sweep_cursors has
// exactly one row per sweep NAME, so it is shared state between tests in this package; leaving one
// behind would silently change where the next test's first run starts.
func resetSweepCursor(t *testing.T, pool *pgxpool.Pool, name repository.SweepCursorName) {
	t.Helper()
	del := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM sweep_cursors WHERE sweep_name = $1`, string(name))
	}
	del()
	t.Cleanup(del)
}

func loadSweepCursorRow(t *testing.T, pool *pgxpool.Pool, name repository.SweepCursorName) (ts1 *time.Time, exclusive bool, present bool) {
	t.Helper()
	err := pool.QueryRow(context.Background(),
		`SELECT ts1, exclusive FROM sweep_cursors WHERE sweep_name = $1`, string(name)).Scan(&ts1, &exclusive)
	if err == pgx.ErrNoRows {
		return nil, false, false
	}
	require.NoError(t, err)
	return ts1, exclusive, true
}

// runDeletionSweep performs ONE Work() invocation and returns the queue that recorded it. A fresh
// queue per run is what makes "which users did THIS run reach" answerable.
func runDeletionSweep(t *testing.T, pool *pgxpool.Pool, pageSize, capUsers int, failFor map[string]bool) *recordingJobQueue {
	t.Helper()
	q := &recordingJobQueue{failFor: failFor}
	worker := NewScheduledDeletionCheckWorker(ScheduledDeletionCheckWorkerDeps{
		ProfileRepo:      repository.NewProfileRepository(pool, newJobsTestFieldEncryptor(t)),
		JobQueue:         q,
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		PageSize:         pageSize,
		MaxUsersPerSweep: capUsers,
		SweepCursors:     repository.NewSweepCursorRepository(pool),
	})
	require.NoError(t, worker.Work(context.Background(),
		newTestScheduledDeletionCheckJob(ScheduledDeletionCheckArgs{TriggerSource: "sweep-cursor-integration"})))
	return q
}

// assertSweepSeesExactlySeeded is the setup guard that turns a shared-database flake into a clear
// failure. These tests reason about which of THEIR rows a capped run reaches, which is only
// meaningful if no foreign fixture row from another test outranks them. Every row seeded here uses
// a deliberately ancient deletion_scheduled_at, so a foreign candidate would have to be older still
// — this asserts that it is not.
func assertSweepSeesExactlySeeded(t *testing.T, pool *pgxpool.Pool, seeded []string) {
	t.Helper()
	repo := repository.NewProfileRepository(pool, newJobsTestFieldEncryptor(t))
	users, err := repo.GetUsersScheduledForDeletionPage(context.Background(), nil, len(seeded))
	require.NoError(t, err)
	got := make([]string, 0, len(users))
	for _, u := range users {
		got = append(got, u.ID)
	}
	require.ElementsMatch(t, seeded, got,
		"setup guard: the oldest %d deletion candidates on this database must be exactly the rows this test seeded; "+
			"a foreign fixture row ranking ahead of them would invalidate every cap-based assertion below", len(seeded))
}

// ---------------------------------------------------------------------------------------------
// 1. THE STARVATION CASE. Must fail before #1477: without a persisted cursor the second run
//    restarts from the oldest row, re-reads the same permanently-failing head, and never reaches
//    the row behind it — on that run or any other.
// ---------------------------------------------------------------------------------------------

func TestScheduledDeletionSweepCursor_PermanentlyFailingHead_LaterRunReachesTheRowBehindIt(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorScheduledDeletion)

	base := time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)
	headA := seedDeletionCandidate(t, pool, base)
	headB := seedDeletionCandidate(t, pool, base.Add(1*time.Second))
	behind := seedDeletionCandidate(t, pool, base.Add(2*time.Second))
	assertSweepSeesExactlySeeded(t, pool, []string{headA, headB, behind})

	// The head can never drain: its enqueue always fails, so the users rows stay eligible forever.
	failing := map[string]bool{headA: true, headB: true}

	run1 := runDeletionSweep(t, pool, 2, 2, failing)
	require.True(t, run1.attemptedSet()[headA], "run 1 must reach the oldest row")
	require.True(t, run1.attemptedSet()[headB], "run 1 must reach the second row")
	require.False(t, run1.attemptedSet()[behind], "run 1 is capped at 2 and must not reach the third row")

	_, _, present := loadSweepCursorRow(t, pool, repository.SweepCursorScheduledDeletion)
	require.True(t, present, "a run that stopped on its cap must persist where it got to")

	run2 := runDeletionSweep(t, pool, 2, 2, failing)
	require.True(t, run2.attemptedSet()[behind],
		"THE DEFECT: the second run must reach the row ranked behind the permanently-failing head. "+
			"Without a persisted cursor it restarts from the oldest row, re-reads headA/headB forever, "+
			"and this row is never reached on any run")
}

// ---------------------------------------------------------------------------------------------
// 2. THE MANDATORY SIBLING. Without this, test 1 would also pass if the sweep had simply stopped
//    working — e.g. if the cursor were saved unconditionally and every run skipped the whole set.
// ---------------------------------------------------------------------------------------------

func TestScheduledDeletionSweepCursor_UnderCapRun_ProcessesEveryRowAndSkipsNone(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorScheduledDeletion)

	base := time.Date(1990, 2, 1, 0, 0, 0, 0, time.UTC)
	seeded := []string{
		seedDeletionCandidate(t, pool, base),
		seedDeletionCandidate(t, pool, base.Add(1*time.Second)),
		seedDeletionCandidate(t, pool, base.Add(2*time.Second)),
		seedDeletionCandidate(t, pool, base.Add(3*time.Second)),
		seedDeletionCandidate(t, pool, base.Add(4*time.Second)),
	}
	assertSweepSeesExactlySeeded(t, pool, seeded)

	// pageSize 2 with a cap of 100: several pages, no cap stop.
	run := runDeletionSweep(t, pool, 2, 100, nil)

	got := run.attemptedSet()
	for _, id := range seeded {
		require.True(t, got[id], "under-cap run must reach every seeded row; missed %s", id)
	}

	_, _, present := loadSweepCursorRow(t, pool, repository.SweepCursorScheduledDeletion)
	require.False(t, present,
		"a run that reached the END of the candidate set must CLEAR its position, not store a "+
			"past-the-end one — that reset is what stops a forward-only cursor stalling the sweep forever")
}

// ---------------------------------------------------------------------------------------------
// 3. WRAP-AROUND. A stored position past the end must resume from the beginning, in the same run,
//    and must be distinguishable from "no work to do".
// ---------------------------------------------------------------------------------------------

func TestScheduledDeletionSweepCursor_PositionPastTheEnd_WrapsAndFindsNewlyEligibleRows(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorScheduledDeletion)

	base := time.Date(1990, 3, 1, 0, 0, 0, 0, time.UTC)
	seeded := []string{
		seedDeletionCandidate(t, pool, base),
		seedDeletionCandidate(t, pool, base.Add(1*time.Second)),
	}
	assertSweepSeesExactlySeeded(t, pool, seeded)

	// Park the cursor past every candidate: far in the future, so nothing is at or after it.
	pastTheEnd := time.Date(2999, 1, 1, 0, 0, 0, 0, time.UTC)
	cursors := repository.NewSweepCursorRepository(pool)
	require.NoError(t, cursors.Save(context.Background(), repository.SweepCursorScheduledDeletion,
		repository.SweepPosition{TS1: &pastTheEnd}))

	run := runDeletionSweep(t, pool, 10, 100, nil)

	got := run.attemptedSet()
	for _, id := range seeded {
		require.True(t, got[id],
			"a cursor parked past the end must WRAP to the beginning within the same run and reach %s. "+
				"Reporting 'no work to do' here is the silent-stall failure that is worse than the bug being fixed", id)
	}

	_, _, present := loadSweepCursorRow(t, pool, repository.SweepCursorScheduledDeletion)
	require.False(t, present, "after wrapping and exhausting the set, the position must be cleared")
}

// ---------------------------------------------------------------------------------------------
// 4. THE TIE-BREAKER. Every fixture in this repository seeded strictly increasing timestamps, so
//    the `id ASC` component of all three sweeps' sort keys was load-bearing and yet removable
//    without any test noticing. These rows share ONE deletion_scheduled_at to the microsecond.
// ---------------------------------------------------------------------------------------------

func TestScheduledDeletionSweepCursor_IdenticalTimestamps_TotalOrderAcrossPageBoundaries(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorScheduledDeletion)

	// Identical to the microsecond — Postgres timestamptz resolution — so deletion_scheduled_at
	// alone cannot order these rows at all.
	tied := time.Date(1990, 4, 1, 0, 0, 0, 0, time.UTC)
	seeded := []string{
		seedDeletionCandidate(t, pool, tied),
		seedDeletionCandidate(t, pool, tied),
		seedDeletionCandidate(t, pool, tied),
		seedDeletionCandidate(t, pool, tied),
		seedDeletionCandidate(t, pool, tied),
	}
	assertSweepSeesExactlySeeded(t, pool, seeded)

	// pageSize 2 forces THREE page boundaries to fall inside the tie group: without a total order
	// the keyset predicate both repeats and skips rows across those boundaries.
	run := runDeletionSweep(t, pool, 2, 100, nil)

	seen := map[string]int{}
	for _, id := range run.attempted {
		seen[id]++
	}
	for _, id := range seeded {
		require.Equal(t, 1, seen[id],
			"row %s tied on deletion_scheduled_at must be visited EXACTLY once across the page "+
				"boundaries; %d visits means the id tie-breaker is not making the order total", id, seen[id])
	}
	require.Len(t, seen, len(seeded),
		"the run must have visited exactly the tied rows and nothing else")
}

// ---------------------------------------------------------------------------------------------
// 5. THE EXCLUSIVE ADVANCE. A tie group LARGER than the cap in which nothing ever drains would,
//    under a purely inclusive resume, hand every run the identical page forever — reintroducing
//    the starvation this whole mechanism removes. NextPosition escapes it after one extra run.
//    This pins the escape, and the honest price: the group's rows past the first cap-worth are not
//    reached, which is documented in repository/sweep_cursor_repository.go rather than claimed away.
// ---------------------------------------------------------------------------------------------

func TestScheduledDeletionSweepCursor_OverCapTieGroup_AdvancesPastItAndReachesRowsBehind(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorScheduledDeletion)

	tied := time.Date(1990, 5, 1, 0, 0, 0, 0, time.UTC)
	tiedA := seedDeletionCandidate(t, pool, tied)
	tiedB := seedDeletionCandidate(t, pool, tied)
	tiedC := seedDeletionCandidate(t, pool, tied)
	behind := seedDeletionCandidate(t, pool, tied.Add(1*time.Second))
	assertSweepSeesExactlySeeded(t, pool, []string{tiedA, tiedB, tiedC, behind})

	// Nothing in the tie group ever drains.
	failing := map[string]bool{tiedA: true, tiedB: true, tiedC: true}

	// Run 1: cap 2 inside a 3-row tie group. Saves the tied timestamp INCLUSIVELY.
	runDeletionSweep(t, pool, 2, 2, failing)
	ts1, exclusive, present := loadSweepCursorRow(t, pool, repository.SweepCursorScheduledDeletion)
	require.True(t, present)
	require.False(t, exclusive, "the first capped run has nothing to compare against and must save inclusively")
	require.NotNil(t, ts1)

	// Run 2: resumes at the same tied timestamp, spends its whole cap on the same key again, and
	// must therefore flip to an EXCLUSIVE position rather than saving the identical one forever.
	runDeletionSweep(t, pool, 2, 2, failing)
	_, exclusive, present = loadSweepCursorRow(t, pool, repository.SweepCursorScheduledDeletion)
	require.True(t, present)
	require.True(t, exclusive,
		"a capped run that made no progress along the timestamp axis must go EXCLUSIVE; saving the "+
			"same inclusive position would replay the identical page on every future run forever")

	// Run 3: steps strictly past the tie group and reaches the row behind it.
	run3 := runDeletionSweep(t, pool, 2, 2, failing)
	require.True(t, run3.attemptedSet()[behind],
		"the exclusive advance must let the sweep reach the row ranked BEHIND an over-cap, "+
			"never-draining tie group — that is what it is for")
}
