package services

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Issue #1685 todo 9: the sync service surfaces degraded actuals reasons and
// emits the PostHog contract sourced from the returned *domain.Budget.
// =============================================================================

// degradedActualsSyncFixture is a BudgetSyncRepository that ALSO implements
// repository.BudgetActualsRepository, so RecalculateBudget takes the atomic
// branch and reads the reasons off the returned Budget.
type degradedActualsSyncFixture struct {
	MockBudgetSyncRepository
	fresh *domain.Budget
	err   error
}

func (f *degradedActualsSyncFixture) RecalculateBudgetActuals(context.Context, string, string, bool) (*domain.Budget, error) {
	return f.fresh, f.err
}

// capturingAnalytics records every CaptureEvent call so a test can assert the
// event name, subject and properties without a real PostHog client.
type capturingAnalytics struct {
	events []capturedAnalyticsEvent
}

type capturedAnalyticsEvent struct {
	distinctID string
	event      string
	properties map[string]interface{}
}

func (c *capturingAnalytics) CaptureEvent(_ context.Context, internalUserID string, event phclient.TypedEvent) {
	c.events = append(c.events, capturedAnalyticsEvent{
		distinctID: internalUserID,
		event:      string(event.EventName()),
		properties: event.Properties(),
	})
}

func newDegradedSyncBudget() *domain.Budget {
	return &domain.Budget{
		ID:        "budget",
		UserID:    "user",
		Period:    domain.BudgetPeriodMonthly,
		StartDate: time.Now().UTC().AddDate(0, 0, -1),
	}
}

// TestBudgetSyncSurfacesDegradedReasons is the todo 9 happy path: a publish that
// returns reasons must emit exactly one budget_actuals_incomplete event, sourced
// from the returned Budget, carrying the reason count.
func TestBudgetSyncSurfacesDegradedReasons(t *testing.T) {
	reasons := []string{"currency_mismatch", "overlapping_individual_lines"}
	repo := &degradedActualsSyncFixture{fresh: &domain.Budget{
		ID:                       "budget",
		UserID:                   "user",
		ActualsIncompleteReasons: reasons,
	}}
	analytics := &capturingAnalytics{}
	svc := NewBudgetSyncService(repo, new(MockBudgetIncomeCalculator), new(MockBudgetSpentCalculator), testSyncServiceLogger())
	svc.SetAnalytics(analytics)

	require.NoError(t, svc.RecalculateBudget(context.Background(), newDegradedSyncBudget()))

	require.Len(t, analytics.events, 1, "a degraded publish must emit exactly one analytics event")
	got := analytics.events[0]
	require.Equal(t, "user", got.distinctID, "the event subject is the returned Budget's owner")
	require.Equal(t, string(phclient.EventBudgetActualsIncomplete), got.event)
	require.Equal(t, int64(len(reasons)), got.properties["reason_count"])
}

// TestBudgetSyncNoEventWhenReasonsEmpty is the negative control: a clean publish
// must not emit the degraded event.
func TestBudgetSyncNoEventWhenReasonsEmpty(t *testing.T) {
	repo := &degradedActualsSyncFixture{fresh: &domain.Budget{ID: "budget", UserID: "user"}}
	analytics := &capturingAnalytics{}
	svc := NewBudgetSyncService(repo, new(MockBudgetIncomeCalculator), new(MockBudgetSpentCalculator), testSyncServiceLogger())
	svc.SetAnalytics(analytics)

	require.NoError(t, svc.RecalculateBudget(context.Background(), newDegradedSyncBudget()))
	require.Empty(t, analytics.events, "a clean publish must not emit the degraded event")
}

// TestBudgetSyncFatalBarrierStillErrors pins that a FATAL barrier (the sentinel)
// still propagates as an error through the atomic branch, with %w preserving the
// sentinel so the Huma mapper can turn it into a 422.
func TestBudgetSyncFatalBarrierStillErrors(t *testing.T) {
	repo := &degradedActualsSyncFixture{err: domain.NewBudgetActualsIncompleteError("mixed_currency")}
	svc := NewBudgetSyncService(repo, new(MockBudgetIncomeCalculator), new(MockBudgetSpentCalculator), testSyncServiceLogger())

	err := svc.RecalculateBudget(context.Background(), newDegradedSyncBudget())
	require.Error(t, err)
	require.ErrorIs(t, err, domain.ErrBudgetActualsIncomplete, "%w must preserve the sentinel")
	require.ErrorContains(t, err, "mixed_currency")
}

// TestBudgetSyncLegacyBranchPropagatesSentinel pins the legacy (non-atomic) branch:
// a per-category sentinel must be propagated rather than logged and swallowed, or
// the caller sees a silent degrade with no reasons and no 422.
func TestBudgetSyncLegacyBranchPropagatesSentinel(t *testing.T) {
	repo := new(MockBudgetSyncRepository)
	repo.On("UpdateBudgetIncome", mock.Anything, "budget", mock.Anything).Return(nil)
	repo.On("GetCategoriesByBudgetID", mock.Anything, "budget").Return([]domain.BudgetCategory{{ID: "cat"}}, nil)

	income := new(MockBudgetIncomeCalculator)
	income.On("CalculateBudgetIncome", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(100.0, nil)

	spent := new(MockBudgetSpentCalculator)
	spent.On("CalculateCategorySpent", mock.Anything, "user", mock.Anything, mock.Anything, mock.Anything).
		Return(0.0, domain.NewBudgetActualsIncompleteError("legacy_savings_origin"))

	svc := NewBudgetSyncService(repo, income, spent, testSyncServiceLogger())

	err := svc.RecalculateBudget(context.Background(), newDegradedSyncBudget())
	require.Error(t, err, "the legacy branch must not swallow the sentinel")
	require.ErrorIs(t, err, domain.ErrBudgetActualsIncomplete)
	require.ErrorContains(t, err, "legacy_savings_origin")
}