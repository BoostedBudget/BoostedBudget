package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// ─── the purge worker ────────────────────────────────────────────────────────────────────────────

type fakePurger struct {
	calls  int
	asOf   time.Time
	limit  int
	purged int
	err    error
}

func (f *fakePurger) PurgeExpiredTokens(_ context.Context, asOf time.Time, limit int) (int, error) {
	f.calls++
	f.asOf = asOf
	f.limit = limit
	return f.purged, f.err
}

func purgeJob() *river.Job[PlaidOrphanTokenPurgeArgs] {
	return &river.Job[PlaidOrphanTokenPurgeArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Kind: PlaidOrphanTokenPurgeArgs{}.Kind(), Queue: "low"},
		Args:   PlaidOrphanTokenPurgeArgs{TriggerSource: "periodic"},
	}
}

// THE HEADLINE PROPERTY AT THE JOB LEVEL: this worker takes no Plaid client, no retry service, and no
// knowledge that retrying exists. It cannot be made to depend on the retry job, because there is
// nothing in its dependencies to depend on — PlaidOrphanTokenPurgeWorkerDeps has exactly one field
// besides the logger and the test override, and it is the repository.
func TestPlaidOrphanTokenPurgeWorker_Work_PurgesWithNoRetryServiceInSight(t *testing.T) {
	purger := &fakePurger{purged: 3}
	w := NewPlaidOrphanTokenPurgeWorker(PlaidOrphanTokenPurgeWorkerDeps{Repo: purger})

	require.NoError(t, w.Work(context.Background(), purgeJob()))
	assert.Equal(t, 1, purger.calls)
	assert.Equal(t, maxOrphanTokenPurgesPerSweep, purger.limit,
		"the production cap must be applied when nothing overrides it")
	assert.False(t, purger.asOf.IsZero(), "the run must decide on a real instant, not the zero time")
}

// A purge run that could not purge must FAIL, not report success. A green job record for a run that
// left credentials past their deadline is the exact "state marked, work never happened" shape this job
// exists to prevent — and River retrying it is the recovery.
func TestPlaidOrphanTokenPurgeWorker_Work_FailsLoudlyWhenThePurgeFails(t *testing.T) {
	w := NewPlaidOrphanTokenPurgeWorker(PlaidOrphanTokenPurgeWorkerDeps{
		Repo: &fakePurger{err: errors.New("connection reset")},
	})

	err := w.Work(context.Background(), purgeJob())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection reset")
}

// An unwired repository is a misconfiguration, and it must be a LOUD one for the same reason: a worker
// that silently does nothing is indistinguishable from one that had nothing to do.
func TestPlaidOrphanTokenPurgeWorker_Work_RefusesWhenUnwired(t *testing.T) {
	w := NewPlaidOrphanTokenPurgeWorker(PlaidOrphanTokenPurgeWorkerDeps{})
	err := w.Work(context.Background(), purgeJob())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NOT being purged")
}

func TestPlaidOrphanTokenPurgeWorker_Work_HonoursTheTestOverrideCap(t *testing.T) {
	purger := &fakePurger{purged: 2}
	w := NewPlaidOrphanTokenPurgeWorker(PlaidOrphanTokenPurgeWorkerDeps{Repo: purger, MaxPerRun: 2})
	require.NoError(t, w.Work(context.Background(), purgeJob()))
	assert.Equal(t, 2, purger.limit)
}

// ─── the retry worker ────────────────────────────────────────────────────────────────────────────

type fakeSweeper struct {
	calls       int
	pageSize    int
	maxAttempts int
	res         services.OrphanRetrySweepResult
	err         error
}

func (f *fakeSweeper) Sweep(_ context.Context, _ time.Time, pageSize, maxAttempts int) (services.OrphanRetrySweepResult, error) {
	f.calls++
	f.pageSize = pageSize
	f.maxAttempts = maxAttempts
	return f.res, f.err
}

func retryJob() *river.Job[PlaidOrphanRetryArgs] {
	return &river.Job[PlaidOrphanRetryArgs]{
		JobRow: &rivertype.JobRow{ID: 2, Kind: PlaidOrphanRetryArgs{}.Kind(), Queue: "low"},
		Args:   PlaidOrphanRetryArgs{TriggerSource: "periodic"},
	}
}

func TestPlaidOrphanRetryWorker_Work_PassesTheProductionBounds(t *testing.T) {
	sweeper := &fakeSweeper{res: services.OrphanRetrySweepResult{Attempted: 2, Resolved: 1, Failed: 1}}
	w := NewPlaidOrphanRetryWorker(PlaidOrphanRetryWorkerDeps{Service: sweeper})

	require.NoError(t, w.Work(context.Background(), retryJob()))
	assert.Equal(t, orphanRetryPageSize, sweeper.pageSize)
	assert.Equal(t, maxOrphanRetriesPerSweep, sweeper.maxAttempts)
}

func TestPlaidOrphanRetryWorker_Work_ReturnsTheSweepError(t *testing.T) {
	w := NewPlaidOrphanRetryWorker(PlaidOrphanRetryWorkerDeps{
		Service: &fakeSweeper{err: errors.New("read retryable page: boom")},
	})
	err := w.Work(context.Background(), retryJob())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

// Plaid unconfigured. The retry worker refuses — and the purge worker above is unaffected, which is the
// whole reason they are two jobs.
func TestPlaidOrphanRetryWorker_Work_RefusesWhenUnwired(t *testing.T) {
	w := NewPlaidOrphanRetryWorker(PlaidOrphanRetryWorkerDeps{})
	err := w.Work(context.Background(), retryJob())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NOT being retried")
}

// ─── registration plumbing ───────────────────────────────────────────────────────────────────────

// Both kinds must be resolvable by the dead-letter replay path. unique_opts_by_kind_test.go already
// fails if a kind declared in this package is missing from the table; this asserts the values are the
// ones the real producers pass, which that test cannot see.
func TestPlaidOrphanSweeps_UniqueOptsAreRegisteredForReplay(t *testing.T) {
	purgeOpts, known := UniqueOptsForKind(PlaidOrphanTokenPurgeArgs{}.Kind())
	require.True(t, known, "an unknown kind makes a dead-letter replay refuse")
	require.NotNil(t, purgeOpts)
	assert.Equal(t, PlaidOrphanTokenPurgeUniqueOpts(), purgeOpts)

	retryOpts, known := UniqueOptsForKind(PlaidOrphanRetryArgs{}.Kind())
	require.True(t, known)
	require.NotNil(t, retryOpts)
	assert.Equal(t, PlaidOrphanRetryUniqueOpts(), retryOpts)
}

// RunOnStart with dedup, and a stable periodic ID so River restores nextRunAt across restarts rather
// than recomputing now+interval on every leader election — the Issue #1448 invariant, asserted here for
// the two new kinds as well as by cmd/server's own periodic-jobs test.
func TestPlaidOrphanSweeps_PeriodicOptsCarryRunOnStartAndAStableID(t *testing.T) {
	purge := PlaidOrphanTokenPurgePeriodicOpts()
	assert.True(t, purge.RunOnStart)
	assert.Equal(t, "plaid_orphan_token_purge", purge.ID)

	retry := PlaidOrphanRetryPeriodicOpts()
	assert.True(t, retry.RunOnStart)
	assert.Equal(t, "plaid_orphan_retry", retry.ID)
}

// Completed must be in both dedup state sets, because both registrations use RunOnStart: true. Without
// it every deploy enqueues a duplicate of a run that finished minutes ago (Issue #1448).
func TestPlaidOrphanSweeps_UniqueOptsIncludeCompletedForRunOnStart(t *testing.T) {
	for name, got := range map[string][]queue.JobState{
		"purge": PlaidOrphanTokenPurgeUniqueOpts().ByState,
		"retry": PlaidOrphanRetryUniqueOpts().ByState,
	} {
		assert.Contains(t, got, queue.JobStateCompleted,
			"%s: RunOnStart without Completed dedup re-enqueues on every deploy", name)
	}
}
