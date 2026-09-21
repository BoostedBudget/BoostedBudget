package jobs

// Issue #1685 todo 13 — the resync worker's incomplete-actuals semantics, defined for BOTH the
// atomic branch (the production path: the budget repo implements
// repository.BudgetActualsRepository) and the legacy branch (the calculator path, which cannot
// distinguish a FATAL from a degraded run).
//
// The decided semantics:
//   - A FATAL sentinel FAILS the job. River retries any returned error, so failing = returning the
//     error with the sentinel and its reasons intact; the reason is recorded on the job (its error)
//     and in a structured error log. This holds in BOTH branches.
//   - A degraded run COMPLETES. The atomic branch re-reads the persisted
//     budgets.actuals_incomplete_reasons (carried on the returned *domain.Budget by
//     RecalculateBudgetActuals, todo 6) and includes them in the completion log.
//   - The legacy branch cannot tell FATAL from degraded, so it is specified to fail retryably on
//     ANY sentinel rather than guess.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ─── stubs ──────────────────────────────────────────────────────────────────────────────────────

type resyncStubCategoryRepo struct {
	category *domain.CustomCategory
	children []*domain.CustomCategory
}

func (s *resyncStubCategoryRepo) GetByID(_ context.Context, _ string) (*domain.CustomCategory, error) {
	return s.category, nil
}

func (s *resyncStubCategoryRepo) GetChildren(_ context.Context, _ string) ([]*domain.CustomCategory, error) {
	return s.children, nil
}

// resyncStubBudgetRepo implements services.BudgetSyncRepository ONLY — deliberately NOT
// repository.BudgetActualsRepository, so the worker takes the LEGACY branch.
type resyncStubBudgetRepo struct {
	budgets    []*domain.Budget
	categories []domain.BudgetCategory
}

func (s *resyncStubBudgetRepo) GetActiveByUserID(_ context.Context, _ string) ([]*domain.Budget, error) {
	return s.budgets, nil
}

func (s *resyncStubBudgetRepo) GetCategoriesByBudgetID(_ context.Context, _ string) ([]domain.BudgetCategory, error) {
	return s.categories, nil
}

func (s *resyncStubBudgetRepo) UpdateBudgetIncome(_ context.Context, _ string, _ float64) error {
	return nil
}

func (s *resyncStubBudgetRepo) UpdateCategorySpentAmount(_ context.Context, _ string, _ float64) error {
	return nil
}

// resyncStubAtomicBudgetRepo embeds the legacy stub and adds RecalculateBudgetActuals, so the
// worker takes the ATOMIC branch.
type resyncStubAtomicBudgetRepo struct {
	resyncStubBudgetRepo
	recalcBudget *domain.Budget
	recalcErr    error
}

func (s *resyncStubAtomicBudgetRepo) RecalculateBudgetActuals(_ context.Context, _, _ string, _ bool) (*domain.Budget, error) {
	return s.recalcBudget, s.recalcErr
}

type resyncStubSpentCalc struct {
	spent float64
	err   error
}

func (s *resyncStubSpentCalc) CalculateCategorySpent(_ context.Context, _ string, _ *domain.BudgetCategory, _, _ time.Time) (float64, error) {
	return s.spent, s.err
}

// ─── helpers ────────────────────────────────────────────────────────────────────────────────────

func resyncStringPtr(s string) *string { return &s }

// resyncJob builds a minimal River job for the worker.
func resyncJob(userID, customCategoryID string) *river.Job[BudgetHierarchyResyncArgs] {
	return &river.Job[BudgetHierarchyResyncArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Kind: "budget_hierarchy_resync", Queue: "default", Attempt: 1},
		Args: BudgetHierarchyResyncArgs{
			UserID:           userID,
			CustomCategoryID: customCategoryID,
		},
	}
}

// resyncLogRecord returns the decoded JSON log record whose "msg" equals want, or nil.
func resyncLogRecord(t *testing.T, buf *bytes.Buffer, want string) map[string]any {
	t.Helper()
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec["msg"] == want {
			return rec
		}
	}
	return nil
}

// resyncEligibleBudget is an active monthly budget whose stored period covers today.
func resyncEligibleBudget(id string) *domain.Budget {
	return &domain.Budget{
		ID:        id,
		IsActive:  true,
		Period:    domain.BudgetPeriodMonthly,
		StartDate: time.Now().UTC().AddDate(0, 0, -1),
	}
}

// resyncAffectedCategory is a budgeted custom row for the reparented category, which is what makes
// budgetHierarchyNeedsResync true.
func resyncAffectedCategory(customCategoryID string) domain.BudgetCategory {
	return domain.BudgetCategory{
		ID:               "bc-1",
		CategoryKind:     "custom",
		CustomCategoryID: resyncStringPtr(customCategoryID),
	}
}

// ─── happy: a degraded run completes and records the persisted reasons ───────────────────────────

// TestHierarchyResyncCompletesWithDegradedActuals pins the DECIDED semantics for a degraded run in
// the atomic branch: the job must NOT fail, and the reasons persisted on the budget row must reach
// the completion log.
func TestHierarchyResyncCompletesWithDegradedActuals(t *testing.T) {
	const (
		userID   = "user-degraded"
		ccID     = "cc-degraded"
		budgetID = "budget-degraded"
	)

	var logBuf bytes.Buffer
	budgetRepo := &resyncStubAtomicBudgetRepo{
		resyncStubBudgetRepo: resyncStubBudgetRepo{
			budgets:    []*domain.Budget{resyncEligibleBudget(budgetID)},
			categories: []domain.BudgetCategory{resyncAffectedCategory(ccID)},
		},
		// RecalculateBudgetActuals returns the budget carrying the reasons it just persisted
		// (todo 6); the worker must surface them, not swallow them.
		recalcBudget: &domain.Budget{
			ID:                       budgetID,
			ActualsIncompleteReasons: []string{"overlapping_individual_lines"},
		},
	}

	worker := NewBudgetHierarchyResyncWorker(BudgetHierarchyResyncWorkerDeps{
		CustomCategoryRepo: &resyncStubCategoryRepo{category: &domain.CustomCategory{ID: ccID}},
		BudgetRepo:         budgetRepo,
		SpentCalc:          &resyncStubSpentCalc{},
		Logger:             slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})

	require.NoError(t, worker.Work(context.Background(), resyncJob(userID, ccID)),
		"a degraded run completes; only a FATAL sentinel may fail the job")

	completion := resyncLogRecord(t, &logBuf, "budget hierarchy resync completed")
	require.NotNil(t, completion, "the worker must emit its completion log")

	reasons, ok := completion["actuals_incomplete_reasons"].([]any)
	require.True(t, ok, "the completion log must carry actuals_incomplete_reasons, got %v", completion["actuals_incomplete_reasons"])
	require.Len(t, reasons, 1)
	assert.Equal(t, "overlapping_individual_lines", reasons[0],
		"the persisted reasons must be re-read from the budget row and recorded on completion")
}

// ─── failure: a FATAL sentinel fails retryably, in BOTH branches ─────────────────────────────────

// TestHierarchyResyncFailsRetryablyOnFatalSentinel pins the DECIDED semantics for a FATAL sentinel:
// the job fails (River retries any returned error) and the sentinel + its reason survive on the
// job's error. The atomic branch can distinguish FATAL from degraded; the legacy branch cannot, so
// it fails retryably on any sentinel.
func TestHierarchyResyncFailsRetryablyOnFatalSentinel(t *testing.T) {
	const (
		userID = "user-fatal"
		ccID   = "cc-fatal"
	)

	t.Run("atomic branch", func(t *testing.T) {
		const budgetID = "budget-fatal-atomic"
		var logBuf bytes.Buffer
		budgetRepo := &resyncStubAtomicBudgetRepo{
			resyncStubBudgetRepo: resyncStubBudgetRepo{
				budgets:    []*domain.Budget{resyncEligibleBudget(budgetID)},
				categories: []domain.BudgetCategory{resyncAffectedCategory(ccID)},
			},
			recalcErr: domain.NewBudgetActualsIncompleteError("currency_mismatch"),
		}

		worker := NewBudgetHierarchyResyncWorker(BudgetHierarchyResyncWorkerDeps{
			CustomCategoryRepo: &resyncStubCategoryRepo{category: &domain.CustomCategory{ID: ccID}},
			BudgetRepo:         budgetRepo,
			SpentCalc:          &resyncStubSpentCalc{},
			Logger:             slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})),
		})

		err := worker.Work(context.Background(), resyncJob(userID, ccID))
		require.Error(t, err, "a FATAL sentinel must fail the job so River retries it")
		assert.ErrorIs(t, err, domain.ErrBudgetActualsIncomplete,
			"the returned error must keep the sentinel so the failure is classifiable")
		assert.Contains(t, err.Error(), "currency_mismatch", "the reason must be recorded on the job error")

		abort := resyncLogRecord(t, &logBuf, "budget hierarchy resync aborted on a fatal actuals sentinel")
		require.NotNil(t, abort, "the FATAL reason must be recorded in a structured error log")
		reasons, _ := abort["reasons"].([]any)
		require.Len(t, reasons, 1)
		assert.Equal(t, "currency_mismatch", reasons[0])
	})

	t.Run("legacy branch", func(t *testing.T) {
		const budgetID = "budget-fatal-legacy"
		var logBuf bytes.Buffer
		// Deliberately only services.BudgetSyncRepository: no RecalculateBudgetActuals, so the
		// worker takes the legacy calculator path.
		budgetRepo := &resyncStubBudgetRepo{
			budgets:    []*domain.Budget{resyncEligibleBudget(budgetID)},
			categories: []domain.BudgetCategory{resyncAffectedCategory(ccID)},
		}

		worker := NewBudgetHierarchyResyncWorker(BudgetHierarchyResyncWorkerDeps{
			CustomCategoryRepo: &resyncStubCategoryRepo{category: &domain.CustomCategory{ID: ccID}},
			BudgetRepo:         budgetRepo,
			SpentCalc:          &resyncStubSpentCalc{err: domain.NewBudgetActualsIncompleteError("legacy_savings_origin")},
			Logger:             slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})),
		})

		err := worker.Work(context.Background(), resyncJob(userID, ccID))
		require.Error(t, err, "the legacy branch cannot distinguish FATAL from degraded, so any sentinel fails retryably")
		assert.True(t, errors.Is(err, domain.ErrBudgetActualsIncomplete),
			"the legacy branch must not swallow the sentinel")
		assert.Contains(t, err.Error(), "legacy_savings_origin", "the reason must be recorded on the job error")

		abort := resyncLogRecord(t, &logBuf, "budget hierarchy resync aborted on a fatal actuals sentinel")
		require.NotNil(t, abort, "the FATAL reason must be recorded in a structured error log")
		reasons, _ := abort["reasons"].([]any)
		require.Len(t, reasons, 1)
		assert.Equal(t, "legacy_savings_origin", reasons[0])
	})
}
