// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/riverqueue/river"
)

// BoostEvaluationArgs contains the arguments for a boost evaluation job.
// This job evaluates automation rules when events occur (e.g., budget_updated,
// transaction_created) and executes matching rules asynchronously.
type BoostEvaluationArgs struct {
	// UserID is the ID of the user whose rules should be evaluated
	UserID string `json:"user_id"`

	// EventType is the type of event that triggered this evaluation
	// Examples: "budget_updated", "transaction_created", "account_balance_changed"
	EventType string `json:"event_type"`

	// EventData contains the event payload used for condition evaluation
	// Structure depends on event type (e.g., budget_id, percent_used for budget events)
	EventData map[string]interface{} `json:"event_data"`
}

// Kind returns the unique identifier for this job type.
func (BoostEvaluationArgs) Kind() string { return "boost_evaluation" }

// BoostEvaluationWorker processes boost evaluation jobs by checking automation
// rules against event data and executing matching rules.
type BoostEvaluationWorker struct {
	river.WorkerDefaults[BoostEvaluationArgs]

	boostExecutionService services.BoostExecutionService
	logger               *slog.Logger
}

// BoostEvaluationWorkerDeps contains the dependencies for BoostEvaluationWorker.
type BoostEvaluationWorkerDeps struct {
	BoostExecutionService services.BoostExecutionService
	Logger               *slog.Logger
}

// NewBoostEvaluationWorker creates a new boost evaluation worker.
func NewBoostEvaluationWorker(deps BoostEvaluationWorkerDeps) *BoostEvaluationWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &BoostEvaluationWorker{
		boostExecutionService: deps.BoostExecutionService,
		logger:               logger,
	}
}

// Work processes a rule evaluation job.
//
// The worker:
//  1. Retrieves all enabled automation rules for the user
//  2. Evaluates rule conditions against the event data
//  3. Executes matching rules (e.g., send notifications, create suggestions)
//
// Rule execution failures for individual rules don't fail the entire job.
// Each rule execution is logged and tracked independently.
func (w *BoostEvaluationWorker) Work(ctx context.Context, job *river.Job[BoostEvaluationArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting rule evaluation job",
		"job_id", job.ID,
		"user_id", args.UserID,
		"event_type", args.EventType,
		"attempt", job.Attempt,
	)

	start := time.Now()
	defer func() {
		status := "success"
		if err != nil {
			status = "failed"
		}
		observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
		observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	}()

	startTime := time.Now()

	// Process the event trigger - this evaluates all matching rules
	err = w.boostExecutionService.ProcessEventTrigger(ctx, args.UserID, args.EventType, args.EventData)
	if err != nil {
		w.logger.Error("rule evaluation failed",
			"job_id", job.ID,
			"user_id", args.UserID,
			"event_type", args.EventType,
			"error", err,
			"duration_ms", time.Since(startTime).Milliseconds(),
		)
		return fmt.Errorf("rule evaluation failed: %w", err)
	}

	w.logger.Info("rule evaluation completed",
		"job_id", job.ID,
		"user_id", args.UserID,
		"event_type", args.EventType,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	return nil
}

// BoostEvaluationUniqueOpts returns the recommended uniqueness options for BoostEvaluationArgs.
// This provides a 1-minute debounce period for the same user+event type combination.
// This prevents rapid-fire rule evaluations when many events occur quickly.
func BoostEvaluationUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs:   true,
		ByPeriod: 1 * time.Minute,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}
