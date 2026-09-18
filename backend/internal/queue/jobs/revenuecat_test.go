package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	rc "github.com/maxnussbaum/BoostedBudget/internal/integration/revenuecat"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

// newTestRevenueCatSyncJob mirrors the convention every other worker test in this package follows:
// a real JobRow, because Work records the generic jobs_completed_total / job_duration_seconds pair
// from job.Kind and job.Queue.
func newTestRevenueCatSyncJob(args RevenueCatSyncArgs) *river.Job[RevenueCatSyncArgs] {
	return &river.Job[RevenueCatSyncArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Kind: "revenuecat_sync", Queue: "revenuecat", Attempt: 1},
		Args:   args,
	}
}

type revenueCatWorkerService struct {
	refreshes, operations int
	err                   error
}

func (s *revenueCatWorkerService) Refresh(context.Context, string) (domain.RevenueCatState, error) {
	s.refreshes++
	return domain.RevenueCatState{}, s.err
}
func (s *revenueCatWorkerService) ProcessOperation(context.Context, string, string) error {
	s.operations++
	return s.err
}

type revenueCatWebhookRecorder struct {
	messages []string
	err      error
}

func (r *revenueCatWebhookRecorder) CompleteWebhook(_ context.Context, _ string, message string) error {
	r.messages = append(r.messages, message)
	return r.err
}

func TestRevenueCatSyncWorkerRefreshesInsteadOfApplyingEventPayload(t *testing.T) {
	svc, recorder := &revenueCatWorkerService{}, &revenueCatWebhookRecorder{}
	w := NewRevenueCatSyncWorker(svc, recorder)
	err := w.Work(context.Background(), newTestRevenueCatSyncJob(RevenueCatSyncArgs{UserID: "user", EventID: "event"}))
	require.NoError(t, err)
	require.Equal(t, 1, svc.refreshes)
	require.Zero(t, svc.operations)
	require.Equal(t, []string{""}, recorder.messages)
}

func TestRevenueCatSyncWorkerProcessesDurableOperation(t *testing.T) {
	svc := &revenueCatWorkerService{}
	w := NewRevenueCatSyncWorker(svc, nil)
	err := w.Work(context.Background(), newTestRevenueCatSyncJob(RevenueCatSyncArgs{UserID: "user", OperationID: "operation"}))
	require.NoError(t, err)
	require.Equal(t, 1, svc.operations)
	require.Zero(t, svc.refreshes)
}

func TestRevenueCatSyncWorkerHonorsRetryAfterAndDoesNotMarkApplied(t *testing.T) {
	svc := &revenueCatWorkerService{err: &rc.HTTPError{StatusCode: 429, RetryAfter: 90 * time.Second}}
	recorder := &revenueCatWebhookRecorder{}
	w := NewRevenueCatSyncWorker(svc, recorder)
	err := w.Work(context.Background(), newTestRevenueCatSyncJob(RevenueCatSyncArgs{UserID: "user", EventID: "event"}))
	var snooze *river.JobSnoozeError
	require.True(t, errors.As(err, &snooze))
	require.Equal(t, 90*time.Second, snooze.Duration)
	require.Equal(t, []string{"rate_limited"}, recorder.messages)
	recorder.err = errors.New("database unavailable")
	err = w.Work(context.Background(), newTestRevenueCatSyncJob(RevenueCatSyncArgs{UserID: "user", EventID: "event"}))
	require.True(t, errors.As(err, &snooze), "recording failure must not shorten provider backoff")
	require.Equal(t, 90*time.Second, snooze.Duration)
}

func TestRevenueCatSyncWorkerRejectsAmbiguousArguments(t *testing.T) {
	svc := &revenueCatWorkerService{}
	w := NewRevenueCatSyncWorker(svc, nil)
	for _, args := range []RevenueCatSyncArgs{{}, {UserID: "user", OperationID: "op", EventID: "event"}} {
		require.Error(t, w.Work(context.Background(), newTestRevenueCatSyncJob(args)))
	}
	require.Zero(t, svc.refreshes)
	require.Zero(t, svc.operations)
}

type revenueCatRepairSource struct{}

func (revenueCatRepairSource) PendingOperations(context.Context, int) ([]domain.RevenueCatOperation, error) {
	return []domain.RevenueCatOperation{{ID: "operation", UserID: "grant-user"}}, nil
}
func (revenueCatRepairSource) ListDue(context.Context, time.Time, int) ([]string, error) {
	return []string{"refresh-user"}, nil
}

type revenueCatRecordingQueue struct {
	jobs    []RevenueCatSyncArgs
	options []queue.EnqueueOptions
	err     error
}

func (q *revenueCatRecordingQueue) Enqueue(_ context.Context, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	if q.err != nil {
		return nil, q.err
	}
	q.jobs = append(q.jobs, job.(RevenueCatSyncArgs))
	q.options = append(q.options, queue.ApplyOptions(opts))
	return &queue.JobResult{}, nil
}

func TestRevenueCatRepairRecoversDurableOperationsBeforeSnapshots(t *testing.T) {
	q := &revenueCatRecordingQueue{}
	w := NewRevenueCatRepairWorker(revenueCatRepairSource{}, q)
	require.NoError(t, w.Work(context.Background(), &river.Job[RevenueCatRepairArgs]{}))
	require.Equal(t, []RevenueCatSyncArgs{{UserID: "grant-user", OperationID: "operation"}, {UserID: "refresh-user"}}, q.jobs)
	require.Equal(t, 1, q.options[0].Priority)
	require.Equal(t, 4, q.options[1].Priority)
	for _, opt := range q.options {
		require.Equal(t, "revenuecat", opt.Queue)
		require.Equal(t, RevenueCatSyncUniqueOpts(), opt.Unique)
	}
	q.err = errors.New("queue unavailable")
	require.Error(t, w.Work(context.Background(), &river.Job[RevenueCatRepairArgs]{}))
}

func TestRevenueCatRepairQueueCanBeWiredAfterConstruction(t *testing.T) {
	w := NewRevenueCatRepairWorker(revenueCatRepairSource{}, nil)
	require.Error(t, w.Work(context.Background(), &river.Job[RevenueCatRepairArgs]{}))
	q := &revenueCatRecordingQueue{}
	w.SetJobQueue(q)
	require.NoError(t, w.Work(context.Background(), &river.Job[RevenueCatRepairArgs]{}))
	require.Len(t, q.jobs, 2)
}

func TestRevenueCatJobsReplayUsesProducerUniqueness(t *testing.T) {
	for kind, want := range map[string]*queue.UniqueOpts{
		"revenuecat_sync": RevenueCatSyncUniqueOpts(), "revenuecat_repair": RevenueCatRepairUniqueOpts(),
	} {
		got, ok := UniqueOptsForKind(kind)
		require.True(t, ok)
		require.Equal(t, want, got)
	}
	require.True(t, RevenueCatRepairPeriodicOpts().RunOnStart)
	require.Equal(t, "revenuecat_repair", RevenueCatRepairPeriodicOpts().ID)
	require.Equal(t, riverUniqueOpts(RevenueCatRepairUniqueOpts()), RevenueCatRepairInsertOpts().UniqueOpts)
}
