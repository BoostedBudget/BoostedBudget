package jobs

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"time"

	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/riverqueue/river"
)

const (
	postHogErasureLease       = 2 * time.Minute
	postHogErasureMaxAttempts = 12
	postHogErasureMaxBackoff  = 24 * time.Hour
)

type PostHogErasureArgs struct {
	TriggerSource string `json:"trigger_source,omitempty"`
}

func (PostHogErasureArgs) Kind() string { return "posthog_erasure" }

type postHogErasureStore interface {
	ClaimDue(context.Context, time.Time, time.Duration) (*repository.PostHogErasureRecord, error)
	MarkAccepted(context.Context, string, string, time.Time) (bool, error)
	RecordRetry(context.Context, string, string, string, time.Time, int) error
}

type postHogErasureProvider interface {
	RequestPersonDeletion(context.Context, string, string, []string) (phclient.ErasureDeleteResult, error)
}

type postHogErasureBacklogStore interface {
	Backlog(context.Context, time.Time) (repository.PostHogErasureBacklog, error)
}

type PostHogErasureWorkerDeps struct {
	Store    postHogErasureStore
	Provider postHogErasureProvider
	Logger   *slog.Logger
	Now      func() time.Time
}

type PostHogErasureWorker struct {
	river.WorkerDefaults[PostHogErasureArgs]
	store    postHogErasureStore
	provider postHogErasureProvider
	logger   *slog.Logger
	now      func() time.Time
}

func NewPostHogErasureWorker(deps PostHogErasureWorkerDeps) *PostHogErasureWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &PostHogErasureWorker{store: deps.Store, provider: deps.Provider, logger: logger, now: now}
}

func (w *PostHogErasureWorker) Work(ctx context.Context, _ *river.Job[PostHogErasureArgs]) error {
	if w.store == nil {
		return fmt.Errorf("PostHog erasure worker has no durable store")
	}
	now := w.now()
	defer w.publishBacklog(ctx, now)
	record, err := w.store.ClaimDue(ctx, now, postHogErasureLease)
	if err != nil || record == nil {
		return err
	}
	if w.provider == nil {
		observability.RecordPostHogErasureAttempt("person_delete", "retry", "configuration")
		return w.persistRetry(ctx, record, "configuration", 0)
	}
	_, err = w.provider.RequestPersonDeletion(ctx, record.ProjectID, record.Region, record.Identities)
	if err != nil {
		class, retryAfter := classifyErasureFailure(err)
		observability.RecordPostHogErasureAttempt("person_delete", "retry", class)
		return w.persistRetry(ctx, record, class, retryAfter)
	}
	updated, err := w.store.MarkAccepted(ctx, record.ID, record.LeaseToken, now)
	if err != nil {
		return err
	}
	if !updated {
		return fmt.Errorf("PostHog erasure lease was lost before acceptance could be recorded")
	}
	w.logger.Info("PostHog erasure accepted; awaiting manual verification", "phase", "support_manual")
	observability.RecordPostHogErasureAttempt("person_delete", "accepted", "none")
	return nil
}

func (w *PostHogErasureWorker) publishBacklog(ctx context.Context, now time.Time) {
	store, ok := w.store.(postHogErasureBacklogStore)
	if !ok {
		return
	}
	backlog, err := store.Backlog(ctx, now)
	if err != nil {
		w.logger.Warn("PostHog erasure backlog metrics unavailable", "error_class", "database")
		return
	}
	observability.PublishPostHogErasureBacklog(backlog.Counts, backlog.OldestSeconds)
}

func (w *PostHogErasureWorker) persistRetry(ctx context.Context, record *repository.PostHogErasureRecord, class string, retryAfter time.Duration) error {
	if retryAfter <= 0 {
		retryAfter = erasureBackoff(record.ID, record.AttemptCount)
	}
	if err := w.store.RecordRetry(ctx, record.ID, record.LeaseToken, class, w.now().Add(retryAfter), postHogErasureMaxAttempts); err != nil {
		return err
	}
	w.logger.Warn("PostHog erasure attempt deferred", "phase", "person_delete", "error_class", class)
	return nil
}

func classifyErasureFailure(err error) (string, time.Duration) {
	var providerErr *phclient.ErasureProviderError
	if errors.As(err, &providerErr) {
		return providerErr.Class, providerErr.RetryAfter
	}
	return "provider", 0
}

func erasureBackoff(id string, attempt int) time.Duration {
	exponent := min(max(attempt-1, 0), 10)
	base := time.Minute * time.Duration(1<<exponent)
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	jitter := time.Duration(h.Sum32()%60000) * time.Millisecond
	return min(base+jitter, postHogErasureMaxBackoff)
}

func PostHogErasureInsertOpts() *river.InsertOpts {
	return riverInsertOptsFromUnique(queue.QueueLow, PostHogErasureUniqueOpts())
}

func PostHogErasureUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: false, ByPeriod: time.Minute,
		ByState: []queue.JobState{queue.JobStateAvailable, queue.JobStateScheduled, queue.JobStateRunning, queue.JobStateRetryable, queue.JobStatePending},
	}
}

func PostHogErasurePeriodicOpts() *river.PeriodicJobOpts {
	return &river.PeriodicJobOpts{RunOnStart: true, ID: PostHogErasureArgs{}.Kind()}
}

var _ river.Worker[PostHogErasureArgs] = (*PostHogErasureWorker)(nil)
