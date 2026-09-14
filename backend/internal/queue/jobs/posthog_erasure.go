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
	MarkPersonUUIDsResolved(context.Context, string, string, []string) (bool, error)
	MarkAccepted(context.Context, string, string, int) (bool, error)
	RecordPollPending(context.Context, string, string) (bool, error)
	MarkPersonEventsVerified(context.Context, string, string) (bool, error)
	RecordRetry(context.Context, string, string, string, time.Time, int) (string, error)
}

type postHogErasureProvider interface {
	ResolvePersonUUIDs(context.Context, string, string, []string) ([]string, error)
	RequestPersonDeletion(context.Context, string, string, []string) (phclient.ErasureDeleteResult, error)
	PersonEventDeletionComplete(context.Context, string, string, []string) (bool, error)
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
		return w.persistRetry(ctx, record, "configuration", 0)
	}
	switch record.Phase {
	case "person_resolve":
		var personUUIDs []string
		personUUIDs, err = w.provider.ResolvePersonUUIDs(ctx, record.ProjectID, record.Region, record.Identities)
		if err == nil {
			var updated bool
			updated, err = w.store.MarkPersonUUIDsResolved(ctx, record.ID, record.LeaseToken, personUUIDs)
			if err == nil && !updated {
				err = fmt.Errorf("PostHog erasure lease lost")
			}
			if err == nil {
				outcome := "accepted"
				if len(personUUIDs) == 0 {
					outcome = "failure"
				}
				observability.RecordPostHogErasureAttempt("person_resolve", outcome, "none")
				return nil
			}
		}
	case "person_delete":
		var result phclient.ErasureDeleteResult
		result, err = w.provider.RequestPersonDeletion(ctx, record.ProjectID, record.Region, record.Identities)
		if err == nil && (result.PersonsFound != len(record.PersonUUIDs) || result.PersonsDeleted != len(record.PersonUUIDs)) {
			err = &phclient.ErasureProviderError{Class: "invalid_response"}
		}
		if err == nil {
			var updated bool
			updated, err = w.store.MarkAccepted(ctx, record.ID, record.LeaseToken, len(record.PersonUUIDs))
			if err == nil && !updated {
				err = fmt.Errorf("PostHog erasure lease lost")
			}
			if err == nil {
				observability.RecordPostHogErasureAttempt("person_delete", "accepted", "none")
				return nil
			}
		}
	case "person_poll":
		var complete bool
		complete, err = w.provider.PersonEventDeletionComplete(ctx, record.ProjectID, record.Region, record.PersonUUIDs)
		if err == nil && !complete {
			updated, updateErr := w.store.RecordPollPending(ctx, record.ID, record.LeaseToken)
			if updateErr != nil {
				return updateErr
			}
			if !updated {
				return fmt.Errorf("PostHog erasure lease lost")
			}
			return nil
		}
		if err == nil {
			var updated bool
			updated, err = w.store.MarkPersonEventsVerified(ctx, record.ID, record.LeaseToken)
			if err == nil && !updated {
				err = fmt.Errorf("PostHog erasure lease lost")
			}
			if err == nil {
				observability.RecordPostHogErasureAttempt("person_poll", "accepted", "none")
				return nil
			}
		}
	default:
		err = fmt.Errorf("unknown PostHog erasure phase")
	}
	if err != nil {
		class, retryAfter := classifyErasureFailure(err)
		return w.persistRetry(ctx, record, class, retryAfter)
	}
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
	state, err := w.store.RecordRetry(ctx, record.ID, record.LeaseToken, class, w.now().Add(retryAfter), postHogErasureMaxAttempts)
	if err != nil {
		return err
	}
	outcome := "retry"
	if state == "dead_letter" {
		outcome = "dead_letter"
	}
	observability.RecordPostHogErasureAttempt(record.Phase, outcome, class)
	w.logger.Warn("PostHog erasure attempt deferred", "phase", record.Phase, "error_class", class, "state", state)
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
