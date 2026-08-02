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
	// GetCategoriesByBudgetIDs batch-loads categories for a page of budget IDs.
	// Used to populate per-category EventData for budget_period_end rule evaluation.
	// Issue #1271 Item 6: replaces per-budget GetCategoriesByBudgetID for efficiency.
	GetCategoriesByBudgetIDs(ctx context.Context, budgetIDs []string) (map[string][]domain.BudgetCategory, error)
}

// UserTimezoneLookup provides batch timezone lookup for the period-close scanner.
// The scanner uses it to resolve each budget owner's timezone so period boundaries
// are detected in the user's local time rather than server UTC.
type UserTimezoneLookup interface {
	GetTimezonesByUserIDs(ctx context.Context, userIDs []string) (map[string]string, error)
}

// TransferSuggestionExpirer defines the expiry methods needed by the daily worker.
// Only pending suggestions whose expires_at is in the past are affected; accepted
// suggestions are never auto-expired because the user has explicitly acted on them.
type TransferSuggestionExpirer interface {
	// ExpirePending marks stale pending suggestions (expires_at < now) as expired.
	// Returns the count of rows updated.
	ExpirePending(ctx context.Context) (int64, error)
	// ClearStaleReservations releases pending/accepted suggestions whose expires_at has
	// passed by setting matched_transaction_id = NULL. Suggestion status is unchanged
	// (accepted stays accepted). Returns the number of reservations cleared.
	// GAP AH HIGH#1: prevents a never-confirmed reservation from permanently pinning
	// an inflow and blocking it from being matched by a future suggestion.
	ClearStaleReservations(ctx context.Context) (int64, error)
}

// BudgetPeriodEventsRepo defines the marker methods needed by the worker.
type BudgetPeriodEventsRepo interface {
	HasMarker(ctx context.Context, userID string, budgetID string, periodStart time.Time, periodEnd time.Time, eventType string) (bool, error)
	// InsertMarker inserts the marker and returns true if the row was newly
	// inserted, false if it already existed (ON CONFLICT). Callers should only
	// proceed with enqueueing when the return value is true.
	InsertMarker(ctx context.Context, userID string, budgetID string, periodStart time.Time, periodEnd time.Time, eventType string) (bool, error)
	// DeleteMarker removes a previously-inserted marker so the next daily run can
	// retry. Used to clean up an orphaned marker when Enqueue fails.
	DeleteMarker(ctx context.Context, userID string, budgetID string, periodStart time.Time, periodEnd time.Time, eventType string) error
}

// BudgetPeriodCloseCheckWorker processes the daily period-close scan.
// For each active budget it computes whether a period boundary was crossed between
// yesterday and today. If so, and no marker exists yet, it inserts the marker (the
// exactly-once guard) and — only when the insert succeeds — enqueues a
// RuleEvaluationArgs job for that user/budget. Inserting before enqueueing prevents
// duplicate jobs when two workers both see HasMarker=false and race to process.
// At the end of every run it also calls TransferSuggestionExpirer.ExpirePending to
// transition stale pending suggestions to "expired" status.
type BudgetPeriodCloseCheckWorker struct {
	river.WorkerDefaults[BudgetPeriodCloseCheckArgs]

	budgetRepo                BudgetPeriodCheckRepository
	periodEventsRepo          BudgetPeriodEventsRepo
	jobQueue                  queue.JobQueue
	transferSuggestionExpirer TransferSuggestionExpirer // Issue #972: GAP E
	userTimezones             UserTimezoneLookup        // Issue #972: Task 2e — per-user timezone (nil → UTC)
	logger                    *slog.Logger
}

// BudgetPeriodCloseCheckWorkerDeps contains the dependencies for the worker.
type BudgetPeriodCloseCheckWorkerDeps struct {
	BudgetRepo                BudgetPeriodCheckRepository
	PeriodEventsRepo          BudgetPeriodEventsRepo
	JobQueue                  queue.JobQueue
	TransferSuggestionExpirer TransferSuggestionExpirer // Issue #972: GAP E — expire stale pending suggestions
	UserTimezones             UserTimezoneLookup        // Issue #972: Task 2e — per-user timezone (nil → UTC)
	Logger                    *slog.Logger
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
		budgetRepo:                deps.BudgetRepo,
		periodEventsRepo:          deps.PeriodEventsRepo,
		jobQueue:                  deps.JobQueue,
		transferSuggestionExpirer: deps.TransferSuggestionExpirer,
		userTimezones:             deps.UserTimezones,
		logger:                    logger,
	}
}

// SetJobQueue sets the job queue after worker creation.
func (w *BudgetPeriodCloseCheckWorker) SetJobQueue(jq queue.JobQueue) {
	w.jobQueue = jq
}

const budgetPeriodCheckPageSize = 100

// Work processes a budget period close check job.
// It delegates to workAt with the current time to allow clock injection in tests.
func (w *BudgetPeriodCloseCheckWorker) Work(ctx context.Context, job *river.Job[BudgetPeriodCloseCheckArgs]) (err error) {
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

	return w.workAt(ctx, job, time.Now())
}

// workAt performs period-close scanning with an injectable clock for testing.
// Pages through active budgets, detects period boundaries in each user's local timezone,
// and enqueues RuleEvaluationArgs when a period just closed. Uses InsertMarker as the
// exactly-once guard: only the worker that wins the DB race proceeds to enqueue.
// Marker boundaries are UTC-anchored for stability across timezone changes.
func (w *BudgetPeriodCloseCheckWorker) workAt(ctx context.Context, job *river.Job[BudgetPeriodCloseCheckArgs], now time.Time) error {
	args := job.Args

	w.logger.Info("starting budget period close check",
		"job_id", job.ID,
		"trigger_source", args.TriggerSource,
		"attempt", job.Attempt,
	)

	wallStart := time.Now() // wall-clock for duration logging

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

		// Batch-resolve timezones for this page of budgets (nil → UTC fallback).
		// Deduplicate user IDs: a single user may own multiple budgets on this page.
		var tzMap map[string]string
		if w.userTimezones != nil {
			userIDs := deduplicateUserIDs(budgets)
			var tzErr error
			tzMap, tzErr = w.userTimezones.GetTimezonesByUserIDs(ctx, userIDs)
			if tzErr != nil {
				w.logger.Warn("failed to resolve user timezones; falling back to UTC",
					"job_id", job.ID,
					"offset", offset,
					"error", tzErr,
				)
				tzMap = nil
			}
		}

		// First pass: determine which budgets just closed and filter out any that
		// already have a marker (idempotency check). Collect IDs for the marker-free
		// set so we can batch-load their categories before committing any InsertMarker.
		// Issue #1271 Item 6: batch category pre-load prevents per-budget round-trips.
		type closedBudgetEntry struct {
			budget      *domain.Budget
			periodStart time.Time
			periodEnd   time.Time
		}
		var needsEmit []closedBudgetEntry
		var needsEmitIDs []string
		for _, budget := range budgets {
			totalProcessed++
			loc := resolveLocation(budget.UserID, tzMap)
			periodStart, periodEnd, closed := computeJustClosedPeriod(budget, now, loc)
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

			needsEmit = append(needsEmit, closedBudgetEntry{budget, periodStart, periodEnd})
			needsEmitIDs = append(needsEmitIDs, budget.ID)
		}

		if len(needsEmit) == 0 {
			if len(budgets) < budgetPeriodCheckPageSize {
				break
			}
			offset += budgetPeriodCheckPageSize
			continue
		}

		// Batch-load categories for all budgets that need a marker insert.
		// On error, skip the entire set — no InsertMarker for anyone on this page —
		// so no orphaned markers are created that would block next-day retry.
		catsByID, catErr := w.budgetRepo.GetCategoriesByBudgetIDs(ctx, needsEmitIDs)
		if catErr != nil {
			w.logger.Warn("failed to batch-load categories for period-end page; skipping page",
				"job_id", job.ID,
				"offset", offset,
				"budget_count", len(needsEmitIDs),
				"error", catErr,
			)
			totalErrors++
			if len(budgets) < budgetPeriodCheckPageSize {
				break
			}
			offset += budgetPeriodCheckPageSize
			continue
		}

		for _, entry := range needsEmit {
			budget := entry.budget
			periodStart := entry.periodStart
			periodEnd := entry.periodEnd

			// InsertMarker is the exactly-once guard. Inserting BEFORE enqueueing
			// prevents duplicate jobs when two workers both see HasMarker=false and
			// race here: the DB ON CONFLICT ensures only one insert wins (returns
			// true) and only the winner proceeds to enqueue.
			inserted, insertErr := w.periodEventsRepo.InsertMarker(ctx,
				budget.UserID, budget.ID,
				periodStart, periodEnd,
				domain.EventBudgetPeriodEnd,
			)
			if insertErr != nil {
				w.logger.Warn("failed to insert period event marker",
					"budget_id", budget.ID,
					"user_id", budget.UserID,
					"error", insertErr,
				)
				totalErrors++
				continue
			}
			if !inserted {
				// A concurrent worker already inserted the marker; skip enqueueing
				// to guarantee exactly-once delivery.
				w.logger.Debug("concurrent worker already inserted marker; skipping enqueue",
					"budget_id", budget.ID,
					"period_start", periodStart,
					"period_end", periodEnd,
				)
				totalSkipped++
				continue
			}

			// Assign batch-loaded categories for per-category condition evaluation.
			budget.Categories = catsByID[budget.ID]

			evalArgs := BoostEvaluationArgs{
				UserID:    budget.UserID,
				EventType: domain.EventBudgetPeriodEnd,
				// EventData keys must match domain.EventDataKey* constants so that
				// calculateTransferAmount, calculateSavingsGoalAmount, and evaluateCondition
				// can all read them. Always use the domain method (Budget.PercentUsed) rather
				// than re-deriving, so the value is consistent with any other path that reads
				// the same domain method.
				EventData: map[string]interface{}{
					domain.EventDataKeyBudgetID:     budget.ID,
					domain.EventDataKeyRemaining:    budget.RemainingAmount(),
					domain.EventDataKeyIncomeAmount: budget.IncomeAmount,
					domain.EventDataKeyAmount:       budget.Amount,
					domain.EventDataKeyTotalSpent:   budget.TotalSpent(),
					domain.EventDataKeyPercentUsed:  budget.PercentUsed(),
					domain.EventDataKeyPeriodStart:  periodStart.Format("2006-01-02"),
					domain.EventDataKeyPeriodEnd:    periodEnd.Format("2006-01-02"),
					domain.EventDataKeyCategories:   domain.BuildCategoryEventData(budget.Categories),
				},
			}

			result, enqueueErr := w.jobQueue.Enqueue(ctx, evalArgs,
				queue.WithQueue(queue.QueueDefault),
			)
			if enqueueErr != nil {
				// Enqueue failed AFTER a successful InsertMarker. Without cleanup the
				// orphaned marker would permanently suppress budget_period_end for this
				// period. Delete the marker so the next daily run retries.
				// Worst case: the job actually enqueued but returned an error — the
				// retry produces a duplicate rule evaluation for the same period. This
				// is safe: rules check actual budget state and duplicate evaluations are
				// idempotent (same suggestion would be created or deduped).
				if delErr := w.periodEventsRepo.DeleteMarker(ctx,
					budget.UserID, budget.ID, periodStart, periodEnd, domain.EventBudgetPeriodEnd,
				); delErr != nil {
					w.logger.Error("failed to delete marker after enqueue failure",
						"budget_id", budget.ID,
						"user_id", budget.UserID,
						"delete_error", delErr,
					)
				}
				w.logger.Error("failed to enqueue rule evaluation for period end; marker removed for retry",
					"budget_id", budget.ID,
					"user_id", budget.UserID,
					"error", enqueueErr,
				)
				totalErrors++
				continue
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

	// Issue #972 GAP E + GAP AH HIGH#1: expiry + reservation cleanup on every daily run.
	// ORDER MATTERS: ClearStaleReservations MUST run before ExpirePending.
	// ExpirePending transitions pending→expired; ClearStaleReservations only targets
	// pending/accepted. If ExpirePending ran first, newly-expired rows would keep their
	// matched_transaction_id permanently, blocking the inflow from future matching.
	if w.transferSuggestionExpirer != nil {
		// Step 1: Release reservations on pending/accepted suggestions with past expiry,
		// freeing the inflow before the suggestion status changes.
		clearedCount, clearErr := w.transferSuggestionExpirer.ClearStaleReservations(ctx)
		if clearErr != nil {
			w.logger.Warn("failed to clear stale transfer suggestion reservations",
				"job_id", job.ID,
				"error", clearErr,
			)
		} else if clearedCount > 0 {
			w.logger.Info("cleared stale fuzzy-match reservations",
				"job_id", job.ID,
				"cleared_count", clearedCount,
			)
		}

		// Step 2: Expire stale pending suggestions. Accepted suggestions are never
		// auto-expired (user has acted on them). Now that reservations are cleared,
		// any pending rows that transition to expired no longer pin an inflow.
		expiredCount, expireErr := w.transferSuggestionExpirer.ExpirePending(ctx)
		if expireErr != nil {
			w.logger.Warn("failed to expire pending transfer suggestions",
				"job_id", job.ID,
				"error", expireErr,
			)
		} else if expiredCount > 0 {
			w.logger.Info("expired stale pending transfer suggestions",
				"job_id", job.ID,
				"expired_count", expiredCount,
			)
		}
	}

	w.logger.Info("budget period close check completed",
		"job_id", job.ID,
		"total_processed", totalProcessed,
		"fired", totalFired,
		"skipped", totalSkipped,
		"errors", totalErrors,
		"duration_ms", time.Since(wallStart).Milliseconds(),
	)

	return nil
}

// deduplicateUserIDs returns a deduplicated slice of user IDs from budgets.
// A single user may own multiple budgets on a page, so deduplication
// avoids redundant timezone lookups.
func deduplicateUserIDs(budgets []*domain.Budget) []string {
	seenUsers := make(map[string]struct{}, len(budgets))
	userIDs := make([]string, 0, len(budgets))
	for _, b := range budgets {
		if _, dup := seenUsers[b.UserID]; !dup {
			seenUsers[b.UserID] = struct{}{}
			userIDs = append(userIDs, b.UserID)
		}
	}
	return userIDs
}

// resolveLocation looks up the timezone for a user from the pre-fetched tzMap.
// Returns time.UTC when the user has no entry or the IANA name is invalid.
func resolveLocation(userID string, tzMap map[string]string) *time.Location {
	tz := "UTC"
	if tzMap != nil {
		if v, ok := tzMap[userID]; ok && v != "" {
			tz = v
		}
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

// computeJustClosedPeriod determines if a budget period boundary was crossed
// between yesterday and today in the user's local timezone. Returns (start, end, true)
// if a period just closed, otherwise (zero values, false).
//
// For custom periods: closed when current date > EndDate.
// For recurring periods: closed when yesterday and today fall in different periods.
//
// Returned start/end are UTC-anchored for marker stability across timezone changes.
func computeJustClosedPeriod(budget *domain.Budget, now time.Time, loc *time.Location) (start, end time.Time, closed bool) {
	if loc == nil {
		loc = time.UTC
	}

	// Resolve today and yesterday in the user's local timezone, then normalize to
	// UTC midnight for comparison and marker identity.
	nowLocal := now.In(loc)
	todayLocal := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, time.UTC)

	if budget.Period == domain.BudgetPeriodCustom {
		if budget.EndDate == nil {
			return time.Time{}, time.Time{}, false
		}
		endDate := time.Date(budget.EndDate.Year(), budget.EndDate.Month(), budget.EndDate.Day(), 0, 0, 0, 0, time.UTC)
		// Closed on any day after EndDate (unique marker prevents duplicate firings).
		if !todayLocal.After(endDate) {
			return time.Time{}, time.Time{}, false
		}
		startDate := time.Date(budget.StartDate.Year(), budget.StartDate.Month(), budget.StartDate.Day(), 0, 0, 0, 0, time.UTC)
		return startDate, endDate, true
	}

	// Recurring periods: detect boundary crossing between yesterday and today in loc.
	// We fabricate a "yesterday" instant that, when viewed in loc, lands on the
	// previous local day. Subtracting 24h is correct for DST-safe day subtraction
	// because we only care about the local calendar date.
	yesterdayLocal := nowLocal.AddDate(0, 0, -1)
	yesterdayUTC := time.Date(yesterdayLocal.Year(), yesterdayLocal.Month(), yesterdayLocal.Day(), 0, 0, 0, 0, time.UTC)

	// Pass UTC-normalized instants to computeCurrentPeriod so it uses the correct
	// local calendar date for period arithmetic, while keeping period boundaries
	// UTC-anchored for marker stability.
	todayStart, todayEnd := computeCurrentPeriod(budget, todayLocal, time.UTC)
	yesterdayStart, yesterdayEnd := computeCurrentPeriod(budget, yesterdayUTC, time.UTC)

	// A boundary was crossed when yesterday and today fall in different periods.
	if yesterdayStart.Equal(todayStart) && yesterdayEnd.Equal(todayEnd) {
		return time.Time{}, time.Time{}, false
	}

	// The just-closed period is the one containing yesterday.
	return yesterdayStart, yesterdayEnd, true
}

// computeCurrentPeriod returns the period boundaries containing t for the budget,
// mapping t to a calendar date in the given location. loc=UTC for UTC boundaries,
// or a user's location for local boundaries. Always returns UTC midnight timestamps
// for marker stability.
//
// For custom periods: returns [StartDate, EndDate].
// For recurring: computes the period containing t by advancing from StartDate.
func computeCurrentPeriod(budget *domain.Budget, t time.Time, loc *time.Location) (start, end time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	// Normalize t to a calendar date in loc, then represent it as UTC midnight
	// so all downstream arithmetic is timezone-free.
	tInLoc := t.In(loc)
	tDate := time.Date(tInLoc.Year(), tInLoc.Month(), tInLoc.Day(), 0, 0, 0, 0, time.UTC)

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
		// Calendar month containing t (in loc).
		monthStart := time.Date(tDate.Year(), tDate.Month(), 1, 0, 0, 0, 0, time.UTC)
		// Last day of that calendar month.
		monthEnd := monthStart.AddDate(0, 1, -1)
		return monthStart, monthEnd

	case domain.BudgetPeriodQuarterly:
		// Calendar quarter containing t (in loc).
		quarter := (int(tDate.Month()) - 1) / 3 // 0-indexed quarter
		quarterStartMonth := time.Month(quarter*3 + 1)
		quarterStart := time.Date(tDate.Year(), quarterStartMonth, 1, 0, 0, 0, 0, time.UTC)
		quarterEnd := quarterStart.AddDate(0, 3, -1)
		return quarterStart, quarterEnd

	case domain.BudgetPeriodYearly:
		// Calendar year containing t (in loc).
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
