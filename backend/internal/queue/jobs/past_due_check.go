// Package jobs contains River job definitions and workers for async processing.
// Issue #898: Past-due check periodic job — disconnects Plaid and auto-cancels after grace period.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
)

type PastDueCheckArgs struct {
	TriggerSource string `json:"trigger_source,omitempty"`
}

func (PastDueCheckArgs) Kind() string { return "past_due_check" }

type PastDueCheckProfileRepository interface {
	GetUsersInPastDue(ctx context.Context) ([]domain.User, error)
}

type PastDueCheckService interface {
	ProcessPastDueUser(ctx context.Context, user domain.User, now time.Time) error
}

type PastDueCheckWorker struct {
	river.WorkerDefaults[PastDueCheckArgs]

	profileRepo    PastDueCheckProfileRepository
	pastDueService PastDueCheckService
	logger         *slog.Logger
}

type PastDueCheckWorkerDeps struct {
	ProfileRepo    PastDueCheckProfileRepository
	PastDueService PastDueCheckService
	Logger         *slog.Logger
}

func NewPastDueCheckWorker(deps PastDueCheckWorkerDeps) *PastDueCheckWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &PastDueCheckWorker{
		profileRepo:    deps.ProfileRepo,
		pastDueService: deps.PastDueService,
		logger:         logger,
	}
}

func (w *PastDueCheckWorker) Work(ctx context.Context, job *river.Job[PastDueCheckArgs]) (err error) {
	w.logger.Info("starting past_due check",
		"job_id", job.ID,
		"trigger_source", job.Args.TriggerSource,
		"attempt", job.Attempt,
	)

	start := time.Now()
	defer func() {
		status := "success"
		if err != nil {
			status = "failed"
		} else {
			observability.RecordJobSuccess(job.Kind) // Issue #1413 P4: last-success heartbeat
		}
		observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
		observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	}()

	users, err := w.profileRepo.GetUsersInPastDue(ctx)
	if err != nil {
		w.logger.Error("failed to fetch past_due users", "job_id", job.ID, "error", err)
		return fmt.Errorf("failed to fetch past_due users: %w", err)
	}

	if len(users) == 0 {
		w.logger.Info("no past_due users to process", "job_id", job.ID)
		return nil
	}

	w.logger.Info("found past_due users", "job_id", job.ID, "user_count", len(users))

	now := time.Now()
	var processedCount, errorCount int
	for _, user := range users {
		if processErr := w.pastDueService.ProcessPastDueUser(ctx, user, now); processErr != nil {
			w.logger.Error("failed to process past_due user",
				"job_id", job.ID,
				"user_id", user.ID,
				"error", processErr,
			)
			errorCount++
			continue
		}
		processedCount++
	}

	w.logger.Info("past_due check completed",
		"job_id", job.ID,
		"total_users", len(users),
		"processed", processedCount,
		"errors", errorCount,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return nil
}

func PastDueCheckUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs:   false,
		ByPeriod: 1 * time.Hour,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}

// PastDueCheckInsertOpts returns the *river.InsertOpts this job's periodic registration in
// main.go must use — Queue: "low" plus the uniqueness options from PastDueCheckUniqueOpts(),
// converted to river's UniqueOpts type.
//
// Issue #1448: main.go's periodic registration previously built a bare
// &river.InsertOpts{Queue: "low"} and never called PastDueCheckUniqueOpts() at all — even
// though its PeriodicJobOpts already set RunOnStart: true, the unsafe
// RunOnStart-without-dedup combination this package's retention/manual-tier helpers document
// explicitly. This job disconnects Plaid at 14 days past due and auto-cancels at 21 —
// duplicate runs risk re-processing the same user twice in one pass.
func PastDueCheckInsertOpts() *river.InsertOpts {
	return riverInsertOptsFromUnique(queue.QueueLow, PastDueCheckUniqueOpts())
}

// PastDueCheckPeriodicOpts returns the *river.PeriodicJobOpts this job's periodic
// registration in main.go must use. RunOnStart: true is only safe because
// PastDueCheckInsertOpts() above always carries PastDueCheckUniqueOpts()'s dedup. ID is set
// so River restores nextRunAt across restarts/leader elections instead of recomputing
// now+interval every time — Issue #1448.
func PastDueCheckPeriodicOpts() *river.PeriodicJobOpts {
	return &river.PeriodicJobOpts{RunOnStart: true, ID: PastDueCheckArgs{}.Kind()}
}
