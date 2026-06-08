// Package jobs contains River job definitions and workers for async processing.
// Issue #1270: Task 4 — BudgetPeriodCloseCheck emits budget_period_end events with per-category data.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/riverqueue/river"
)

// BudgetPeriodCloseCheckArgs contains the arguments for a budget period close check job.
// This job fires a budget_period_end event for a specific budget at the end of its period,
// enabling automation rules that trigger on period close (e.g., rollover suggestions).
type BudgetPeriodCloseCheckArgs struct {
	// BudgetID is the ID of the budget whose period is closing.
	BudgetID string `json:"budget_id"`

	// UserID is the owner of the budget (required for user-scoped event dispatch).
	UserID string `json:"user_id"`
}

// Kind returns the unique identifier for this job type.
func (BudgetPeriodCloseCheckArgs) Kind() string { return "budget_period_close_check" }

// BudgetPeriodRepository defines the repository methods needed for budget period close checks.
type BudgetPeriodRepository interface {
	// GetBudgetWithCategoriesByID returns the budget and its categories for a specific user.
	GetBudgetWithCategoriesByID(ctx context.Context, budgetID, userID string) (*domain.Budget, error)
}

// BudgetPeriodEventProcessor defines the interface for emitting budget period end events.
type BudgetPeriodEventProcessor interface {
	ProcessEventTrigger(ctx context.Context, userID, eventType string, eventData map[string]interface{}) error
}

// BudgetPeriodCloseCheckWorker processes budget period close jobs by emitting a
// budget_period_end event with per-category metrics suitable for condition evaluation.
type BudgetPeriodCloseCheckWorker struct {
	river.WorkerDefaults[BudgetPeriodCloseCheckArgs]

	budgetRepo     BudgetPeriodRepository
	eventProcessor BudgetPeriodEventProcessor // Optional: nil = no-op
	logger         *slog.Logger
}

// BudgetPeriodCloseCheckWorkerDeps contains the dependencies for BudgetPeriodCloseCheckWorker.
type BudgetPeriodCloseCheckWorkerDeps struct {
	BudgetRepo     BudgetPeriodRepository
	EventProcessor BudgetPeriodEventProcessor // Optional
	Logger         *slog.Logger
}

// NewBudgetPeriodCloseCheckWorker creates a new BudgetPeriodCloseCheckWorker.
func NewBudgetPeriodCloseCheckWorker(deps BudgetPeriodCloseCheckWorkerDeps) *BudgetPeriodCloseCheckWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &BudgetPeriodCloseCheckWorker{
		budgetRepo:     deps.BudgetRepo,
		eventProcessor: deps.EventProcessor,
		logger:         logger,
	}
}

// Work processes a budget period close check job.
//
// The worker:
//  1. Loads the budget and its categories.
//  2. Emits a budget_period_end event with per-category metrics (percent_used, remaining, total_spent).
//
// If no event processor is configured, the job is a no-op (succeeds without firing).
func (w *BudgetPeriodCloseCheckWorker) Work(ctx context.Context, job *river.Job[BudgetPeriodCloseCheckArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting budget period close check",
		"job_id", job.ID,
		"budget_id", args.BudgetID,
		"user_id", args.UserID,
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

	budget, err := w.budgetRepo.GetBudgetWithCategoriesByID(ctx, args.BudgetID, args.UserID)
	if err != nil {
		return fmt.Errorf("failed to load budget %s: %w", args.BudgetID, err)
	}

	if w.eventProcessor == nil {
		w.logger.Debug("no event processor configured; skipping budget_period_end event",
			"budget_id", args.BudgetID,
		)
		return nil
	}

	totalSpent := budget.TotalSpent()
	remaining := budget.Amount - totalSpent
	var percentUsed float64
	if budget.Amount > 0 {
		percentUsed = (totalSpent / budget.Amount) * 100
	}

	eventData := map[string]interface{}{
		domain.EventDataKeyBudgetID:    budget.ID,
		domain.EventDataKeyBudgetName:  budget.Name,
		domain.EventDataKeyTotalSpent:  totalSpent,
		domain.EventDataKeyPercentUsed: percentUsed,
		domain.EventDataKeyAmount:      budget.Amount,
		domain.EventDataKeyRemaining:   remaining,
		domain.EventDataKeyCategories:  domain.BuildCategoryEventData(budget.Categories),
	}

	if err := w.eventProcessor.ProcessEventTrigger(ctx, args.UserID, domain.EventBudgetPeriodEnd, eventData); err != nil {
		w.logger.Warn("failed to process budget_period_end event",
			"budget_id", args.BudgetID,
			"user_id", args.UserID,
			"error", err,
		)
		// Fire and forget — event failures should not fail the job itself.
	}

	w.logger.Info("budget period close check completed",
		"job_id", job.ID,
		"budget_id", args.BudgetID,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return nil
}
