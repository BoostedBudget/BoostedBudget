package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	rc "github.com/maxnussbaum/BoostedBudget/internal/integration/revenuecat"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
)

// Arguments contain references only, never credentials or provider payloads.
type RevenueCatSyncArgs struct {
	UserID      string `json:"user_id"`
	OperationID string `json:"operation_id,omitempty"`
	EventID     string `json:"event_id,omitempty"`
}

func (RevenueCatSyncArgs) Kind() string { return "revenuecat_sync" }

type revenueCatSyncService interface {
	Refresh(context.Context, string) (domain.RevenueCatState, error)
	ProcessOperation(context.Context, string, string) error
}
type revenueCatWebhookStore interface {
	CompleteWebhook(context.Context, string, string) error
}

type RevenueCatSyncWorker struct {
	river.WorkerDefaults[RevenueCatSyncArgs]
	service  revenueCatSyncService
	webhooks revenueCatWebhookStore
}

func NewRevenueCatSyncWorker(service revenueCatSyncService, webhooks revenueCatWebhookStore) *RevenueCatSyncWorker {
	return &RevenueCatSyncWorker{service: service, webhooks: webhooks}
}

// Work records the generic River job outcome pair for this kind — the async counterpart to the
// handler-side revenuecat_webhook_processing_duration_seconds. A rate-limit snooze is neither a
// success nor a failure: River will re-run the job, so it is recorded as "snoozed" rather than
// inflating the failure rate with an expected throttle.
func (w *RevenueCatSyncWorker) Work(ctx context.Context, job *river.Job[RevenueCatSyncArgs]) error {
	start := time.Now()
	err := w.work(ctx, job)
	status := "success"
	var snooze *river.JobSnoozeError
	switch {
	case errors.As(err, &snooze):
		status = "snoozed"
	case err != nil:
		status = "failed"
	}
	observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
	observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	return err
}

func (w *RevenueCatSyncWorker) work(ctx context.Context, job *river.Job[RevenueCatSyncArgs]) error {
	a := job.Args
	if a.UserID == "" || (a.OperationID != "" && a.EventID != "") {
		return errors.New("invalid RevenueCat job references")
	}
	if w.service == nil || (a.EventID != "" && w.webhooks == nil) {
		return errors.New("RevenueCat synchronization unavailable")
	}
	var err error
	if a.OperationID != "" {
		err = w.service.ProcessOperation(ctx, a.UserID, a.OperationID)
	} else {
		_, err = w.service.Refresh(ctx, a.UserID)
	}
	message := ""
	var providerError *rc.HTTPError
	limited := errors.As(err, &providerError) && providerError.StatusCode == 429
	if err != nil {
		message = "synchronization_failed"
		if limited {
			message = "rate_limited"
		}
	}
	var recordErr error
	if a.EventID != "" {
		recordErr = w.webhooks.CompleteWebhook(ctx, a.EventID, message)
	}
	if limited {
		delay := providerError.RetryAfter
		if delay <= 0 {
			delay = time.Minute
		}
		return river.JobSnooze(delay)
	}
	if recordErr != nil {
		return errors.New("RevenueCat processing result could not be recorded")
	}
	if err != nil {
		return errors.New("RevenueCat synchronization failed; see customer diagnostics")
	}
	return nil
}

// All producers and DLQ replay share these options. Completed refreshes must not
// suppress a later refresh; only outstanding identical work is coalesced.
func RevenueCatSyncUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{ByArgs: true, ByState: []queue.JobState{
		queue.JobStateAvailable, queue.JobStatePending, queue.JobStateRunning,
		queue.JobStateRetryable, queue.JobStateScheduled,
	}}
}

func RevenueCatSyncEnqueueOptions(interactive bool) []queue.EnqueueOption {
	priority := 4
	if interactive {
		priority = 1
	}
	return []queue.EnqueueOption{queue.WithQueue("revenuecat"), queue.WithPriority(priority), queue.WithUnique(*RevenueCatSyncUniqueOpts())}
}

type RevenueCatRepairArgs struct{}

func (RevenueCatRepairArgs) Kind() string { return "revenuecat_repair" }

type revenueCatRepairStore interface {
	PendingOperations(context.Context, int) ([]domain.RevenueCatOperation, error)
	ListDue(context.Context, time.Time, int) ([]string, error)
}
type revenueCatJobEnqueuer interface {
	Enqueue(context.Context, queue.Job, ...queue.EnqueueOption) (*queue.JobResult, error)
}

type RevenueCatRepairWorker struct {
	river.WorkerDefaults[RevenueCatRepairArgs]
	store revenueCatRepairStore
	queue revenueCatJobEnqueuer
}

func NewRevenueCatRepairWorker(store revenueCatRepairStore, q revenueCatJobEnqueuer) *RevenueCatRepairWorker {
	return &RevenueCatRepairWorker{store: store, queue: q}
}

// SetJobQueue resolves the worker/client construction cycle before workers start.
func (w *RevenueCatRepairWorker) SetJobQueue(q revenueCatJobEnqueuer) {
	w.queue = q
}

// Work recovers durable grant intents before scheduling snapshot repair. The
// repository's next_refresh_at covers jittered active repair, expiry, and daily
// inactive repair. Repeated enqueueing coalesces against existing River work.
func (w *RevenueCatRepairWorker) Work(ctx context.Context, _ *river.Job[RevenueCatRepairArgs]) error {
	if w.store == nil || w.queue == nil {
		return errors.New("RevenueCat repair unavailable")
	}
	operations, err := w.store.PendingOperations(ctx, 100)
	if err != nil {
		return err
	}
	for _, op := range operations {
		if _, err = w.queue.Enqueue(ctx, RevenueCatSyncArgs{UserID: op.UserID, OperationID: op.ID}, RevenueCatSyncEnqueueOptions(true)...); err != nil {
			return err
		}
	}
	users, err := w.store.ListDue(ctx, time.Now(), 100)
	if err != nil {
		return err
	}
	for _, id := range users {
		if _, err = w.queue.Enqueue(ctx, RevenueCatSyncArgs{UserID: id}, RevenueCatSyncEnqueueOptions(false)...); err != nil {
			return err
		}
	}
	return nil
}

func RevenueCatRepairUniqueOpts() *queue.UniqueOpts {
	opts := RevenueCatSyncUniqueOpts()
	opts.ByArgs = false
	opts.ByPeriod = time.Minute
	opts.ByState = append(opts.ByState, queue.JobStateCompleted)
	return opts
}
func RevenueCatRepairInsertOpts() *river.InsertOpts {
	return riverInsertOptsFromUnique(queue.QueueLow, RevenueCatRepairUniqueOpts())
}
func RevenueCatRepairPeriodicOpts() *river.PeriodicJobOpts {
	return &river.PeriodicJobOpts{RunOnStart: true, ID: RevenueCatRepairArgs{}.Kind()}
}
