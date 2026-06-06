// Package jobs contains River job definitions and workers for async processing.
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

// BudgetPeriodCloseCheckArgs contains the arguments for the budget period close check job.
// This job runs daily, detects budget periods that just closed, and fires the
// budget_period_end event exactly once per period per budget.
//
// Issue #972: Savings Transfer Automation — period-close scanner.
type BudgetPeriodCloseCheckArgs struct {
	// TriggerSource indicates what triggered this check (e.g., "periodic", "manual")
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (BudgetPeriodCloseCheckArgs) Kind() string { return "budget_period_close_check" }

// BudgetPeriodCheckRepository defines the budget access methods needed by the worker.
type BudgetPeriodCheckRepository interface {
	// GetAllActivePaginated returns all active budgets ordered by id ASC, paginated.
	GetAllActivePaginated(ctx context.Context, limit int, offset int) ([]*domain.Budget, error)
}

// BudgetPeriodEventsRepo defines the marker methods needed by the worker.
type BudgetPeriodEventsRepo interface {
	HasMarker(ctx context.Context, userID string, budgetID string, periodStart time.Time, periodEnd time.Time, eventType string) (bool, error)
	// InsertMarker inserts the marker and returns true if the row was newly
	// inserted, false if it already existed (ON CONFLICT). Callers should only
	// proceed with enqueueing when the return value is true.
	InsertMarker(ctx context.Context, userID string, budgetID string, periodStart time.Time, periodEnd time.Time, eventType string) (bool, error)
}

// BudgetPeriodCloseCheckWorker processes the daily period-close scan.
// For each active budget it computes whether a period boundary was crossed between
// yesterday and today. If so, and no marker exists yet, it inserts the marker and
// enqueues a RuleEvaluationArgs job for that user/budget.
type BudgetPeriodCloseCheckWorker struct {
	river.WorkerDefaults[BudgetPeriodCloseCheckArgs]

	budgetRepo      BudgetPeriodCheckRepository
	periodEventsRepo BudgetPeriodEventsRepo
	jobQueue        queue.JobQueue
	logger          *slog.Logger
}

// BudgetPeriodCloseCheckWorkerDeps contains the dependencies for the worker.
type BudgetPeriodCloseCheckWorkerDeps struct {
	BudgetRepo       BudgetPeriodCheckRepository
	PeriodEventsRepo BudgetPeriodEventsRepo
	JobQueue         queue.JobQueue
	Logger           *slog.Logger
}

// NewBudgetPeriodCloseCheckWorker creates a new BudgetPeriodCloseCheckWorker.
// Note: JobQueue can be nil initially and set later via SetJobQueue() to handle
// the circular dependency when configuring periodic jobs.
func NewBudgetPeriodCloseCheckWorker(deps BudgetPeriodCloseCheckWorkerDeps) *BudgetPeriodCloseCheckWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &BudgetPeriodCloseCheckWorker{
		budgetRepo:        deps.BudgetRepo,
		periodEventsRepo:  deps.PeriodEventsRepo,
		jobQueue:          deps.JobQueue,
		logger:            logger,
	}
}

// SetJobQueue sets the job queue after worker creation.
func (w *BudgetPeriodCloseCheckWorker) SetJobQueue(jq queue.JobQueue) {
	w.jobQueue = jq
}

const budgetPeriodCheckPageSize = 100

// Work processes a budget period close check job.
//
// Algorithm:
//  1. Pages through ALL active budgets via GetAllActivePaginated.
//  2. For each budget, calls computeJustClosedPeriod(budget, now) to determine whether
//     a period boundary was crossed between yesterday and today.
//  3. If a period just closed: checks HasMarker; if no marker, inserts one and
//     enqueues a RuleEvaluationArgs job for the user with event data.
func (w *BudgetPeriodCloseCheckWorker) Work(ctx context.Context, job *river.Job[BudgetPeriodCloseCheckArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting budget period close check",
		"job_id", job.ID,
		"trigger_source", args.TriggerSource,
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

	now := time.Now()

	var offset int
	var totalProcessed, totalFired, totalSkipped, totalErrors int

	for {
		budgets, fetchErr := w.budgetRepo.GetAllActivePaginated(ctx, budgetPeriodCheckPageSize, offset)
		if fetchErr != nil {
			w.logger.Error("failed to fetch active budgets",
				"job_id", job.ID,
				"offset", offset,
				"error", fetchErr,
			)
			return fmt.Errorf("failed to fetch active budgets: %w", fetchErr)
		}

		if len(budgets) == 0 {
			break
		}

		for _, budget := range budgets {
			totalProcessed++

			periodStart, periodEnd, closed := computeJustClosedPeriod(budget, now)
			if !closed {
				totalSkipped++
				continue
			}

			hasMarker, markerErr := w.periodEventsRepo.HasMarker(ctx,
				budget.UserID, budget.ID,
				periodStart, periodEnd,
				domain.EventBudgetPeriodEnd,
			)
			if markerErr != nil {
				w.logger.Error("failed to check period event marker",
					"budget_id", budget.ID,
					"user_id", budget.UserID,
					"error", markerErr,
				)
				totalErrors++
				continue
			}
			if hasMarker {
				w.logger.Debug("period event marker already exists, skipping",
					"budget_id", budget.ID,
					"period_start", periodStart,
					"period_end", periodEnd,
				)
				totalSkipped++
				continue
			}

			evalArgs := RuleEvaluationArgs{
				UserID:    budget.UserID,
				EventType: domain.EventBudgetPeriodEnd,
				EventData: map[string]interface{}{
					"budget_id":      budget.ID,
					"remaining":      budget.RemainingAmount(),
					"income_amount":  budget.IncomeAmount,
					"amount":         budget.Amount,
					"total_spent":    budget.TotalSpent(),
					"period_start":   periodStart.Format("2006-01-02"),
					"period_end":     periodEnd.Format("2006-01-02"),
				},
			}

			result, enqueueErr := w.jobQueue.Enqueue(ctx, evalArgs,
				queue.WithQueue(queue.QueueDefault),
			)
			if enqueueErr != nil {
				w.logger.Error("failed to enqueue rule evaluation for period end",
					"budget_id", budget.ID,
					"user_id", budget.UserID,
					"error", enqueueErr,
				)
				totalErrors++
				continue
			}

			// Insert marker AFTER successful enqueue. If marker insert fails (e.g.
			// concurrent worker won the race), log a warning but do not treat as
			// error — the job is already enqueued and will run. The next daily run
			// will see the marker and skip. If InsertMarker returns false (conflict),
			// a concurrent worker already claimed this period — skip silently.
			inserted, insertErr := w.periodEventsRepo.InsertMarker(ctx,
				budget.UserID, budget.ID,
				periodStart, periodEnd,
				domain.EventBudgetPeriodEnd,
			)
			if insertErr != nil {
				w.logger.Warn("failed to insert period event marker (job already enqueued)",
					"budget_id", budget.ID,
					"user_id", budget.UserID,
					"error", insertErr,
				)
			} else if !inserted {
				w.logger.Debug("concurrent worker already claimed this period, job still enqueued",
					"budget_id", budget.ID,
					"period_start", periodStart,
					"period_end", periodEnd,
				)
			}

			w.logger.Info("fired budget_period_end event",
				"budget_id", budget.ID,
				"user_id", budget.UserID,
				"period_start", periodStart,
				"period_end", periodEnd,
				"rule_eval_job_id", result.ID,
			)
			totalFired++
		}

		if len(budgets) < budgetPeriodCheckPageSize {
			break
		}
		offset += budgetPeriodCheckPageSize
	}

	w.logger.Info("budget period close check completed",
		"job_id", job.ID,
		"total_processed", totalProcessed,
		"fired", totalFired,
		"skipped", totalSkipped,
		"errors", totalErrors,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return nil
}

// computeJustClosedPeriod determines whether a budget period boundary was crossed
// between yesterday and today. Returns the just-closed period's (start, end, true)
// if a boundary was crossed, otherwise returns zero values and false.
//
// For custom (fixed) periods: the period is closed on the first day after EndDate.
// The unique marker prevents re-firing if the worker runs late (tDate > EndDate+1).
//
// For recurring periods: computes the period containing yesterday and today; if they
// differ a boundary was crossed and yesterday's period just closed.
func computeJustClosedPeriod(budget *domain.Budget, now time.Time) (start, end time.Time, closed bool) {
	tDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	if budget.Period == domain.BudgetPeriodCustom {
		if budget.EndDate == nil {
			return time.Time{}, time.Time{}, false
		}
		endDate := time.Date(budget.EndDate.Year(), budget.EndDate.Month(), budget.EndDate.Day(), 0, 0, 0, 0, time.UTC)
		// Closed on any day after EndDate (unique marker prevents duplicate firings).
		if !tDate.After(endDate) {
			return time.Time{}, time.Time{}, false
		}
		startDate := time.Date(budget.StartDate.Year(), budget.StartDate.Month(), budget.StartDate.Day(), 0, 0, 0, 0, time.UTC)
		return startDate, endDate, true
	}

	// Recurring periods: detect boundary crossing between yesterday and today.
	yesterday := now.AddDate(0, 0, -1)
	todayStart, todayEnd := computeCurrentPeriod(budget, now)
	yesterdayStart, yesterdayEnd := computeCurrentPeriod(budget, yesterday)

	// A boundary was crossed when yesterday and today fall in different periods.
	if yesterdayStart.Equal(todayStart) && yesterdayEnd.Equal(todayEnd) {
		return time.Time{}, time.Time{}, false
	}

	// The just-closed period is the one containing yesterday.
	return yesterdayStart, yesterdayEnd, true
}

// computeCurrentPeriod computes the period boundaries (start inclusive, end inclusive)
// that contain the given instant t for the specified budget.
//
// For custom budgets the single explicit period [StartDate, EndDate] is returned.
// For recurring periods (weekly/monthly/quarterly/yearly) the period that contains t
// is computed by advancing from the budget's StartDate.
func computeCurrentPeriod(budget *domain.Budget, t time.Time) (start, end time.Time) {
	// Normalize to date-only in UTC to avoid timezone edge effects.
	tDate := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)

	switch budget.Period {
	case domain.BudgetPeriodCustom:
		// Fixed single period.
		s := time.Date(budget.StartDate.Year(), budget.StartDate.Month(), budget.StartDate.Day(), 0, 0, 0, 0, time.UTC)
		e := s
		if budget.EndDate != nil {
			e = time.Date(budget.EndDate.Year(), budget.EndDate.Month(), budget.EndDate.Day(), 0, 0, 0, 0, time.UTC)
		}
		return s, e

	case domain.BudgetPeriodWeekly:
		// Budget rolls over every 7 days starting from StartDate.
		baseDate := time.Date(budget.StartDate.Year(), budget.StartDate.Month(), budget.StartDate.Day(), 0, 0, 0, 0, time.UTC)
		if tDate.Before(baseDate) {
			// Before the budget started — return the first period.
			return baseDate, baseDate.AddDate(0, 0, 6)
		}
		daysSinceStart := int(tDate.Sub(baseDate).Hours() / 24)
		weekNum := daysSinceStart / 7
		periodStart := baseDate.AddDate(0, 0, weekNum*7)
		periodEnd := periodStart.AddDate(0, 0, 6)
		return periodStart, periodEnd

	case domain.BudgetPeriodMonthly:
		// Calendar month containing t.
		monthStart := time.Date(tDate.Year(), tDate.Month(), 1, 0, 0, 0, 0, time.UTC)
		// Last day of that calendar month.
		monthEnd := monthStart.AddDate(0, 1, -1)
		return monthStart, monthEnd

	case domain.BudgetPeriodQuarterly:
		// Calendar quarter containing t.
		quarter := (int(tDate.Month()) - 1) / 3 // 0-indexed quarter
		quarterStartMonth := time.Month(quarter*3 + 1)
		quarterStart := time.Date(tDate.Year(), quarterStartMonth, 1, 0, 0, 0, 0, time.UTC)
		quarterEnd := quarterStart.AddDate(0, 3, -1)
		return quarterStart, quarterEnd

	case domain.BudgetPeriodYearly:
		// Calendar year containing t.
		yearStart := time.Date(tDate.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
		yearEnd := time.Date(tDate.Year(), time.December, 31, 0, 0, 0, 0, time.UTC)
		return yearStart, yearEnd

	default:
		// Unknown period type — fall back to current calendar month.
		monthStart := time.Date(tDate.Year(), tDate.Month(), 1, 0, 0, 0, 0, time.UTC)
		monthEnd := monthStart.AddDate(0, 1, -1)
		return monthStart, monthEnd
	}
}

// BudgetPeriodCloseCheckUniqueOpts returns the recommended uniqueness options.
// Only one check job should run at a time to prevent duplicate processing.
func BudgetPeriodCloseCheckUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs:   false,
		ByPeriod: 24 * time.Hour,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}
