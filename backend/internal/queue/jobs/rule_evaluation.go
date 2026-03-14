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

// RuleEvaluationArgs contains the arguments for a rule evaluation job.
// This job evaluates automation rules when events occur (e.g., budget_updated,
// transaction_created) and executes matching rules asynchronously.
type RuleEvaluationArgs struct {
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
func (RuleEvaluationArgs) Kind() string { return "rule_evaluation" }

// RuleEvaluationWorker processes rule evaluation jobs by checking automation
// rules against event data and executing matching rules.
type RuleEvaluationWorker struct {
	river.WorkerDefaults[RuleEvaluationArgs]

	ruleExecutionService services.RuleExecutionService
	logger               *slog.Logger
}

// RuleEvaluationWorkerDeps contains the dependencies for RuleEvaluationWorker.
type RuleEvaluationWorkerDeps struct {
	RuleExecutionService services.RuleExecutionService
	Logger               *slog.Logger
}

// NewRuleEvaluationWorker creates a new rule evaluation worker.
func NewRuleEvaluationWorker(deps RuleEvaluationWorkerDeps) *RuleEvaluationWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &RuleEvaluationWorker{
		ruleExecutionService: deps.RuleExecutionService,
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
func (w *RuleEvaluationWorker) Work(ctx context.Context, job *river.Job[RuleEvaluationArgs]) (err error) {
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
	err = w.ruleExecutionService.ProcessEventTrigger(ctx, args.UserID, args.EventType, args.EventData)
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

// RuleEvaluationUniqueOpts returns the recommended uniqueness options for RuleEvaluationArgs.
// This provides a 1-minute debounce period for the same user+event type combination.
// This prevents rapid-fire rule evaluations when many events occur quickly.
func RuleEvaluationUniqueOpts() *queue.UniqueOpts {
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
