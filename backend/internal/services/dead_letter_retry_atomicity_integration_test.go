//go:build integration

package services

// dead_letter_retry_atomicity_integration_test.go — Issue #1508.
//
// WHAT THIS FILE IS FOR. DeadLetterService.Retry claims the row's single replay slot and then
// enqueues the replacement job. While those were two separate commits, they could disagree in two
// ways that BOTH left retry_job_id = 0:
//
//	claim committed, enqueue never happened   → slot consumed, no job, row unrecoverable
//	enqueue happened, id write failed         → slot consumed at 0, with a live job behind it
//
// Because both look identical in the column, no reaper could tell them apart (that is why #1496's
// reaper was deferred). The fix is to run the claim, the insert and the id write in ONE transaction.
//
// WHY REAL POSTGRES. Every assertion here is about what SURVIVES a rollback — of a
// dead_letter_jobs UPDATE and of a river_job INSERT made through the same transaction. pgxmock
// validates call shape and never evaluates a transaction boundary, so it cannot see any of this.
// River's own guarantee (v0.38.0 client.go InsertTx: "Because of snapshot visibility guarantees
// across transactions, the job will not be worked until the transaction has committed, and if the
// transaction rolls back, so too is the inserted job") is likewise a property of the database, not
// of a mock.
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/services/ -run TestDeadLetterRetryAtomicity \
//	    -tags integration -p 1 -count=1 -v

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	riverqueue "github.com/maxnussbaum/BoostedBudget/internal/queue/river"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/riverqueue/river"
)

// dlqAtomicityKind is a probe kind owned by this file alone. River refuses to insert a kind that is
// not in the client's Workers bundle, so the worker below must be registered — but the client is
// never Start()ed, so nothing ever works the jobs and they stay countable in river_job.
const dlqAtomicityKind = "dlq_atomicity_probe_1508"

// dlqProbeArgs/dlqProbeWorker exist only to satisfy River's insert-time registration check.
type dlqProbeArgs struct{}

func (dlqProbeArgs) Kind() string { return dlqAtomicityKind }

type dlqProbeWorker struct {
	river.WorkerDefaults[dlqProbeArgs]
}

func (dlqProbeWorker) Work(context.Context, *river.Job[dlqProbeArgs]) error { return nil }

var errInjectedEnqueueFailure = errors.New("injected: queue unavailable")

// failingJobQueue fails BOTH enqueue entry points, so this fixture describes the same fault
// whether the service under test enqueues inside a transaction or outside one.
type failingJobQueue struct{}

func (failingJobQueue) Enqueue(context.Context, queue.Job, ...queue.EnqueueOption) (*queue.JobResult, error) {
	return nil, errInjectedEnqueueFailure
}

func (failingJobQueue) EnqueueTx(context.Context, pgx.Tx, queue.Job, ...queue.EnqueueOption) (*queue.JobResult, error) {
	return nil, errInjectedEnqueueFailure
}

func (failingJobQueue) EnqueueMany(context.Context, []queue.Job, ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	return nil, errInjectedEnqueueFailure
}

func (failingJobQueue) EnqueueManyTx(context.Context, pgx.Tx, []queue.Job, ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	return nil, errInjectedEnqueueFailure
}

// unreleasableDLQRepo is the real repository with any compensating release broken.
//
// This is the ticket's actual stranding scenario — "if the release itself fails … the row keeps
// retried_at set with no live job" — and it is the fault a compensation-based design cannot
// survive by construction. The override names the pre-#1508 method deliberately: the point of the
// transaction is that the outcome no longer depends on a second statement succeeding, so this
// method being dead is the fix, not a gap in the fixture.
type unreleasableDLQRepo struct {
	*repository.DeadLetterJobRepository
}

func (unreleasableDLQRepo) ReleaseRetryClaim(context.Context, int64) error {
	return errors.New("injected: release failed")
}

// idWriteBrokenDLQRepo is the real repository with the retry-id write broken — the second of the
// two disagreements above. Both spellings are overridden so the fixture injects the same fault
// before and after the id write moved inside the transaction.
type idWriteBrokenDLQRepo struct {
	*repository.DeadLetterJobRepository
}

var errInjectedIDWriteFailure = errors.New("injected: retry id write failed")

func (idWriteBrokenDLQRepo) MarkRetried(context.Context, int64, int64) error {
	return errInjectedIDWriteFailure
}

func (idWriteBrokenDLQRepo) MarkRetriedInTx(context.Context, pgx.Tx, int64, int64) error {
	return errInjectedIDWriteFailure
}

// dlqAtomicityFixture is one dead_letter_jobs row plus the pool it lives in.
type dlqAtomicityFixture struct {
	pool *pgxpool.Pool
	repo *repository.DeadLetterJobRepository
	id   int64
	args json.RawMessage
}

func setupDLQAtomicityFixture(t *testing.T) *dlqAtomicityFixture {
	t.Helper()
	pool := setupHouseholdServiceIntegrationDB(t)

	// context.Background(), never t.Context(): the test context is cancelled before t.Cleanup runs,
	// so cleanup keyed to it silently DELETEs nothing and leaks rows into another test.
	ctx := context.Background()

	// Args unique per test, so the river_job count below can identify THIS replay's insert and
	// nothing else — including a leftover from an earlier run against a reused database.
	args := json.RawMessage(`{"probe":"` + t.Name() + `"}`)

	var id int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO dead_letter_jobs (original_job_id, kind, queue, args, errors, max_attempts, created_at, failed_at)
		VALUES (0, $1, 'default', $2::jsonb, '[]'::jsonb, 5, NOW(), NOW())
		RETURNING id`, dlqAtomicityKind, args).Scan(&id))

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM dead_letter_jobs WHERE id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE kind = $1 AND args = $2::jsonb`,
			dlqAtomicityKind, args)
	})

	return &dlqAtomicityFixture{pool: pool, repo: repository.NewDeadLetterJobRepository(pool), id: id, args: args}
}

// claim reads the two columns the whole ticket is about.
func (f *dlqAtomicityFixture) claim(t *testing.T) (retriedAt *string, retryJobID *int64) {
	t.Helper()
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT retried_at::text, retry_job_id FROM dead_letter_jobs WHERE id = $1`, f.id).
		Scan(&retriedAt, &retryJobID))
	return retriedAt, retryJobID
}

// riverJobCount counts the jobs THIS replay would have inserted.
func (f *dlqAtomicityFixture) riverJobCount(t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM river_job WHERE kind = $1 AND args = $2::jsonb`,
		dlqAtomicityKind, f.args).Scan(&n))
	return n
}

// realRiverQueue builds a River client over the test pool. It is never Start()ed, so nothing works
// the jobs it inserts — but every insert goes through the production code path
// (queue/river.Client.Enqueue / EnqueueTx → river.Client.Insert / InsertTx).
func realRiverQueue(t *testing.T, pool *pgxpool.Pool) queue.JobQueue {
	t.Helper()
	workers := river.NewWorkers()
	river.AddWorker(workers, &dlqProbeWorker{})
	client, err := riverqueue.NewClient(pool, workers, riverqueue.DefaultConfig())
	require.NoError(t, err)
	return client
}

func dlqAtomicityResolver() UniqueOptsResolver {
	return resolverForKind(dlqAtomicityKind, nil)
}

// TestDeadLetterRetryAtomicity_Integration_EnqueueFailureLeavesTheRowRetryable is failure mode one:
// the claim must not survive an enqueue that did not happen, EVEN IF the compensating release is
// unavailable. Before #1508 the claim was its own committed statement and the release was the only
// thing that could undo it, so this left retried_at stamped with retry_job_id = 0 and no job —
// the row's single replay spent, and unrecoverable through the API.
func TestDeadLetterRetryAtomicity_Integration_EnqueueFailureLeavesTheRowRetryable(t *testing.T) {
	f := setupDLQAtomicityFixture(t)
	ctx := context.Background()

	svc := NewDeadLetterService(unreleasableDLQRepo{f.repo}, failingJobQueue{}, dlqAtomicityResolver())

	_, err := svc.Retry(ctx, f.id)
	require.ErrorIs(t, err, errInjectedEnqueueFailure, "the operator must see the enqueue failure")

	retriedAt, retryJobID := f.claim(t)
	assert.Nil(t, retriedAt, "an enqueue that never happened must not consume the row's one replay slot")
	assert.Nil(t, retryJobID, "no phantom retry_job_id = 0 may be left behind")
	assert.Zero(t, f.riverJobCount(t), "nothing was enqueued")

	// "Retryable" as a behaviour, not as a column value: the real path must now succeed.
	working := NewDeadLetterService(f.repo, realRiverQueue(t, f.pool), dlqAtomicityResolver())
	resp, err := working.Retry(ctx, f.id)
	require.NoError(t, err, "the row must still be replayable after a failed replay")
	require.True(t, resp.Success)
	assert.Equal(t, 1, f.riverJobCount(t))
}

// TestDeadLetterRetryAtomicity_Integration_IDWriteFailureLeavesNoJobAndNoClaim is failure mode two:
// the enqueue succeeded and the retry-id write failed. Before #1508 that write was explicitly
// best-effort, so the row stayed at retry_job_id = 0 forever WITH a live job behind it — the case
// that made the column undecidable and blocked the reaper. Now the insert is in the same
// transaction, so a failed id write takes the job with it and the whole replay simply did not
// happen.
func TestDeadLetterRetryAtomicity_Integration_IDWriteFailureLeavesNoJobAndNoClaim(t *testing.T) {
	f := setupDLQAtomicityFixture(t)
	ctx := context.Background()

	svc := NewDeadLetterService(idWriteBrokenDLQRepo{f.repo}, realRiverQueue(t, f.pool), dlqAtomicityResolver())

	_, err := svc.Retry(ctx, f.id)
	require.Error(t, err, "a replay whose bookkeeping failed must not be reported as a success")

	retriedAt, retryJobID := f.claim(t)
	assert.Nil(t, retriedAt, "the claim must roll back with the insert")
	assert.Nil(t, retryJobID)
	assert.Zero(t, f.riverJobCount(t),
		"the river_job insert was made inside the caller's transaction, so the rollback must remove it")
}

// TestDeadLetterRetryAtomicity_Integration_SuccessfulReplayClaimsAndEnqueuesExactlyOnce is the
// mandatory sibling. Without it, both tests above are satisfied by a Retry that is broken outright
// and never enqueues anything.
func TestDeadLetterRetryAtomicity_Integration_SuccessfulReplayClaimsAndEnqueuesExactlyOnce(t *testing.T) {
	f := setupDLQAtomicityFixture(t)
	ctx := context.Background()

	svc := NewDeadLetterService(f.repo, realRiverQueue(t, f.pool), dlqAtomicityResolver())

	resp, err := svc.Retry(ctx, f.id)
	require.NoError(t, err)
	require.True(t, resp.Success)
	require.NotNil(t, resp.NewJobID)
	require.Positive(t, *resp.NewJobID)

	retriedAt, retryJobID := f.claim(t)
	assert.NotNil(t, retriedAt, "a real replay claims the slot")
	require.NotNil(t, retryJobID, "the committed row must carry the REAL river job id, never the 0 placeholder")
	assert.Equal(t, *resp.NewJobID, *retryJobID)
	assert.Equal(t, 1, f.riverJobCount(t), "exactly one job, and it is the one the response names")

	var state string
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT state FROM river_job WHERE id = $1`, *retryJobID).Scan(&state),
		"the id recorded on the row must resolve to a real river_job")
	assert.Equal(t, "available", state)
}

// TestDeadLetterRetryAtomicity_Integration_SecondReplayIsStillRefused re-pins the single-shot
// property across the change: the claim moving inside a transaction must not weaken it.
func TestDeadLetterRetryAtomicity_Integration_SecondReplayIsStillRefused(t *testing.T) {
	f := setupDLQAtomicityFixture(t)
	ctx := context.Background()

	svc := NewDeadLetterService(f.repo, realRiverQueue(t, f.pool), dlqAtomicityResolver())

	_, err := svc.Retry(ctx, f.id)
	require.NoError(t, err)

	_, err = svc.Retry(ctx, f.id)
	require.ErrorIs(t, err, ErrDeadLetterJobAlreadyRetried)
	assert.Equal(t, 1, f.riverJobCount(t), "a refused second replay must not have enqueued anything")
}
