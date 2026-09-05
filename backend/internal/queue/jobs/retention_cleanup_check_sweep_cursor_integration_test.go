//go:build integration

package jobs

// retention_cleanup_check_sweep_cursor_integration_test.go — Issue #1477, real-Postgres proof for
// the expired-retention sweep. Same four claims as its scheduled-deletion sibling, on the sweep
// whose per-row action IS the hard delete (ExecuteDeletion → DeleteUserCascade's DELETE FROM users,
// in-process, with no intervening queue).
//
// These tests drive deleteExpiredAccounts directly rather than Work(): Work() also runs the
// retention-clock repair, the Plaid-disconnect repair, three monitoring passes and the 30d/7d
// notices, none of which have anything to do with the cursor, and all of which would need their own
// collaborators wired just to reach step 3.
//
// Run:
//
//	DATABASE_URL=postgres://... go test ./internal/queue/jobs/... \
//	  -run TestRetentionSweepCursor -tags integration -p 1 -count=1 -v

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/require"
)

// recordingDeletionService records every ExecuteDeletion it is asked to perform and fails
// permanently for a nominated set of user ids — the head of a queue that can never drain. It does
// NOT actually delete: the users row therefore stays eligible for every subsequent run, which is
// precisely the starvation shape. For the ids it does not fail, it soft-removes the row from the
// candidate set the way a real completed deletion would (clearing retention_expires_at), so the
// under-cap sibling test observes a genuinely draining sweep.
type recordingDeletionService struct {
	pool      *pgxpool.Pool
	attempted []string
	failFor   map[string]bool
}

func (s *recordingDeletionService) ExecuteDeletion(ctx context.Context, userID string, kind repository.DeletionClaimKind) error {
	s.attempted = append(s.attempted, userID)
	if s.failFor[userID] {
		return fmt.Errorf("simulated permanent deletion failure for %s (kind %v)", userID, kind)
	}
	// Not a real DeleteUserCascade: clearing the retention clock is enough to take the row out of
	// GetUsersWithExpiredRetentionPage's predicate, and it leaves the fixture row for t.Cleanup.
	_, err := s.pool.Exec(ctx, `UPDATE users SET retention_expires_at = NULL WHERE id = $1`, userID)
	return err
}

func (s *recordingDeletionService) attemptedSet() map[string]bool {
	set := make(map[string]bool, len(s.attempted))
	for _, id := range s.attempted {
		set[id] = true
	}
	return set
}

// seedRetentionCandidate inserts one user whose retention clock has already expired.
//
// ⚠ BOTH PRE-DELETION NOTICE MARKERS ARE STAMPED, and they have to be. Since Issue #1569 T1 the
// sweep refuses any expired user who was not actually warned at 30 and 7 days
// (retentionNoticesDeliveredPredicate) — apps/web/src/legal/PrivacyPolicy.tsx promises those
// notices and until T1 nothing enforced the promise. A candidate seeded with NULL markers is a
// WITHHELD account, not a deletion candidate, so these cursor/paging tests would be asserting
// against an empty population. The markers are backdated relative to expiry so the fixture
// describes a user who really was warned before their clock ran out.
func seedRetentionCandidate(t *testing.T, pool *pgxpool.Pool, expiresAt time.Time) string {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region,
		                   retention_expires_at, retention_notice_30d_sent_at, retention_notice_7d_sent_at)
		VALUES ($1, $2, $3, 'premium', 'cancelled', 'iad', $4, $5, $6)
	`, id, "sweepret|"+id.String(), "sweepret-"+id.String()+"@example.com", expiresAt,
		expiresAt.Add(-30*24*time.Hour), expiresAt.Add(-7*24*time.Hour))
	require.NoError(t, err, "seed retention candidate")

	// context.Background(), never t.Context() — see seedDeletionCandidate.
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	return id.String()
}

// runRetentionSweep performs ONE deleteExpiredAccounts pass over the REAL profile repository and
// the REAL sweep-cursor repository.
func runRetentionSweep(t *testing.T, pool *pgxpool.Pool, pageSize, capUsers int, failFor map[string]bool) *recordingDeletionService {
	t.Helper()
	svc := &recordingDeletionService{pool: pool, failFor: failFor}
	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:               repository.NewProfileRepository(pool, newJobsTestFieldEncryptor(t)),
		DeletionService:           svc,
		Logger:                    slog.New(slog.NewTextHandler(io.Discard, nil)),
		ExpiredRetentionPageSize:  pageSize,
		MaxUsersPerRetentionSweep: capUsers,
		SweepCursors:              repository.NewSweepCursorRepository(pool),
	})
	// A permanently-failing head makes deleteExpiredAccounts return a partial-failure error by
	// design (Step 3 propagates so River retries). That is expected here and is not what these
	// tests are about, so the error is deliberately not asserted either way.
	_ = worker.deleteExpiredAccounts(context.Background(), 1477)
	return svc
}

// assertRetentionSweepSeesExactlySeeded is the shared-database setup guard — see
// assertSweepSeesExactlySeeded for why every cap-based assertion below needs one.
func assertRetentionSweepSeesExactlySeeded(t *testing.T, pool *pgxpool.Pool, seeded []string) {
	t.Helper()
	repo := repository.NewProfileRepository(pool, newJobsTestFieldEncryptor(t))
	users, err := repo.GetUsersWithExpiredRetentionPage(context.Background(), nil, len(seeded))
	require.NoError(t, err)
	got := make([]string, 0, len(users))
	for _, u := range users {
		got = append(got, u.ID)
	}
	require.ElementsMatch(t, seeded, got,
		"setup guard: the oldest %d expired-retention candidates on this database must be exactly the rows "+
			"this test seeded; a foreign fixture row ranking ahead of them would invalidate every cap-based "+
			"assertion below", len(seeded))
}

// ---------------------------------------------------------------------------------------------
// 1. THE STARVATION CASE.
// ---------------------------------------------------------------------------------------------

func TestRetentionSweepCursor_PermanentlyFailingHead_LaterRunReachesTheRowBehindIt(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorExpiredRetention)

	base := time.Date(1991, 1, 1, 0, 0, 0, 0, time.UTC)
	headA := seedRetentionCandidate(t, pool, base)
	headB := seedRetentionCandidate(t, pool, base.Add(1*time.Second))
	behind := seedRetentionCandidate(t, pool, base.Add(2*time.Second))
	assertRetentionSweepSeesExactlySeeded(t, pool, []string{headA, headB, behind})

	failing := map[string]bool{headA: true, headB: true}

	run1 := runRetentionSweep(t, pool, 2, 2, failing)
	require.True(t, run1.attemptedSet()[headA], "run 1 must reach the oldest expired row")
	require.True(t, run1.attemptedSet()[headB], "run 1 must reach the second expired row")
	require.False(t, run1.attemptedSet()[behind], "run 1 is capped at 2 and must not reach the third row")

	_, _, present := loadSweepCursorRow(t, pool, repository.SweepCursorExpiredRetention)
	require.True(t, present, "a run that stopped on its cap must persist where it got to")

	run2 := runRetentionSweep(t, pool, 2, 2, failing)
	require.True(t, run2.attemptedSet()[behind],
		"THE DEFECT: the second run must reach the row ranked behind the permanently-failing head. "+
			"Without a persisted cursor it restarts from the oldest expired row and this one is never "+
			"reached on any run")
}

// ---------------------------------------------------------------------------------------------
// 2. THE MANDATORY SIBLING.
// ---------------------------------------------------------------------------------------------

func TestRetentionSweepCursor_UnderCapRun_ProcessesEveryRowAndSkipsNone(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorExpiredRetention)

	base := time.Date(1991, 2, 1, 0, 0, 0, 0, time.UTC)
	seeded := []string{
		seedRetentionCandidate(t, pool, base),
		seedRetentionCandidate(t, pool, base.Add(1*time.Second)),
		seedRetentionCandidate(t, pool, base.Add(2*time.Second)),
		seedRetentionCandidate(t, pool, base.Add(3*time.Second)),
		seedRetentionCandidate(t, pool, base.Add(4*time.Second)),
	}
	assertRetentionSweepSeesExactlySeeded(t, pool, seeded)

	run := runRetentionSweep(t, pool, 2, 100, nil)

	got := run.attemptedSet()
	for _, id := range seeded {
		require.True(t, got[id], "under-cap run must reach every seeded row; missed %s", id)
	}

	_, _, present := loadSweepCursorRow(t, pool, repository.SweepCursorExpiredRetention)
	require.False(t, present,
		"a run that reached the END of the candidate set must CLEAR its position, not store a past-the-end one")
}

// ---------------------------------------------------------------------------------------------
// 3. WRAP-AROUND.
// ---------------------------------------------------------------------------------------------

func TestRetentionSweepCursor_PositionPastTheEnd_WrapsAndFindsNewlyEligibleRows(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorExpiredRetention)

	base := time.Date(1991, 3, 1, 0, 0, 0, 0, time.UTC)
	seeded := []string{
		seedRetentionCandidate(t, pool, base),
		seedRetentionCandidate(t, pool, base.Add(1*time.Second)),
	}
	assertRetentionSweepSeesExactlySeeded(t, pool, seeded)

	// Past the end, but still in the past so the rows would remain eligible if the sweep looked.
	pastTheEnd := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	cursors := repository.NewSweepCursorRepository(pool)
	require.NoError(t, cursors.Save(context.Background(), repository.SweepCursorExpiredRetention,
		repository.SweepPosition{TS1: &pastTheEnd}))

	run := runRetentionSweep(t, pool, 10, 100, nil)

	got := run.attemptedSet()
	for _, id := range seeded {
		require.True(t, got[id],
			"a cursor parked past the end must WRAP to the beginning within the same run and reach %s; "+
				"reporting 'no users with expired retention' here is the silent stall", id)
	}

	_, _, present := loadSweepCursorRow(t, pool, repository.SweepCursorExpiredRetention)
	require.False(t, present, "after wrapping and exhausting the set, the position must be cleared")
}

// ---------------------------------------------------------------------------------------------
// 4. THE TIE-BREAKER — identical retention_expires_at to the microsecond.
// ---------------------------------------------------------------------------------------------

func TestRetentionSweepCursor_IdenticalTimestamps_TotalOrderAcrossPageBoundaries(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorExpiredRetention)

	tied := time.Date(1991, 4, 1, 0, 0, 0, 0, time.UTC)
	seeded := []string{
		seedRetentionCandidate(t, pool, tied),
		seedRetentionCandidate(t, pool, tied),
		seedRetentionCandidate(t, pool, tied),
		seedRetentionCandidate(t, pool, tied),
		seedRetentionCandidate(t, pool, tied),
	}
	assertRetentionSweepSeesExactlySeeded(t, pool, seeded)

	// Every row FAILS, so nothing leaves the candidate set mid-run: without the id tie-breaker the
	// keyset predicate would then have to both repeat and skip rows across the page boundaries.
	failing := map[string]bool{}
	for _, id := range seeded {
		failing[id] = true
	}

	run := runRetentionSweep(t, pool, 2, 100, failing)

	seen := map[string]int{}
	for _, id := range run.attempted {
		seen[id]++
	}
	for _, id := range seeded {
		require.Equal(t, 1, seen[id],
			"row %s tied on retention_expires_at must be visited EXACTLY once across the page boundaries; "+
				"%d visits means the id tie-breaker is not making the order total", id, seen[id])
	}
	require.Len(t, seen, len(seeded), "the run must have visited exactly the tied rows and nothing else")
}
